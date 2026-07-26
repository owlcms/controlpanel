package cameras

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	customdialog "controlpanel/cameras/dialog"
	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Release represents a GitHub release
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// hasAsset reports whether the release publishes the named asset.
func (r Release) hasAsset(name string) bool {
	for _, a := range r.Assets {
		if a.Name == name {
			return true
		}
	}
	return false
}

var (
	showPrereleases      bool = false
	allReleases          []string
	releaseDropdown      *fyne.Container
	prereleaseCheckbox   *widget.Check
	updateTitle          *widget.RichText
	updateTitleContainer *fyne.Container
	downloadButtonTitle  *widget.Hyperlink
)

const repoURL = "https://api.github.com/repos/owlcms/replays/releases"
const downloadURLPrefix = "https://github.com/owlcms/replays/releases/download"

func fetchReleases() ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(repoURL)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("invalid response format: %w", err)
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	// Only keep releases that ship a binary for the current platform: macOS
	// support was added in 2.5.0, older releases have no darwin assets.
	exeName := camerasExeName()
	releaseNames := make([]string, 0, len(releases))
	for _, r := range releases {
		if !r.hasAsset(exeName) {
			continue
		}
		releaseNames = append(releaseNames, r.TagName)
	}

	if len(releaseNames) == 0 {
		return nil, fmt.Errorf("no release provides %s", exeName)
	}

	sort.Slice(releaseNames, func(i, j int) bool {
		return shared.CompareVersions(releaseNames[i], releaseNames[j])
	})

	return releaseNames, nil
}

func containsPreReleaseTag(version string) bool {
	return shared.IsPrerelease(version)
}

func getMostRecentStableRelease() (string, error) {
	return shared.GetMostRecentStable(allReleases)
}

func getMostRecentPrerelease() (string, error) {
	return shared.GetMostRecentPrerelease(allReleases)
}

func populateReleaseSelect(selectWidget *widget.Select) {
	var filtered []string
	var stable []string
	for _, r := range allReleases {
		if showPrereleases || !containsPreReleaseTag(r) {
			filtered = append(filtered, r)
		}
		if !containsPreReleaseTag(r) {
			stable = append(stable, r)
		}
	}
	if !showPrereleases && len(stable) > 20 {
		filtered = stable[:20]
	}
	selectWidget.Options = filtered
	selectWidget.Refresh()
}

func createReleaseDropdown(w fyne.Window) (*widget.Select, *fyne.Container) {
	releaseSelect := widget.NewSelect([]string{}, func(selected string) {
		if selected != "" {
			confirmAndDownloadVersion(selected, w)
		}
	})
	releaseSelect.PlaceHolder = "Select a version to install"

	prereleaseCheckbox = widget.NewCheck("Show Prereleases", func(checked bool) {
		showPrereleases = checked
		go func() {
			releases, err := fetchReleases()
			if err != nil {
				log.Printf("failed to fetch releases after prerelease toggle: %v", err)
				return
			}
			allReleases = releases
			populateReleaseSelect(releaseSelect)
			checkForNewerVersion()
			ShowDownloadables()
			if downloadContainer != nil {
				downloadContainer.Refresh()
			}
		}()
	})
	prereleaseCheckbox.Hide()

	dropdownContainer := container.NewHBox(releaseSelect, prereleaseCheckbox)
	return releaseSelect, dropdownContainer
}

func setupReleaseDropdown(w fyne.Window) {
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("Video setupReleaseDropdown: fetchReleases failed: %v", err)
		}
	}

	selectWidget, dropdownContainer := createReleaseDropdown(w)
	releaseDropdown = dropdownContainer

	if len(allReleases) > 0 {
		downloadContainer.Objects = []fyne.CanvasObject{
			updateTitleContainer,
			singleOrMultiVersionLabel,
			downloadButtonTitle,
			dropdownContainer,
		}
		dropdownContainer.Hide()
		if prereleaseCheckbox != nil {
			prereleaseCheckbox.Hide()
		}
		downloadsShown = false
	} else {
		downloadContainer.Objects = []fyne.CanvasObject{
			updateTitleContainer,
			singleOrMultiVersionLabel,
			downloadButtonTitle,
		}
	}
	populateReleaseSelect(selectWidget)
	downloadContainer.Refresh()
}

func confirmAndDownloadVersion(version string, w fyne.Window) {
	dialog.ShowConfirm("Confirm Download",
		fmt.Sprintf("Do you want to download and install Cameras module version %s?", version),
		func(confirm bool) {
			if confirm {
				shared.PromptForInstallVersionName(installDir, version, w, func(newVersion string) {
					go downloadAndInstallVersion(version, newVersion, w)
				})
			}
		}, w)
}

