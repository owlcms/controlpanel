package owlcms

import (
	"fmt"
	"image/color"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"controlpanel/owlcms/installutils"
	"controlpanel/owlcms/javacheck"
	"controlpanel/shared"
	"controlpanel/tracker"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	statusLabel      *widget.Label
	statusRow        *fyne.Container
	stopButton       *widget.Button
	versionContainer *fyne.Container
	stopContainer    *fyne.Container
	// TEMPORARY TEST FLAG: when true, treat OWLCMS as not installed.
	// Keep variable for testing; default to false to use real detection.
	forceUninstalledOwlcms    = false
	singleOrMultiVersionLabel *widget.Label
	downloadContainer         *fyne.Container
	downloadsShown            bool
	urlLink                   *widget.Hyperlink
	trackerConnectionLabel    *widget.Label
	trackerURLLink            *widget.Hyperlink
	trackerConnectionRow      *fyne.Container
	appDirLink                *widget.Hyperlink
	tailLogLink               *widget.Hyperlink
	startupLogText            *widget.Entry
	startupLogContainer       *fyne.Container
	startupLogHost            *fyne.Container
	selectionContent          *fyne.Container
	runningContent            *fyne.Container
	modeStack                 *fyne.Container
	topInstallContent         *fyne.Container
	topVersionContent         *fyne.Container
	topRunContent             *fyne.Container
	topModeStack              *fyne.Container
	mainWindow                fyne.Window
)

