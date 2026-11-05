// Command portprobe inspects and optionally stops TCP listeners and their parents.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	psnet "github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
)

type identity struct {
	pid        int32
	created    int64
	parent     int32
	executable string
	name       string
	cmdline    string
	cwd        string
}

type listener struct {
	pid     int32
	address string
}

// runtimeGOOS is a variable so tests can exercise the Windows branch.
var runtimeGOOS = runtime.GOOS

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
	listeners, err := findListeners(*port)
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
		if listeners, err = findListeners(*port); err != nil {
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
		fmt.Fprintf(out, "\nListener: %s PID %d\n", l.address, l.pid)
		if l.pid <= 0 {
			fmt.Fprintln(out, "Owner unavailable; cannot stop this listener.")
			failed = true
			continue
		}
		if seen[l.pid] {
			fmt.Fprintln(out, "This PID was already handled.")
			continue
		}
		seen[l.pid] = true
		if err := handleListener(reader, out, *port, l.pid); err != nil {
			fmt.Fprintf(out, "Could not complete stop for PID %d: %v\n", l.pid, err)
			failed = true
		}
	}
	remaining, err := findListeners(*port)
	if err != nil {
		return fmt.Errorf("could not verify port %d: %w", *port, err)
	}
	if len(remaining) != 0 {
		fmt.Fprintf(out, "\nPort %d still has detected listeners:\n", *port)
		for _, l := range remaining {
			fmt.Fprintf(out, "  %s PID %d\n", l.address, l.pid)
		}
		return fmt.Errorf("port %d is still occupied", *port)
	}
	if failed {
		return fmt.Errorf("one or more processes could not be inspected or stopped; port ownership may be incomplete")
	}
	fmt.Fprintf(out, "\nNo listeners remain detected on port %d; restricted processes may not be visible.\n", *port)
	return nil
}

func findListeners(port int) ([]listener, error) {
	conns, err := psnet.Connections("tcp")
	if err != nil {
		return nil, fmt.Errorf("list TCP connections: %w", err)
	}
	var result []listener
	for _, c := range conns {
		if c.Laddr.Port == uint32(port) && strings.EqualFold(c.Status, "LISTEN") {
			result = append(result, listener{pid: c.Pid, address: net.JoinHostPort(c.Laddr.IP, strconv.Itoa(port))})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].pid != result[j].pid {
			return result[i].pid < result[j].pid
		}
		return result[i].address < result[j].address
	})
	return result, nil
}

func inspect(pid int32) (identity, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return identity{}, fmt.Errorf("inspect PID %d: %w", pid, err)
	}
	created, err := p.CreateTime()
	if err != nil || created <= 0 {
		return identity{}, fmt.Errorf("read creation time for PID %d: %v", pid, err)
	}
	parent, err := p.Ppid()
	if err != nil {
		return identity{}, fmt.Errorf("read parent of PID %d: %w", pid, err)
	}
	executable, err := p.Exe()
	if err != nil {
		executable = fmt.Sprintf("(unavailable: %v)", err)
	} else if executable == "" {
		executable = "(unavailable)"
	}
	name, err := p.Name()
	if err != nil || name == "" {
		name = "(unavailable)"
	}
	cmdline, err := p.Cmdline()
	if err != nil || strings.TrimSpace(cmdline) == "" {
		cmdline = "(unavailable)"
	}
	cwd, err := p.Cwd()
	if err != nil || cwd == "" {
		// Windows refuses the working directory of elevated processes
		cwd = "(unavailable)"
	}
	return identity{pid: pid, created: created, parent: parent, executable: executable,
		name: name, cmdline: cmdline, cwd: cwd}, nil
}

// trimCommand returns a displayable command line: the classpath and module
// path arguments are elided so the java invocation and main class stay
// visible, and the result is length-capped.
func trimCommand(cmdline string) string {
	args := strings.Fields(cmdline)
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		out = append(out, args[i])
		if (args[i] == "-cp" || args[i] == "-classpath" || args[i] == "--class-path" ||
			args[i] == "-p" || args[i] == "--module-path") && i+1 < len(args) {
			out = append(out, "(elided)")
			i++
		}
	}
	trimmed := strings.Join(out, " ")
	const maxLen = 600
	if len(trimmed) > maxLen {
		trimmed = trimmed[:maxLen-len("…")] + "…"
	}
	return trimmed
}

