package tracker

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"controlpanel/shared"
	"controlpanel/tracker/downloadutils"

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
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
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

func fetchReleases() ([]string, error) {
	url := "https://api.github.com/repos/owlcms/owlcms-tracker/releases"

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
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

	releaseNames := make([]string, 0, len(releases))
	for _, release := range releases {
		// Use tag_name as it's more reliable for versioning
		releaseNames = append(releaseNames, release.TagName)
	}

	// Sort the release names in semver order, most recent at the top
	// Uses shared.CompareVersions which considers SNAPSHOT more recent than other prereleases
	sort.Slice(releaseNames, func(i, j int) bool {
		return shared.CompareVersions(releaseNames[i], releaseNames[j])
	})

	// Log the latest versions
	var latestStable, latestPrerelease string
	for _, release := range releaseNames {
		if !containsPreReleaseTag(release) {
			latestStable = release
			break
		}
	}
	for _, release := range releaseNames {
		if containsPreReleaseTag(release) {
			latestPrerelease = release
			break
		}
	}
	log.Printf("Tracker - Available from GitHub - Latest stable: %s, Latest prerelease: %s\n",
		latestStable, latestPrerelease)

	return releaseNames, nil
}

func populateReleaseSelect(selectWidget *widget.Select) {
	filteredReleases := []string{}
	stableReleases := []string{}
	for _, release := range allReleases {
		if showPrereleases || !containsPreReleaseTag(release) {
			filteredReleases = append(filteredReleases, release)
		}
		if !containsPreReleaseTag(release) {
			stableReleases = append(stableReleases, release)
		}
	}
	if !showPrereleases && len(stableReleases) > 20 {
		filteredReleases = stableReleases[:20]
	}
	selectWidget.Options = filteredReleases
	selectWidget.Refresh()
}