// CreateTab creates and returns the OWLCMS tab content
// This should be called from the main application after the window is created
func CreateTab(w fyne.Window, _ fyne.App) *fyne.Container {
	// Store main window reference
	mainWindow = w

	log.Println("Creating OWLCMS tab content")

	// Initialize environment early
	if err := InitEnv(); err != nil {
		log.Printf("Failed to initialize environment: %v", err)
		dialog.ShowError(fmt.Errorf("failed to initialize environment: %w", err), w)
	}

	// Create stop button and status label
	stopButton = widget.NewButton("Stop", nil)
	stopButton.Importance = widget.HighImportance
	statusLabel = widget.NewLabel("Initializing OWLCMS Control Panel...")
	statusLabel.Wrapping = fyne.TextWrapOff

	// Create URL hyperlink
	urlLink = widget.NewHyperlink("", nil)
	urlLink.Hide()
	statusRow = container.NewHBox(statusLabel, urlLink)
	trackerConnectionLabel = widget.NewLabel("")
	trackerURLLink = widget.NewHyperlink("", nil)
	trackerConnectionRow = container.NewHBox(trackerConnectionLabel, trackerURLLink)
	trackerConnectionRow.Hide()

	// Create application directory hyperlink
	appDirLink = widget.NewHyperlink("", nil)
	appDirLink.Hide()

	// Create Tail logs hyperlink (only shown when running on Windows)
	tailLogLink = widget.NewHyperlink("", nil)
	tailLogLink.Hide()

	// Initialize containers
	downloadContainer = container.NewVBox()
	versionContainer = container.NewStack()
	stopContainer = container.NewVBox(widget.NewSeparator(), stopButton, statusRow, trackerConnectionRow, appDirLink, tailLogLink)

	// Initialize download titles
	updateTitle = widget.NewRichTextFromMarkdown("")

	// Initialize release notes hyperlink
	releaseNotesLink = widget.NewHyperlink("Release Notes", nil)
	releaseNotesLink.Hide()

	// Initialize install hyperlink for available version
	installAvailableLink = widget.NewHyperlink("Install as new version", nil)
	installAvailableLink.OnTapped = func() {
		if availableVersion != "" {
			confirmAndDownloadVersion(availableVersion, mainWindow)
		}
	}
	installAvailableLink.Hide()

	// Create container to hold update title and hyperlinks
	updateTitleContainer = container.NewHBox(updateTitle, releaseNotesLink, installAvailableLink)

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
		if stopInProgress.Load() {
			log.Println("Stop button tapped while OWLCMS stop is already in progress")
			return
		}
		log.Println("Stop button tapped")
		stopButton.Disable()
		confirmDialog := dialog.NewConfirm(
			"Confirm Stop",
			"Stopping OWLCMS will stop the current competition on all platforms. Make sure this is a correct time to stop.",
			func(confirm bool) {
				if confirm {
					stopProcess(currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
				} else {
					stopButton.Enable()
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
	installTopSpacer := canvas.NewRectangle(color.Transparent)
	installTopSpacer.SetMinSize(fyne.NewSize(1, 8))

	// Two different layouts:
	// - Selection mode: version list (center) + download section (bottom)
	// - Running mode: startup log host (center), no bottom section
	startupLogHost = container.NewStack()
	startupLogHost.Hide()

	selectionContent = container.NewBorder(
		nil,
		downloadContainer,
		nil,
		nil,
		versionContainer,
	)

	runningContent = container.NewStack(startupLogHost)
	runningContent.Hide()

	modeStack = container.NewStack(selectionContent, runningContent)

	topInstallContent = container.NewVBox(createInstallMenuBar(w), installTopSpacer)
	topVersionContent = container.NewVBox(menuBar, topSpacer)
	topRunContent = container.NewVBox(stopContainer)
	topModeStack = container.NewStack(topInstallContent, topVersionContent, topRunContent)

	mainContent := container.NewBorder(
		topModeStack,
		nil,       // Bottom (handled by selectionContent)
		nil,       // Left
		nil,       // Right
		modeStack, // Center switches between selection/running layouts
	)

	// If OWLCMS install directory does not exist, reset tab to explanation mode
	if forceUninstalledOwlcms || func() bool { _, err := os.Stat(installDir); return os.IsNotExist(err) }() {
		setOwlcmsTabModeUninstalled(w)
		return mainContent
	} else {
		// Start initialization in a goroutine
		go initializeOwlcmsTab(w)
	}

	return mainContent
}

func showSelectionLayout() {
	if selectionContent != nil {
		selectionContent.Show()
	}
	if runningContent != nil {
		runningContent.Hide()
	}
	if startupLogHost != nil {
		startupLogHost.Hide()
	}
}

func showRunningLayout() {
	if selectionContent != nil {
		selectionContent.Hide()
	}
	if runningContent != nil {
		runningContent.Show()
	}
}

func showInstallMode() {
	showSelectionLayout()
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
	showSelectionLayout()
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

// setOwlcmsTabModeRunning switches the tab into the running layout (no version picker).
func setOwlcmsTabModeRunning() {
	showRunningLayout()
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
	}
}

func createOptionsMenu(w fyne.Window) *widget.Button {
	trackerToggleItem := fyne.NewMenuItem("Local Tracker Connection", func() {
		showTrackerConnectionDialog(w)
	})

	setPortItem := fyne.NewMenuItem("Default Host Name and Port Number", func() {
		showPortNumberDialog(w)
	})
	trackerToggleItem.Label = "Default Tracker Connection"

	return shared.CreateMenuButton("Options", []*fyne.MenuItem{setPortItem, trackerToggleItem})
}

func installVersionFromZip(w fyne.Window) {
	selectLocalZip(w, func(path string, err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("file selection failed: %w", err), w)
			return
		}
		if path == "" {
			return
		}
		installutils.ProcessLocalZipFile(path, w, installDir, copyFile, updateExplanation, recomputeVersionList, checkForNewerVersion)
	})
}

func createInstallMenuBar(w fyne.Window) *fyne.Container {
	fileMenu := shared.CreateMenuButton("Files", []*fyne.MenuItem{
		fyne.NewMenuItem("Install OWLCMS version from ZIP", func() {
			installVersionFromZip(w)
		}),
	})

	return container.NewHBox(fileMenu, createOptionsMenu(w))
}

// createMenuBar creates the menu bar with File, Processes, and Options menus
func createMenuBar(w fyne.Window) *fyne.Container {
	// Create the File menu button with popup
	fileMenuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open OWLCMS Installation Directory", func() {
			if err := shared.OpenFileExplorer(installDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open installation directory: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Refresh Available Versions", func() {
			refreshAvailableVersions(w)
		}),
		fyne.NewMenuItem("Install OWLCMS version from ZIP", func() {
			installVersionFromZip(w)
		}),
		fyne.NewMenuItem("Save installed OWLCMS version as ZIP", func() {
			installutils.ZipCurrentSetup(w, installDir, getAllInstalledVersions, selectSaveZip)
		}),
		fyne.NewMenuItemSeparator(),
		// Commented out: remove all versions via Files menu (use Uninstall instead)
		// fyne.NewMenuItem("Remove All OWLCMS Versions", func() {
		// 	removeAllVersions()
		// }),
		// Commented out: remove bundled Java via Files menu
		// fyne.NewMenuItem("Remove OWLCMS Java", func() {
		// 	removeJava()
		// }),
		fyne.NewMenuItem("Uninstall OWLCMS", func() {
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

	optionsMenu := createOptionsMenu(w)

	// Add small vertical padding
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 5))

	return container.NewVBox(
		spacer,
		container.NewHBox(fileMenu, processMenu, optionsMenu),
	)
}

func defaultTrackerPort() string {
	return tracker.GetPort()
}

func showTrackerConnectionDialog(w fyne.Window) {
	trackerURL, trackerPort, enabled := GetTrackerConnectionSettings()
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder(defaultTrackerConnectionURL)
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder(defaultTrackerPort())
	urlEntry.SetText(trackerURL)
	portEntry.SetText(trackerPort)
	enabledCheck := widget.NewCheck("Enable tracker connection by default", nil)
	enabledCheck.SetChecked(enabled)
	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	updateStatusText := func() {
		if !enabledCheck.Checked {
			statusLabel.SetText(fmt.Sprintf("New OWLCMS versions will not connect by default. Versions can use %s on port %s individually.", urlEntry.Text, portEntry.Text))
		} else {
			statusLabel.SetText(fmt.Sprintf("New OWLCMS versions will connect to Tracker at %s on port %s.", urlEntry.Text, portEntry.Text))
		}
	}
	enabledCheck.OnChanged = func(_ bool) {
		updateStatusText()
	}
	urlEntry.OnChanged = func(_ string) {
		updateStatusText()
	}
	portEntry.OnChanged = func(_ string) {
		updateStatusText()
	}
	updateStatusText()

	content := container.NewVBox(
		enabledCheck,
		widget.NewForm(
			widget.NewFormItem("URL", container.NewGridWrap(fyne.NewSize(300, urlEntry.MinSize().Height), urlEntry)),
			widget.NewFormItem("Port Number", portEntry),
		),
		statusLabel,
	)

	d := dialog.NewCustomConfirm(
		"Default Tracker Connection",
		"Save",
		"Cancel",
		content,
		func(ok bool) {
			if !ok {
				return
			}

			baseURL := strings.TrimSpace(urlEntry.Text)
			port := strings.TrimSpace(portEntry.Text)
			if port != "" {
				portNumber, err := strconv.Atoi(port)
				if err != nil || portNumber < 1 || portNumber > 65535 {
					dialog.ShowError(fmt.Errorf("tracker connection port must be an integer between 1 and 65535"), w)
					return
				}
			}
			if _, err := url.Parse(baseURL); err != nil {
				dialog.ShowError(fmt.Errorf("tracker connection URL is invalid"), w)
				return
			}
			if err := SaveDefaultTrackerConnection(baseURL, port, enabledCheck.Checked); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save default tracker connection: %w", err), w)
				return
			}
			dialog.ShowInformation("Default Tracker Connection", "Default Tracker Connection updated. Restart OWLCMS to apply the change.", w)
		},
		w,
	)
	d.Show()
}

