// Command portprobe inspects and optionally stops TCP listeners and their parents.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"controlpanel/shared/portowner"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("portprobe", flag.ContinueOnError)
	flags.SetOutput(out)
	port := flags.Int("port", 8080, "TCP port to inspect")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *port < 1 || *port > 65535 {
		return fmt.Errorf("specify a port between 1 and 65535 using -port")
	}

	fmt.Fprintf(out, "Inspecting TCP port %d (no elevation).\n", *port)
	listeners, err := portowner.FindListeners(*port)
	if err != nil {
		return err
	}
	if len(listeners) == 0 {
		fmt.Fprintln(out, "No listener was found. Discovery may be incomplete; this does not prove the port is free.")
		return nil
	}
	reader := bufio.NewReader(in)
	if probeControlPanel(out, *port) == http.StatusMethodNotAllowed {
		freed, err := offerHTTPStop(reader, out, *port)
		if err != nil {
			return err
		}
		if freed {
			return nil
		}
		if listeners, err = portowner.FindListeners(*port); err != nil {
			return err
		}
		if len(listeners) == 0 {
			fmt.Fprintln(out, "No listener remains detected; restricted processes may not be visible.")
			return nil
		}
	}
	fmt.Fprintln(out, "\nProcess termination. Warning: stopping a parent may affect other processes; a supervisor may restart a listener.")
	seen := make(map[int32]bool)
	var failed bool
	for _, l := range listeners {
		fmt.Fprintf(out, "\nListener: %s PID %d\n", l.Address, l.PID)
		if l.PID <= 0 {
			fmt.Fprintln(out, "Owner unavailable; cannot stop this listener.")
			failed = true
			continue
		}
		if seen[l.PID] {
			fmt.Fprintln(out, "This PID was already handled.")
			continue
		}
		seen[l.PID] = true
		if err := handleListener(reader, out, *port, l.PID); err != nil {
			fmt.Fprintf(out, "Could not complete stop for PID %d: %v\n", l.PID, err)
			failed = true
		}
	}
	remaining, err := portowner.FindListeners(*port)
	if err != nil {
		return fmt.Errorf("could not verify port %d: %w", *port, err)
	}
	if len(remaining) != 0 {
		fmt.Fprintf(out, "\nPort %d still has detected listeners:\n", *port)
		for _, l := range remaining {
			fmt.Fprintf(out, "  %s PID %d\n", l.Address, l.PID)
		}
		return fmt.Errorf("port %d is still occupied", *port)
	}
	if failed {
		return fmt.Errorf("one or more processes could not be inspected or stopped; port ownership may be incomplete")
	}
	fmt.Fprintf(out, "\nNo listeners remain detected on port %d; restricted processes may not be visible.\n", *port)
	return nil
}

func showIdentity(out io.Writer, label string, id portowner.Identity) {
	fmt.Fprintf(out, "%s: PID %d, parent PID %d, started %s\n", label, id.PID, id.Parent,
		time.UnixMilli(id.Created).UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(out, "  name: %s\n  executable: %s\n  command: %s\n  working directory: %s\n  looks like: %s\n",
		id.Name, id.Executable, portowner.TrimCommand(id.Cmdline), id.Cwd, portowner.Classify(id))
}

// probeControlPanel issues GET /controlpanel/stop, which OWLCMS answers with
// 405 without stopping anything. A 405 is a hint, not proof: another server
// can answer 405, and a timeout does not prove OWLCMS is absent. Returns the
// HTTP status, or 0 when there was no answer.
func probeControlPanel(out io.Writer, port int) int {
	url := controlPanelStopURL(port)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(out, "HTTP check: no answer from %s (%v)\n", url, err)
		return 0
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusMethodNotAllowed:
		fmt.Fprintf(out, "HTTP check: %s answered 405 — consistent with OWLCMS 66.2.2+\n", url)
	case http.StatusNotFound:
		fmt.Fprintf(out, "HTTP check: %s answered 404 — not OWLCMS with this endpoint (or older than 66.2.2)\n", url)
	default:
		fmt.Fprintf(out, "HTTP check: %s answered %s — not conclusive\n", url, resp.Status)
	}
	return resp.StatusCode
}

func controlPanelStopURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/controlpanel/stop", port)
}

const tokenEnv = "OWLCMS_CONTROLPANELTOKEN"

// dummyToken is presented when tokenEnv is unset. OWLCMS 66-68 ignore the token; OWLCMS 69+
// accepts the stop only when it was launched with this same token.
const dummyToken = "portprobe-dummy-token"

