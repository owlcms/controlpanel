package video

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
	videoPIDFile = filepath.Join(getInstallDir(), "video.pid")
)

// videoExeName returns the platform-specific binary name for video
func videoExeName() string {
	switch shared.GetGoos() {
	case "windows":
		return "video_windows.exe"
	case "darwin":
		if shared.GetGoarch() == "amd64" {
			return "video_darwin_amd64"
		}
		return "video_darwin_arm64"
	case "linux":
		if shared.GetGoarch() == "arm64" {
			return "video_linux_arm64"
		}
		return "video_linux_amd64"
	default:
		return "video_linux_amd64"
	}
}

// launchVideo starts the video binary. It touches UI widgets directly and
// must be called on the Fyne main goroutine. FFmpeg availability is ensured by
// the caller, off the main goroutine.
func launchVideo(version string, _ *widget.Button, _ fyne.Window) error {
	versionDir := filepath.Join(installDir, version)
	configDir := versionDir
	exePath := filepath.Join(versionDir, videoExeName())
	targetPort := getPortForRelease(version)

	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("video binary not found: %s", exePath)
	}

	// Make executable on Linux
	if shared.GetGoos() != "windows" {
		if err := os.Chmod(exePath, 0755); err != nil {
			log.Printf("Warning: could not chmod video binary: %v", err)
		}
	}

	if shared.ShouldRunVideoExtract(versionDir, "video") {
		log.Printf("Running video extract bootstrap for %s", versionDir)
		if err := shared.RunVideoExtractBootstrap(exePath, versionDir); err != nil {
			return err
		}
	}

	cmd := exec.Command(exePath, "--configDir", configDir)
	cmd.Dir = versionDir

	logPath := filepath.Join(versionDir, "logs", "video.log")
	if err := shared.ResetLogFile(logPath); err != nil {
		return fmt.Errorf("failed to reset video log: %w", err)
	}

	cmd.Env = shared.BuildVideoLaunchEnv(versionDir)

	if targetPort != "" && shared.CheckPort(targetPort) == nil {
		log.Printf("Video port %s is in use, attempting to free it...", targetPort)
		if err := shared.StopPIDFileOrPortProcess(videoPIDFile, targetPort); err != nil {
			return fmt.Errorf("failed to free Video port %s: %w", targetPort, err)
		}
		if shared.CheckPort(targetPort) == nil {
			return fmt.Errorf("video port %s is still in use after cleanup", targetPort)
		}
	}

	log.Printf("Starting video %s: %s", version, exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start video %s: %w", version, err)
	}

	videoProcess = cmd
	videoVersion = version

	pid := cmd.Process.Pid
	if err := os.WriteFile(videoPIDFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		log.Printf("Failed to write video PID file: %v", err)
	}

	if statusLabel != nil {
		statusLabel.SetText(fmt.Sprintf("Video %s running (PID: %d)", version, pid))
	}
	videoStopButton.SetText(fmt.Sprintf("Stop Video %s", version))
	videoStopButton.Show()
	updateStopContainer()
	setVideoTabModeRunning()

	if appDirLink != nil {
		appDirLink.Hide()
	}
	configureVideoRunLinks(version, versionDir)

	go func() {
		err := cmd.Wait()
		pid := cmd.Process.Pid

		if killedByUs {
			log.Printf("Video %s (PID: %d) stopped by user\n", version, pid)
		} else if err != nil {
			log.Printf("Video %s (PID: %d) exited with error: %v\n", version, pid, err)
		} else {
			log.Printf("Video %s (PID: %d) exited normally\n", version, pid)
		}

		videoProcess = nil
		killedByUs = false
		os.Remove(videoPIDFile)
		fyne.Do(func() {
			videoStopButton.Hide()
			updateStopContainer()

			if statusLabel != nil {
				statusLabel.SetText("")
			}
			setVideoTabMode(mainWindow)
			hideAllRunLinks()
			checkForNewerVersion()
		})
	}()

	return nil
}

func configureVideoRunLinks(version, versionDir string) {
	if videoDirLink != nil {
		videoDirLink.SetText(fmt.Sprintf("Open Video %s configuration directory", version))
		videoDirLink.SetURL(nil)
		videoDirLink.OnTapped = func() {
			if err := shared.OpenFileExplorer(versionDir); err != nil && statusLabel != nil {
				statusLabel.SetText(fmt.Sprintf("Failed to open Video directory: %v", err))
			}
		}
		videoDirLink.Show()
	}

	if videoLogLink != nil {
		logPath := filepath.Join(versionDir, "logs", "video.log")
		videoLogLink.SetText(fmt.Sprintf("Tail video %s logs", version))
		videoLogLink.SetURL(nil)
		videoLogLink.OnTapped = func() {
			if err := shared.TailLogFile(logPath); err != nil && statusLabel != nil {
				statusLabel.SetText(fmt.Sprintf("Failed to tail video logs: %v", err))
			}
		}
		videoLogLink.Show()
	}
}

func hideAllRunLinks() {
	if appDirLink != nil {
		appDirLink.Hide()
	}
	if videoDirLink != nil {
		videoDirLink.Hide()
	}
	if videoLogLink != nil {
		videoLogLink.Hide()
	}
}