// classify guesses what a process is, from OS-reported facts only. It is a
// display hint, never a reason to stop anything.
func classify(id identity) string {
	exe := id.executable
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		exe = exe[i+1:]
	}
	exe = strings.ToLower(exe)
	cmdline := strings.ToLower(id.cmdline)
	cwd := strings.ToLower(id.cwd)
	isJava := exe == "java" || exe == "java.exe" || exe == "javaw" || exe == "javaw.exe"
	switch {
	case isJava && strings.Contains(cmdline, "app.owlcms.mainwrapper"):
		return "OWLCMS (MainWrapper supervisor)"
	case isJava && strings.Contains(cmdline, "app.owlcms.main"):
		return "OWLCMS (app.owlcms.Main)"
	case isJava && strings.Contains(cmdline, "owlcms.jar"):
		return "OWLCMS (java -jar owlcms.jar)"
	case isJava && strings.Contains(cwd, "owlcms"):
		return "probably OWLCMS (Java in an owlcms directory)"
	case exe == "controlpanel" || exe == "controlpanel.exe":
		return "Control Panel"
	case (exe == "node" || exe == "node.exe") &&
		(strings.Contains(cmdline, "tracker") || strings.Contains(cwd, "tracker")):
		return "probably Tracker (node)"
	default:
		return "unrecognized"
	}
}

