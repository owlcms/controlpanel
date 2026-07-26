package firmata

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"controlpanel/firmata/javacheck"
	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/Masterminds/semver/v3"
	"github.com/gofrs/flock"
)

func configureTailLogLink(version, appDir string) {
	if tailLogLink == nil {
		return
	}

	if !firmataSupportsStartupLog(version) {
		tailLogLink.Hide()
		return
	}

	logPath := filepath.Join(appDir, "logs", "owlcms-firmata.log")
	tailLogLink.SetText(fmt.Sprintf("Tail owlcms-firmata %s logs", version))
	tailLogLink.SetURL(nil)
	tailLogLink.OnTapped = func() {
		if err := shared.TailLogFile(logPath); err != nil {
			log.Printf("Failed to tail owlcms-firmata logs: %v", err)
			if statusLabel != nil {
				statusLabel.SetText("Failed to tail logs")
			}
		}
	}

	tailLogLink.Show()
}

var (
	lockFilePath       = filepath.Join(installDir, "java.lock")
	pidFilePath        = filepath.Join(installDir, "java.pid")
	javaPID            int          // Add a global variable to store the Java process PID
	lock               *flock.Flock // Add a global variable to store the lock
	startupLogMu       sync.Mutex
	startupLogStopCh   chan struct{}
	startupLogLastText string
	startupLogUpdating bool
)

func acquireJavaLock() (*flock.Flock, error) {
	pid, source, err := shared.ResolvePIDFromFileOrPort(pidFilePath, GetPort())
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}

	if source == "pid file" {
		log.Printf("Another instance of owlcms-firmata is already running with PID %d", pid)
		return nil, fmt.Errorf("another instance of owlcms-firmata is already running with PID %d", pid)
	}

	// Port is held by something we did not start (no PID file). Do NOT
	// silently kill it — the user might have launched it on purpose.
	// Surface the situation so the caller can ask for confirmation.
	return nil, &ExternalPortOwnerError{PID: pid, Port: GetPort()}
}

// ExternalPortOwnerError is returned by acquireJavaLock when the configured
// port is held by a process that was not started via this PID file. The
// caller should ask the user before forcefully terminating it.
type ExternalPortOwnerError struct {
	PID  int
	Port string
}

func (e *ExternalPortOwnerError) Error() string {
	return fmt.Sprintf("port %s is in use by PID %d (not started by this controlpanel)", e.Port, e.PID)
}

// ForceFreeJavaPort terminates whatever is holding the firmata port. Intended
// to be called only after the user has confirmed via a dialog.
func ForceFreeJavaPort() error {
	port := GetPort()
	os.Remove(pidFilePath)
	if err := shared.StopPIDFileOrPortProcess(pidFilePath, port); err != nil {
		return fmt.Errorf("freeing port %s: %w", port, err)
	}
	return nil
}

func releaseJavaLock() {
	log.Println("Released Java lock")
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
		releaseJavaLock()
		return err
	}

	releaseJavaLock()
	log.Printf("Freed port %s\n", port)
	return nil
}

