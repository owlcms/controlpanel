package firmata

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	customdialog "controlpanel/firmata/dialog"
	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// Release represents a GitHub release
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
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
	urls := []string{
		"https://api.github.com/repos/owlcms/owlcms-firmata/releases",
	}

	var allReleasesLocal []Release
	client := &http.Client{
		Timeout: 5 * time.Second, // Set a timeout for the HTTP request
	}

	for _, url := range urls {
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

		allReleasesLocal = append(allReleasesLocal, releases...)
	}

	if len(allReleasesLocal) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	releaseNames := make([]string, 0, len(allReleasesLocal))
	seen := map[string]struct{}{}
	for _, release := range allReleasesLocal {
		v := strings.TrimSpace(release.TagName)
		if v == "" {
			v = strings.TrimSpace(release.Name)
		}
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		releaseNames = append(releaseNames, v)
	}

	// Sort the release names in semver order, most recent at the top
	// Uses shared.CompareVersions which considers SNAPSHOT more recent than other prereleases
	sort.Slice(releaseNames, func(i, j int) bool {
		return shared.CompareVersions(releaseNames[i], releaseNames[j])
	})

	// After sorting releases but before returning, log the latest versions
	var latestStable, latestPrerelease string
	for _, release := range releaseNames {
		version := shared.ExtractSemver(release)
		if !containsPreReleaseTag(version) {
			latestStable = version
			break // First one is latest since they're sorted
		}
	}
	for _, release := range releaseNames {
		version := shared.ExtractSemver(release)
		if containsPreReleaseTag(version) {
			latestPrerelease = version
			break // First one is latest since they're sorted
		}
	}
	log.Printf("Available from GitHub - Latest stable: %s, Latest prerelease: %s\n",
		latestStable, latestPrerelease)

	return releaseNames, nil
}

func populateReleaseSelect(selectWidget *widget.Select) {
	filteredReleases := []string{}
	stableReleases := []string{}
	for _, release := range allReleases {
		version := shared.ExtractSemver(release)
		if showPrereleases || !containsPreReleaseTag(version) {
			filteredReleases = append(filteredReleases, release)
		}
		if !containsPreReleaseTag(version) {
			stableReleases = append(stableReleases, release)
		}
	}
	if !showPrereleases && len(stableReleases) > 20 {
		filteredReleases = stableReleases[:20]
	}
	selectWidget.Options = filteredReleases
	selectWidget.Refresh()
}

func createReleaseDropdown(w fyne.Window) (*widget.Select, *fyne.Container) {
	selectWidget := widget.NewSelect([]string{}, func(selected string) {
		// Extract clean version from selected string
		version := shared.ExtractSemver(selected)
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

		dialog.ShowConfirm("Confirm Download",
			fmt.Sprintf("Do you want to download and install owlcms-firmata version %s?", selected),
			func(ok bool) {
				if !ok {
					return
				}

				shared.PromptForInstallVersionName(installDir, version, w, func(installVersion string) {
					// Show progress dialog with progress bar
					cancel := make(chan bool)
					progressDialog, progressBar := customdialog.NewDownloadDialog(
						"Installing owlcms-firmata",
						w,
						cancel)
					progressDialog.Show()

					go func() {
						extractPath := filepath.Join(owlcmsDir, installVersion)
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

						// Hide progress dialog before showing any error dialogs
						if err := EnsureParentEnvDefaults(); err != nil {
							log.Println("Installation completed but env.properties initialization failed")
							fyne.Do(func() {
								progressDialog.Hide()
								customdialog.ShowWideError(fmt.Errorf("installation completed but failed to create configuration file: %w\n\nYou may need to check permissions on the installation directory", err), w)
								setFirmataTabMode(w)
							})
							return
						}

						// Log before closing the dialog
						log.Println("Closing progress dialog")

						// Show success panel with installation details
						message := fmt.Sprintf(
							"Successfully installed owlcms-firmata version %s\n\n"+
								"Location: %s\n\n"+
								"The program files have been extracted to the above directory.",
							installVersion, extractPath)

						fyne.Do(func() {
							progressDialog.Hide()
							dialog.ShowInformation("Installation Complete", message, w)
							HideDownloadables()
							setFirmataTabMode(w)
						})
					}()
				})
			},
			w)
	})
	selectWidget.PlaceHolder = "Choose a release to download"

	// Always create a new checkbox bound to THIS select widget (dropdown is rebuilt at runtime).
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
			populateReleaseSelect(selectWidget)
			checkForNewerVersion()
			// Keep the dropdown visible while the user is toggling prereleases.
			ShowDownloadables()
			if downloadContainer != nil {
				downloadContainer.Refresh()
			}
		}()
	})
	prereleaseCheckbox.Hide()

	populateReleaseSelect(selectWidget)
	releaseDropdown = container.New(layout.NewHBoxLayout(), selectWidget, prereleaseCheckbox)
	releaseDropdown.Resize(fyne.NewSize(200, 200))

	return selectWidget, releaseDropdown
}

// setupReleaseDropdown initializes the release dropdown and populates it with available releases.
// It fetches releases on-demand if not already loaded.
func setupReleaseDropdown(w fyne.Window) {
	// Fetch releases on-demand if not already loaded
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("Firmata setupReleaseDropdown: fetchReleases failed: %v", err)
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

func containsPreReleaseTag(version string) bool {
	return shared.IsPrerelease(version)
}

func getMostRecentStableRelease() (string, error) {
	return shared.GetMostRecentStable(allReleases)
}

func getMostRecentPrerelease() (string, error) {
	return shared.GetMostRecentPrerelease(allReleases)
}

// InstallDefault performs the default install action for the Firmata package.
// It selects the most recent stable or pre-release version depending on the flag.
func InstallDefault(w fyne.Window, usePrerelease bool) {
	// Before installing, ensure Java runtime is available. Delegate to shared helper
	// which will call the package-local `checkJava` implementation.
	ver := GetTemurinVersion()
	if err := shared.CheckAndInstallJava(ver, statusLabel, w, checkJava); err != nil {
		return
	}

	// Fetch releases on-demand if not already loaded
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("Firmata InstallDefault: background fetchReleases failed: %v", err)
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
		// Start a first-install flow: hide download UI and kick off install.
		HideDownloadables()
		if statusLabel != nil {
			statusLabel.SetText("Installing Firmata " + latest)
			statusLabel.Show()
		}
		go downloadAndInstallVersion(latest, w)
	} else {
		track := "stable"
		if usePrerelease {
			track = "pre-release"
		}
		log.Printf("Firmata InstallDefault: no %s release found, showing download UI", track)
		ShowDownloadables()
	}
}
