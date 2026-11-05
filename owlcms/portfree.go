package owlcms

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"controlpanel/shared"
	"controlpanel/shared/portowner"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// owlcmsPortTarget is the process found listening on the OWLCMS port.
type owlcmsPortTarget struct {
	assessment portowner.Assessment
	port       int
	// stopAccepted is set once OWLCMS accepted the HTTP stop: it has released
	// the port and is running its shutdown hooks, so it is then revalidated by
	// identity rather than by port ownership.
	stopAccepted bool
}

func (t owlcmsPortTarget) validate() error {
	if t.stopAccepted {
		return portowner.ValidateAlive(t.assessment.Listener)
	}
	return portowner.ValidateListener(t.assessment.Listener, t.port)
}

// errOwlcmsPortOwnerUnknown means the port is occupied but no inspectable owner was found.
var errOwlcmsPortOwnerUnknown = errors.New("the process using the port cannot be identified without administrator rights")

// findOwlcmsPortTarget returns the first inspectable listener on port, or nil
// when nothing listens.
func findOwlcmsPortTarget(port int) (*owlcmsPortTarget, error) {
	listeners, err := portowner.FindListeners(port)
	if err != nil {
		return nil, err
	}
	for _, l := range listeners {
		if l.PID <= 0 {
			continue
		}
		a, err := portowner.Assess(l.PID)
		if err != nil {
			return nil, err
		}
		return &owlcmsPortTarget{assessment: a, port: port}, nil
	}
	if len(listeners) > 0 || shared.CheckPort(strconv.Itoa(port)) == nil {
		return nil, errOwlcmsPortOwnerUnknown
	}
	return nil, nil
}

func describeProcess(id portowner.Identity) string {
	return fmt.Sprintf("PID %d — %s\n%s", id.PID, portowner.Classify(id), id.Executable)
}

func describeOwlcmsPortTarget(t *owlcmsPortTarget) string {
	a := t.assessment
	var b strings.Builder
	fmt.Fprintf(&b, "Port %d is used by:\n%s\n\n", t.port, describeProcess(a.Listener))
	switch a.ParentKind {
	case portowner.ParentOrphan:
		b.WriteString("Its parent has exited; nothing will restart it.")
	case portowner.ParentUnavailable:
		b.WriteString("Its parent cannot be inspected; it may be restarted after it is stopped.")
	case portowner.ParentProtected, portowner.ParentOther:
		fmt.Fprintf(&b, "Its parent is:\n%s", describeProcess(*a.Parent))
	case portowner.ParentSupervisor:
		fmt.Fprintf(&b, "It is supervised by:\n%s", describeProcess(*a.Parent))
	}
	return b.String()
}

// freeOwlcmsPort asks before stopping whatever prevents OWLCMS from using port,
// then calls onDone on the UI thread with whether the port is now free. For an
// OWLCMS process it first asks OWLCMS to stop itself (exit 0, never restarted).
// On Windows, a forced stop exits 1, which a supervising Control Panel or
// MainWrapper restarts, so the supervisor is offered first. Must be called on
// the UI thread.
func freeOwlcmsPort(port string, w fyne.Window, onDone func(freed bool)) {
	portNum, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || portNum < 1 || portNum > 65535 {
		dialog.ShowError(fmt.Errorf("invalid port %q", port), w)
		onDone(false)
		return
	}
	progress := showFreePortProgress(w, fmt.Sprintf("Inspecting port %d...", portNum))
	go func() {
		target, err := findOwlcmsPortTarget(portNum)
		fyne.Do(func() {
			progress.Hide()
			switch {
			case err != nil:
				log.Printf("Cannot inspect port %d: %v", portNum, err)
				dialog.ShowError(fmt.Errorf("port %d is in use: %w", portNum, err), w)
				onDone(false)
			case target == nil:
				onDone(true)
			case target.assessment.Parent != nil && target.assessment.Parent.PID == int32(os.Getpid()):
				dialog.ShowInformation("OWLCMS Running", "This OWLCMS was started by this Control Panel. Use the Stop button.", w)
				onDone(false)
			default:
				confirmStopOwlcmsPortTarget(target, w, onDone)
			}
		})
	}()
}

