package video

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func stopVideoProcess(curProcess *exec.Cmd, curVersion string, stopBtn *widget.Button, w fyne.Window) {
	log.Printf("Stopping video %s...\n", curVersion)
	if statusLabel != nil {
		statusLabel.SetText(fmt.Sprintf("Stopping video %s...", curVersion))
	}
	port := getPortForRelease(curVersion)
	if port == "" {
		port = runtimeVideoPort()
	}
	killedByUs = true

	var err error
	if curProcess != nil && curProcess.Process != nil {
		pid := curProcess.Process.Pid
		log.Printf("Stopping owned video process with Go process handle (PID: %d)", pid)
		err = shared.StopOwnedProcess(curProcess, 10*time.Second)
	} else {
		err = shared.StopPIDFileOrPortProcess(videoPIDFile, port)
	}
	if err != nil {
		killedByUs = false
		dialog.ShowError(fmt.Errorf("failed to stop video %s: %w", curVersion, err), w)
		return
	}

	log.Printf("Video %s stopped\n", curVersion)
	if statusLabel != nil {
		statusLabel.SetText(fmt.Sprintf("Video %s stopped", curVersion))
	}
	videoProcess = nil
	os.Remove(videoPIDFile)
	if stopBtn != nil {
		stopBtn.Hide()
	}
	updateStopContainer()
	checkForNewerVersion()
	downloadContainer.Show()
	versionContainer.Show()
	hideAllRunLinks()
}

func killLockingProcess() error {
	if err := shared.StopPIDFileOrPortProcess(videoPIDFile, runtimeVideoPort()); err != nil {
		return err
	}
	os.Remove(videoPIDFile)
	return nil
}

// updateStopContainer refreshes the stop container visibility based on what's running
func updateStopContainer() {
	if stopContainer == nil {
		return
	}
	if videoProcess == nil {
		stopContainer.Hide()
	} else {
		stopContainer.Show()
	}
	stopContainer.Refresh()
}
