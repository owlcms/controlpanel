package replays

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

var (
	camerasPIDFile = filepath.Join(getInstallDir(), "cameras.pid")
	replaysPIDFile = filepath.Join(getInstallDir(), "replays.pid")
)

// replaysExeName returns the platform-specific binary name for replays
func replaysExeName() string {
	switch shared.GetGoos() {
	case "windows":
		return "replays_windows.exe"
	case "darwin":
		if shared.GetGoarch() == "amd64" {
			return "replays_darwin_amd64"
		}
		return "replays_darwin_arm64"
	case "linux":
		if shared.GetGoarch() == "arm64" {
			return "replays_linux_arm64"
		}
		return "replays_linux_amd64"
	default:
		return "replays_linux_amd64"
	}
}

// launchReplays starts the replays binary. It touches UI widgets directly and
// must be called on the Fyne main goroutine. FFmpeg availability is ensured by
// the caller, off the main goroutine.
func launchReplays(version string, _ *widget.Button, _ fyne.Window) error {
	versionDir := filepath.Join(installDir, version)
	configDir := versionDir
	exePath := filepath.Join(versionDir, replaysExeName())
	targetPort := getPortForRelease(version)

	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("replays binary not found: %s", exePath)
	}

	// Make executable on Linux
	if shared.GetGoos() != "windows" {
		if err := os.Chmod(exePath, 0755); err != nil {
			log.Printf("Warning: could not chmod replays binary: %v", err)
		}
	}

	if shared.ShouldRunVideoExtract(versionDir, "replays") {
		log.Printf("Running replays extract bootstrap for %s", versionDir)
		if err := shared.RunVideoExtractBootstrap(exePath, versionDir); err != nil {
			return err
		}
	}

	cmd := exec.Command(exePath, "--configDir", configDir)
	cmd.Dir = versionDir

	logPath := filepath.Join(versionDir, "logs", "replays.log")
	if err := shared.ResetLogFile(logPath); err != nil {
		return fmt.Errorf("failed to reset replays log: %w", err)
	}

	cmd.Env = shared.BuildVideoLaunchEnv(versionDir)

	if targetPort != "" && shared.CheckPort(targetPort) == nil {
		log.Printf("Replays port %s is in use, attempting to free it...", targetPort)
		if err := shared.StopPIDFileOrPortProcess(replaysPIDFile, targetPort); err != nil {
			return fmt.Errorf("failed to free Replays port %s: %w", targetPort, err)
		}
		if shared.CheckPort(targetPort) == nil {
			return fmt.Errorf("replays port %s is still in use after cleanup", targetPort)
		}
	}

	log.Printf("Starting replays %s: %s", version, exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start replays %s: %w", version, err)
	}

	replaysProcess = cmd
	replaysVersion = version

	pid := cmd.Process.Pid
	if err := os.WriteFile(replaysPIDFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		log.Printf("Failed to write replays PID file: %v", err)
	}

	if statusLabel != nil {
		statusLabel.SetText(fmt.Sprintf("Replays %s running (PID: %d)", version, pid))
	}
	replaysStopButton.SetText(fmt.Sprintf("Stop Replays %s", version))
	replaysStopButton.Show()
	updateStopContainer()
	setVideoTabModeRunning()

	if appDirLink != nil {
		appDirLink.Hide()
	}
	configureReplaysRunLinks(version, versionDir)

	go func() {
		err := cmd.Wait()
		pid := cmd.Process.Pid

		if killedByUs {
			log.Printf("Replays %s (PID: %d) stopped by user\n", version, pid)
		} else if err != nil {
			log.Printf("Replays %s (PID: %d) exited with error: %v\n", version, pid, err)
		} else {
			log.Printf("Replays %s (PID: %d) exited normally\n", version, pid)
		}

		replaysProcess = nil
		killedByUs = false
		os.Remove(replaysPIDFile)
		fyne.Do(func() {
			replaysStopButton.Hide()
			updateStopContainer()

			if camerasProcess == nil {
				// Both stopped — restore the full UI
				if statusLabel != nil {
					statusLabel.SetText("")
				}
				setVideoTabMode(mainWindow)
				hideAllRunLinks()
				checkForNewerVersion()
			} else {
				if statusLabel != nil {
					statusLabel.SetText(fmt.Sprintf("Cameras %s running (PID: %d)", camerasVersion, camerasProcess.Process.Pid))
				}
			}
		})
	}()

	return nil
}

func configureReplaysRunLinks(version, versionDir string) {
	if replaysDirLink != nil {
		replaysDirLink.SetText(fmt.Sprintf("Open Replays %s configuration directory", version))
		replaysDirLink.SetURL(nil)
		replaysDirLink.OnTapped = func() {
			if err := shared.OpenFileExplorer(versionDir); err != nil && statusLabel != nil {
				statusLabel.SetText(fmt.Sprintf("Failed to open Replays directory: %v", err))
			}
		}
		replaysDirLink.Show()
	}

	if replaysLogLink != nil {
		logPath := filepath.Join(versionDir, "logs", "replays.log")
		replaysLogLink.SetText(fmt.Sprintf("Tail replays %s logs", version))
		replaysLogLink.SetURL(nil)
		replaysLogLink.OnTapped = func() {
			if err := shared.TailLogFile(logPath); err != nil && statusLabel != nil {
				statusLabel.SetText(fmt.Sprintf("Failed to tail replays logs: %v", err))
			}
		}
		replaysLogLink.Show()
	}
}

func hideAllRunLinks() {
	if appDirLink != nil {
		appDirLink.Hide()
	}
	if camerasDirLink != nil {
		camerasDirLink.Hide()
	}
	if replaysDirLink != nil {
		replaysDirLink.Hide()
	}
	if camerasLogLink != nil {
		camerasLogLink.Hide()
	}
	if replaysLogLink != nil {
		replaysLogLink.Hide()
	}
}
