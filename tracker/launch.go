package tracker

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"controlpanel/shared"
	customdialog "controlpanel/tracker/dialog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gofrs/flock"
)

func configureTailLogLink(version, appDir string) {
	if tailLogLink == nil {
		return
	}

	logPath := filepath.Join(appDir, "logs", "tracker.log")
	fyne.Do(func() {
		tailLogLink.SetText(fmt.Sprintf("Tail tracker %s logs", version))
		tailLogLink.SetURL(nil)
		tailLogLink.OnTapped = func() {
			if err := shared.TailLogFile(logPath); err != nil {
				log.Printf("Failed to tail tracker logs: %v", err)
				if statusLabel != nil {
					fyne.Do(func() {
						statusLabel.SetText("Failed to tail logs")
					})
				}
			}
		}

		tailLogLink.Show()
	})
}

var (
	lockFilePath  = filepath.Join(getInstallDir(), "tracker.lock")
	pidFilePath   = filepath.Join(getInstallDir(), "tracker.pid")
	nodePID       int          // Store the Node process PID
	lock          *flock.Flock // Store the lock
	activeRuntime *shared.RuntimeMetadata
)

func runtimeMetadataPath() string {
	return filepath.Join(installDir, "tracker-run.json")
}

// RuntimeMetadataPath returns the path to the Tracker runtime metadata file.
func RuntimeMetadataPath() string {
	return runtimeMetadataPath()
}

// PIDFilePath returns the path to the tracker child process PID file.
func PIDFilePath() string {
	return pidFilePath
}

func clearRuntimeState() {
	activeRuntime = nil
	if err := shared.ClearRuntimeMetadata(runtimeMetadataPath()); err != nil {
		log.Printf("Failed to clear tracker runtime metadata: %v", err)
	}
}

type trackerLaunchParams struct {
	VersionDir      string
	ScriptToRun     string
	RequiredNodeVer string
	TargetPort      string
	Env             []string
}

