package tracker

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	installDir = getInstallDir()
	// TEMPORARY TEST FLAG: when true, treat Tracker as not installed.
	// Keep variable for testing; default to false to use real detection.
	forceUninstalledTracker   = false
	currentProcess            *exec.Cmd
	currentVersion            string
	statusLabel               *widget.Label
	stopButton                *widget.Button
	versionContainer          *fyne.Container
	stopContainer             *fyne.Container
	singleOrMultiVersionLabel *widget.Label
	downloadContainer         *fyne.Container
	downloadsShown            bool
	urlLink                   *widget.Hyperlink
	appDirLink                *widget.Hyperlink
	tailLogLink               *widget.Hyperlink
	mainWindow                fyne.Window
	killedByUs                bool
	topInstallContent         *fyne.Container
	topVersionContent         *fyne.Container
	topRunContent             *fyne.Container
	topModeStack              *fyne.Container
)

// IsRunning returns true if Tracker is currently running
func IsRunning() bool {
	return currentProcess != nil || activeRuntime != nil
}

// IsLocalProcessRunning returns true when the running process should stop with this control panel.
func IsLocalProcessRunning() bool {
	return currentProcess != nil || (activeRuntime != nil && !activeRuntime.Daemon)
}

// IsRecoveredDaemonRunning returns true when the UI reattached to an existing daemon process.
func IsRecoveredDaemonRunning() bool {
	return currentProcess == nil && activeRuntime != nil && activeRuntime.Daemon
}

// OnTabSelected is called when the Tracker tab is selected.
// It refreshes the version list to update the OWLCMS version warning.
func OnTabSelected() {
	if mainWindow != nil && versionContainer != nil && len(getAllInstalledVersions()) > 0 {
		// If tracker is running, don't show the version list
		if IsRunning() {
			return
		}
		recomputeVersionList(mainWindow)
	}
}

