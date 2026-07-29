package owlcms

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// Release represents a GitHub release
type Release struct {
	// GitHub Releases API fields
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

var (
	showPrereleases      bool = false
	allReleases          []string
	releaseDropdown      *fyne.Container
	prereleaseCheckbox   *widget.Check
	updateTitle          *widget.RichText
	downloadButtonTitle  *widget.Hyperlink
	updateTitleContainer *fyne.Container
	installAvailableLink *widget.Hyperlink
	releaseNotesLink     *widget.Hyperlink
	availableVersion     string
	fetchReleasesFromURL = func(url string) ([]Release, error) {
		client := &http.Client{Timeout: 5 * time.Second}
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
		return releases, nil
	}
)

func fetchReleases() ([]string, error) {
	return fetchReleasesForCatalog(catalogNeedsPrereleases(showPrereleases, getAllInstalledVersions()))
}

func catalogNeedsPrereleases(explicitRequest bool, installedVersions []string) bool {
	if explicitRequest {
		return true
	}
	return slices.ContainsFunc(installedVersions, containsPreReleaseTag)
}

func releaseCatalogHasPrerelease(releases []string) bool {
	return slices.ContainsFunc(releases, containsPreReleaseTag)
}

func fetchReleasesForCatalog(includePrereleases bool) ([]string, error) {

	stableURL := "https://api.github.com/repos/owlcms/releases/releases"
	preURL := "https://api.github.com/repos/owlcms/prereleases/releases"

	stable, err := fetchReleasesFromURL(stableURL)
	if err != nil {
		return nil, err
	}
	all := append([]Release{}, stable...)
	if includePrereleases {
		pre, err := fetchReleasesFromURL(preURL)
		if err != nil {
			return nil, err
		}
		all = append(all, pre...)
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	// Prefer tag_name (always machine-readable), fall back to name.
	seen := map[string]struct{}{}
	releaseNames := make([]string, 0, len(all))
	for _, r := range all {
		v := strings.TrimSpace(r.TagName)
		if v == "" {
			v = strings.TrimSpace(r.Name)
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

func downloadReleaseWithProgress(downloadVersion, installVersion string, w fyne.Window, isInitialDownload bool) {
	var progressBar *widget.ProgressBar
	var messageLabel *widget.Label
	var progressDialog dialog.Dialog

	if w != nil {
		progressBar = widget.NewProgressBar()
		messageLabel = widget.NewLabel(fmt.Sprintf("Downloading OWLCMS %s...", downloadVersion))
		progressContent := container.NewVBox(messageLabel, progressBar)
		progressDialog = dialog.NewCustom(
			"Installing OWLCMS",
			"Please wait...",
			progressContent,
			w)
		progressDialog.Show()
	} else {
		log.Printf("Downloading OWLCMS %s...", downloadVersion)
		fmt.Printf("Downloading OWLCMS %s...\n", downloadVersion)
	}

	go func() {
		var dialogClosed bool
		closeDialog := func() {
			if !dialogClosed && progressDialog != nil {
				dialogClosed = true
				fyne.Do(func() {
					progressDialog.Hide()
				})
			}
		}
		defer closeDialog()

		progressCallback := func(downloaded, total int64) {
			if total > 0 {
				progress := float64(downloaded) / float64(total)
				if w != nil {
					fyne.Do(func() {
						progressBar.SetValue(progress)
						messageLabel.SetText(fmt.Sprintf("Downloading OWLCMS %s... %.1f%%", downloadVersion, progress*100))
						messageLabel.Refresh()
					})
				} else {
					log.Printf("Downloading OWLCMS %s... %.1f%%", downloadVersion, progress*100)
					fmt.Printf("\rDownloading OWLCMS %s... %.1f%%", downloadVersion, progress*100)
				}
			}
		}

		result, err := InstallRelease(downloadVersion, installVersion, progressCallback)
		if err != nil {
			log.Printf("Install failed: %v", err)
			if w != nil {
				installErr := err
				fyne.Do(func() {
					dialog.ShowError(installErr, w)
				})
			} else {
				log.Printf("Error: %v", err)
			}
			return
		}

		if w != nil {
			fyne.Do(func() {
				messageLabel.SetText("Extracting files...")
				messageLabel.Refresh()
			})
		}

		message := fmt.Sprintf(
			"Successfully installed OWLCMS version %s\n\n"+
				"Location: %s\n\n"+
				"The program files have been extracted to the above directory.",
			result.Version, result.Path)

		log.Printf("Download and extraction complete for version %s, showing dialog", result.Version)
		if w != nil {
			fyne.Do(func() {
				dialog.ShowInformation("Installation Complete", message, w)
				_ = isInitialDownload // keep parameter stable even if caller distinguishes paths
				HideDownloadables()
				setOwlcmsTabMode(w)

				log.Printf("Refreshing window content")
				w.Content().Refresh()
			})
		} else {
			log.Println("Installation Complete:\n" + message)
			fmt.Println("Installation Complete:\n" + message)
		}
	}()
}

// normalizeExtractedDir flattens a single top-level directory if the archive contained one.
// This keeps the install directory consistent even when installing under a renamed folder.
func normalizeExtractedDir(extractPath string) error {
	entries, err := os.ReadDir(extractPath)
	if err != nil {
		return err
	}

	var subdirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			subdirs = append(subdirs, entry)
			continue
		}
		// If any file exists at the top level, assume layout is already correct.
		return nil
	}

	if len(subdirs) != 1 {
		return nil
	}

	childPath := filepath.Join(extractPath, subdirs[0].Name())
	childEntries, err := os.ReadDir(childPath)
	if err != nil {
		return err
	}

	for _, entry := range childEntries {
		src := filepath.Join(childPath, entry.Name())
		dst := filepath.Join(extractPath, entry.Name())
		if _, statErr := os.Stat(dst); statErr == nil {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}

	return os.Remove(childPath)
}

// GetReleaseTagURL returns the GitHub releases/tag URL for the given version string,
// using the prerelease repository when the version indicates a prerelease.
func GetReleaseTagURL(version string) string {
	if containsPreReleaseTag(version) {
		return fmt.Sprintf("https://github.com/owlcms/owlcms4-prerelease/releases/tag/%s", version)
	}
	return fmt.Sprintf("https://github.com/owlcms/owlcms4/releases/tag/%s", version)
}

func confirmAndDownloadVersion(version string, w fyne.Window) {
	dialog.ShowConfirm("Confirm Download",
		fmt.Sprintf("Do you want to download and install OWLCMS version %s?", version),
		func(ok bool) {
			if ok {
				shared.PromptForInstallVersionName(installDir, version, w, func(newVersion string) {
					downloadReleaseWithProgress(version, newVersion, w, false)
				})
			}
		},
		w)
}

func createReleaseDropdown(w fyne.Window) (*widget.Select, *fyne.Container) {
	selectWidget := widget.NewSelect([]string{}, func(selected string) {
		confirmAndDownloadVersion(selected, w)
	})
	selectWidget.PlaceHolder = "Choose a release to download"

	// IMPORTANT: The release dropdown is rebuilt at runtime (e.g. via setOwlcmsTabModeInstalled).
	// If the checkbox were a singleton, its callback could capture an old select widget.
	// Always create a new checkbox bound to THIS select widget.
	prereleaseCheckbox = widget.NewCheck("Show Prereleases", func(checked bool) {
		showPrereleases = checked
		// Refetch so OWLCMS can include the separate prerelease repository when enabled.
		go func() {
			releases, err := fetchReleases()
			if err != nil {
				log.Printf("failed to fetch releases after prerelease toggle: %v", err)
				return
			}
			fyne.Do(func() {
				allReleases = releases
				populateReleaseSelect(selectWidget)
				checkForNewerVersion()
				// Keep the dropdown visible while the user is toggling prereleases.
				ShowDownloadables()
				if downloadContainer != nil {
					downloadContainer.Refresh()
				}
			})
		}()
	})
	prereleaseCheckbox.Hide()

	populateReleaseSelect(selectWidget)
	horiz := container.New(layout.NewHBoxLayout(), selectWidget, prereleaseCheckbox)
	releaseDropdown = horiz
	releaseDropdown.Resize(fyne.NewSize(300, 200))

	return selectWidget, releaseDropdown
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

// InstallDefault performs the default install action for the OWLCMS package.
// It selects the most recent stable or pre-release version depending on the flag.
func InstallDefault(w fyne.Window, usePrerelease bool) {
	// Before installing, ensure Java runtime is available. Delegate to shared helper
	// which will call the package-local `checkJava` implementation.
	if err := EnsureParentEnvDefaults(); err != nil {
		dialog.ShowError(fmt.Errorf("failed to initialize env.properties: %w", err), w)
		return
	}
	ver := GetTemurinVersion()
	if err := shared.CheckAndInstallJava(ver, statusLabel, w, checkJava); err != nil {
		return
	}

	// Fetch releases on-demand. A prerelease install request must fetch the
	// prerelease catalog even on first install, before any prerelease is installed.
	needPrereleaseCatalog := catalogNeedsPrereleases(usePrerelease || showPrereleases, getAllInstalledVersions())
	if len(allReleases) == 0 || (needPrereleaseCatalog && !releaseCatalogHasPrerelease(allReleases)) {
		if r, err := fetchReleasesForCatalog(needPrereleaseCatalog); err == nil {
			allReleases = r
		} else {
			log.Printf("OWLCMS InstallDefault: background fetchReleases failed: %v", err)
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
		log.Printf("OWLCMS InstallDefault: no %s release found, showing download UI", track)
		ShowDownloadables()
	}
}

func setupReleaseDropdown(w fyne.Window) {
	// Fetch releases on-demand if not already loaded
	if len(allReleases) == 0 {
		if r, err := fetchReleases(); err == nil {
			allReleases = r
		} else {
			log.Printf("OWLCMS setupReleaseDropdown: fetchReleases failed: %v", err)
		}
	}

	selectWidget, dropdownContainer := createReleaseDropdown(w)
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
