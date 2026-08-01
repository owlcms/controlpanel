package video

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"os/exec"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	installDir                = getInstallDir()
	forceUninstalledVideo     = false
	videoProcess              *exec.Cmd
	videoVersion              string
	killedByUs                bool
	statusLabel               *widget.Label
	videoStopButton           *widget.Button
	versionContainer          *fyne.Container
	stopContainer             *fyne.Container
	singleOrMultiVersionLabel *widget.Label
	downloadContainer         *fyne.Container
	downloadsShown            bool
	appDirLink                *widget.Hyperlink
	videoDirLink              *widget.Hyperlink
	videoLogLink              *widget.Hyperlink
	mainWindow                fyne.Window
	topInstallContent         *fyne.Container
	topVersionContent         *fyne.Container
	topRunContent             *fyne.Container
	topModeStack              *fyne.Container
)

// IsRunning returns true if the video process is running
func IsRunning() bool {
	return videoProcess != nil
}

// StopRunningProcess stops the running video process
func StopRunningProcess(w fyne.Window) {
	if videoProcess != nil && videoProcess.Process != nil {
		log.Println("Stopping Video process")
		stopVideoProcess(videoProcess, videoVersion, videoStopButton, w)
	}
}

// HandleSignalCleanup forcefully stops the video process on signal
func HandleSignalCleanup() {
	killedByUs = true
	if videoProcess != nil && videoProcess.Process != nil {
		pid := videoProcess.Process.Pid
		log.Printf("Forcefully stopping Video (PID: %d)", pid)
		if err := videoProcess.Process.Kill(); err != nil {
			log.Printf("Failed to kill Video process %d: %v", pid, err)
		}
		videoProcess = nil
	}
	os.Remove(videoPIDFile)
}

// CreateTab creates and returns the Video tab content
func CreateTab(w fyne.Window) *fyne.Container {
	mainWindow = w

	log.Println("Creating Video tab content")

	videoStopButton = widget.NewButtonWithIcon("Stop Video", theme.CancelIcon(), nil)
	videoStopButton.Importance = widget.DangerImportance

	statusLabel = widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	downloadContainer = container.NewVBox()
	versionContainer = container.NewStack()
	appDirLink = widget.NewHyperlink("", nil)
	appDirLink.Hide()

	videoDirLink = widget.NewHyperlink("", nil)
	videoDirLink.Hide()
	videoLogLink = widget.NewHyperlink("", nil)
	videoLogLink.Hide()

	videoColumn := container.NewVBox(
		widget.NewLabelWithStyle("Video", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		videoStopButton,
		videoDirLink,
		videoLogLink,
	)
	stopContainer = container.NewVBox(
		widget.NewSeparator(),
		videoColumn,
		statusLabel,
	)

	// Initialize download UI widgets
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

	videoStopButton.OnTapped = func() {
		dialog.NewConfirm("Confirm Stop", "Stop the running Video process?",
			func(confirm bool) {
				if confirm {
					stopVideoProcess(videoProcess, videoVersion, videoStopButton, w)
				}
			}, w).Show()
	}

	videoStopButton.Hide()
	stopContainer.Hide()

	menuBar := createMenuBar(w)
	topSpacer := canvas.NewRectangle(color.Transparent)
	topSpacer.SetMinSize(fyne.NewSize(1, 8))

	topInstallContent = container.NewVBox()
	topVersionContent = container.NewVBox(menuBar, topSpacer)
	topRunContent = container.NewVBox(stopContainer)
	topModeStack = container.NewStack(topInstallContent, topVersionContent, topRunContent)

	mainContent := container.NewBorder(
		topModeStack,
		downloadContainer,
		nil,
		nil,
		versionContainer,
	)

	statusLabel.SetText("Checking installation status...")
	statusLabel.Refresh()
	statusLabel.Show()
	stopContainer.Show()

	if forceUninstalledVideo || func() bool { _, err := os.Stat(installDir); return os.IsNotExist(err) }() {
		resetToExplainMode(w)
		return mainContent
	}

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

func setVideoTabModeRunning() {
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

func createMenuBar(w fyne.Window) *fyne.Container {
	fileMenuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open Video Installation Directory", func() {
			if err := shared.OpenFileExplorer(installDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open directory: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Refresh Available Versions", func() {
			refreshAvailableVersions(w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Uninstall Video", func() {
			uninstallAll()
		}),
	}
	fileMenu := shared.CreateMenuButton("Files", fileMenuItems)

	processMenuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open Version Directory", func() {
			if err := shared.OpenFileExplorer(installDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open directory: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Kill Already Running Process", func() {
			if err := killLockingProcess(); err != nil {
				dialog.ShowError(fmt.Errorf("failed to kill running process: %w", err), w)
			} else {
				dialog.ShowInformation("Success", "Successfully killed running Video processes", w)
			}
		}),
	}
	processMenu := shared.CreateMenuButton("Processes", processMenuItems)

	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 5))

	return container.NewVBox(spacer, container.NewHBox(fileMenu, processMenu))
}

func refreshAvailableVersions(w fyne.Window) {
	go func() {
		releases, err := fetchReleases()
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to refresh available versions: %w", err), w)
			return
		}
		allReleases = releases
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

func initializeTab(w fyne.Window) {
	fyne.Do(func() {
		if len(getAllInstalledVersions()) == 0 {
			setVideoTabModeUninstalled(w)
		} else {
			setVideoTabModeInstalled(w)
		}
		log.Println("Video tab setup done.")
	})
}

func setVideoTabModeUninstalled(w fyne.Window) {
	showInstallMode()
	resetToExplainMode(w)
	log.Printf("Video UI Mode: Uninstalled")
}

func setVideoTabModeInstalled(w fyne.Window) {
	if IsRunning() {
		log.Printf("Video UI Mode: Running - not switching to installed mode")
		return
	}

	setupReleaseDropdown(w)
	recomputeVersionList(w)
	checkForNewerVersion()
	showVersionListMode()
	videoStopButton.Hide()
	statusLabel.Hide()

	log.Printf("Video UI Mode: Installed (%d versions)", len(getAllInstalledVersions()))
}

func setVideoTabMode(w fyne.Window) {
	if len(getAllInstalledVersions()) == 0 {
		setVideoTabModeUninstalled(w)
		return
	}
	setVideoTabModeInstalled(w)
}

// HideDownloadables hides the release dropdown
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

// ShowDownloadables shows the release dropdown
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