func showTrackerConnectionDialogForVersion(w fyne.Window, version string) {
	if len(tracker.GetAllInstalledVersions()) == 0 {
		dialog.ShowInformation("No Tracker Installed", "Install a Tracker version first (on the Tracker tab), then configure the connection.", w)
		return
	}

	defaultURL, defaultPort, _ := GetTrackerConnectionSettings()
	urlEntry := widget.NewEntry()
	urlEntry.SetText(defaultURL)
	portEntry := widget.NewEntry()
	portEntry.SetText(defaultPort)
	enabledCheck := widget.NewCheck("Enable Tracker connection", nil)
	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	updateStatusText := func() {
		if enabledCheck.Checked {
			statusLabel.SetText(fmt.Sprintf("Version %s will connect to Tracker at %s on port %s.", version, urlEntry.Text, portEntry.Text))
		} else {
			statusLabel.SetText(fmt.Sprintf("Version %s will not connect to Tracker.", version))
		}
	}
	enabledCheck.OnChanged = func(_ bool) {
		updateStatusText()
	}
	urlEntry.OnChanged = func(_ string) {
		updateStatusText()
	}
	portEntry.OnChanged = func(_ string) {
		updateStatusText()
	}
	enabledCheck.SetChecked(GetTrackerConnectionPortForRelease(version) != "")
	updateStatusText()

	content := container.NewVBox(
		enabledCheck,
		widget.NewForm(
			widget.NewFormItem("URL", container.NewGridWrap(fyne.NewSize(300, urlEntry.MinSize().Height), urlEntry)),
			widget.NewFormItem("Port Number", portEntry),
		),
		statusLabel,
	)
	d := dialog.NewCustomConfirm(
		"Tracker Connection",
		"Save",
		"Cancel",
		content,
		func(ok bool) {
			if !ok {
				return
			}

			if enabledCheck.Checked {
				baseURL := strings.TrimSpace(urlEntry.Text)
				port := strings.TrimSpace(portEntry.Text)
				if portNumber, err := strconv.Atoi(port); err != nil || portNumber < 1 || portNumber > 65535 {
					dialog.ShowError(fmt.Errorf("tracker connection port must be an integer between 1 and 65535"), w)
					return
				}
				if _, err := url.Parse(baseURL); err != nil {
					dialog.ShowError(fmt.Errorf("tracker connection URL is invalid"), w)
					return
				}
				if err := ConfigureTrackerConnectionForReleaseURL(version, baseURL, port); err != nil {
					dialog.ShowError(fmt.Errorf("failed to enable tracker connection: %w", err), w)
					return
				}
				dialog.ShowInformation("Tracker Connection", fmt.Sprintf("Tracker connection enabled for version %s on port %s. Restart OWLCMS for that version to apply the change.", version, port), w)
				return
			}

			if err := DisableTrackerConnectionForRelease(version); err != nil {
				dialog.ShowError(fmt.Errorf("failed to disable tracker connection: %w", err), w)
				return
			}
			dialog.ShowInformation("Tracker Connection", fmt.Sprintf("Tracker connection disabled for version %s. Restart OWLCMS for that version to apply the change.", version), w)
		},
		w,
	)
	d.Show()
}