func launchFirmata(version string, launchButton *widget.Button) error {
	currentVersion = version // Store current version

	// Acquire lock file
	var err error
	lock, err = acquireJavaLock()
	if err != nil {
		// failure to lock has already shown the error message
		goBackToMainScreen()
		return err
	}

	targetPort := GetPortForRelease(version)

	// Check if port is already in use
	if err := checkPort(targetPort); err == nil {
		statusLabel.SetText(fmt.Sprintf("Another program is running on port %s", targetPort))
		statusLabel.Refresh()
		goBackToMainScreen()
		log.Printf("Another program is running on port %s", targetPort)
		return fmt.Errorf("another program is running on port %s", targetPort)
	}

	statusLabel.SetText(fmt.Sprintf("Starting owlcms-firmata %s...", version))
	statusLabel.Refresh()
	statusLabel.Show() // Show the status label when starting Java
	// Store current directory to restore it later
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Ensure the firmata directory exists
	owlcmsDir := installDir
	if _, err := os.Stat(owlcmsDir); os.IsNotExist(err) {
		if err := shared.EnsureDir0755(owlcmsDir); err != nil {
			return fmt.Errorf("creating firmata directory: %w", err)
		}
	}

	// Look for firmata.jar in the version directory
	jarPath := filepath.Join(owlcmsDir, version, "owlcms-firmata.jar")
	if _, err := os.Stat(jarPath); os.IsNotExist(err) {
		launchButton.Show() // Show launch button again if start fails
		return fmt.Errorf("'owlcms-firmata.jar' not found in %s directory", jarPath)
	}

	// Change to version directory
	if err := os.Chdir(filepath.Join(owlcmsDir, version)); err != nil {
		launchButton.Show() // Show launch button again if start fails
		return fmt.Errorf("changing to version directory: %w", err)
	}
	defer os.Chdir(originalDir)

	if !EnsureEnvWithDialog(mainWindow) {
		statusLabel.SetText("Failed to initialize env.properties")
		launchButton.Show()
		goBackToMainScreen()
		return fmt.Errorf("failed to initialize env.properties")
	}

	// Get version-specific Temurin version
	temurinVersion := GetTemurinVersionForRelease(version)
	// find the java runtime binary
	localJava, err := javacheck.FindLocalJavaForVersion(temurinVersion)
	if err != nil {
		statusLabel.SetText(fmt.Sprintf("Failed to find local Java: %v", err))
		launchButton.Show()
		goBackToMainScreen()
		return fmt.Errorf("failed to find local Java: %w", err)
	}

	if err := LoadEnvironmentForRelease(version); err != nil {
		statusLabel.SetText(fmt.Sprintf("Failed to initialize environment: %v", err))
		launchButton.Show()
		goBackToMainScreen()
		releaseJavaLock()
		return fmt.Errorf("failed to initialize environment: %w", err)
	}
	targetPort = GetPort()

	env := shared.NormalizeChildProcessEnv(os.Environ())
	shared.PurgeJSerialCommCaches()
	newVar := shared.GetLauncherVersionSemver()
	env = append(env, fmt.Sprintf("FIRMATA_LAUNCHER=%s", newVar))
	env = append(env, fmt.Sprintf("OWLCMS_CONTROLPANEL=%s", newVar))
	log.Printf("Setting OWLCMS_CONTROLPANEL=%s for firmata process", newVar)
	// Add all properties from env.properties to the process env
	for _, key := range environment.Keys() {
		value, _ := environment.Get(key)
		log.Printf("   %s=%s", key, value)
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Force jSerialComm to use the correct native library by extracting it from
	// the jar to a deterministic location and pointing the JVM at it. This
	// bypasses jSerialComm's autodetection (which is unreliable when the JVM
	// is launched from a Go binary under WoW emulation on Windows).
	versionDir := filepath.Join(installDir, version)
	jarFullPath := filepath.Join(versionDir, "owlcms-firmata.jar")
	jSerialLibPath, jsErr := shared.ExtractJSerialCommNative(jarFullPath, versionDir)
	if jsErr != nil {
		log.Printf("Warning: could not extract jSerialComm native: %v (falling back to jSerialComm autodetection)", jsErr)
	}

	javaArgs := []string{}
	if jsErr == nil {
		javaArgs = append(javaArgs, "-DjSerialComm.library.path="+jSerialLibPath)
	}
	javaArgs = append(javaArgs, "-jar", "owlcms-firmata.jar", "--port", targetPort, "--device-configs", "./config")

	cmd := exec.Command(localJava, javaArgs...)
	shared.ConfigureNoConsoleWindow(cmd)
	cmd.Env = env

	// Remove startup.log if it exists to ensure fresh log output
	if firmataSupportsStartupLog(version) {
		versionDir := filepath.Join(installDir, version)
		startupLogPath := filepath.Join(versionDir, "logs", "startup.log")
		if err := os.Remove(startupLogPath); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: Failed to remove old startup.log: %v", err)
		}
	}

	log.Printf("Starting owlcms-firmata %s with command: %v\n", version, cmd.Args)
	if err := cmd.Start(); err != nil {
		statusLabel.SetText(fmt.Sprintf("Failed to start owlcms-firmata %s", version))
		releaseJavaLock()
		launchButton.Show() // Show launch button again if start fails
		goBackToMainScreen()
		log.Printf("Failed to start owlcms-firmata %s: %v\n", version, err)
		return fmt.Errorf("failed to start owlcms-firmata %s: %w", version, err)
	}

	// Store the PID in the PID file and globally
	javaPID = cmd.Process.Pid
	if err := os.WriteFile(pidFilePath, fmt.Appendf(nil, "%d\n", javaPID), 0644); err != nil {
		log.Printf("Failed to write PID to PID file: %v\n", err)
	} else {
		log.Printf("Wrote PID %d to PID file %s\n", javaPID, pidFilePath)
	}

	log.Printf("Launching owlcms-firmata %s (PID: %d), waiting for port %s...\n", version, javaPID, targetPort)
	statusLabel.SetText(fmt.Sprintf("Starting owlcms-firmata %s (PID: %d), waiting for port %s.\nFull startup can take up to 30 seconds.", version, javaPID, targetPort))
	currentProcess = cmd
	stopButton.SetText(fmt.Sprintf("Stop owlcms-firmata %s", version))
	stopButton.Show()
	stopContainer.Show()
	downloadContainer.Hide()
	versionContainer.Hide()
	setFirmataTabModeRunning()

	appDir := filepath.Join(installDir, version)
	appDirLink.SetText(fmt.Sprintf("Open owlcms-firmata %s directory", version))
	appDirLink.SetURL(nil)
	appDirLink.OnTapped = func() {
		shared.OpenFileExplorer(appDir)
	}
	appDirLink.Show()
	configureTailLogLink(version, appDir)

	// Start monitoring for startup.log
	go monitorStartupLog(appDir, version)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Monitor the process in background
	monitorChan := monitorProcess(done, targetPort)

	// Wait for monitoring result in background
	go func() {
		if err := <-monitorChan; err != nil {
			log.Printf("owlcms-firmata process %d failed to start properly: %v\n", javaPID, err)
			statusLabel.SetText(fmt.Sprintf("owlcms-firmata process %d failed to start properly", javaPID))
			stopButton.Hide()
			stopContainer.Hide()
			launchButton.Show()
			currentProcess = nil
			downloadContainer.Show()
			versionContainer.Show()
			showSelectionLayout()
			releaseJavaLock()
			return
		}

		log.Printf("owlcms-firmata process %d is ready (port %s responding)\n", javaPID, targetPort)
		statusLabel.SetText(fmt.Sprintf("owlcms-firmata running (PID: %d) on port %s", javaPID, targetPort))
		url := fmt.Sprintf("http://localhost:%s", targetPort)
		urlLink.SetURLFromString(url)
		urlLink.SetText("Open owlcms-firmata in a browser")
		urlLink.Show()

		// Close the startup log area now that firmata is ready
		hideStartupLogArea()

		appDir := filepath.Join(installDir, version)
		appDirLink.SetText(fmt.Sprintf("Open owlcms-firmata %s directory", version))
		appDirLink.SetURL(nil)
		appDirLink.OnTapped = func() {
			shared.OpenFileExplorer(appDir)
		}
		appDirLink.Show()
		configureTailLogLink(version, appDir)

		// Process is stable, wait for it to end
		err := <-done
		pid := cmd.Process.Pid

		if killedByUs {
			// If we killed it, just report normal termination
			log.Printf("owlcms-firmata %s (PID: %d) was stopped by user\n", version, pid)
			statusLabel.SetText(fmt.Sprintf("owlcms-firmata %s (PID: %d) was stopped by user", version, pid))
		} else if err != nil {
			// Only report error if it wasn't killed by us
			log.Printf("owlcms-firmata %s (PID: %d) terminated with error: %v\n", version, pid, err)
			statusLabel.SetText(fmt.Sprintf("owlcms-firmata %s (PID: %d) terminated with error", version, pid))
		} else {
			log.Printf("owlcms-firmata %s (PID: %d) exited normally\n", version, pid)
			statusLabel.SetText(fmt.Sprintf("owlcms-firmata %s (PID: %d) exited normally", version, pid))
		}

		currentProcess = nil
		killedByUs = false // Reset flag
		stopButton.Hide()
		stopContainer.Hide()
		launchButton.Show()
		downloadContainer.Show()
		versionContainer.Show()
		showSelectionLayout()
		urlLink.Hide()
		if appDirLink != nil {
			appDirLink.Hide()
		}
		if tailLogLink != nil {
			tailLogLink.Hide()
		}
		hideStartupLogArea()
		releaseJavaLock()
	}()

	return nil
}