func showIdentity(out io.Writer, label string, id identity) {
	fmt.Fprintf(out, "%s: PID %d, parent PID %d, started %s\n", label, id.pid, id.parent,
		time.UnixMilli(id.created).UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(out, "  name: %s\n  executable: %s\n  command: %s\n  working directory: %s\n  looks like: %s\n",
		id.name, id.executable, trimCommand(id.cmdline), id.cwd, classify(id))
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

// offerHTTPStop asks OWLCMS to stop itself. OWLCMS then exits 0 after its
// shutdown hooks, which neither MainWrapper nor the Control Panel restarts.
// Returns true when the port no longer has a detected listener.
func offerHTTPStop(reader *bufio.Reader, out io.Writer, port int) (bool, error) {
	token := strings.TrimSpace(os.Getenv(tokenEnv))
	if token == "" {
		fmt.Fprintf(out, "No token in %s; sending the request without one (OWLCMS 66-68 accept it from localhost).\n", tokenEnv)
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
		remaining, err := findListeners(port)
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

// isSupervisor reports whether a parent relaunches OWLCMS when it exits with a
// code in 1..127. Both the Go Control Panel and the Java MainWrapper do.
func isSupervisor(id identity) bool {
	switch classify(id) {
	case "Control Panel", "OWLCMS (MainWrapper supervisor)":
		return true
	}
	return false
}

func handleListener(reader *bufio.Reader, out io.Writer, port int, pid int32) error {
	target, err := inspect(pid)
	if err != nil {
		return err
	}
	showIdentity(out, "Listener", target)
	ancestors, err := ownAncestors()
	if err != nil {
		return fmt.Errorf("cannot safely identify this CLI's ancestors: %w", err)
	}
	if ancestors[target.pid] {
		return fmt.Errorf("refusing to stop the CLI or its ancestors (PID %d)", target.pid)
	}

	// Inspect the parent before asking anything, so the user sees the whole picture.
	var parent *identity
	if target.parent > 1 {
		p, parentErr := inspect(target.parent)
		switch {
		case parentErr != nil:
			fmt.Fprintf(out, "Parent PID %d unavailable: %v\n", target.parent, parentErr)
		case p.created > target.created:
			fmt.Fprintf(out, "Parent PID %d was created after the listener; it may be a reused PID.\n", p.pid)
		case ancestors[p.pid] || unsafeParent(p.executable):
			showIdentity(out, "Protected parent", p)
			fmt.Fprintln(out, "  This parent will not be stopped by the CLI.")
		default:
			parent = &p
			if isSupervisor(p) {
				showIdentity(out, "Supervisor", p)
			} else {
				showIdentity(out, "Parent (not a known supervisor)", p)
			}
		}
	} else {
		fmt.Fprintf(out, "Parent PID %d is not an eligible stop target.\n", target.parent)
	}

	// On Windows the only forced kill is taskkill /F, which sets exit code 1.
	// Both the Control Panel and MainWrapper read 1 as "relaunch me". On Unix a
	// SIGTERM makes OWLCMS exit 0 and a SIGKILL is a signal death, neither of
	// which is restarted, so the parent can be left alone there.
	parentFirst := parent != nil && isSupervisor(*parent) && runtimeGOOS == "windows"
	if parentFirst {
		fmt.Fprintln(out, "  On Windows, forcing OWLCMS to exit makes this supervisor relaunch it. Stop the supervisor first.")
	}

	stopListener, err := confirm(reader, out, fmt.Sprintf("Stop listener PID %d?", target.pid))
	if err != nil || !stopListener {
		return err
	}

	if parent != nil {
		prompt := fmt.Sprintf("Stop parent PID %d as well?", parent.pid)
		if parentFirst {
			prompt = fmt.Sprintf("Stop supervisor PID %d first (recommended)?", parent.pid)
		}
		stopParent, promptErr := confirm(reader, out, prompt)
		if promptErr != nil {
			return promptErr
		}
		if stopParent {
			if err := validateTarget(target, port); err != nil {
				return err
			}
			if err := validateParent(target, *parent); err != nil {
				return err
			}
			if err := stopProcess(out, *parent, func() error { return validateParent(target, *parent) }); err != nil {
				return fmt.Errorf("parent PID %d: %w", parent.pid, err)
			}
		} else if parentFirst {
			fmt.Fprintln(out, "  The supervisor is still running; it may relaunch OWLCMS after the forced kill.")
		}
	}

	alive, err := sameProcess(target)
	if err != nil {
		return err
	}
	if !alive {
		fmt.Fprintf(out, "Listener PID %d has exited.\n", target.pid)
		return nil
	}
	if err := validateTarget(target, port); err != nil {
		return err
	}
	return stopProcess(out, target, func() error { return validateTarget(target, port) })
}

func ownAncestors() (map[int32]bool, error) {
	result := make(map[int32]bool)
	pid := int32(os.Getpid())
	for pid > 1 {
		if result[pid] {
			return nil, fmt.Errorf("parent cycle at PID %d", pid)
		}
		result[pid] = true
		p, err := process.NewProcess(pid)
		if err != nil {
			return nil, err
		}
		pid, err = p.Ppid()
		if err != nil {
			return nil, err
		}
	}
	result[1] = true
	return result, nil
}

func unsafeParent(executable string) bool {
	name := strings.ToLower(filepath.Base(executable))
	switch name {
	case "", "(unavailable)", "systemd", "launchd", "init", "bash", "sh", "zsh", "fish", "dash",
		"cmd.exe", "powershell.exe", "pwsh.exe":
		return true
	}
	return strings.HasPrefix(executable, "(unavailable:")
}

func sameProcess(id identity) (bool, error) {
	exists, err := process.PidExists(id.pid)
	if err != nil || !exists {
		return false, err
	}
	p, err := process.NewProcess(id.pid)
	var created int64
	if err == nil {
		created, err = p.CreateTime()
		if err == nil && created <= 0 {
			err = fmt.Errorf("invalid creation time for PID %d", id.pid)
		}
	}
	if err != nil {
		exists, checkErr := process.PidExists(id.pid)
		if checkErr != nil {
			return false, fmt.Errorf("inspect PID %d: %v; recheck existence: %w", id.pid, err, checkErr)
		}
		if !exists {
			return false, nil
		}
		return false, fmt.Errorf("inspect PID %d: %w", id.pid, err)
	}
	if created != id.created {
		return false, fmt.Errorf("PID %d was reused; refusing to act", id.pid)
	}
	return true, nil
}

func validateTarget(id identity, port int) error {
	alive, err := sameProcess(id)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("PID %d no longer exists", id.pid)
	}
	listeners, err := findListeners(port)
	if err != nil {
		return err
	}
	for _, l := range listeners {
		if l.pid == id.pid {
			return nil
		}
	}
	return fmt.Errorf("PID %d no longer appears as a listener on port %d; refusing to act", id.pid, port)
}

func validateParent(child, parent identity) error {
	alive, err := sameProcess(parent)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("parent PID %d has exited", parent.pid)
	}
	current, err := inspect(child.pid)
	if err != nil {
		return err
	}
	if current.created != child.created || current.parent != parent.pid {
		return fmt.Errorf("listener identity or parent has changed")
	}
	return nil
}

func stopProcess(out io.Writer, id identity, validate func() error) error {
	if err := validate(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Requesting soft termination of PID %d...\n", id.pid)
	softErr := terminate(id.pid, false)
	if softErr != nil {
		fmt.Fprintf(out, "Soft termination failed: %v\n", softErr)
	}
	exited, err := waitForExit(id, 5*time.Second)
	if err != nil || exited {
		return err
	}
	if err := validate(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Requesting forced termination of PID %d...\n", id.pid)
	if err := terminate(id.pid, true); err != nil {
		return fmt.Errorf("forced termination failed: %w", err)
	}
	exited, err = waitForExit(id, 5*time.Second)
	if err != nil {
		return err
	}
	if !exited {
		return fmt.Errorf("PID %d is still running after forced termination", id.pid)
	}
	return nil
}

func waitForExit(id identity, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		alive, err := sameProcess(id)
		if err != nil || !alive {
			return !alive && err == nil, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}
