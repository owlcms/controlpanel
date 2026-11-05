package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"controlpanel/shared"
	"controlpanel/shared/portowner"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// runningControlPanel is another Control Panel process found on this machine.
type runningControlPanel struct {
	Identity portowner.Identity
	// InstanceDir is the control panel directory the process serves, or empty
	// when it could not be determined.
	InstanceDir string
}

const controlPanelInstallDirEnv = "CONTROLPANEL_INSTALLDIR"

// findRunningControlPanels lists Control Panel processes other than this one
// and its ancestors, identified from the OS process table rather than from
// recorded startup files. Only processes with a Control Panel executable name
// are inspected; a full inventory took 20 s on macOS.
func findRunningControlPanels() ([]runningControlPanel, error) {
	processes, err := portowner.ListProcessesNamed(portowner.ControlPanelExecutableNames...)
	if err != nil {
		return nil, err
	}
	ancestors, err := portowner.OwnAncestors()
	if err != nil {
		return nil, err
	}
	var result []runningControlPanel
	for _, id := range processes {
		if ancestors[id.PID] || portowner.Classify(id) != portowner.KindControlPanel {
			continue
		}
		result = append(result, runningControlPanel{
			Identity:    id,
			InstanceDir: controlPanelInstanceDir(id.Args, portowner.Environ(id.PID)),
		})
	}
	return result, nil
}

// controlPanelInstanceDir resolves the control panel directory of a Control
// Panel launched with args, the same way applyCLIInstanceOptions does: an
// instance flag wins, then CONTROLPANEL_INSTALLDIR in its environment, then
// the default directory. Returns empty when the command line is unavailable.
func controlPanelInstanceDir(args, environ []string) string {
	if len(args) == 0 {
		return ""
	}
	opts := parseCLIOptions(args[1:])
	instanceArg := strings.TrimSpace(opts.instanceArg)
	if instanceArg == "" && opts.init {
		instanceArg = mainInstanceName
	}
	if instanceArg != "" {
		paths, err := resolveInstancePaths(instanceArg)
		if err != nil {
			return ""
		}
		return paths.ControlPanelDir
	}
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, controlPanelInstallDirEnv+"="); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return shared.DefaultControlPanelInstallDir()
}

func sameInstanceDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if shared.GetGoos() == "windows" || shared.GetGoos() == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// conflictingControlPanels returns the running Control Panels serving the same
// instance directory as this process. One whose instance could not be
// determined is included, since it may be a conflict.
func conflictingControlPanels() ([]runningControlPanel, error) {
	all, err := findRunningControlPanels()
	if err != nil {
		return nil, err
	}
	mine := shared.GetControlPanelInstallDir()
	var result []runningControlPanel
	for _, cp := range all {
		if cp.InstanceDir == "" || sameInstanceDir(cp.InstanceDir, mine) {
			result = append(result, cp)
		} else {
			log.Printf("Control Panel PID %d serves another instance (%s); no conflict", cp.Identity.PID, cp.InstanceDir)
		}
	}
	return result, nil
}

func describeRunningControlPanel(cp runningControlPanel) string {
	id := cp.Identity
	parts := []string{fmt.Sprintf("PID %d", id.PID), id.Executable}
	if cp.InstanceDir == "" {
		parts = append(parts, "instance unknown")
	}
	parts = append(parts, "started "+time.UnixMilli(id.Created).Local().Format("2006-01-02 15:04:05"))
	return strings.Join(parts, ", ")
}

// stopRunningControlPanel ends cp without letting it run its shutdown
// handlers, verifying first that the PID still belongs to the same process.
// A soft stop would make cp stop the servers it supervises (its signal cleanup
// does so); killing it outright leaves OWLCMS, Tracker and Firmata running as
// orphans, which this Control Panel then adopts. Video is a desktop
// application and is not adopted: it is stopped first. Windows already
// skipped the cleanup because taskkill /F is its only termination; this makes
// Linux and macOS match and stops Video on all three.
func stopRunningControlPanel(cp runningControlPanel) error {
	id := cp.Identity
	validate := func() error {
		alive, err := portowner.SameProcess(id)
		if err != nil {
			return err
		}
		if !alive {
			return fmt.Errorf("Control Panel PID %d has already exited", id.PID)
		}
		return nil
	}
	report := func(line string) { log.Printf("Stopping Control Panel PID %d: %s", id.PID, line) }
	stopVideoChildren(id, report)
	err := portowner.Kill(id, validate, report)
	if err != nil && strings.Contains(err.Error(), "already exited") {
		return nil
	}
	return err
}

// stopVideoChildren stops the Video processes cp launched. Failures are logged:
// a Video left running only holds its own window and port.
func stopVideoChildren(cp portowner.Identity, report func(string)) {
	videos, err := portowner.ListProcessesMatching(portowner.IsVideoExecutable)
	if err != nil {
		report(fmt.Sprintf("Cannot list Video processes: %v", err))
		return
	}
	for _, v := range videos {
		if v.Parent != cp.PID || v.Created < cp.Created {
			continue
		}
		child := v
		validate := func() error { return portowner.ValidateParent(child, cp) }
		report(fmt.Sprintf("Stopping its Video, PID %d", child.PID))
		if err := portowner.Stop(child, validate, report); err != nil {
			report(fmt.Sprintf("Could not stop Video PID %d: %v", child.PID, err))
		}
	}
}

// removeLegacyControlPanelRuntimeFiles deletes the startup files older
// control panels used to detect each other.
func removeLegacyControlPanelRuntimeFiles() {
	dir := shared.GetControlPanelInstallDir()
	for _, name := range []string{"controlpanel-run.json", "controlpanel.pid"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove legacy %s: %v", name, err)
		}
	}
}

func startControlPanelRuntimeGate(a fyne.App, w fyne.Window, initialWindowSize fyne.Size, start func()) bool {
	removeLegacyControlPanelRuntimeFiles()
	conflicts, err := conflictingControlPanels()
	if err != nil {
		log.Printf("Cannot list running Control Panels: %v", err)
	}
	if len(conflicts) == 0 {
		start()
		return true
	}

	var described []string
	for _, cp := range conflicts {
		described = append(described, describeRunningControlPanel(cp))
	}
	message := fmt.Sprintf(
		"Another OWLCMS Control Panel is already running on this instance:\n%s\n\nStop it and continue?",
		strings.Join(described, "\n"),
	)
	w.Resize(initialWindowSize)
	w.SetContent(container.NewCenter(widget.NewLabel("Another OWLCMS Control Panel is already running.")))
	w.Show()

	confirmDialog := dialog.NewConfirm(
		"Control Panel Already Running",
		message,
		func(confirm bool) {
			if !confirm {
				a.Quit()
				return
			}

			go func() {
				for _, cp := range conflicts {
					log.Printf("Stopping existing control panel %s", describeRunningControlPanel(cp))
					if err := stopRunningControlPanel(cp); err != nil {
						log.Printf("Failed to stop existing control panel: %v", err)
						fyne.Do(func() {
							dialog.ShowError(fmt.Errorf("failed to stop running Control Panel: %w", err), w)
						})
						return
					}
				}
				fyne.Do(start)
			}()
		},
		w,
	)
	confirmDialog.SetConfirmText("Stop Existing and Continue")
	confirmDialog.SetDismissText("Cancel")
	confirmDialog.Show()
	return true
}