func containsPreReleaseTag(version string) bool {
	return shared.IsPrerelease(version)
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
		// Refetch to get updated list when toggling prereleases.
		go func() {
			releases, err := fetchReleases()
			if err != nil {
				log.Printf("failed to fetch releases after prerelease toggle: %v", err)
				return
			}
			allReleases = releases
			populateReleaseSelect(releaseSelect)
			checkForNewerVersion()
			// Keep the dropdown visible while the user is toggling prereleases.
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

// setupReleaseDropdown initializes the release dropdown and populates it with available releases.
// It fetches releases on-demand if not already loaded.
func setupReleaseDropdown(w fyne.Window) {
	// Fetch releases on-demand if not already loaded
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("Tracker setupReleaseDropdown: fetchReleases failed: %v", err)
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
		// Hide dropdown and checkbox initially; show only the link
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
		fmt.Sprintf("Do you want to download and install Tracker version %s?", version),
		func(confirm bool) {
			if confirm {
				shared.PromptForInstallVersionName(installDir, version, w, func(newVersion string) {
					downloadAndInstallVersion(version, newVersion, w)
				})
			}
		}, w)
}

func downloadAndInstallVersion(downloadVersion, installVersion string, w fyne.Window) {
	var progressBar *widget.ProgressBar
	var messageLabel *widget.Label
	var progressDialog dialog.Dialog

	if w != nil {
		progressBar = widget.NewProgressBar()
		progressBar.SetValue(0.01)
		messageLabel = widget.NewLabel(fmt.Sprintf("Preparing to download Tracker %s...", downloadVersion))
		content := container.NewVBox(messageLabel, progressBar)
		progressDialog = dialog.NewCustom(
			"Installing Tracker",
			"Please wait...",
			content,
			w)
		progressDialog.Show()
	} else {
		log.Printf("Preparing to download Tracker %s...\n", downloadVersion)
		fmt.Printf("Preparing to download Tracker %s...\n", downloadVersion)
	}

	runInstall := func() {
		if w != nil {
			fyne.Do(func() {
				messageLabel.SetText(fmt.Sprintf("Downloading Tracker %s...", downloadVersion))
				messageLabel.Refresh()
			})
		} else {
			log.Printf("Downloading Tracker %s...\n", downloadVersion)
			fmt.Printf("Downloading Tracker %s...\n", downloadVersion)
		}

		progressCallback := func(downloaded, total int64) {
			if total > 0 {
				progress := float64(downloaded) / float64(total)
				if w != nil {
					fyne.Do(func() {
						progressBar.SetValue(progress)
					})
				} else {
					log.Printf("Downloading Tracker %s... %.1f%%", downloadVersion, progress*100)
					fmt.Printf("\rDownloading Tracker %s... %.1f%%", downloadVersion, progress*100)
				}
			}
		}

		if w != nil {
			fyne.Do(func() {
				messageLabel.SetText("Extracting files...")
				messageLabel.Refresh()
			})
		} else {
			log.Println("\nExtracting files...")
			fmt.Println("Extracting files...")
		}

		extractProgress := func(extracted, total int64) {
			if total > 0 {
				if w != nil {
					fyne.Do(func() {
						progressBar.SetValue(float64(extracted) / float64(total))
					})
				} else {
					log.Printf("Extracted: %d/%d bytes", extracted, total)
				}
			}
		}
		result, err := InstallRelease(downloadVersion, installVersion, progressCallback, extractProgress)
		if err != nil {
			if w != nil {
				fyne.Do(func() {
					progressDialog.Hide()
					dialog.ShowError(err, w)
				})
			} else {
				log.Printf("Error: %v", err)
			}
			return
		}

		if w != nil {
			fyne.Do(func() {
				progressBar.SetValue(1.0)
			})
		}
		log.Println("Extraction completed")

		message := fmt.Sprintf(
			"Successfully installed Tracker version %s\n\n"+
				"Location: %s\n\n"+
				"The program files have been extracted to the above directory.",
			result.Version, result.Path)

		if w != nil {
			fyne.Do(func() {
				progressDialog.Hide()
				HideDownloadables()
				setTrackerTabMode(w)
				dialog.ShowInformation("Installation Complete", message, w)
			})
		}

		if w == nil {
			log.Println("Installation Complete:\n" + message)
			fmt.Println("Installation Complete:\n" + message)
		}
	}
	if w != nil {
		go runInstall()
	} else {
		runInstall()
	}
}

// getAssetNames returns possible asset names to try, starting with device-independent
// then falling back to device-dependent names for backward compatibility
func getAssetNames(version string) []string {
	goos := downloadutils.GetGoos()
	goarch := downloadutils.GetGoarch()

	// Try device-independent first
	assetNames := []string{fmt.Sprintf("owlcms-tracker_%s.zip", version)}

	// Fall back to platform-specific names for backward compatibility
	switch goos {
	case "windows":
		assetNames = append(assetNames, fmt.Sprintf("owlcms-tracker-windows_%s.zip", version))
	case "darwin":
		if goarch == "arm64" {
			assetNames = append(assetNames, fmt.Sprintf("owlcms-tracker-macos-arm64_%s.zip", version))
		} else {
			assetNames = append(assetNames, fmt.Sprintf("owlcms-tracker-macos-x64_%s.zip", version))
		}
	case "linux":
		// Linux uses the Raspberry Pi build
		assetNames = append(assetNames, fmt.Sprintf("owlcms-tracker-rpi_%s.zip", version))
	default:
		assetNames = append(assetNames, fmt.Sprintf("owlcms-tracker-rpi_%s.zip", version))
	}

	return assetNames
}

// checkAssetExists performs a HEAD request to check if an asset exists
func checkAssetExists(url string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func getMostRecentStableRelease() (string, error) {
	return shared.GetMostRecentStable(allReleases)
}

func getMostRecentPrerelease() (string, error) {
	return shared.GetMostRecentPrerelease(allReleases)
}

// InstallDefault performs the default install action for the Tracker package.
// It selects the most recent stable or pre-release version depending on the flag,
// or shows the download UI if no matching release is found.
func InstallDefault(w fyne.Window, usePrerelease bool) {
	// If we haven't fetched releases yet, try to fetch them now so the
	// Install button can auto-select the latest release.
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("Tracker InstallDefault: background fetchReleases failed: %v", err)
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
		log.Printf("Tracker InstallDefault: no %s release found, showing download UI", track)
		ShowDownloadables()
	}
}

func checkForNewerVersion() {
	latestInstalled := findLatestInstalled()
	updateExplanation()

	if latestInstalled != "" {
		latestInstalledBase, _ := shared.ParseVersionWithBuild(latestInstalled)
		latestInstalledVersion, err := shared.NewVersionForComparison(latestInstalledBase)
		if err == nil {
			log.Printf("Tracker - Latest installed version: %s\n", latestInstalled)

			// Check for newer versions (both stable and prerelease)
			for _, release := range allReleases {
				releaseVersion, err := shared.NewVersionForComparison(release)
				if err == nil && releaseVersion.GreaterThan(latestInstalledVersion) {
					log.Printf("Tracker - Found newer version: %s\n", release)
					releaseURL := fmt.Sprintf("https://github.com/owlcms/owlcms-tracker/releases/tag/%s", release)

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
						versionToInstall := release
						if versionToInstall == "" {
							return
						}
						confirmAndDownloadVersion(versionToInstall, mainWindow)
					}

					messageBox := shared.CreateUpdateNotification(versionType, releaseVersion.String(), installLink, releaseNotesLink)
					updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
					updateTitleContainer.Refresh()
					updateTitleContainer.Show()
					return
				}
			}

			// Show message with release notes link (no newer version found)
			releaseURL := fmt.Sprintf("https://github.com/owlcms/owlcms-tracker/releases/tag/%s", latestInstalled)
			parsedURL, _ := url.Parse(releaseURL)
			releaseNotesLink := widget.NewHyperlink("Release Notes", parsedURL)
			// Ensure hyperlink visible for installed prerelease/stable
			releaseNotesLink.Show()
			// Log what we think is installed for debugging
			log.Printf("Tracker:updateTitle - latestInstalled=%q installedVersions=%v", latestInstalled, getAllInstalledVersions())

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
		}
	} else {
		messageBox := container.NewHBox(
			widget.NewLabel("No version installed. Select a version to download below."),
		)
		updateTitleContainer.Objects = []fyne.CanvasObject{messageBox}
		updateTitleContainer.Refresh()
		updateTitleContainer.Show()
	}
}