// showStartupLogArea creates and shows the startup log text area
func showStartupLogArea() {
	// Reset/initialize stop channel for this run
	startupLogMu.Lock()
	if startupLogStopCh != nil {
		select {
		case <-startupLogStopCh:
		default:
			close(startupLogStopCh)
		}
	}
	startupLogStopCh = make(chan struct{})
	startupLogMu.Unlock()

	if startupLogText == nil {
		startupLogText = widget.NewMultiLineEntry()
		startupLogText.SetPlaceHolder("Waiting for startup log...")
		startupLogText.Wrapping = fyne.TextWrapWord
		startupLogText.OnChanged = func(s string) {
			// Keep it non-editable while remaining visually enabled.
			if startupLogUpdating {
				return
			}
			if s != startupLogLastText {
				startupLogUpdating = true
				startupLogText.SetText(startupLogLastText)
				startupLogUpdating = false
			}
		}
	}

	if startupLogContainer == nil {
		// Use a Border layout so the text area expands to fill remaining space.
		// VBox would size children to MinSize and leave unused space below.
		header := container.NewVBox(
			widget.NewSeparator(),
			widget.NewLabel("Startup Log:"),
		)
		scroller := container.NewScroll(startupLogText)
		startupLogContainer = container.NewBorder(header, nil, nil, nil, scroller)
	}

	// Show within the running layout center so it can expand.
	if startupLogHost != nil {
		log.Printf("showStartupLogArea: Showing startup log container")
		startupLogHost.Objects = []fyne.CanvasObject{startupLogContainer}
		startupLogHost.Show()
		startupLogHost.Refresh()
	} else {
		log.Printf("showStartupLogArea: WARNING - startupLogHost is nil!")
	}

	// Auto-close after 60 seconds unless firmata becomes ready first.
	startupLogMu.Lock()
	stopCh := startupLogStopCh
	startupLogMu.Unlock()
	go func(stopCh chan struct{}) {
		select {
		case <-time.After(60 * time.Second):
			hideStartupLogArea()
		case <-stopCh:
			return
		}
	}(stopCh)
}