func downloadAndInstallVersion(downloadVersion, installVersion string, w fyne.Window) {
	cancel := make(chan bool)
	var progressDialog dialog.Dialog
	var progressBar *widget.ProgressBar
	fyne.DoAndWait(func() {
		progressDialog, progressBar = customdialog.NewDownloadDialog("Installing Cameras Module", w, cancel)
		progressDialog.Show()
	})

	camsFile := camerasExeName()

	versionDir := filepath.Join(installDir, installVersion)
	if err := shared.EnsureDir0755(versionDir); err != nil {
		fyne.Do(func() {
			progressDialog.Hide()
			dialog.ShowError(fmt.Errorf("creating version directory: %w", err), w)
		})
		return
	}

	progressCallback := func(downloaded, total int64) {
		if total > 0 {
			progress := float64(downloaded) / float64(total)
			fyne.Do(func() {
				progressBar.SetValue(progress)
			})
		}
	}

	// Download cameras binary
	camsURL := fmt.Sprintf("%s/%s/%s", downloadURLPrefix, downloadVersion, camsFile)
	camsPath := filepath.Join(versionDir, camsFile)
	log.Printf("Downloading cameras from: %s", camsURL)
	fyne.Do(func() {
		progressBar.SetValue(0.01)
	})

	if err := shared.DownloadArchive(camsURL, camsPath, progressCallback, cancel); err != nil {
		cancelled := err.Error() == "download cancelled"
		fyne.Do(func() {
			progressDialog.Hide()
			if !cancelled {
				dialog.ShowError(fmt.Errorf("cameras download failed: %w", err), w)
			}
		})
		return
	}

	if shared.GetGoos() != "windows" {
		os.Chmod(camsPath, 0755)
	}

	// Extract editable config files into the version directory.
	log.Printf("Extracting camera config files using %s --configDir %s --extractConfig", camsPath, versionDir)
	if err := runExtractConfig(camsPath, versionDir); err != nil {
		log.Printf("Failed to extract camera config files (binary=%s, configDir=%s): %v", camsPath, versionDir, err)
		fyne.Do(func() {
			progressDialog.Hide()
			dialog.ShowError(fmt.Errorf("failed to extract camera config files: %w", err), w)
		})
		return
	}

	fyne.DoAndWait(func() {
		progressBar.SetValue(1.0)
		progressDialog.Hide()
	})

	// Ensure FFmpeg is available (download if needed)
	if _, err := shared.EnsureFFmpegPrerequisite(w); err != nil {
		log.Printf("FFmpeg prerequisite failed during cameras install: %v", err)
		fyne.Do(func() {
			dialog.ShowError(fmt.Errorf("FFmpeg installation failed: %w", err), w)
		})
		return
	}

	message := fmt.Sprintf(
		"Successfully installed Cameras module version %s\n\nLocation: %s",
		installVersion, versionDir)
	fyne.Do(func() {
		dialog.ShowInformation("Installation Complete", message, w)
		HideDownloadables()

		setVideoTabMode(w)
		recomputeVersionList(w)
		checkForNewerVersion()
	})
}

func runExtractConfig(binaryPath, configDir string) error {
	cmd := exec.Command(binaryPath, "--configDir", configDir, "--extractConfig")
	cmd.Dir = configDir
	cmd.Env = shared.BuildVideoLaunchEnv(configDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("runExtractConfig failed: cmd=%q dir=%q err=%v output=%q", cmd.String(), cmd.Dir, err, string(output))
		return fmt.Errorf("%w (output: %s)", err, string(output))
	}
	log.Printf("runExtractConfig succeeded: cmd=%q dir=%q output=%q", cmd.String(), cmd.Dir, string(output))
	return nil
}

// InstallDefault downloads the latest stable or pre-release version.
func InstallDefault(w fyne.Window, usePrerelease bool) {
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("Video InstallDefault: fetchReleases failed: %v", err)
		}
	}

	var latest string
	var err error
	if usePrerelease {
		latest, err = getMostRecentPrerelease()
	} else {
		latest, err = getMostRecentStableRelease()
	}
	if err == nil && latest != "" {
		confirmAndDownloadVersion(latest, w)
	} else {
		track := "stable"
		if usePrerelease {
			track = "pre-release"
		}
		log.Printf("Video InstallDefault: no %s release found, showing download UI", track)
		ShowDownloadables()
	}
}