func showPortNumberDialog(w fyne.Window) {
	mdnsHostNameEntry := widget.NewEntry()
	mdnsHostNameEntry.SetText(GetMdnsHostName())
	mdnsHostNameInput := container.NewGridWrap(fyne.NewSize(fyne.MeasureText(strings.Repeat("M", 10), theme.TextSize(), fyne.TextStyle{}).Width, mdnsHostNameEntry.MinSize().Height), mdnsHostNameEntry)
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("8080")
	portEntry.SetText(GetPort())
	portInput := container.NewGridWrap(mdnsHostNameInput.MinSize(), portEntry)
	portStatusLabel := widget.NewLabel("Selected port will apply to future installs. To change the port on an already installed version, use the Option menu for that version.")
	portStatusLabel.Wrapping = fyne.TextWrapWord

	content := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Default .local Host Name", container.NewHBox(mdnsHostNameInput, widget.NewLabel(".local"))),
			widget.NewFormItem("Port Number", portInput),
		),
		portStatusLabel,
	)

	d := dialog.NewCustomConfirm(
		"Default Host Name and Port Number",
		"Save",
		"Cancel",
		content,
		func(ok bool) {
			if !ok {
				return
			}

			mdnsHostName := strings.TrimSpace(mdnsHostNameEntry.Text)
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

			if err := SaveProperty(mdnsEnv, mdnsHostName); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save mDNS host name: %w", err), w)
				return
			}
			if err := SaveProperty("OWLCMS_PORT", newPort); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save OWLCMS port: %w", err), w)
				return
			}
		},
		w,
	)
	d.Resize(fyne.NewSize(520, 240))
	d.Show()
}