// setStartupLogText sets the complete text in the startup log display
func setStartupLogText(text string) {
	if startupLogText != nil {
		startupLogUpdating = true
		startupLogLastText = text
		startupLogText.SetText(text)
		// Scroll to bottom
		startupLogText.CursorRow = len(strings.Split(startupLogLastText, "\n")) - 1
		startupLogUpdating = false
	}
}

// hideStartupLogArea removes the startup log display
func hideStartupLogArea() {
	// Signal any running tail goroutine to stop
	startupLogMu.Lock()
	if startupLogStopCh != nil {
		select {
		case <-startupLogStopCh:
		default:
			close(startupLogStopCh)
		}
		startupLogStopCh = nil
	}
	startupLogMu.Unlock()

	// Hide and clear the UI
	if startupLogHost != nil {
		startupLogHost.Objects = nil
		startupLogHost.Hide()
		startupLogHost.Refresh()
	}
}

func firmataSupportsStartupLog(version string) bool {
	// Normalize version for comparison (handle -SNAPSHOT)
	normalized := strings.ReplaceAll(version, "-SNAPSHOT", "-snapshot")
	v, err := semver.NewVersion(normalized)
	if err != nil {
		log.Printf("Invalid firmata version for semver comparison (%q): %v", version, err)
		return false
	}

	// Firmata versions >= 2.0.0 support startup.log
	min, err := semver.NewVersion("2.0.0")
	if err != nil {
		log.Printf("Internal error parsing minimum startup.log version: %v", err)
		return false
	}

	return !v.LessThan(min)
}