func findLatestInstalled() string {
	versions := getAllInstalledVersions()
	if len(versions) == 0 {
		return ""
	}
	return versions[0]
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
	log.Printf("len(allReleases) = %d\n", len(allReleases))
	x := getAllInstalledVersions()
	log.Printf("Tracker - Updating explanation %d\n", len(x))
	if len(x) == 0 {
		downloadContainer.Remove(singleOrMultiVersionLabel)
		downloadContainer.Refresh()
	} else if len(x) == 1 {
		latestStable, stableErr := getMostRecentStableRelease()
		latestPrerelease, preErr := getMostRecentPrerelease()

		downloadContainer.Remove(singleOrMultiVersionLabel)

		if containsPreReleaseTag(x[0]) {
			if preErr == nil && !shared.CompareVersions(latestPrerelease, x[0]) {
				// Latest prerelease installed
			} else {
				if len(downloadContainer.Objects) > 0 {
					downloadContainer.Objects = append(downloadContainer.Objects[:1], append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects[1:]...)...)
				} else {
					downloadContainer.Objects = append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects...)
				}
				singleOrMultiVersionLabel.SetText("Use the Update button above to install the latest version.")
			}
		} else {
			if stableErr == nil && !shared.CompareVersions(latestStable, x[0]) {
				// Latest stable installed
			} else {
				if len(downloadContainer.Objects) > 0 {
					downloadContainer.Objects = append(downloadContainer.Objects[:1], append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects[1:]...)...)
				} else {
					downloadContainer.Objects = append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects...)
				}
				singleOrMultiVersionLabel.SetText("Use the Update button above to install the latest version.")
			}
		}
	} else {
		singleOrMultiVersionLabel.SetText("You have several versions installed.")
	}
	singleOrMultiVersionLabel.Wrapping = fyne.TextWrapWord
	singleOrMultiVersionLabel.Show()
	singleOrMultiVersionLabel.Refresh()
}