func showPortNumberDialogForVersion(w fyne.Window, version string) {
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("8080")
	portEntry.SetText(GetPortForRelease(version))
	portStatusLabel := widget.NewLabel(fmt.Sprintf("Version %s will use port %s.", version, portEntry.Text))
	portEntry.OnChanged = func(_ string) {
		portStatusLabel.SetText(fmt.Sprintf("Version %s will use port %s.", version, portEntry.Text))
	}

	content := container.NewVBox(
		widget.NewForm(widget.NewFormItem("Port Number", portEntry)),
		portStatusLabel,
	)
	d := dialog.NewCustomConfirm(
		"OWLCMS Port Number",
		"Save",
		"Cancel",
		content,
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

			if err := SavePropertyForRelease(version, "OWLCMS_PORT", newPort); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save OWLCMS port: %w", err), w)
				return
			}

			dialog.ShowInformation("Port Updated", fmt.Sprintf("OWLCMS port for version %s set to %s. Restart OWLCMS to apply the new port.", version, newPort), w)
		},
		w,
	)
	d.Show()
}

func refreshAvailableVersions(w fyne.Window) {
	// Reset release-related state to mirror a fresh app start.
	showPrereleases = false
	allReleases = nil
	if prereleaseCheckbox != nil {
		prereleaseCheckbox.Checked = false
		prereleaseCheckbox.Refresh()
	}

	go func() {
		releases, err := fetchReleases()
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("failed to refresh available versions: %w", err), w)
			})
			return
		}

		fyne.Do(func() {
			allReleases = releases
			// Rebuild the download UI as if the app just started.
			setupReleaseDropdown(w)
			recomputeVersionList(w)
			checkForNewerVersion()
			if downloadContainer != nil {
				downloadContainer.Refresh()
			}
		})
	}()
}

