package firmata

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"controlpanel/shared"
	"controlpanel/shared/portowner"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// adoptedProcess is a Firmata server this control panel found running at
// startup but did not launch, typically left behind by a Control Panel that
// another one replaced. With no process handle, it is stopped through its PID.
var adoptedProcess *portowner.Identity

// findRunningFirmata finds this instance's Firmata server from the process
// table: java -jar owlcms-firmata.jar running in a version directory of this
// instance's Firmata install directory. Returns the process and its version.
func findRunningFirmata() (*portowner.Identity, string) {
	javas, err := portowner.ListProcessesNamed(portowner.JavaExecutableNames...)
	if err != nil {
		log.Printf("Cannot list Java processes to find a running Firmata: %v", err)
		return nil, ""
	}
	for _, id := range javas {
		if portowner.Classify(id) != portowner.KindFirmata {
			continue
		}
		if version := versionFromWorkingDir(id.Cwd); version != "" {
			found := id
			return &found, version
		}
		log.Printf("Firmata PID %d runs in %s, not in this instance's %s", id.PID, id.Cwd, installDir)
	}
	return nil, ""
}

// versionFromWorkingDir returns the installed version whose directory Firmata runs in.
func versionFromWorkingDir(cwd string) string {
	dir := filepath.Clean(strings.TrimSpace(cwd))
	if !strings.EqualFold(filepath.Dir(dir), filepath.Clean(installDir)) {
		return ""
	}
	return filepath.Base(dir)
}

// adoptRunningFirmata attaches the tab to a Firmata found running. Must be
// called on the Fyne thread.
func adoptRunningFirmata(id *portowner.Identity, version string) {
	adoptedProcess = id
	currentVersion = version
	port := GetPortForRelease(version)
	log.Printf("Reattaching to owlcms-firmata PID %d (version %q, port %s)", id.PID, version, port)

	stopButton.SetText(fmt.Sprintf("Stop owlcms-firmata %s", version))
	stopButton.Show()
	statusLabel.SetText(fmt.Sprintf("owlcms-firmata running (PID: %d) on port %s", id.PID, port))
	statusLabel.Show()
	setFirmataTabModeRunning()

	urlLink.SetURLFromString(fmt.Sprintf("http://localhost:%s", port))
	urlLink.SetText("Open owlcms-firmata in a browser")
	urlLink.Show()
	appDir := filepath.Join(installDir, version)
	appDirLink.SetText(fmt.Sprintf("Open owlcms-firmata %s directory", version))
	appDirLink.SetURL(nil)
	appDirLink.OnTapped = func() { shared.OpenFileExplorer(appDir) }
	appDirLink.Show()
	configureTailLogLink(version, appDir)

	go watchAdoptedFirmata(*id)
}

// watchAdoptedFirmata returns the tab to the version list when the adopted
// Firmata exits on its own.
func watchAdoptedFirmata(id portowner.Identity) {
	for {
		time.Sleep(2 * time.Second)
		alive, err := portowner.SameProcess(id)
		if err == nil && alive {
			continue
		}
		fyne.Do(func() {
			if adoptedProcess == nil || adoptedProcess.PID != id.PID {
				return // stopped through the Stop button
			}
			log.Printf("owlcms-firmata PID %d exited", id.PID)
			adoptedProcess = nil
			showAdoptedStopped(fmt.Sprintf("owlcms-firmata %s (PID: %d) exited", currentVersion, id.PID))
		})
		return
	}
}

// stopAdoptedFirmata stops the adopted Firmata off the Fyne thread. Its parent
// Control Panel is gone, so nothing restarts it, even after a forced kill.
func stopAdoptedFirmata(statusLbl *widget.Label, w fyne.Window) {
	id := *adoptedProcess
	version := currentVersion
	statusLbl.SetText(fmt.Sprintf("Stopping owlcms-firmata %s...", version))
	stopButton.Disable()
	go func() {
		report := func(line string) { log.Printf("Stopping adopted owlcms-firmata: %s", line) }
		err := portowner.Stop(id, func() error { return portowner.ValidateAlive(id) }, report)
		fyne.Do(func() {
			stopButton.Enable()
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to stop owlcms-firmata %s (PID: %d): %w", version, id.PID, err), w)
				return
			}
			log.Printf("owlcms-firmata %s (PID: %d) has been stopped", version, id.PID)
			adoptedProcess = nil
			showAdoptedStopped(fmt.Sprintf("owlcms-firmata %s (PID: %d) has been stopped", version, id.PID))
		})
	}()
}

// killAdoptedFirmata ends the adopted Firmata immediately, for signal cleanup.
func killAdoptedFirmata() {
	if adoptedProcess == nil {
		return
	}
	id := *adoptedProcess
	adoptedProcess = nil
	report := func(line string) { log.Printf("Signal cleanup, adopted owlcms-firmata: %s", line) }
	if err := portowner.Kill(id, func() error { return portowner.ValidateAlive(id) }, report); err != nil {
		log.Printf("Could not stop adopted owlcms-firmata PID %d: %v", id.PID, err)
	}
}

func showAdoptedStopped(message string) {
	statusLabel.SetText(message)
	stopButton.Hide()
	urlLink.Hide()
	appDirLink.Hide()
	if tailLogLink != nil {
		tailLogLink.Hide()
	}
	releaseJavaLock()
	goBackToMainScreen()
}
