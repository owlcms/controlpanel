package firmata

import (
	"fmt"
	"image/color"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	customdialog "controlpanel/firmata/dialog"
	"controlpanel/firmata/javacheck"
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
	// TEMPORARY TEST FLAG: when true, treat Firmata as not installed.
	// Keep variable for testing; default to false to use real detection.
	forceUninstalledFirmata   = false
	currentProcess            *exec.Cmd
	currentVersion            string // Add to track current version
	statusLabel               *widget.Label
	stopButton                *widget.Button
	versionContainer          *fyne.Container
	stopContainer             *fyne.Container
	singleOrMultiVersionLabel *widget.Label     // New label for single or multi version update
	downloadContainer         *fyne.Container   // New global to track the same container
	downloadsShown            bool              // New global to track whether downloads are shown
	urlLink                   *widget.Hyperlink // Add this new variable
	mainWindow                fyne.Window       // Reference to the main window
	startupLogText            *widget.Entry
	startupLogContainer       *fyne.Container
	startupLogHost            *fyne.Container
	appDirLink                *widget.Hyperlink
	tailLogLink               *widget.Hyperlink
	selectionContent          *fyne.Container
	runningContent            *fyne.Container
	modeStack                 *fyne.Container
	topInstallContent         *fyne.Container
	topVersionContent         *fyne.Container
	topRunContent             *fyne.Container
	topModeStack              *fyne.Container
)

func initMain() {
	installDir = getInstallDirWithLogging()
	javacheck.InitJavaCheck(installDir, GetTemurinVersion)
}

func resolveInstallDir(logSelection bool) string {
	dirExists := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.IsDir()
	}

	var legacyDir string
	var newDir string

	switch shared.GetGoos() {
	case "windows":
		legacyDir = filepath.Join(os.Getenv("APPDATA"), "firmata")
		newDir = filepath.Join(os.Getenv("APPDATA"), "owlcms-firmata")
	case "darwin":
		legacyDir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "firmata")
		newDir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "owlcms-firmata")
	case "linux":
		legacyDir = filepath.Join(os.Getenv("HOME"), ".local", "share", "firmata")
		newDir = filepath.Join(os.Getenv("HOME"), ".local", "share", "owlcms-firmata")
	default:
		legacyDir = "./firmata"
		newDir = "./owlcms-firmata"
	}

	if dirExists(legacyDir) {
		if logSelection {
			log.Printf("Firmata install directory: using legacy path %s", legacyDir)
		}
		return legacyDir
	}
	if dirExists(newDir) {
		if logSelection {
			log.Printf("Firmata install directory: using existing new path %s", newDir)
		}
		return newDir
	}

	if logSelection {
		log.Printf("Firmata install directory: using new default path %s", newDir)
	}

	return newDir
}

func getInstallDir() string {
	return resolveInstallDir(false)
}

func getInstallDirWithLogging() string {
	return resolveInstallDir(true)
}

// GetInstallDir returns the installation directory used by the firmata package
func GetInstallDir() string {
	return getInstallDir()
}

func checkJava(statusLabel *widget.Label) error {
	statusLabel.SetText("Checking for the Java language runtime.")
	statusLabel.Refresh()
	statusLabel.Show()
	stopButton.Hide()
	stopContainer.Show()
	versionContainer.Hide()
	downloadContainer.Hide()

	err := javacheck.CheckJava(statusLabel)
	if err != nil {
		statusLabel.SetText("Could not install a Java runtime.")
		statusLabel.Refresh()
		return err
	}

	statusLabel.Hide() // Hide the status label if Java check is successful
	return nil
}

func goBackToMainScreen() {
	setFirmataTabMode(fyne.CurrentApp().Driver().AllWindows()[0])
}

func computeVersionScrollHeight(numVersions int) float32 {
	minHeight := 140 // minimum height to provide adequate vertical space
	rowHeight := 50  // approximate height per row
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
				// Do not quit the control panel; refresh UI to show uninstalled explanation
				recomputeVersionList(mainWindow)
			}
		}
	}, fyne.CurrentApp().Driver().AllWindows()[0])
}

// IsRunning returns true if Firmata is currently running
func IsRunning() bool {
	return currentProcess != nil
}