// initializeOwlcmsTab handles the async initialization of the OWLCMS tab
func initializeOwlcmsTab(w fyne.Window) {
	fyne.Do(func() {
		if reconnectOwlcmsRuntime() {
			log.Println("OWLCMS tab reattached to existing runtime")
			return
		}

		// Set the appropriate mode based on installed versions
		if len(getAllInstalledVersions()) == 0 {
			setOwlcmsTabModeUninstalled(w)
		} else {
			setOwlcmsTabModeInstalled(w)
		}
		log.Println("OWLCMS tab setup done.")
	})
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

// setOwlcmsTabModeUninstalled is the ONLY way to show the "0 installed versions" UI.
// It must NOT show the download section.
func setOwlcmsTabModeUninstalled(w fyne.Window) {
	showInstallMode()
	resetToExplainMode(w)
	log.Printf("UI Mode: Uninstalled (0 versions)")
}

// setOwlcmsTabModeInstalled is the ONLY way to show the "≥1 installed versions" UI.
// It MUST show BOTH the version list and the download section.
func setOwlcmsTabModeInstalled(w fyne.Window) {
	if IsRunning() {
		log.Printf("UI Mode: Running - not switching to installed mode")
		return
	}

	showVersionListMode()
	// Fetch releases first so update buttons can be computed
	setupReleaseDropdown(w)
	// Now recompute list with release info available
	recomputeVersionList(w)
	checkForNewerVersion()
	if stopButton != nil {
		stopButton.Hide()
	}
	if statusLabel != nil {
		statusLabel.Hide()
	}

	log.Printf("UI Mode: Installed (versions=%d; list+download visible)", len(getAllInstalledVersions()))
}

// setOwlcmsTabMode is the single switch deciding which of the two modes to show.
// This prevents any code path from leaving the version list visible without the
// matching download section.
func setOwlcmsTabMode(w fyne.Window) {
	if len(getAllInstalledVersions()) == 0 {
		setOwlcmsTabModeUninstalled(w)
		return
	}
	setOwlcmsTabModeInstalled(w)
}

func goBackToMainScreen() {
	setOwlcmsTabMode(mainWindow)
}

func restoreOwlcmsRunningUI(version, port string, pid int) {
	currentVersion = version
	stopButton.SetText(fmt.Sprintf("Stop OWLCMS %s", version))
	stopButton.Show()
	statusLabel.SetText(fmt.Sprintf("OWLCMS running (PID: %d) on port %s", pid, port))
	statusLabel.Show()
	stopContainer.Show()
	setOwlcmsTabModeRunning()

	url := fmt.Sprintf("http://localhost:%s", port)
	urlLink.SetURLFromString(url)
	urlLink.SetText("Open OWLCMS in a browser")
	urlLink.Show()
	configureTrackerConnectionUI(version)

	appDir := filepath.Join(installDir, version)
	appDirLink.SetText(fmt.Sprintf("Open OWLCMS %s directory", version))
	appDirLink.SetURL(nil)
	appDirLink.OnTapped = func() {
		shared.OpenFileExplorer(appDir)
	}
	appDirLink.Show()
	configureTailLogLink(version, appDir)
	hideStartupLogArea()
	stopContainer.Refresh()
}

func configureTrackerConnectionUI(version string) {
	connectionURL := GetTrackerConnectionURLForRelease(version)
	if connectionURL == "" {
		trackerConnectionRow.Hide()
		return
	}
	trackerConnectionLabel.SetText(fmt.Sprintf("Connecting to tracker %s  ", connectionURL))

	browserURL, err := url.Parse(connectionURL)
	if err != nil {
		trackerConnectionRow.Hide()
		return
	}
	switch browserURL.Scheme {
	case "ws":
		browserURL.Scheme = "http"
	case "wss":
		browserURL.Scheme = "https"
	default:
		trackerConnectionRow.Hide()
		return
	}
	browserURL.Path = ""
	trackerURLLink.SetURL(browserURL)
	trackerURLLink.SetText("Open Tracker in a browser")
	trackerConnectionRow.Show()
}

func reconnectOwlcmsRuntime() bool {
	metadata, running := shared.CheckDaemonRunning(runtimeMetadataPath())
	if !running {
		clearRuntimeState()
		releaseJavaLock()
		return false
	}

	activeRuntime = metadata
	restoreOwlcmsRunningUI(metadata.Version, metadata.Port, metadata.PID)
	return true
}

// checkJava checks for Java and downloads it if not found
func checkJava(statusLabel *widget.Label) error {
	statusLabel.SetText("Checking for the Java language runtime.")
	statusLabel.Refresh()
	statusLabel.Show()
	if stopButton != nil {
		stopButton.Hide()
	}
	if stopContainer != nil {
		stopContainer.Show()
	}
	if versionContainer != nil {
		versionContainer.Hide()
	}
	// Don't hide downloadContainer during Java check - it will be restored after

	err := javacheck.CheckJava(statusLabel)
	if err != nil {
		statusLabel.SetText("Could not install a Java runtime.")
		statusLabel.Refresh()
		return err
	}

	statusLabel.Hide()
	return nil
}

func checkForNewerVersion() {
	latestInstalled := findLatestInstalled()

	if latestInstalled != "" {
		updateExplanation()
	}

	if latestInstalled == "" {
		messageBox := container.NewHBox(
			widget.NewLabel("No version is installed."),
		)
		updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
		updateTitleContainer.Refresh()
		updateTitleContainer.Show()
		return
	}

	latestInstalledVersion, err := shared.NewVersionForComparison(latestInstalled)
	if err != nil {
		return
	}

	log.Printf("Latest installed version: %s\n", latestInstalled)

	// Check for newer versions (both stable and prerelease)
	for _, release := range allReleases {
		releaseVersion, err := shared.NewVersionForComparison(release)
		if err != nil {
			continue
		}

		if releaseVersion.GreaterThan(latestInstalledVersion) {
			log.Printf("Found newer version: %s\n", release)
			// Use helper to get the correct releases/tag URL (prerelease vs stable)
			releaseURL := GetReleaseTagURL(release)

			var versionType string
			if containsPreReleaseTag(release) {
				versionType = "prerelease"
				// Only offer prerelease if one is already installed
				if !containsPreReleaseTag(latestInstalled) {
					continue // Skip prerelease if user has stable installed
				}
			} else {
				versionType = "stable"
			}

			// Store the available version for the install link
			availableVersion = release

			// Update release notes link
			parsedURL, _ := url.Parse(releaseURL)
			releaseNotesLink.SetURL(parsedURL)
			releaseNotesLink.Show()

			// Show install link
			installAvailableLink.Show()

			messageBox := shared.CreateUpdateNotification(versionType, releaseVersion.String(), installAvailableLink, releaseNotesLink)
			updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
			updateTitleContainer.Refresh()
			updateTitleContainer.Show()
			return
		}
	}

	// If we get here, no newer version was found
	// Choose the correct releases repo for prerelease installs
	releaseURL := GetReleaseTagURL(latestInstalled)
	parsedURL, _ := url.Parse(releaseURL)
	releaseNotesLink.SetURL(parsedURL)
	// Ensure the release notes hyperlink is visible for both stable and prerelease cases
	releaseNotesLink.Show()

	// Log what we think is installed for debugging
	log.Printf("OWLCMS:updateTitle - latestInstalled=%q installedVersions=%v", latestInstalled, getAllInstalledVersions())

	messageBox := container.NewHBox(
		widget.NewLabel(fmt.Sprintf("The latest %s version %s is installed.", func() string {
			if containsPreReleaseTag(latestInstalled) {
				return "prerelease"
			}
			return "stable"
		}(), latestInstalled)),
		releaseNotesLink,
	)
	updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
	updateTitleContainer.Refresh()
	updateTitleContainer.Show()
	downloadButtonTitle.Show()
	if releaseDropdown != nil {
		// Only auto-hide the dropdown when downloads are not expanded.
		// If the user is interacting with the prerelease toggle, keep it visible.
		if !downloadsShown {
			releaseDropdown.Hide()
		}
	}
	if downloadContainer != nil {
		downloadContainer.Refresh()
	}
}

func updateExplanation() {
	if len(allReleases) == 0 {
		if !downloadsShown {
			return
		}
		downloadContainer.Objects = []fyne.CanvasObject{
			widget.NewLabel("You are not connected to the Internet. Available updates cannot be shown."),
		}
		downloadContainer.Refresh()
		return
	}
	log.Printf("len(allReleases) = %d\n", len(allReleases))
	x := getAllInstalledVersions()
	log.Printf("Updating explanation %d\n", len(x))
	if len(x) == 0 {
		downloadContainer.Remove(singleOrMultiVersionLabel)
		downloadContainer.Refresh()
	} else if len(x) == 1 {
		latestStable, stableErr := getMostRecentStableRelease()
		latestPrerelease, preErr := getMostRecentPrerelease()

		downloadContainer.Remove(singleOrMultiVersionLabel)

		if containsPreReleaseTag(x[0]) {
			if preErr == nil && x[0] == latestPrerelease {
				// Latest prerelease installed
			} else {
				if len(downloadContainer.Objects) > 0 {
					downloadContainer.Objects = append(downloadContainer.Objects[:1], append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects[1:]...)...)
				} else {
					downloadContainer.Objects = append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects...)
				}
				singleOrMultiVersionLabel.SetText("Use the Update button above to install the latest version. The current database will be copied to the new version, as well as local changes made to the configuration since the previous installation.")
			}
		} else {
			if stableErr == nil && x[0] == latestStable {
				// Latest stable installed
			} else {
				if len(downloadContainer.Objects) > 0 {
					downloadContainer.Objects = append(downloadContainer.Objects[:1], append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects[1:]...)...)
				} else {
					downloadContainer.Objects = append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects...)
				}
				singleOrMultiVersionLabel.SetText("Use the Update button above to install the latest version. The current database will be copied to the new version, as well as local changes made to the configuration since the previous installation.")
			}
		}
	} else {
		singleOrMultiVersionLabel.SetText("You have several versions installed. Use the Import button if you wish to copy the database and local configuration changes from a previous version.")
	}
	singleOrMultiVersionLabel.Wrapping = fyne.TextWrapWord
	singleOrMultiVersionLabel.Show()
	singleOrMultiVersionLabel.Refresh()
}