// StopRunningProcess stops the running Tracker process
func StopRunningProcess(w fyne.Window) {
	if currentProcess != nil && currentProcess.Process != nil {
		log.Println("Stopping Tracker process")
		stopProcess(currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
		return
	}

	if activeRuntime != nil {
		log.Println("Stopping attached Tracker process")
		stopProcess(currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
	}
}

// HandleSignalCleanup handles cleanup when the application receives a signal
func HandleSignalCleanup() {
	if currentProcess != nil && currentProcess.Process != nil {
		pid := currentProcess.Process.Pid
		log.Printf("Forcefully stopping Tracker (PID: %d)...\n", pid)
		killedByUs = true
		// Use direct kill for fast cleanup
		if err := shared.ForcefullyKillPID(pid); err != nil {
			log.Printf("Failed to kill Tracker process %d: %v\n", pid, err)
		} else {
			log.Printf("Tracker process %d killed\n", pid)
		}
		currentProcess = nil
		clearRuntimeState()
		releaseTrackerLock()
		return
	}

	if activeRuntime != nil {
		killedByUs = true
		if err := shared.ForcefullyKillPID(activeRuntime.PID); err != nil {
			log.Printf("Failed to kill attached Tracker process %d: %v\n", activeRuntime.PID, err)
		}
		clearRuntimeState()
		releaseTrackerLock()
	}
}

// CreateTab creates and returns the Tracker tab content
func CreateTab(w fyne.Window) *fyne.Container {
	// Store main window reference
	mainWindow = w

	log.Println("Creating Tracker tab content")

	// Create stop button and status label
	stopButton = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), nil)
	stopButton.Importance = widget.SuccessImportance // Dark green, matching Firmata
	statusLabel = widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	// Create containers
	downloadContainer = container.NewVBox()
	versionContainer = container.NewStack()

	// Create URL hyperlink
	urlLink = widget.NewHyperlink("", nil)
	urlLink.Hide()

	// App directory + log tail links (shown when running)
	appDirLink = widget.NewHyperlink("", nil)
	appDirLink.Hide()
	tailLogLink = widget.NewHyperlink("", nil)
	tailLogLink.Hide()

	stopContainer = container.NewVBox(widget.NewSeparator(), stopButton, statusLabel, urlLink, appDirLink, tailLogLink)

	// Initialize download titles
	updateTitle = widget.NewRichTextFromMarkdown("")
	updateTitleContainer = container.NewHBox(updateTitle)
	downloadButtonTitle = widget.NewHyperlink("Click here to install additional versions.", nil)
	downloadButtonTitle.OnTapped = func() {
		if !downloadsShown {
			ShowDownloadables()
		} else {
			HideDownloadables()
		}
	}
	singleOrMultiVersionLabel = widget.NewLabel("")

	// Configure stop button behavior (confirm before stopping)
	stopButton.OnTapped = func() {
		log.Println("Stop button tapped")
		confirmDialog := dialog.NewConfirm(
			"Confirm Stop",
			"Stop the running Tracker process?",
			func(confirm bool) {
				if confirm {
					stopProcess(currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
				}
			},
			w,
		)
		confirmDialog.SetConfirmText("Stop")
		confirmDialog.SetDismissText("Cancel")
		confirmDialog.Show()
	}
	stopButton.Hide()
	stopContainer.Hide()

	// Create menu bar
	menuBar := createMenuBar(w)

	// Add small spacer under the menu bar to increase top area
	topSpacer := canvas.NewRectangle(color.Transparent)
	topSpacer.SetMinSize(fyne.NewSize(1, 8))

	topInstallContent = container.NewVBox()
	topVersionContent = container.NewVBox(menuBar, topSpacer)
	topRunContent = container.NewVBox(stopContainer)
	topModeStack = container.NewStack(topInstallContent, topVersionContent, topRunContent)

	mainContent := container.NewBorder(
		topModeStack,
		downloadContainer, // Bottom
		nil,               // Left
		nil,               // Right
		versionContainer,  // Center (expands to fill space)
	)
	statusLabel.SetText("Checking installation status...")
	statusLabel.Refresh()
	statusLabel.Show()
	stopContainer.Show()

	// If the installation directory does not exist, reset the tab to explanation mode
	if forceUninstalledTracker || func() bool { _, err := os.Stat(installDir); return os.IsNotExist(err) }() {
		resetToExplainMode()
		return mainContent
	}

	// Start initialization in a goroutine for installed case
	go initializeTab(w)

	return mainContent
}

func showInstallMode() {
	if topInstallContent != nil {
		topInstallContent.Show()
	}
	if topVersionContent != nil {
		topVersionContent.Hide()
	}
	if topRunContent != nil {
		topRunContent.Hide()
	}
	if stopContainer != nil {
		stopContainer.Hide()
	}
	if versionContainer != nil {
		versionContainer.Show()
		versionContainer.Refresh()
	}
	if downloadContainer != nil {
		downloadContainer.Hide()
		downloadContainer.Refresh()
	}
}

func showVersionListMode() {
	if topInstallContent != nil {
		topInstallContent.Hide()
	}
	if topVersionContent != nil {
		topVersionContent.Show()
	}
	if topRunContent != nil {
		topRunContent.Hide()
	}
	if stopContainer != nil {
		stopContainer.Hide()
	}
	if versionContainer != nil {
		versionContainer.Show()
		versionContainer.Refresh()
	}
	if downloadContainer != nil {
		downloadContainer.Show()
		downloadContainer.Refresh()
	}
}

func setTrackerTabModeRunning() {
	if topInstallContent != nil {
		topInstallContent.Hide()
	}
	if topVersionContent != nil {
		topVersionContent.Hide()
	}
	if topRunContent != nil {
		topRunContent.Show()
	}
	if stopContainer != nil {
		stopContainer.Show()
		stopContainer.Refresh()
	}
	if versionContainer != nil {
		versionContainer.Hide()
	}
	if downloadContainer != nil {
		downloadContainer.Hide()
		downloadContainer.Refresh()
	}
}

// createMenuBar creates the menu bar with File and Processes menus
func createMenuBar(w fyne.Window) *fyne.Container {
	// Create the File menu button with popup
	fileMenuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open Tracker Installation Directory", func() {
			if err := shared.OpenFileExplorer(installDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open installation directory: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Refresh Available Versions", func() {
			refreshAvailableVersions(w)
		}),
		fyne.NewMenuItem("Install Tracker version from ZIP", func() {
			selectLocalZip(w, func(path string, err error) {
				if err != nil {
					dialog.ShowError(fmt.Errorf("file selection failed: %w", err), w)
					return
				}
				if path == "" {
					return
				}
				ProcessLocalZipFile(path, w, installDir, updateExplanation, recomputeVersionList, checkForNewerVersion)
			})
		}),
		fyne.NewMenuItem("Save installed Tracker version as ZIP", func() {
			ZipCurrentSetup(w, installDir, getAllInstalledVersions, selectSaveZip)
		}),
		fyne.NewMenuItemSeparator(),
		// Commented out: remove all versions via Files menu (use Uninstall instead)
		// fyne.NewMenuItem("Remove All Tracker Versions", func() {
		// 	removeAllVersions()
		// }),
		fyne.NewMenuItem("Uninstall Tracker", func() {
			uninstallAll()
		}),
	}
	fileMenu := shared.CreateMenuButton("Files", fileMenuItems)

	// Create the Processes menu button with popup
	processMenuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Kill Already Running Process", func() {
			if err := killLockingProcess(); err != nil {
				dialog.ShowError(fmt.Errorf("failed to kill already running process: %w", err), w)
			} else {
				dialog.ShowInformation("Success", "Successfully killed the already running process", w)
			}
		}),
	}
	processMenu := shared.CreateMenuButton("Processes", processMenuItems)

	// Create the Options menu button with popup
	setPortItem := fyne.NewMenuItem("Port Number", func() {
		showPortNumberDialog(w)
	})
	optionsMenuItems := []*fyne.MenuItem{setPortItem}
	optionsMenu := shared.CreateMenuButton("Options", optionsMenuItems)

	// Add small vertical padding
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 5))

	menuRow := container.NewHBox(fileMenu, processMenu, optionsMenu)

	return container.NewVBox(
		spacer,
		menuRow,
	)
}