// StopRunningProcess stops the running Firmata process
func StopRunningProcess(w fyne.Window) {
	if currentProcess != nil && currentProcess.Process != nil {
		log.Println("Stopping Firmata process")
		stopProcess(currentProcess, currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
	}
}

// HandleSignalCleanup handles cleanup when the application receives a signal
func HandleSignalCleanup() {
	if currentProcess != nil && currentProcess.Process != nil {
		pid := currentProcess.Process.Pid
		log.Printf("Forcefully stopping Firmata (PID: %d)...\n", pid)
		killedByUs = true
		// Use direct kill for fast cleanup
		if err := currentProcess.Process.Kill(); err != nil {
			log.Printf("Failed to kill Firmata process %d: %v\n", pid, err)
		} else {
			log.Printf("Firmata process %d killed\n", pid)
		}
		currentProcess = nil
	}
	// Always release the lock and remove PID file on signal cleanup
	releaseJavaLock()
}

// CreateTab creates and returns the Firmata tab content
// This should be called from the main application after the window is created
func CreateTab(w fyne.Window) *fyne.Container {
	// Initialize the firmata-specific components
	initMain()
	initConfig()
	if err := InitEnv(); err != nil {
		log.Printf("Failed to initialize Firmata environment: %v", err)
		dialog.ShowError(fmt.Errorf("failed to initialize Firmata environment: %w", err), w)
	}

	// Store main window reference
	mainWindow = w

	log.Println("Creating Firmata tab content")

	// Create stop button and status label
	stopButton = widget.NewButtonWithIcon("Stop", theme.CancelIcon(), nil)
	stopButton.Importance = widget.DangerImportance // Dark red for stop action
	statusLabel = widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord // Allow status messages to wrap

	// Create containers
	downloadContainer = container.NewVBox()
	versionContainer = container.NewStack() // Use Stack so it expands in the center (replaces deprecated NewMax)

	// Create URL hyperlink
	urlLink = widget.NewHyperlink("", nil)
	urlLink.Hide()

	// Create app directory hyperlink
	appDirLink = widget.NewHyperlink("", nil)
	appDirLink.Hide()

	// Create Tail logs hyperlink
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
			"Stop the running Firmata process?",
			func(confirm bool) {
				if confirm {
					stopProcess(currentProcess, currentVersion, stopButton, downloadContainer, versionContainer, statusLabel, w)
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

	topInstallContent = container.NewVBox(createOptionsMenu(w), installTopSpacer)
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
	statusLabel.SetText("Checking installation status...")
	statusLabel.Refresh()
	statusLabel.Show()
	stopContainer.Show()

	// If Firmata install directory does not exist, reset tab to explanation mode
	if forceUninstalledFirmata || func() bool { _, err := os.Stat(installDir); return os.IsNotExist(err) }() {
		resetToExplainMode(w)
		return mainContent
	} else {
		// Start initialization in a goroutine
		go initializeFirmataTab(w)
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

// setFirmataTabModeRunning switches the tab into the running layout (no version picker).
func setFirmataTabModeRunning() {
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
	setPortItem := fyne.NewMenuItem("Default Port Number", func() {
		showDefaultPortDialog(w)
	})
	return shared.CreateMenuButton("Options", []*fyne.MenuItem{setPortItem})
}

func showDefaultPortDialog(w fyne.Window) {
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("8090")
	portEntry.SetText(GetPort())
	portStatusLabel := widget.NewLabel("Selected port will apply to future installs. To change the port on an already installed version, use the Option menu for that version.")
	portStatusLabel.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(
		widget.NewForm(widget.NewFormItem("Port Number", portEntry)),
		portStatusLabel,
	)

	d := dialog.NewCustomConfirm(
		"Default Firmata Port Number",
		"Save",
		"Cancel",
		content,
		func(ok bool) {
			if !ok {
				return
			}

			newPort := strings.TrimSpace(portEntry.Text)
			portNumber, err := strconv.Atoi(newPort)
			if newPort == "" || err != nil || portNumber < 1 || portNumber > 65535 {
				dialog.ShowError(fmt.Errorf("port number must be an integer between 1 and 65535"), w)
				return
			}

			if err := SaveProperty("FIRMATA_PORT", newPort); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save Firmata port: %w", err), w)
				return
			}
		},
		w,
	)
	d.Resize(fyne.NewSize(520, 200))
	d.Show()
}

func showPortNumberDialogForVersion(w fyne.Window, version string) {
	portEntry := widget.NewEntry()
	portEntry.SetPlaceHolder("8090")
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
		"Firmata Port Number",
		"Save",
		"Cancel",
		content,
		func(ok bool) {
			if !ok {
				return
			}

			newPort := strings.TrimSpace(portEntry.Text)
			portNumber, err := strconv.Atoi(newPort)
			if newPort == "" || err != nil || portNumber < 1 || portNumber > 65535 {
				dialog.ShowError(fmt.Errorf("port number must be an integer between 1 and 65535"), w)
				return
			}
			if err := SavePropertyForRelease(version, "FIRMATA_PORT", newPort); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save Firmata port: %w", err), w)
				return
			}

			dialog.ShowInformation("Firmata Port Updated", fmt.Sprintf("Firmata port for version %s set to %s. Restart Firmata for that version to apply the new port.", version, newPort), w)
		},
		w,
	)
	d.Show()
}

// createMenuBar creates the menu bar with File and Processes menus
func createMenuBar(w fyne.Window) *fyne.Container {
	// Create the File menu button with popup
	fileMenuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open Firmata Installation Directory", func() {
			if err := shared.OpenFileExplorer(installDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open installation directory: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Refresh Available Versions", func() {
			refreshAvailableVersions(w)
		}),
		fyne.NewMenuItemSeparator(),
		// Commented out: remove all versions via Files menu (use Uninstall instead)
		// fyne.NewMenuItem("Remove All Firmata Versions", func() {
		// 	removeAllVersions()
		// }),
		// Commented out: remove bundled Java via Files menu
		// fyne.NewMenuItem("Remove Firmata Java", func() {
		// 	removeJava()
		// }),
		fyne.NewMenuItem("Uninstall Firmata", func() {
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

// initializeFirmataTab handles the async initialization of the Firmata tab
func initializeFirmataTab(w fyne.Window) {
	fyne.Do(func() {
		// Set the appropriate mode based on installed versions
		if len(getAllInstalledVersions()) == 0 {
			setFirmataTabModeUninstalled(w)
		} else {
			setFirmataTabModeInstalled(w)
		}
		log.Println("Firmata tab setup done.")
	})
}

// setFirmataTabModeUninstalled shows the install prompt for when no versions are installed.
func setFirmataTabModeUninstalled(w fyne.Window) {
	showInstallMode()
	resetToExplainMode(w)
	log.Printf("UI Mode: Uninstalled (0 versions)")
}

// setFirmataTabModeInstalled shows the version list and download section.
func setFirmataTabModeInstalled(w fyne.Window) {
	// Fetch releases first so update buttons can be computed
	setupReleaseDropdown(w)
	// Now recompute list with release info available
	recomputeVersionList(w)
	checkForNewerVersion()
	showVersionListMode()
	if stopButton != nil {
		stopButton.Hide()
	}
	if statusLabel != nil {
		statusLabel.Hide()
	}

	log.Printf("UI Mode: Installed (versions=%d; list+download visible)", len(getAllInstalledVersions()))
}

// setFirmataTabMode is the single switch deciding which mode to show.
func setFirmataTabMode(w fyne.Window) {
	if len(getAllInstalledVersions()) == 0 {
		setFirmataTabModeUninstalled(w)
		return
	}
	setFirmataTabModeInstalled(w)
}

// HideDownloadables hides the download dropdown and prerelease checkbox
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

// ShowDownloadables shows the download dropdown and prerelease checkbox
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

func downloadAndInstallVersion(version string, w fyne.Window) {
	var urlPrefix string
	if containsPreReleaseTag(version) {
		urlPrefix = "https://github.com/jflamy/owlcms-firmata/releases/download"
	} else {
		urlPrefix = "https://github.com/jflamy/owlcms-firmata/releases/download"
	}
	fileName := "owlcms-firmata.jar"
	zipURL := fmt.Sprintf("%s/%s/%s", urlPrefix, version, fileName)

	// Ensure the firmata directory exists
	owlcmsDir := installDir
	if _, err := os.Stat(owlcmsDir); os.IsNotExist(err) {
		if err := shared.EnsureDir0755(owlcmsDir); err != nil {
			dialog.ShowError(fmt.Errorf("creating firmata directory: %w", err), w)
			return
		}
	}

	// Show progress dialog with progress bar
	cancel := make(chan bool)
	progressDialog, progressBar := customdialog.NewDownloadDialog(
		"Installing owlcms-firmata",
		w,
		cancel)
	progressDialog.Show()

	go func() {
		extractPath := filepath.Join(owlcmsDir, version)
		if err := shared.EnsureDir0755(extractPath); err != nil {
			fyne.Do(func() {
				progressDialog.Hide()
				dialog.ShowError(fmt.Errorf("creating firmata version directory: %w", err), w)
			})
			return
		}
		extractPath = filepath.Join(extractPath, fileName)

		// Download the file using downloadutils with progress tracking
		log.Printf("Starting download from URL: %s\n", zipURL)
		progressCallback := func(downloaded, total int64) {
			if total > 0 {
				percentage := float64(downloaded) / float64(total)
				fyne.Do(func() {
					progressBar.SetValue(percentage)
				})
			}
		}
		err := shared.DownloadArchive(zipURL, extractPath, progressCallback, cancel)
		if err != nil {
			cancelled := err.Error() == "download cancelled"
			fyne.Do(func() {
				progressDialog.Hide()
				if !cancelled {
					dialog.ShowError(fmt.Errorf("download failed: %w", err), w)
				}
			})
			return
		}

		// Log when extraction is done
		log.Println("Extraction completed")

		if err := EnsureParentEnvDefaults(); err != nil {
			log.Println("Installation completed but env.properties initialization failed")
			fyne.Do(func() {
				progressDialog.Hide()
				customdialog.ShowWideError(fmt.Errorf("installation completed but failed to create configuration file: %w\n\nYou may need to check permissions on the installation directory", err), w)
				setFirmataTabMode(w)
			})
			return
		}
		if err := EnsureReleaseEnvFromParent(version); err != nil {
			fyne.Do(func() {
				progressDialog.Hide()
				customdialog.ShowWideError(fmt.Errorf("installation completed but failed to create release configuration: %w", err), w)
			})
			return
		}

		// Show success panel with installation details
		message := fmt.Sprintf(
			"Successfully installed owlcms-firmata version %s\n\n"+
				"Location: %s\n\n"+
				"The program files have been extracted to the above directory.",
			version, extractPath)

		fyne.Do(func() {
			progressDialog.Hide()
			dialog.ShowInformation("Installation Complete", message, w)
			HideDownloadables()
			setFirmataTabMode(w)
		})
	}()
}

func checkForNewerVersion() {
	latestInstalled = findLatestInstalled()
	updateExplanation()

	if latestInstalled != "" {
		latestInstalledVersion, err := shared.NewVersionForComparison(latestInstalled)
		if err == nil {
			log.Printf("Latest installed version: %s\n", latestInstalled)

			// Check for newer versions (both stable and prerelease)
			for _, release := range allReleases {
				releaseVersion, err := shared.NewVersionForComparison(release)
				if err == nil && releaseVersion.GreaterThan(latestInstalledVersion) {
					log.Printf("Found newer version: %s\n", release)
					releaseURL := fmt.Sprintf("https://github.com/jflamy/owlcms-firmata/releases/tag/%s", releaseVersion)
					versionToInstall := shared.ExtractSemver(release)

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

					// Create hyperlinks for Release Notes and install option
					parsedURL, _ := url.Parse(releaseURL)
					releaseNotesLink := widget.NewHyperlink("Release Notes", parsedURL)
					// Ensure hyperlink visible for prerelease/stable announcement
					releaseNotesLink.Show()
					installLink := widget.NewHyperlink("Install as additional version", nil)
					installLink.OnTapped = func() {
						if versionToInstall == "" {
							return
						}
						dialog.ShowConfirm(
							"Confirm Download",
							fmt.Sprintf("Do you want to download and install owlcms-firmata version %s?", versionToInstall),
							func(ok bool) {
								if !ok {
									return
								}
								downloadAndInstallVersion(versionToInstall, mainWindow)
							},
							mainWindow,
						)
					}

					messageBox := shared.CreateUpdateNotification(versionType, releaseVersion.String(), installLink, releaseNotesLink)
					updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
					updateTitleContainer.Refresh()
					updateTitleContainer.Show()
					return
				}
			}

			// If we get here, no newer version was found
			releaseURL := fmt.Sprintf("https://github.com/jflamy/owlcms-firmata/releases/tag/%s", latestInstalled)
			parsedURL, _ := url.Parse(releaseURL)
			releaseNotesLink := widget.NewHyperlink("Release Notes", parsedURL)
			// Ensure hyperlink visible for installed prerelease/stable
			releaseNotesLink.Show()
			// Log what we think is installed for debugging
			log.Printf("Firmata:updateTitle - latestInstalled=%q installedVersions=%v", latestInstalled, getAllInstalledVersions())
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
				releaseDropdown.Hide()
			}
			if downloadContainer != nil {
				downloadContainer.Refresh()
			}
		}
	} else {
		messageBox := container.NewHBox(
			widget.NewLabel("No version is installed."),
		)
		updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
		updateTitleContainer.Refresh()
		updateTitleContainer.Show()
	}
}