func checkForNewerVersion() {
	latestInstalled := findLatestInstalled()
	updateExplanation()

	if latestInstalled == "" {
		messageBox := container.NewHBox(
			widget.NewLabel("No version installed. Select a version to download below."),
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

	log.Printf("Video - Latest installed: %s", latestInstalled)

	for _, release := range allReleases {
		releaseVersion, err := shared.NewVersionForComparison(release)
		if err != nil {
			continue
		}
		if !releaseVersion.GreaterThan(latestInstalledVersion) {
			continue
		}

		var versionType string
		if containsPreReleaseTag(release) {
			// Only offer a prerelease to users who already run a prerelease.
			if !containsPreReleaseTag(latestInstalled) {
				continue
			}
			versionType = "prerelease"
		} else {
			versionType = "stable"
		}

		releaseURL := fmt.Sprintf("https://github.com/owlcms/replays/releases/tag/%s", release)
		parsedURL, _ := url.Parse(releaseURL)
		releaseNotesLink := widget.NewHyperlink("Release Notes", parsedURL)
		installLink := widget.NewHyperlink("Install as additional version", nil)
		installLink.OnTapped = func() {
			confirmAndDownloadVersion(release, mainWindow)
		}

		messageBox := shared.CreateUpdateNotification(versionType, releaseVersion.String(), installLink, releaseNotesLink)
		updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
		updateTitleContainer.Refresh()
		updateTitleContainer.Show()
		return
	}

	// Already up to date
	releasesPageURL, _ := url.Parse("https://github.com/owlcms/replays/releases")
	releasesPageLink := widget.NewHyperlink("Release Notes", releasesPageURL)
	versionType := "stable"
	if containsPreReleaseTag(latestInstalled) {
		versionType = "prerelease"
	}
	messageBox := container.NewHBox(
		widget.NewLabel(fmt.Sprintf("The latest %s version %s is installed.", versionType, latestInstalled)),
		releasesPageLink,
	)
	updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
	updateTitleContainer.Refresh()
	updateTitleContainer.Show()
}

func updateExplanation() {
	if len(allReleases) == 0 {
		if !downloadsShown {
			return
		}
		downloadContainer.Objects = []fyne.CanvasObject{
			widget.NewLabel("You are not connected to the Internet. Available updates cannot be shown."),
		}
		downloadContainer.Show()
		downloadContainer.Refresh()
		return
	}

	versions := getAllInstalledVersions()
	if len(versions) == 0 {
		downloadContainer.Remove(singleOrMultiVersionLabel)
		downloadContainer.Refresh()
	} else if len(versions) == 1 {
		latestStable, stableErr := getMostRecentStableRelease()
		latestPrerelease, preErr := getMostRecentPrerelease()

		downloadContainer.Remove(singleOrMultiVersionLabel)
		alreadyLatest := (!containsPreReleaseTag(versions[0]) && stableErr == nil && versions[0] == latestStable) ||
			(containsPreReleaseTag(versions[0]) && preErr == nil && versions[0] == latestPrerelease)

		if !alreadyLatest {
			if len(downloadContainer.Objects) > 0 {
				downloadContainer.Objects = append(downloadContainer.Objects[:1], append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects[1:]...)...)
			} else {
				downloadContainer.Objects = append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects...)
			}
			singleOrMultiVersionLabel.SetText("Use the Update button above to install the latest version.")
		}
	} else {
		singleOrMultiVersionLabel.SetText("You have several versions installed.")
	}
	singleOrMultiVersionLabel.Wrapping = fyne.TextWrapWord
	singleOrMultiVersionLabel.Show()
	singleOrMultiVersionLabel.Refresh()
}

// updateVersion downloads a newer build over an existing install. It blocks on
// the network, so it must be called from a background goroutine.
func updateVersion(existingVersion, targetVersion string, w fyne.Window) {
	existingDir := filepath.Join(installDir, existingVersion)
	targetBaseVersion, _ := shared.ParseVersionWithBuild(targetVersion)
	existingBuild := shared.GetCurrentBuildString(existingVersion)
	targetInstallVersion := targetVersion
	if existingBuild != "" {
		resolvedBuild := shared.ResolveCollisionForBuild(installDir, targetBaseVersion, existingBuild)
		targetInstallVersion = fmt.Sprintf("%s+%s", targetBaseVersion, resolvedBuild)
	}
	cancel := make(chan bool)
	var progressDialog dialog.Dialog
	var progressBar *widget.ProgressBar
	fyne.DoAndWait(func() {
		progressDialog, progressBar = customdialog.NewDownloadDialog("Updating Cameras Module", w, cancel)
		progressDialog.Show()
	})

	camsFile := camerasExeName()
	newVersionDir := filepath.Join(installDir, targetInstallVersion)
	if err := shared.EnsureDir0755(newVersionDir); err != nil {
		fyne.Do(func() {
			progressDialog.Hide()
			dialog.ShowError(fmt.Errorf("creating version directory: %w", err), w)
		})
		return
	}

	progressCallback := func(downloaded, total int64) {
		if total > 0 {
			progress := float64(downloaded) / float64(total)
			fyne.Do(func() {
				progressBar.SetValue(progress)
			})
		}
	}

	camsPath := filepath.Join(newVersionDir, camsFile)
	camsURL := fmt.Sprintf("%s/%s/%s", downloadURLPrefix, targetVersion, camsFile)
	if err := shared.DownloadArchive(camsURL, camsPath, progressCallback, cancel); err != nil {
		cancelled := err.Error() == "download cancelled"
		fyne.Do(func() {
			progressDialog.Hide()
			if !cancelled {
				dialog.ShowError(fmt.Errorf("cameras download failed: %w", err), w)
			}
		})
		return
	}
	if shared.GetGoos() != "windows" {
		os.Chmod(camsPath, 0755)
	}

	// Copy config from existing version
	if err := copyVersionConfigArtifacts(existingDir, newVersionDir); err != nil {
		log.Printf("No config to copy from %s: %v", existingDir, err)
	}

	fyne.Do(func() {
		progressBar.SetValue(1.0)
		progressDialog.Hide()
		dialog.ShowInformation("Update Complete", fmt.Sprintf("Successfully updated Cameras module to version %s", targetInstallVersion), w)

		recomputeVersionList(w)
		checkForNewerVersion()
	})
}