func showPortNumberDialog(w fyne.Window) {
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("8096")
	portEntry.SetText(GetPort())

	dialog.ShowForm(
		"Tracker Port Number",
		"Save",
		"Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Port Number", portEntry),
		},
		func(ok bool) {
			if !ok {
				return
			}

			newPort := strings.TrimSpace(portEntry.Text)
			if newPort == "" {
				dialog.ShowError(fmt.Errorf("port number is required"), w)
				return
			}

			portNumber, err := strconv.Atoi(newPort)
			if err != nil || portNumber < 1 || portNumber > 65535 {
				dialog.ShowError(fmt.Errorf("port number must be an integer between 1 and 65535"), w)
				return
			}

			if err := SaveProperty("TRACKER_PORT", newPort); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save tracker port: %w", err), w)
				return
			}

			dialog.ShowInformation("Port Updated", fmt.Sprintf("Tracker port set to %s. Restart the tracker to apply the new port.", newPort), w)
		},
		w,
	)
}

func refreshAvailableVersions(w fyne.Window) {
	go func() {
		releases, err := fetchReleases()
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to refresh available versions: %w", err), w)
			return
		}
		allReleases = releases

		// If the dropdown exists, repopulate it.
		if releaseDropdown != nil {
			for _, obj := range releaseDropdown.Objects {
				if selectWidget, ok := obj.(*widget.Select); ok {
					populateReleaseSelect(selectWidget)
					break
				}
			}
		}

		recomputeVersionList(w)
		checkForNewerVersion()
		if downloadContainer != nil {
			downloadContainer.Refresh()
		}
	}()
}

// initializeTab handles the async initialization of the Tracker tab
func initializeTab(w fyne.Window) {
	fyne.Do(func() {
		if reconnectTrackerRuntime() {
			log.Println("Tracker tab reattached to existing runtime")
			return
		}

		// Set the appropriate mode based on installed versions
		if len(getAllInstalledVersions()) == 0 {
			setTrackerTabModeUninstalled(w)
		} else {
			setTrackerTabModeInstalled(w)
		}
		log.Println("Tracker tab setup done.")
	})
}