func monitorStartupLog(appDir, version string) {
	if !firmataSupportsStartupLog(version) {
		// Older firmata versions don't generate logs/startup.log
		showStartupLogArea()
		setStartupLogText(fmt.Sprintf(
			"owlcms-firmata %s does not generate logs/startup.log.\nWaiting for owlcms-firmata to respond on port %s...\n",
			version,
			GetPort(),
		))
		return
	}

	startupLogPath := filepath.Join(appDir, "logs", "startup.log")
	log.Printf("Monitoring for startup.log at: %s\n", startupLogPath)

	// Show the startup log area immediately
	showStartupLogArea()
	setStartupLogText("Waiting for startup.log...\n")

	// Real file monitoring
	// Check every 500ms for up to 60 seconds
	for range 120 {
		if _, err := os.Stat(startupLogPath); err == nil {
			log.Printf("Found startup.log, starting to tail it\n")
			// Tail the file for 60 seconds
			go tailStartupLog(startupLogPath)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("startup.log not found after monitoring period\n")
	setStartupLogText("startup.log not found after monitoring period\n")
}

func tailStartupLog(logPath string) {
	log.Printf("Tailing startup log: %s\n", logPath)
	startupLogMu.Lock()
	stopCh := startupLogStopCh
	startupLogMu.Unlock()

	file, err := os.Open(logPath)
	if err != nil {
		log.Printf("Failed to open startup.log: %v\n", err)
		return
	}
	defer file.Close()

	// Read initial content
	content, err := os.ReadFile(logPath)
	if err == nil {
		setStartupLogText(string(content))
	}

	// Monitor for changes for 60 seconds (or until firmata becomes ready)
	startTime := time.Now()
	lastSize := int64(0)
	if stat, err := file.Stat(); err == nil {
		lastSize = stat.Size()
	}

	foundReady := false
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		if time.Since(startTime) > 60*time.Second {
			log.Println("Stopping startup log tail after 60 seconds")
			break
		}

		// Check for new content
		if stat, err := os.Stat(logPath); err == nil {
			if stat.Size() > lastSize {
				// Read the entire file again
				content, err := os.ReadFile(logPath)
				if err == nil {
					contentStr := string(content)
					setStartupLogText(contentStr)
					lastSize = stat.Size()

					// Check if firmata is ready
					if !foundReady && strings.Contains(contentStr, "Firmata Ready.") {
						foundReady = true
						log.Println("Found 'Firmata Ready.' in startup log")
					}
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	// If we didn't find "Firmata Ready." after timeout, show error dialog
	if !foundReady {
		logsDir := filepath.Dir(logPath)
		go func() {
			dialog.ShowCustomConfirm("owlcms-firmata Startup Issue", "Open Logs Folder", "Close",
				widget.NewLabel("owlcms-firmata did not report ready status within 60 seconds.\nPlease check the logs and send them to owlcms@jflamy.dev if the issue persists."),
				func(ok bool) {
					if ok {
						shared.OpenFileExplorer(logsDir)
					}
				}, mainWindow)
		}()
		// Keep the log display visible so user can see what went wrong
	} else {
		// Close the startup log display only if startup was successful
		hideStartupLogArea()
	}
}