func confirmStopOwlcmsPortTarget(target *owlcmsPortTarget, w fyne.Window, onDone func(bool)) {
	a := target.assessment
	log.Printf("Port %d target: PID %d (%s, %s), parent PID %d kind %d",
		target.port, a.Listener.PID, portowner.Classify(a.Listener), a.Listener.Executable, a.Listener.Parent, a.ParentKind)
	message := widget.NewLabel(describeOwlcmsPortTarget(target) + "\n\nStop it?")
	dialog.ShowCustomConfirm("Port In Use", "Stop", "Cancel", message, func(confirmed bool) {
		if !confirmed {
			log.Printf("User declined stopping PID %d", a.Listener.PID)
			onDone(false)
			return
		}
		progress := showFreePortProgress(w, fmt.Sprintf("Stopping PID %d...", a.Listener.PID))
		go func() {
			stopped := portowner.IsOwlcms(a.Listener) && requestOwlcmsStopTarget(target)
			fyne.Do(func() {
				progress.Hide()
				if stopped {
					verifyOwlcmsPortFreed(target.port, w, onDone)
					return
				}
				if a.SupervisorFirst {
					confirmStopSupervisor(target, w, onDone)
					return
				}
				forceStopOwlcmsPortTarget(target, false, w, onDone)
			})
		}()
	}, w)
}

// requestOwlcmsStopTarget asks OWLCMS to exit through its HTTP endpoint and
// reports whether the target process exited. When the stop is accepted, the
// target is marked so later checks no longer require it to own the port.
func requestOwlcmsStopTarget(target *owlcmsPortTarget) bool {
	if err := requestOwlcmsStop(strconv.Itoa(target.port), owlcmsTokenFor(target.assessment)); err != nil {
		log.Printf("OWLCMS HTTP stop on port %d did not succeed: %v", target.port, err)
		return false
	}
	target.stopAccepted = true
	exited, err := portowner.WaitForExit(target.assessment.Listener, 20*time.Second)
	if err != nil {
		log.Printf("Cannot confirm that PID %d exited after the HTTP stop: %v", target.assessment.Listener.PID, err)
		return false
	}
	if !exited {
		log.Printf("OWLCMS PID %d still running 20s after accepting the stop", target.assessment.Listener.PID)
	}
	return exited
}

func confirmStopSupervisor(target *owlcmsPortTarget, w fyne.Window, onDone func(bool)) {
	parent := *target.assessment.Parent
	message := widget.NewLabel(fmt.Sprintf(
		"OWLCMS did not stop on request. On Windows, forcing it to stop makes its supervisor start it again:\n%s\n\nStop the supervisor first (recommended)?",
		describeProcess(parent)))
	dialog.ShowCustomConfirm("Stop Supervisor", "Stop Supervisor", "Only OWLCMS", message, func(stopParent bool) {
		forceStopOwlcmsPortTarget(target, stopParent, w, onDone)
	}, w)
}

func forceStopOwlcmsPortTarget(target *owlcmsPortTarget, stopParent bool, w fyne.Window, onDone func(bool)) {
	a := target.assessment
	progress := showFreePortProgress(w, fmt.Sprintf("Stopping PID %d...", a.Listener.PID))
	report := func(line string) { log.Printf("Freeing port %d: %s", target.port, line) }
	go func() {
		err := func() error {
			if stopParent {
				parent := *a.Parent
				if err := target.validate(); err != nil {
					return err
				}
				log.Printf("Stopping supervisor PID %d (%s) before PID %d", parent.PID, parent.Executable, a.Listener.PID)
				if err := portowner.Stop(parent, func() error { return portowner.ValidateParent(a.Listener, parent) }, report); err != nil {
					return fmt.Errorf("supervisor PID %d: %w", parent.PID, err)
				}
			}
			alive, err := portowner.SameProcess(a.Listener)
			if err != nil || !alive {
				return err
			}
			if err := portowner.Stop(a.Listener, target.validate, report); err != nil {
				return fmt.Errorf("PID %d: %w", a.Listener.PID, err)
			}
			return nil
		}()
		fyne.Do(func() {
			progress.Hide()
			if err != nil {
				log.Printf("Could not free port %d: %v", target.port, err)
				dialog.ShowError(fmt.Errorf("could not stop %s on port %d: %w",
					portowner.Classify(a.Listener), target.port, err), w)
				onDone(false)
				return
			}
			verifyOwlcmsPortFreed(target.port, w, onDone)
		})
	}()
}