func computeVersionScrollHeight(numVersions int) float32 {
	minHeight := 140 // ensure a reasonable minimum vertical space for the version list
	rowHeight := 50
	return float32(minHeight + (rowHeight * min(numVersions, 4)))
}

func uninstallAll() {
	dialog.ShowConfirm("Confirm Uninstall", "This will remove all the data and configurations currently stored.\nIf you proceed, this cannot be undone. Restarting the program will create new data.", func(confirm bool) {
		if confirm {
			err := os.RemoveAll(installDir)
			if err != nil {
				log.Printf("Failed to remove all data: %v\n", err)
				dialog.ShowError(fmt.Errorf("failed to remove all data: %w", err), fyne.CurrentApp().Driver().AllWindows()[0])
			} else {
				log.Println("All data removed successfully")
				dialog.ShowInformation("Success", "All data removed successfully", fyne.CurrentApp().Driver().AllWindows()[0])
				// Do not quit the control panel. Refresh the UI so the uninstalled explanation appears.
				setOwlcmsTabMode(mainWindow)
			}
		}
	}, fyne.CurrentApp().Driver().AllWindows()[0])
}

// HandleSignalCleanup handles cleanup when the application receives a signal
func HandleSignalCleanup() {
	if currentProcess != nil && currentProcess.Process != nil {
		pid := currentProcess.Process.Pid
		log.Printf("Stopping OWLCMS %s (PID: %d)...\n", currentVersion, pid)

		// Set the killedByUs flag first so the monitoring goroutine knows this was intentional
		killedByUs = true

		// Use forceful termination since we need to exit quickly
		_ = shared.ForcefullyKillPID(pid)
		clearRuntimeState()
		releaseJavaLock()
		return
	}

	if activeRuntime != nil {
		killedByUs = true
		_ = shared.ForcefullyKillPID(activeRuntime.PID)
		clearRuntimeState()
		releaseJavaLock()
	}
}

// IsRunning returns true if OWLCMS is currently running
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

// StopRunningProcess stops the running OWLCMS process
func StopRunningProcess(w fyne.Window) {
	if currentProcess != nil && currentProcess.Process != nil {
		log.Println("Stopping OWLCMS process")
		stopProcess(currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
		return
	}

	if activeRuntime != nil {
		log.Println("Stopping attached OWLCMS process")
		stopProcess(currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
	}
}
