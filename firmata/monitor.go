package firmata

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var killedByUs bool

// checkPort tries to connect to localhost:port and returns nil if successful.
func checkPort(port string) error {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%s", port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func monitorProcess(done <-chan error, port string) chan error {
	result := make(chan error, 1)
	go func() {
		// Try connecting to port for up to 60 seconds
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case err := <-done:
				// Process exited before port was available
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
				if err := checkPort(port); err == nil {
					// Port is responding, process is ready
					result <- nil
					return
				}
			}
		}
	}()
	return result
}

func stopProcess(curProcess *exec.Cmd, curVersion string, stopBtn *widget.Button, downloadGroup, versionCont *fyne.Container, statusLbl *widget.Label, w fyne.Window) {
	if (curProcess == nil || curProcess.Process == nil) && adoptedProcess != nil {
		stopAdoptedFirmata(statusLbl, w)
		return
	}
	log.Printf("Stopping owlcms-firmata %s...\n", curVersion)
	statusLbl.SetText(fmt.Sprintf("Stopping owlcms-firmata %s...", curVersion))

	if curProcess == nil || curProcess.Process == nil {
		return
	}
	pid := curProcess.Process.Pid
	killedByUs = true

	if err := shared.StopOwnedProcess(curProcess, 10*time.Second); err != nil {
		killedByUs = false
		dialog.ShowError(fmt.Errorf("failed to stop owlcms-firmata %s (PID: %d): %w", curVersion, pid, err), w)
		return
	}

	log.Printf("owlcms-firmata %s (PID: %d) has been stopped\n", curVersion, pid)
	statusLbl.SetText(fmt.Sprintf("owlcms-firmata %s (PID: %d) has been stopped", curVersion, pid))
	currentProcess = nil
	stopBtn.Hide()
	urlLink.Hide() // Hide the URL when stopping
	checkForNewerVersion()
	downloadGroup.Show()
	versionCont.Show()
	releaseJavaLock()
}