// offerHTTPStop asks OWLCMS to stop itself. OWLCMS then exits 0 after its
// shutdown hooks, which neither MainWrapper nor the Control Panel restarts.
// Returns true when the port no longer has a detected listener.
func offerHTTPStop(reader *bufio.Reader, out io.Writer, port int) (bool, error) {
	token := strings.TrimSpace(os.Getenv(tokenEnv))
	if token == "" {
		token = dummyToken
		fmt.Fprintf(out, "No token in %s; presenting a dummy token (OWLCMS 66-68 ignore it; 69+ requires its launch token).\n", tokenEnv)
	} else {
		fmt.Fprintf(out, "Token taken from %s (not displayed).\n", tokenEnv)
	}
	ok, err := confirm(reader, out, "Ask OWLCMS to stop itself (POST /controlpanel/stop)?")
	if err != nil || !ok {
		return false, err
	}
	status, err := postStop(port, token)
	switch {
	case err != nil:
		fmt.Fprintf(out, "HTTP stop failed: %v\n", err)
		return false, nil
	case status == http.StatusAccepted:
		fmt.Fprintln(out, "HTTP stop accepted; waiting up to 20s for the port to be released...")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		fmt.Fprintf(out, "HTTP stop refused (%d): OWLCMS requires its launch token. Continuing with process termination.\n", status)
		return false, nil
	default:
		fmt.Fprintf(out, "HTTP stop answered %d; not accepted.\n", status)
		return false, nil
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err := portowner.FindListeners(port)
		if err != nil {
			return false, err
		}
		if len(remaining) == 0 {
			fmt.Fprintf(out, "Port %d released after the HTTP stop; restricted processes may not be visible.\n", port)
			return true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(out, "The port is still in use 20s after the HTTP stop; falling back to process termination.")
	return false, nil
}

func postStop(port int, token string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, controlPanelStopURL(port), nil)
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func confirm(reader *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	for {
		fmt.Fprintf(out, "%s [y/n]: ", prompt)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if errors.Is(err, io.EOF) {
			return false, fmt.Errorf("input ended without a yes/no answer")
		}
		fmt.Fprintln(out, "Enter y or n.")
	}
}

func handleListener(reader *bufio.Reader, out io.Writer, port int, pid int32) error {
	a, err := portowner.Assess(pid)
	if err != nil {
		return err
	}
	target := a.Listener
	showIdentity(out, "Listener", target)
	switch a.ParentKind {
	case portowner.ParentOrphan:
		fmt.Fprintf(out, "Orphan: parent PID %d has exited (or is init); no supervisor will restart this listener.\n", target.Parent)
	case portowner.ParentUnavailable:
		fmt.Fprintf(out, "Parent PID %d unavailable: %v\n", target.Parent, a.ParentErr)
		fmt.Fprintln(out, "  It may be alive but not inspectable; a supervisor may restart this listener.")
	case portowner.ParentProtected:
		showIdentity(out, "Protected parent", *a.Parent)
		fmt.Fprintln(out, "  This parent will not be stopped by the CLI.")
	case portowner.ParentSupervisor:
		showIdentity(out, "Supervisor", *a.Parent)
	case portowner.ParentOther:
		showIdentity(out, "Parent (not a known supervisor)", *a.Parent)
	}

	report := func(line string) { fmt.Fprintln(out, line+"...") }
	var parent *portowner.Identity
	if a.ParentKind == portowner.ParentSupervisor || a.ParentKind == portowner.ParentOther {
		parent = a.Parent
	}
	stopParentNow := func() error {
		if err := portowner.ValidateListener(target, port); err != nil {
			return err
		}
		if err := portowner.Stop(*parent, func() error { return portowner.ValidateParent(target, *parent) }, report); err != nil {
			return fmt.Errorf("parent PID %d: %w", parent.PID, err)
		}
		return nil
	}

	if a.SupervisorFirst {
		fmt.Fprintln(out, "  On Windows, forcing OWLCMS to exit makes this supervisor relaunch it. Stop the supervisor first.")
		stopParent, err := confirm(reader, out, fmt.Sprintf("Stop supervisor PID %d first (recommended)?", parent.PID))
		if err != nil {
			return err
		}
		if stopParent {
			if err := stopParentNow(); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(out, "  The supervisor is still running; it may relaunch OWLCMS after the forced kill.")
		}
	}

	stopListener, err := confirm(reader, out, fmt.Sprintf("Stop listener PID %d?", target.PID))
	if err != nil || !stopListener {
		return err
	}

	if parent != nil && !a.SupervisorFirst {
		stopParent, promptErr := confirm(reader, out, fmt.Sprintf("Stop parent PID %d as well?", parent.PID))
		if promptErr != nil {
			return promptErr
		}
		if stopParent {
			if err := stopParentNow(); err != nil {
				return err
			}
		}
	}

	alive, err := portowner.SameProcess(target)
	if err != nil {
		return err
	}
	if !alive {
		fmt.Fprintf(out, "Listener PID %d has exited.\n", target.PID)
		return nil
	}
	return portowner.Stop(target, func() error { return portowner.ValidateListener(target, port) }, report)
}