// setTrackerTabModeUninstalled shows the install prompt for when no versions are installed.
func setTrackerTabModeUninstalled(_ fyne.Window) {
	showInstallMode()
	resetToExplainMode()
	log.Printf("UI Mode: Uninstalled (0 versions)")
}

// setTrackerTabModeInstalled shows the version list and download section.
func setTrackerTabModeInstalled(w fyne.Window) {
	// If tracker is running, don't switch to version list mode
	if IsRunning() {
		log.Printf("UI Mode: Running - not switching to installed mode")
		return
	}

	// Fetch releases first so update buttons can be computed
	setupReleaseDropdown(w)
	// Now recompute list with release info available
	recomputeVersionList(w)
	checkForNewerVersion()
	showVersionListMode()
	if statusLabel != nil {
		statusLabel.Hide()
	}

	log.Printf("UI Mode: Installed (versions=%d; list+download visible)", len(getAllInstalledVersions()))
}

// setTrackerTabMode is the single switch deciding which mode to show.
func setTrackerTabMode(w fyne.Window) {
	if len(getAllInstalledVersions()) == 0 {
		setTrackerTabModeUninstalled(w)
		return
	}
	setTrackerTabModeInstalled(w)
}

// computeVersionScrollHeight returns a minimum height for the version list scroll
// so it can display up to 4 rows without being too small.
func computeVersionScrollHeight(numVersions int) float32 {
	minHeight := 140 // minimum height to provide adequate vertical space
	rowHeight := 50  // approximate height per row
	return float32(minHeight + (rowHeight * min(numVersions, 4)))
}

// HideDownloadables hides the download dropdown
func HideDownloadables() {
	downloadsShown = false
	if releaseDropdown != nil {
		releaseDropdown.Hide()
	}
	if prereleaseCheckbox != nil {
		prereleaseCheckbox.Hide()
	}
	if downloadButtonTitle != nil {
		downloadButtonTitle.Show()
	}
	if downloadContainer != nil {
		downloadContainer.Refresh()
	}
}

// ShowDownloadables shows the download dropdown
func ShowDownloadables() {
	downloadsShown = true
	if len(allReleases) == 0 {
		if downloadContainer != nil {
			downloadContainer.Objects = []fyne.CanvasObject{
				widget.NewLabel("You are not connected to the Internet. Available updates cannot be shown."),
			}
			downloadContainer.Refresh()
		}
		return
	}
	if releaseDropdown != nil {
		releaseDropdown.Show()
	}
	if prereleaseCheckbox != nil {
		prereleaseCheckbox.Show()
	}
	if downloadButtonTitle != nil {
		downloadButtonTitle.Hide()
	}
	if downloadContainer != nil {
		downloadContainer.Refresh()
	}
}

// resetToExplainMode updates the tab UI to show the explanation and Install button
func resetToExplainMode() {
	if versionContainer == nil {
		return
	}
	// Clear/hide the update and download UI so leftover messages (like "You are using...")
	if updateTitleContainer != nil {
		updateTitleContainer.Objects = []fyne.CanvasObject{}
		updateTitleContainer.Hide()
		updateTitleContainer.Refresh()
	}
	if downloadContainer != nil {
		downloadContainer.Objects = []fyne.CanvasObject{}
		downloadContainer.Hide()
		downloadContainer.Refresh()
	}
	if releaseDropdown != nil {
		releaseDropdown.Hide()
	}
	if prereleaseCheckbox != nil {
		prereleaseCheckbox.Hide()
	}

	// Also hide status/stop UI
	if statusLabel != nil {
		statusLabel.SetText("")
		statusLabel.Hide()
	}
	if stopContainer != nil {
		stopContainer.Hide()
	}
	if appDirLink != nil {
		appDirLink.Hide()
	}
	if tailLogLink != nil {
		tailLogLink.Hide()
	}

	// Now show the uninstalled explanation and Install button
	shared.ShowUninstalledTabContent(versionContainer, "asset/tracker.md", func(usePrerelease bool) { InstallDefault(fyne.CurrentApp().Driver().AllWindows()[0], usePrerelease) }, nil)
	// Ensure the version container is visible after switching to explanation mode
	versionContainer.Show()
	versionContainer.Refresh()
}