// prepareTrackerLaunch resolves paths, verifies the startup script, loads the
// release environment, builds the process env slice, and handles shebang stripping.
// Callers must ensure InitEnv() has been called.
func prepareTrackerLaunch(version string) (*trackerLaunchParams, error) {
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		if err := shared.EnsureDir0755(installDir); err != nil {
			return nil, fmt.Errorf("creating tracker directory: %w", err)
		}
	}

	versionDir := filepath.Join(installDir, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("version directory not found: %s", versionDir)
	}

	mainScript := filepath.Join(versionDir, "start-with-ws.js")
	if _, err := os.Stat(mainScript); os.IsNotExist(err) {
		return nil, fmt.Errorf("start-with-ws.js not found in %s", versionDir)
	}

	if err := LoadEnvironmentForRelease(version); err != nil {
		return nil, fmt.Errorf("failed to initialize environment: %w", err)
	}

	targetPort := GetPort()

	// Handle shebang stripping on Windows
	scriptToRun := mainScript
	if shared.GetGoos() == "windows" {
		content, err := os.ReadFile(mainScript)
		if err == nil && len(content) > 2 && content[0] == '#' && content[1] == '!' {
			lines := strings.Split(string(content), "\n")
			if len(lines) > 1 && strings.HasPrefix(lines[0], "#!") {
				tempScript := filepath.Join(versionDir, "start-with-ws-temp.js")
				cleanContent := strings.Join(lines[1:], "\n")
				if err := os.WriteFile(tempScript, []byte(cleanContent), 0644); err == nil {
					scriptToRun = tempScript
					log.Printf("Created temp script without shebang: %s\n", tempScript)
				}
			}
		}
	}

	// Build environment
	env := shared.NormalizeChildProcessEnv(os.Environ())
	lv := shared.GetLauncherVersionSemver()
	env = append(env, fmt.Sprintf("TRACKER_LAUNCHER=%s", lv))
	env = append(env, fmt.Sprintf("OWLCMS_CONTROLPANEL=%s", lv))
	env = shared.UpsertEnv(env, "PORT", targetPort)
	if environment != nil {
		for _, key := range environment.Keys() {
			value, _ := environment.Get(key)
			log.Printf("   %s=%s", key, value)
			if key == "TRACKER_PORT" {
				continue
			}
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	var requiredNodeVersion string
	if environment != nil {
		if v, exists := environment.Get("NODE_VERSION"); exists {
			requiredNodeVersion = v
			log.Printf("Found NODE_VERSION requirement: %s\n", requiredNodeVersion)
		}
	}

	return &trackerLaunchParams{
		VersionDir:      versionDir,
		ScriptToRun:     scriptToRun,
		RequiredNodeVer: requiredNodeVersion,
		TargetPort:      targetPort,
		Env:             env,
	}, nil
}

// recordTrackerStart writes the PID file and runtime metadata after a successful cmd.Start().
func recordTrackerStart(pid int, version, port string, daemon bool) *shared.RuntimeMetadata {
	if err := os.WriteFile(pidFilePath, fmt.Appendf(nil, "%d\n", pid), 0644); err != nil {
		log.Printf("Failed to write PID to PID file: %v\n", err)
	} else {
		log.Printf("Wrote PID %d to PID file %s\n", pid, pidFilePath)
	}

	SaveLastRunVersion(version)

	metadata, err := shared.WriteRuntimeMetadata(runtimeMetadataPath(), pid, version, port, daemon)
	if err != nil {
		log.Printf("Failed to write tracker runtime metadata: %v", err)
		return nil
	}
	return metadata
}

// SaveLastRunVersion persists the version so that --tracker previous can find it.
func SaveLastRunVersion(version string) {
	p := filepath.Join(installDir, "last-version.txt")
	if err := os.WriteFile(p, []byte(version), 0644); err != nil {
		log.Printf("Failed to save tracker last-run version: %v", err)
	}
}

// GetLastRunVersion returns the previously launched tracker version, or empty string.
func GetLastRunVersion() string {
	p := filepath.Join(installDir, "last-version.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// LaunchDaemon starts the tracker headlessly (no UI) in daemon mode.
// It assumes Node.js is already installed locally.
func LaunchDaemon(version string) error {
	log.Printf("LaunchDaemon: starting tracker %s headlessly", version)

	if err := InitEnv(); err != nil {
		return fmt.Errorf("failed to initialize environment: %w", err)
	}

	params, err := prepareTrackerLaunch(version)
	if err != nil {
		return err
	}

	if shared.CheckPort(params.TargetPort) == nil {
		return fmt.Errorf("port %s is already in use", params.TargetPort)
	}

	nodePath, err := shared.FindLocalNodeForVersion(params.RequiredNodeVer, shared.GetGoos)
	if err != nil {
		return fmt.Errorf("node.js not found locally: %w", err)
	}

	if shared.GetGoos() != "windows" {
		os.Chmod(nodePath, 0755)
	}

	cmd := exec.Command(nodePath, params.ScriptToRun)
	shared.ConfigureNoConsoleWindow(cmd)
	shared.ConfigureDetachedDaemonProcess(cmd, true)
	cmd.Env = params.Env
	cmd.Dir = params.VersionDir

	appDir := filepath.Join(installDir, version)
	logPath := filepath.Join(appDir, "logs", "tracker.log")
	_ = os.Remove(logPath)
	_ = shared.EnsureDir0755(filepath.Dir(logPath))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("Warning: could not open tracker log: %v", err)
	} else {
		// Use the *os.File directly so Go passes the fd to the child
		// instead of creating a pipe.  A pipe would break (SIGPIPE) when
		// the control-panel parent exits in headless mode.
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	log.Printf("LaunchDaemon: command %v in %s", cmd.Args, params.VersionDir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tracker %s: %w", version, err)
	}

	pid := cmd.Process.Pid
	activeRuntime = recordTrackerStart(pid, version, params.TargetPort, true)

	log.Printf("LaunchDaemon: tracker %s (PID %d), waiting for port %s...", version, pid, params.TargetPort)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if shared.CheckPort(params.TargetPort) == nil {
			log.Printf("LaunchDaemon: tracker %s ready on port %s (PID %d)", version, params.TargetPort, pid)
			return nil
		}
		if !shared.IsProcessRunning(pid) {
			return fmt.Errorf("tracker %s (PID %d) exited before becoming ready", version, pid)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for tracker %s on port %s", version, params.TargetPort)
}

// LaunchForeground starts Tracker from the command line and blocks until it exits.
func LaunchForeground(version string) error {
	log.Printf("LaunchForeground: starting tracker %s", version)

	if err := InitEnv(); err != nil {
		return fmt.Errorf("failed to initialize environment: %w", err)
	}

	params, err := prepareTrackerLaunch(version)
	if err != nil {
		return err
	}
	if shared.CheckPort(params.TargetPort) == nil {
		log.Printf("LaunchForeground: port %s is in use, attempting to free it...", params.TargetPort)
		if err := shared.EnsurePortFree(params.TargetPort); err != nil {
			return fmt.Errorf("port %s is in use and could not be freed: %w", params.TargetPort, err)
		}
		if shared.CheckPort(params.TargetPort) == nil {
			return fmt.Errorf("port %s is still in use after cleanup", params.TargetPort)
		}
	}

	nodePath, err := shared.FindLocalNodeForVersion(params.RequiredNodeVer, shared.GetGoos)
	if err != nil {
		return fmt.Errorf("node.js not found locally: %w", err)
	}
	if shared.GetGoos() != "windows" {
		os.Chmod(nodePath, 0755)
	}

	cmd := exec.Command(nodePath, params.ScriptToRun)
	shared.ConfigureNoConsoleWindow(cmd)
	cmd.Env = params.Env
	cmd.Dir = params.VersionDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("LaunchForeground: command %v in %s", cmd.Args, params.VersionDir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tracker %s: %w", version, err)
	}

	pid := cmd.Process.Pid
	activeRuntime = recordTrackerStart(pid, version, params.TargetPort, false)
	log.Printf("LaunchForeground: tracker %s (PID %d), waiting for port %s...", version, pid, params.TargetPort)

	deadline := time.Now().Add(30 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if shared.CheckPort(params.TargetPort) == nil {
			ready = true
			break
		}
		if !shared.IsProcessRunning(pid) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if ready {
		log.Printf("LaunchForeground: tracker %s ready on port %s (PID %d)", version, params.TargetPort, pid)
		fmt.Printf("tracker %s started successfully\n", version)
	}

	waitErr := cmd.Wait()
	clearRuntimeState()
	if waitErr == nil {
		log.Printf("LaunchForeground: tracker %s exited normally", version)
		return nil
	}
	return waitErr
}

func restoreTrackerRunningUI(version, port string, pid int) {
	currentVersion = version
	stopButton.SetText(fmt.Sprintf("Stop owlcms-tracker %s", version))
	stopButton.Show()
	statusLabel.SetText(fmt.Sprintf("owlcms-tracker running (PID: %d) on port %s", pid, port))
	statusLabel.Show()
	stopContainer.Show()
	setTrackerTabModeRunning()

	url := fmt.Sprintf("http://localhost:%s", port)
	urlLink.SetURLFromString(url)
	urlLink.SetText("Open owlcms-tracker in a browser")
	urlLink.Show()

	appDir := filepath.Join(installDir, version)
	appDirLink.SetText(fmt.Sprintf("Open tracker %s directory", version))
	appDirLink.SetURL(nil)
	appDirLink.OnTapped = func() {
		shared.OpenFileExplorer(appDir)
	}
	appDirLink.Show()
	configureTailLogLink(version, appDir)
	stopContainer.Refresh()
}

func reconnectTrackerRuntime() bool {
	metadata, running := shared.CheckDaemonRunning(runtimeMetadataPath())
	if !running {
		clearRuntimeState()
		releaseTrackerLock()
		return false
	}

	activeRuntime = metadata
	restoreTrackerRunningUI(metadata.Version, metadata.Port, metadata.PID)
	return true
}

func acquireTrackerLock() (*flock.Flock, error) {
	pid, source, err := shared.ResolvePIDFromFileOrPort(pidFilePath, GetPort())
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}

	if source == "pid file" {
		log.Printf("Another instance of owlcms-tracker is already running with PID %d", pid)
		return nil, fmt.Errorf("another instance of owlcms-tracker is already running with PID %d", pid)
	}

	log.Printf("No running PID from PID file, stopping PID %d resolved from %s", pid, source)
	os.Remove(pidFilePath)
	if err := shared.StopPIDFileOrPortProcess(pidFilePath, GetPort()); err != nil {
		log.Printf("Warning: failed to stop process after stale PID cleanup: %v", err)
	}

	return nil, nil
}

func releaseTrackerLock() {
	log.Println("Released Tracker lock")
	if lock != nil {
		lock.Unlock()
		lock = nil
	}
	os.Remove(lockFilePath)
	os.Remove(pidFilePath)
}

func killLockingProcess() error {
	port := GetPort()
	if err := shared.StopPIDFileOrPortProcess(pidFilePath, port); err != nil {
		releaseTrackerLock()
		return err
	}

	releaseTrackerLock()
	log.Printf("Freed port %s\n", port)
	return nil
}

func launchTracker(version string, launchButton, stopBtn *widget.Button) error {
	currentVersion = version // Store current version

	// Acquire lock file
	var err error
	lock, err = acquireTrackerLock()
	if err != nil {
		goBackToMainScreen()
		return err
	}

	targetPort := GetPortForRelease(version)

	// Free port if already in use
	if shared.CheckPort(targetPort) == nil {
		log.Printf("Port %s is in use, attempting to free it...", targetPort)
		progressLabel := widget.NewLabel(fmt.Sprintf("Port %s is busy, stopping the process that is using it...", targetPort))
		progressDialog := dialog.NewCustom("Freeing Port", "", progressLabel, mainWindow)
		progressDialog.Show()

		go func() {
			err := shared.EnsurePortFree(targetPort)
			progressDialog.Hide()

			if err != nil {
				log.Printf("Cannot free port %s: %v", targetPort, err)
				dialog.ShowError(fmt.Errorf("cannot free port %s: %w", targetPort, err), mainWindow)
				goBackToMainScreen()
				return
			}
			if shared.CheckPort(targetPort) == nil {
				log.Printf("Port %s is still in use after cleanup", targetPort)
				dialog.ShowError(fmt.Errorf("port %s is still in use after cleanup", targetPort), mainWindow)
				goBackToMainScreen()
				return
			}

			log.Printf("Port %s successfully freed", targetPort)
			dialog.ShowInformation("Port Freed", fmt.Sprintf("Port %s has been freed. Launching tracker %s...", targetPort, version), mainWindow)
			continueTrackerLaunch(version, targetPort, launchButton, stopBtn)
		}()
		return nil
	}

	continueTrackerLaunch(version, targetPort, launchButton, stopBtn)
	return nil
}

func continueTrackerLaunch(version, targetPort string, launchButton, stopBtn *widget.Button) {
	statusLabel.SetText(fmt.Sprintf("Starting owlcms-tracker %s...", version))
	statusLabel.Refresh()
	statusLabel.Show()

	// Store current directory to restore it later
	originalDir, err := os.Getwd()
	if err != nil {
		dialog.ShowError(fmt.Errorf("getting current directory: %w", err), mainWindow)
		goBackToMainScreen()
		return
	}

	params, err := prepareTrackerLaunch(version)
	if err != nil {
		statusLabel.SetText(err.Error())
		launchButton.Show()
		goBackToMainScreen()
		return
	}
	targetPort = params.TargetPort

	if err := os.Chdir(params.VersionDir); err != nil {
		launchButton.Show()
		dialog.ShowError(fmt.Errorf("changing to version directory: %w", err), mainWindow)
		return
	}
	defer os.Chdir(originalDir)

	// Find Node.js locally, download if needed
	var nodeExe string
	nodePath, err := shared.FindLocalNodeForVersion(params.RequiredNodeVer, shared.GetGoos)
	if err != nil {
		// No suitable Node.js found, download appropriate version
		var targetVersion string
		if params.RequiredNodeVer != "" {
			log.Printf("No Node.js found meeting requirement %s, downloading...\n", params.RequiredNodeVer)
			targetVersion = params.RequiredNodeVer
		} else {
			log.Printf("No Node.js found locally, downloading latest LTS version...")
			var err error
			targetVersion, err = shared.FindLatestNodeRelease("")
			if err != nil {
				launchButton.Show()
				goBackToMainScreen()
				dialog.ShowError(fmt.Errorf("failed to find latest Node.js release: %w", err), mainWindow)
				return
			}
		}

		statusLabel.SetText("Downloading Node.js...")
		statusLabel.Refresh()

		log.Printf("Target Node.js release: %s\n", targetVersion)

		// Create a cancel channel for the download
		cancel := make(chan bool)

		// Get the active window for the dialog
		var appWindow fyne.Window
		windows := fyne.CurrentApp().Driver().AllWindows()
		if len(windows) > 0 {
			appWindow = windows[0]
		}

		// Show the progress dialog
		var progressDialog dialog.Dialog
		var progressBar *widget.ProgressBar
		if appWindow != nil {
			progressDialog, progressBar = customdialog.NewDownloadDialog(
				"Installing Node.js",
				appWindow,
				cancel)
			progressDialog.Show()
			progressBar.SetValue(0.01) // Set initial activity indicator
		}

		nodePath, err = shared.DownloadAndInstallNode(targetVersion, func(downloaded, total int64) {
			if progressBar != nil && total > 0 {
				percentage := float64(downloaded) / float64(total)
				progressBar.SetValue(percentage)
			}
		})

		if progressDialog != nil {
			progressDialog.Hide()
		}

		if err != nil {
			launchButton.Show()
			goBackToMainScreen()
			dialog.ShowError(fmt.Errorf("failed to download Node.js: %w", err), mainWindow)
			return
		}

		log.Printf("Successfully installed Node.js at: %s\n", nodePath)
		statusLabel.SetText(fmt.Sprintf("Starting owlcms-tracker %s...", version))
		statusLabel.Refresh()
	}

	nodeExe = nodePath

	if _, err := os.Stat(nodeExe); os.IsNotExist(err) {
		launchButton.Show()
		goBackToMainScreen()
		dialog.ShowError(fmt.Errorf("node executable not found at %s", nodeExe), mainWindow)
		return
	}

	// Make sure node is executable on Unix systems
	if shared.GetGoos() != "windows" {
		os.Chmod(nodeExe, 0755)
	}

	cmd := exec.Command(nodeExe, params.ScriptToRun)
	shared.ConfigureNoConsoleWindow(cmd)

	cmd.Env = params.Env
	cmd.Dir = params.VersionDir

	// Capture stdout/stderr to logs/tracker.log (remove previous file first)
	appDir := filepath.Join(installDir, version)
	logPath := filepath.Join(appDir, "logs", "tracker.log")
	_ = os.Remove(logPath)
	if err := shared.EnsureDir0755(filepath.Dir(logPath)); err != nil {
		log.Printf("Failed to create log directory: %v", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("Failed to open tracker log file %s: %v", logPath, err)
		logFile = nil
	} else {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	log.Printf("Starting owlcms-tracker %s\n", version)
	log.Printf("  Working directory: %s\n", cmd.Dir)
	log.Printf("  Command: %s %s\n", nodeExe, params.ScriptToRun)
	log.Printf("  Full command: %v\n", cmd.Args)

	if err := cmd.Start(); err != nil {
		statusLabel.SetText(fmt.Sprintf("Failed to start owlcms-tracker %s", version))
		releaseTrackerLock()
		launchButton.Show()
		goBackToMainScreen()
		log.Printf("Failed to start owlcms-tracker %s: %v\n", version, err)
		if logFile != nil {
			_ = logFile.Close()
		}
		dialog.ShowError(fmt.Errorf("failed to start owlcms-tracker %s: %w", version, err), mainWindow)
		return
	}

	// Store the PID in the PID file and globally (after Start() succeeds)
	nodePID = cmd.Process.Pid
	activeRuntime = recordTrackerStart(nodePID, version, targetPort, false)

	log.Printf("Launching owlcms-tracker %s (PID: %d), waiting for port %s...\n", version, nodePID, targetPort)
	statusLabel.SetText(fmt.Sprintf("Starting owlcms-tracker %s (PID: %d), waiting for port %s.\nFull startup can take up to 15 seconds.", version, nodePID, targetPort))
	currentProcess = cmd
	stopBtn.SetText(fmt.Sprintf("Stop owlcms-tracker %s", version))
	stopBtn.Show()
	stopContainer.Show()
	setTrackerTabModeRunning()

	appDirLink.SetText(fmt.Sprintf("Open tracker %s directory", version))
	appDirLink.SetURL(nil)
	appDirLink.OnTapped = func() {
		shared.OpenFileExplorer(appDir)
	}
	appDirLink.Show()
	configureTailLogLink(version, appDir)

	// Monitor the process in background
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	monitorChan := monitorProcess(done, targetPort)

	// Wait for monitoring result in background
	go func() {
		if err := <-monitorChan; err != nil {
			log.Printf("owlcms-tracker process %d failed to start properly: %v\n", nodePID, err)
			currentProcess = nil
			clearRuntimeState()
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("owlcms-tracker process %d failed to start properly", nodePID))
				stopBtn.Hide()
				stopContainer.Hide()
				launchButton.Show()
				setTrackerTabMode(mainWindow)
			})
			releaseTrackerLock()
			return
		}

		log.Printf("owlcms-tracker process %d is ready (port %s responding)\n", nodePID, targetPort)
		url := fmt.Sprintf("http://localhost:%s", targetPort)
		fyne.Do(func() {
			statusLabel.SetText(fmt.Sprintf("owlcms-tracker running (PID: %d) on port %s", nodePID, targetPort))
			urlLink.SetURLFromString(url)
			urlLink.SetText("Open owlcms-tracker in a browser")
			urlLink.Show()

			appDirLink.SetText(fmt.Sprintf("Open tracker %s directory", version))
			appDirLink.SetURL(nil)
			appDirLink.OnTapped = func() {
				shared.OpenFileExplorer(appDir)
			}
			appDirLink.Show()
		})
		configureTailLogLink(version, appDir)
		fyne.Do(func() {
			stopContainer.Refresh()
		})

		// Auto-open the browser when the tracker is ready
		if err := shared.OpenBrowser(url); err != nil {
			log.Printf("Failed to open browser: %v\n", err)
		}

		// Process is stable, wait for it to end
		err := <-done
		pid := cmd.Process.Pid
		if logFile != nil {
			_ = logFile.Close()
		}

		exitMessage := ""
		if killedByUs {
			// If we killed it, just report normal termination
			log.Printf("owlcms-tracker %s (PID: %d) was stopped by user\n", version, pid)
			exitMessage = fmt.Sprintf("owlcms-tracker %s (PID: %d) was stopped by user", version, pid)
		} else if err != nil {
			// Only report error if it wasn't killed by us
			log.Printf("owlcms-tracker %s (PID: %d) terminated with error: %v\n", version, pid, err)
			exitMessage = fmt.Sprintf("owlcms-tracker %s (PID: %d) terminated with error", version, pid)
		} else {
			log.Printf("owlcms-tracker %s (PID: %d) exited normally\n", version, pid)
			exitMessage = fmt.Sprintf("owlcms-tracker %s (PID: %d) exited normally", version, pid)
		}

		currentProcess = nil
		killedByUs = false // Reset flag
		clearRuntimeState()
		fyne.Do(func() {
			statusLabel.SetText(exitMessage)
			stopBtn.Hide()
			stopContainer.Hide()
			launchButton.Show()
			setTrackerTabMode(mainWindow)
			urlLink.Hide()
			if appDirLink != nil {
				appDirLink.Hide()
			}
			if tailLogLink != nil {
				tailLogLink.Hide()
			}
		})
		releaseTrackerLock()
	}()
}

func goBackToMainScreen() {
	setTrackerTabMode(fyne.CurrentApp().Driver().AllWindows()[0])
}
