package owlcms

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"controlpanel/shared"
	"controlpanel/shared/portowner"
)

const controlPanelStopPath = "/controlpanel/stop"

// controlPanelTokenEnv gives OWLCMS a token at launch; presenting it on stop proves the
// request comes from this control panel. It is also the emergency admin password. OWLCMS 66-68
// ignore the token; OWLCMS 69+ refuses a stop request that does not carry the token it was launched with.
const controlPanelTokenEnv = "OWLCMS_CONTROLPANELTOKEN"

// lastLaunchToken is the token given to the most recent OWLCMS launch, restarts included. Each launch gets a new
// random token, so a token used for an emergency login stops working once OWLCMS restarts. Never logged.
var lastLaunchToken string

const controlPanelTokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// newControlPanelToken has the same format as OWLCMS account tokens: 12 characters from a 64-symbol URL-safe
// alphabet (72 bits).
func newControlPanelToken() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		log.Printf("cannot generate the OWLCMS stop token: %v", err)
		return ""
	}
	for i := range b {
		// 64 symbols divide 256 evenly, so masking keeps the distribution uniform
		b[i] = controlPanelTokenAlphabet[b[i]&63]
	}
	return string(b)
}

// withNewControlPanelToken returns a copy of env for one OWLCMS launch, carrying a new token, and that token.
// The token is set last so that no other setting can replace it. Without a token (generation failed), OWLCMS is
// launched without one.
func withNewControlPanelToken(env []string) ([]string, string) {
	next := append([]string(nil), env...)
	token := newControlPanelToken()
	lastLaunchToken = token
	if token == "" {
		return removeEnvKey(next, controlPanelTokenEnv), ""
	}
	return shared.UpsertEnv(next, controlPanelTokenEnv, token), token
}

// runningOwlcmsToken is the token the running OWLCMS was given: the one recorded at launch, possibly by an earlier
// control panel run, or empty when that OWLCMS was launched without one.
func runningOwlcmsToken() string {
	if activeRuntime != nil {
		return activeRuntime.Token
	}
	return lastLaunchToken
}

// owlcmsTokenFor returns the launch token of the OWLCMS process in a, read from
// the process's own environment: the stop must carry the token that process was
// started with, whoever started it, and only the same user on the same machine
// can read it. The token this control panel gave at launch is used only when
// the environment cannot be read.
func owlcmsTokenFor(a portowner.Assessment) string {
	if token := strings.TrimSpace(portowner.EnvValue(a.Listener.PID, controlPanelTokenEnv)); token != "" {
		return token
	}
	if m := activeRuntime; m != nil && m.Token != "" &&
		(m.PID == int(a.Listener.PID) || (a.ParentKind != portowner.ParentOrphan && m.PID == int(a.Listener.Parent))) {
		log.Printf("Token of OWLCMS PID %d not readable from its environment; using the one given at launch", a.Listener.PID)
		return m.Token
	}
	return ""
}

// stopTrace collects the stages of a stop for display. TEMPORARY: shown in a
// dialog after the Stop button for visual confirmation; remove once verified.
type stopTrace struct{ lines []string }

func (t *stopTrace) add(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Printf("Stop trace: %s", line)
	t.lines = append(t.lines, line)
}

func (t *stopTrace) String() string { return strings.Join(t.lines, "\n") }

// StopRunningOwlcms stops this instance's OWLCMS when this control panel holds
// no process handle for it: it asks OWLCMS to stop itself (exit 0, never
// restarted), then terminates the listener. It acts only on the OWLCMS listening
// on the port and never on its parent; returns nil when no OWLCMS is running.
// The returned trace describes what was found and done.
func StopRunningOwlcms() (*stopTrace, error) {
	trace := &stopTrace{}
	running, err := FindRunningOwlcms()
	if err != nil {
		trace.add("Inventory failed: %v", err)
		return trace, err
	}
	if running == nil {
		trace.add("No OWLCMS of this instance is listening")
		return trace, nil
	}
	a := running.Assessment
	listener := a.Listener
	portNum, _ := strconv.Atoi(running.Port)
	trace.add("Found PID %d (%s) on port %s", listener.PID, portowner.Classify(listener), running.Port)
	trace.add("Working directory: %s", listener.Cwd)
	trace.add("This instance's OWLCMS directory: %s (version %q)", installDir, running.Version)
	switch a.ParentKind {
	case portowner.ParentOrphan:
		trace.add("Parent PID %d has exited (orphan)", listener.Parent)
	case portowner.ParentUnavailable:
		trace.add("Parent PID %d not inspectable", listener.Parent)
	default:
		trace.add("Parent PID %d: %s", a.Parent.PID, portowner.Classify(*a.Parent))
	}
	envToken := strings.TrimSpace(portowner.EnvValue(listener.PID, controlPanelTokenEnv))
	token := owlcmsTokenFor(a)
	switch {
	case envToken != "":
		trace.add("Token from process environment: %s", envToken)
	case token != "":
		trace.add("Token not in process environment; using launch token: %s", token)
	default:
		trace.add("No token: not in the process environment and not launched by this control panel")
	}
	report := func(line string) { trace.add("%s", line) }

	// Once OWLCMS accepts the stop it releases the port first and exits after its
	// shutdown hooks, so wait for the process rather than for the port.
	validate := func() error { return portowner.ValidateListener(listener, portNum) }
	if err := requestOwlcmsStop(running.Port, token); err != nil {
		trace.add("HTTP stop not accepted: %v", err)
	} else {
		trace.add("HTTP stop accepted; port released")
		exited, err := portowner.WaitForExit(listener, 20*time.Second)
		if err != nil {
			trace.add("Cannot confirm exit: %v", err)
			return trace, err
		}
		if exited {
			trace.add("PID %d exited", listener.PID)
		} else {
			trace.add("PID %d still running 20s after accepting the stop; terminating it", listener.PID)
		}
		validate = func() error { return portowner.ValidateAlive(listener) }
	}
	alive, err := portowner.SameProcess(listener)
	if err != nil {
		trace.add("Identity check failed: %v", err)
		return trace, err
	}
	if alive {
		if err := portowner.Stop(listener, validate, report); err != nil {
			trace.add("Termination failed: %v", err)
			return trace, fmt.Errorf("PID %d: %w", listener.PID, err)
		}
		trace.add("PID %d terminated", listener.PID)
	}
	remaining, err := portowner.FindListeners(portNum)
	if err != nil {
		return trace, err
	}
	if len(remaining) > 0 {
		trace.add("Port %s is in use again by PID %d", running.Port, remaining[0].PID)
		return trace, fmt.Errorf("OWLCMS PID %d stopped, but port %s is in use by PID %d", listener.PID, running.Port, remaining[0].PID)
	}
	trace.add("Port %s is free", running.Port)
	return trace, nil
}

func requestOwlcmsStop(port, token string) error {
	trimmedPort := strings.TrimSpace(port)
	if trimmedPort == "" {
		return fmt.Errorf("missing port")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%s%s", trimmedPort, controlPanelStopPath)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("clean stop endpoint %s is not available in this OWLCMS version", controlPanelStopPath)
		}
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("OWLCMS refused the clean stop request from this host")
		}
		return fmt.Errorf("%s returned %s: %s", controlPanelStopPath, resp.Status, strings.TrimSpace(string(body)))
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if shared.CheckPort(trimmedPort) != nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("OWLCMS accepted stop request but port %s stayed open", trimmedPort)
}
