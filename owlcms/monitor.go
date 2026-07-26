package owlcms

import (
	"controlpanel/shared"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var killedByUs bool
var stopInProgress atomic.Bool

func monitorProcess(done <-chan error, port string) chan error {
	result := make(chan error, 1)
	go func() {
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case err := <-done:
				if err != nil {
					result <- fmt.Errorf("process failed: %w", err)
				} else {
					result <- fmt.Errorf("process exited before becoming ready")
				}
				return
			case <-timeout:
				result <- fmt.Errorf("timed out waiting for process to become ready")
				return
			case <-ticker.C:
				// Readiness must go through HTTP: OWLCMS binds the port before it
				// initializes the database, and holds the bootstrap page until the
				// database is ready. A bare TCP connect would report ready too early.
				if shared.CheckPortReady(port) == nil {
					result <- nil
					return
				}
			}
		}
	}()
	return result
}

func stopProcess(version string, stopBtn *widget.Button, downloadGroup, versionCont *fyne.Container, statusLbl *widget.Label, w fyne.Window) {
	if !stopInProgress.CompareAndSwap(false, true) {
		log.Printf("OWLCMS stop already in progress")
		return
	}

	log.Printf("Stopping OWLCMS %s...\n", version)
	statusLbl.SetText(fmt.Sprintf("Stopping OWLCMS %s...", version))
	stopBtn.Disable()

	port := GetPort()
	if activeRuntime != nil && activeRuntime.Port != "" {
		port = activeRuntime.Port
	}

	killedByUs = true

	go func() {
		var err error
		if currentProcess != nil && currentProcess.Process != nil {
			pid := currentProcess.Process.Pid
			log.Printf("Stopping owned OWLCMS process with Go process handle (PID: %d)", pid)
			err = shared.StopOwnedProcess(currentProcess, 10*time.Second)
		} else {
			err = StopProcessByPort(port)
		}
		if err == nil {
			time.Sleep(300 * time.Millisecond)
		}
		fyne.Do(func() {
			if err != nil {
				stopInProgress.Store(false)
				killedByUs = false
				stopBtn.Enable()
				statusLbl.SetText(fmt.Sprintf("OWLCMS %s is still running", version))
				dialog.ShowError(fmt.Errorf("failed to stop OWLCMS on port %s: %w", port, err), w)
				return
			}

			log.Printf("OWLCMS %s has been stopped (port %s freed)\n", version, port)
			statusLbl.SetText(fmt.Sprintf("OWLCMS %s has been stopped", version))
			currentProcess = nil
			clearRuntimeState()

			stopBtn.Enable()
			stopBtn.Hide()
			stopContainer.Hide()

			// Restore UI using centralized mode switching (prevents desync).
			_ = downloadGroup
			_ = versionCont
			setOwlcmsTabMode(w)

			urlLink.Hide()
			appDirLink.Hide()
			if tailLogLink != nil {
				tailLogLink.Hide()
			}
			stopInProgress.Store(false)
			releaseJavaLock()
		})
	}()
}