// verifyOwlcmsPortFreed checks the port again. A new occupant, for example a
// replacement started by a supervisor, needs a new confirmation.
func verifyOwlcmsPortFreed(port int, w fyne.Window, onDone func(bool)) {
	progress := showFreePortProgress(w, fmt.Sprintf("Checking port %d...", port))
	go func() {
		// a supervisor restarts OWLCMS after a short delay
		time.Sleep(2 * time.Second)
		target, err := findOwlcmsPortTarget(port)
		fyne.Do(func() {
			progress.Hide()
			switch {
			case err != nil:
				log.Printf("Port %d is still in use: %v", port, err)
				dialog.ShowError(fmt.Errorf("port %d is still in use: %w", port, err), w)
				onDone(false)
			case target == nil:
				log.Printf("Port %d is free", port)
				onDone(true)
			default:
				log.Printf("Port %d is in use again by PID %d", port, target.assessment.Listener.PID)
				confirmStopOwlcmsPortTarget(target, w, onDone)
			}
		})
	}()
}

func showFreePortProgress(w fyne.Window, text string) dialog.Dialog {
	d := dialog.NewCustomWithoutButtons("Freeing Port", widget.NewLabel(text), w)
	d.Show()
	return d
}

// RunningOwlcms is an OWLCMS process found listening on a TCP port.
type RunningOwlcms struct {
	Assessment portowner.Assessment
	Port       string
	// Version is the installed version whose directory OWLCMS runs in, or empty.
	Version string
}

// isOurOwlcms reports whether id is an OWLCMS server running from this
// instance's OWLCMS directory. The working directory is reported by the OS and
// distinguishes instances when several run on one machine.
func isOurOwlcms(id portowner.Identity) bool {
	return portowner.IsOwlcms(id) && versionFromWorkingDir(id.Cwd) != ""
}

// FindRunningOwlcms finds this instance's running OWLCMS from the process
// inventory only: the one listening on the configured port, or failing that
// any process running from this instance's OWLCMS directory that listens on a
// TCP port. A port held by anything else, including another instance's OWLCMS,
// is not reported here; the launch flow then shows it as a port conflict.
func FindRunningOwlcms() (*RunningOwlcms, error) {
	configured, err := strconv.Atoi(strings.TrimSpace(GetPort()))
	if err != nil {
		return nil, fmt.Errorf("invalid OWLCMS port %q", GetPort())
	}
	target, err := findOwlcmsPortTarget(configured)
	if err != nil && !errors.Is(err, errOwlcmsPortOwnerUnknown) {
		return nil, err
	}
	port := configured
	if target == nil || !isOurOwlcms(target.assessment.Listener) {
		if target != nil {
			log.Printf("Port %d is held by PID %d (%s, in %s), not this instance's OWLCMS", configured,
				target.assessment.Listener.PID, portowner.Classify(target.assessment.Listener), target.assessment.Listener.Cwd)
		}
		target, port, err = findOwlcmsAnywhere()
		if err != nil || target == nil {
			return nil, err
		}
	}
	a := target.assessment
	return &RunningOwlcms{Assessment: a, Port: strconv.Itoa(port), Version: versionFromWorkingDir(a.Listener.Cwd)}, nil
}

// findOwlcmsAnywhere scans the process inventory for this instance's OWLCMS
// listening on a TCP port, for an instance launched on a port other than the
// configured one. Returns the target and its lowest listening port.
func findOwlcmsAnywhere() (*owlcmsPortTarget, int, error) {
	ports, err := portowner.ListeningPorts()
	if err != nil {
		return nil, 0, err
	}
	for pid, listening := range ports {
		id, err := portowner.Inspect(pid)
		if err != nil || !isOurOwlcms(id) {
			continue
		}
		port := listening[0]
		for _, p := range listening[1:] {
			if p < port {
				port = p
			}
		}
		a, err := portowner.Assess(pid)
		if err != nil {
			return nil, 0, err
		}
		log.Printf("OWLCMS PID %d found listening on port %d instead of the configured port", pid, port)
		return &owlcmsPortTarget{assessment: a, port: port}, port, nil
	}
	return nil, 0, nil
}

// versionFromWorkingDir returns the installed version whose directory OWLCMS runs in.
func versionFromWorkingDir(cwd string) string {
	dir := filepath.Clean(strings.TrimSpace(cwd))
	if !strings.EqualFold(filepath.Dir(dir), filepath.Clean(installDir)) {
		return ""
	}
	return filepath.Base(dir)
}
