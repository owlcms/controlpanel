package firmata

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	customdialog "controlpanel/firmata/dialog"
	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/Masterminds/semver/v3"
)

var (
	versionList     *widget.List
	latestInstalled string
)

func getAllInstalledVersions() []string {
	owlcmsDir := installDir
	entries, err := os.ReadDir(owlcmsDir)
	if err != nil {
		return nil
	}

	type versionWithMeta struct {
		semver   *semver.Version
		original string
	}

	var versions []versionWithMeta
	for _, entry := range entries {
		if entry.IsDir() {
			// Try to parse the directory name, stripping build metadata for comparison
			baseVersion, _ := shared.ParseVersionWithBuild(entry.Name())
			v, err := semver.NewVersion(baseVersion)
			if err == nil {
				versions = append(versions, versionWithMeta{
					semver:   v,
					original: entry.Name(),
				})
			}
		}
	}

	// Sort by semver (build metadata is ignored in comparison)
	sort.Slice(versions, func(i, j int) bool {
		return versions[j].semver.LessThan(versions[i].semver)
	})

	var versionStrings []string
	for _, v := range versions {
		versionStrings = append(versionStrings, v.original)
	}

	return versionStrings
}

func findLatestInstalled() string {
	owlcmsDir := installDir
	entries, err := os.ReadDir(owlcmsDir)
	if err != nil {
		return ""
	}

	var versions []*semver.Version
	for _, entry := range entries {
		if entry.IsDir() {
			v, err := semver.NewVersion(entry.Name())
			if err == nil {
				versions = append(versions, v)
			}
		}
	}

	if len(versions) == 0 {
		return ""
	}

	sort.Sort(sort.Reverse(semver.Collection(versions)))
	return versions[0].String()
}

func findLatestStableInstalledVersion() string {
	var latestStable *semver.Version
	for _, dir := range getAllInstalledVersions() {
		// Clean the version string before processing
		version := shared.ExtractSemver(dir)
		v, err := semver.NewVersion(version)
		if err == nil && !containsPreReleaseTag(version) {
			if latestStable == nil || v.GreaterThan(latestStable) {
				latestStable = v
			}
		}
	}
	if latestStable != nil {
		return latestStable.String()
	}
	return ""
}

func findLatestPrereleaseInstalledVersion() string {
	owlcmsDir := installDir
	entries, err := os.ReadDir(owlcmsDir)
	if err != nil {
		return ""
	}

	var versions []*semver.Version
	for _, entry := range entries {
		if entry.IsDir() {
			v, err := semver.NewVersion(entry.Name())
			if err == nil && v.Prerelease() != "" {
				versions = append(versions, v)
			}
		}
	}

	if len(versions) == 0 {
		return ""
	}

	sort.Sort(sort.Reverse(semver.Collection(versions)))
	return versions[0].String()
}

func createVersionList(w fyne.Window) *widget.List {
	versions := getAllInstalledVersions()

	versionList = widget.NewList(
		func() int { return len(versions) },
		func() fyne.CanvasObject {
			// Template item for the version list. Used to compute sizes
			label := widget.NewLabelWithStyle("LabelTemplate", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			launchButton := widget.NewButton("ButtonTemplate", nil)
			launchButton.Resize(fyne.NewSize(80, 25))
			launchButton.Importance = widget.HighImportance
			buttonContainer := container.NewHBox(
				container.NewPadded(launchButton),
				layout.NewSpacer(), // Add spacer to push buttons to the left
			)
			grid := container.New(layout.NewHBoxLayout(), container.NewGridWrap(fyne.NewSize(250, 25), label), buttonContainer)
			return grid
		},
		func(index widget.ListItemID, item fyne.CanvasObject) {
			// This function is called for each item in the list to build the actual entries
			version := versions[index]

			grid := item.(*fyne.Container)

			label := grid.Objects[0].(*fyne.Container).Objects[0].(*widget.Label)
			label.SetText(version)
			label.TextStyle = fyne.TextStyle{Bold: true} // Make the version number bold
			label.Refresh()

			buttonContainer := grid.Objects[1].(*fyne.Container)
			buttonContainer.RemoveAll()

			createLaunchButton(w, version, buttonContainer)
			createFilesButton(version, w, buttonContainer)
			if len(allReleases) > 0 {
				createUpdateButton(version, w, buttonContainer)
			}
			if len(versions) > 1 {
				createImportButton(versions, version, w, buttonContainer)
			}
			shared.CreateDuplicateButton(installDir, version, w, buttonContainer, func(newVersion string) {
				recomputeVersionList(w)
			})
			shared.CreateRenameButton(installDir, version, w, buttonContainer, func(newVersion string) {
				recomputeVersionList(w)
			})
			createRemoveButton(version, w, buttonContainer)
			buttonContainer.Add(layout.NewSpacer()) // Add spacer to push buttons to the left
			buttonContainer.Refresh()
		},
	)

	versionList.OnSelected = func(id widget.ListItemID) {
		if id < len(versions) {
			log.Printf("Selected version: %s\n", versions[id])
		}
	}

	if len(versions) > 0 {
		versionList.Select(0)
	}

	if latest := findLatestInstalled(); latest != "" {
		for i, v := range versions {
			if v == latest {
				versionList.Select(i)
				break
			}
		}
	}

	return versionList
}

func createImportButton(versions []string, version string, w fyne.Window, buttonContainer *fyne.Container) {
	importButton := widget.NewButton("Import", nil)
	importButton.Show()
	importButton.OnTapped = func() {
		// Open a dialog to select the source version
		sourceVersions := filterVersions(versions, version) // Filter out the current version
		sourceVersionDropdown := widget.NewSelect(sourceVersions, func(selected string) {})

		label := widget.NewLabel("Copy the configuration from a previous installation")
		label.Wrapping = fyne.TextWrapWord
		selectContainer := container.NewGridWrap(fyne.NewSize(420, 35), sourceVersionDropdown)
		content := container.NewVBox(label, selectContainer)

		d := dialog.NewCustomConfirm("Import Config",
			"Import",
			"Cancel",
			content,
			func(ok bool) {
				if !ok {
					return
				}

				sourceVersion := sourceVersionDropdown.Selected
				if sourceVersion == "" {
					dialog.ShowError(fmt.Errorf("source version cannot be empty"), w)
					return
				}

				sourceDir := filepath.Join(installDir, sourceVersion)
				destDir := filepath.Join(installDir, version)

				if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
					dialog.ShowError(fmt.Errorf("source version %s does not exist", sourceVersion), w)
					return
				}

				// Copy config files
				if err := copyFiles(filepath.Join(sourceDir, "config"), filepath.Join(destDir, "config"), true); err != nil {
					log.Printf("No config files to copy from %s\n", sourceDir)
				}

				// Copy version-specific env.properties if it exists
				srcEnv := filepath.Join(sourceDir, "env.properties")
				if _, err := os.Stat(srcEnv); err == nil {
					destEnv := filepath.Join(destDir, "env.properties")
					if err := copyFile(srcEnv, destEnv); err != nil {
						log.Printf("Failed to copy env.properties: %v\n", err)
					}
				}

				dialog.ShowInformation("Import Complete", fmt.Sprintf("Successfully imported config from version %s to version %s", sourceVersion, version), w)
			},
			w)
		d.Show()
	}
	buttonContainer.Add(container.NewPadded(importButton))
}

func createUpdateButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	updateButton := widget.NewButton("Update", nil)
	var mostRecent string
	var err error

	latestStable, stableErr := getMostRecentStableRelease()
	latestPrerelease, preErr := getMostRecentPrerelease()
	latestStableInstalled := findLatestStableInstalledVersion()
	latestPrereleaseInstalled := findLatestPrereleaseInstalledVersion()

	if (!containsPreReleaseTag(latestStableInstalled) && stableErr == nil && latestStableInstalled == latestStable) ||
		(containsPreReleaseTag(latestPrereleaseInstalled) && preErr == nil && latestPrereleaseInstalled == latestPrerelease) {
		// there is no point in updating since the most recent version is already installed
		return
	}

	// Check if the current version is stable or a prerelease
	if !containsPreReleaseTag(version) {
		mostRecent, err = getMostRecentStableRelease()
		if err == nil {
			adjustUpdateButton(mostRecent, version, updateButton, buttonContainer, w)
		}
	} else {
		mostRecent, err = getMostRecentPrerelease()
		if err == nil {
			adjustUpdateButton(mostRecent, version, updateButton, buttonContainer, w)
		} else {
			log.Printf("failed to get most recent prerelease: %v", err)
		}
	}
	buttonContainer.Add(container.NewPadded(updateButton))
}

func createRemoveButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	removeButton := widget.NewButton("Remove", nil)
	removeButton.OnTapped = func() {
		dialog.ShowConfirm("Confirm Remove",
			fmt.Sprintf("Do you want to remove owlcms-firmata version %s?", version),
			func(ok bool) {
				if !ok {
					return
				}

				log.Printf("Removing version %s\n", version)
				err := os.RemoveAll(filepath.Join(installDir, version))
				if err != nil {
					dialog.ShowError(fmt.Errorf("failed to remove owlcms-firmata %s: %w", version, err), w)
					return
				}

				// Recompute the version list
				log.Print("Reinitializing version list")
				recomputeVersionList(w)

				// Check if a more recent version is available
				latestInstalled = findLatestInstalled()
				log.Printf("latestInstalled: %s\n", latestInstalled)
				checkForNewerVersion()
				downloadContainer.Refresh()
			},
			w)
	}
	buttonContainer.Add(container.NewPadded(removeButton))
}

func createFilesButton(version string, w fyne.Window, buttonContainer *fyne.Container) *widget.Button {
	filesButton := widget.NewButton("Files", nil)
	filesButton.Resize(fyne.NewSize(80, 25))
	filesButton.SetText("Files")
	filesButton.OnTapped = func() {
		versionDir := filepath.Join(installDir, version)
		if err := shared.OpenFileExplorer(versionDir); err != nil {
			dialog.ShowError(fmt.Errorf("failed to open file explorer for %s: %w", versionDir, err), w)
		}
	}
	buttonContainer.Add(container.NewPadded(filesButton))
	return filesButton
}

// NewGreenButton creates a button with danger importance (dark red)
func NewGreenButton(label string, tapped func()) *widget.Button {
	btn := widget.NewButton(label, tapped)
	btn.Importance = widget.DangerImportance
	return btn
}

func createLaunchButton(w fyne.Window, version string, buttonContainer *fyne.Container) {
	launchButton := NewGreenButton("Launch", nil)
	launchButton.OnTapped = func() {
		if currentProcess != nil {
			dialog.ShowError(fmt.Errorf("owlcms-firmata is already running"), w)
			return
		}

		log.Printf("Launching version %s\n", version)
		if !EnsureEnvWithDialog(w) {
			return
		}
		// Get version-specific Temurin version
		ver := GetTemurinVersionForRelease(version)
		if err := shared.CheckAndInstallJava(ver, statusLabel, w, checkJava); err != nil {
			return
		}

		if err := launchFirmata(version, launchButton); err != nil {
			var ext *ExternalPortOwnerError
			if errors.As(err, &ext) {
				dialog.ShowConfirm(
					"Process already running",
					fmt.Sprintf("A process (PID %d) is already using port %s.\n\nThis may be a previous owlcms-firmata that did not shut down cleanly, or it may be running on purpose.\n\nForcefully terminate it and launch %s?", ext.PID, ext.Port, version),
					func(confirm bool) {
						if !confirm {
							return
						}
						if killErr := ForceFreeJavaPort(); killErr != nil {
							dialog.ShowError(killErr, w)
							return
						}
						if retryErr := launchFirmata(version, launchButton); retryErr != nil {
							dialog.ShowError(retryErr, w)
						}
					},
					w,
				)
				return
			}
			dialog.ShowError(err, w)
			return
		}
	}
	buttonContainer.Add(container.NewPadded(launchButton))
}

func adjustUpdateButton(mostRecent string, version string, updateButton *widget.Button, buttonContainer *fyne.Container, w fyne.Window) {
	if shared.CompareVersions(mostRecent, version) {
		updateButton.SetText(fmt.Sprintf("Update to %s", mostRecent))
		updateButton.OnTapped = func() {
			go updateVersion(version, mostRecent, w)
		}
		updateButton.Refresh()
	} else {
		buttonContainer.Refresh()
	}
}

func updateVersion(existingVersion string, targetVersion string, w fyne.Window) {
	// Note the timestamp of the current version's top-level directory
	currentVersionDir := filepath.Join(installDir, existingVersion)
	targetBaseVersion, _ := shared.ParseVersionWithBuild(targetVersion)
	existingBuild := shared.GetCurrentBuildString(existingVersion)
	targetInstallVersion := targetVersion
	if existingBuild != "" {
		resolvedBuild := shared.ResolveCollisionForBuild(installDir, targetBaseVersion, existingBuild)
		targetInstallVersion = fmt.Sprintf("%s+%s", targetBaseVersion, resolvedBuild)
	}

	// Download and extract the version given by string
	var urlPrefix string
	if containsPreReleaseTag(targetVersion) {
		urlPrefix = "https://github.com/jflamy/owlcms-firmata/releases/download"
	} else {
		urlPrefix = "https://github.com/jflamy/owlcms-firmata/releases/download"
	}
	fileName := "owlcms-firmata.jar"
	jarURL := fmt.Sprintf("%s/%s/%s", urlPrefix, targetVersion, fileName)

	extractDir := filepath.Join(installDir, targetInstallVersion)
	if err := shared.EnsureDir0755(extractDir); err != nil {
		fyne.Do(func() {
			dialog.ShowError(fmt.Errorf("creating install directory: %w", err), w)
		})
		return
	}
	extractPath := filepath.Join(extractDir, fileName)

	cancel := make(chan bool)
	var progressDialog dialog.Dialog
	var progressBar *widget.ProgressBar
	fyne.DoAndWait(func() {
		progressDialog, progressBar = customdialog.NewDownloadDialog(
			"Updating owlcms-firmata",
			w,
			cancel)
		progressDialog.Show()
	})

	progressCallback := func(downloaded, total int64) {
		if total > 0 {
			percentage := float64(downloaded) / float64(total)
			fyne.Do(func() {
				progressBar.SetValue(percentage)
			})
		}
	}
	err := shared.DownloadArchive(jarURL, extractPath, progressCallback, cancel)
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

	// Copy config files from the current version to the new version
	newConfig := filepath.Join(extractDir, "config")
	oldConfig := filepath.Join(currentVersionDir, "config")
	log.Printf("Copying config files from %s to %s\n", oldConfig, newConfig)
	err = copyFiles(oldConfig, newConfig, true)
	if err != nil {
		log.Printf("No config files to copy from %s\n", currentVersionDir)
	}

	// Copy version-specific env.properties if it exists
	srcEnv := filepath.Join(currentVersionDir, "env.properties")
	if _, statErr := os.Stat(srcEnv); statErr == nil {
		destEnv := filepath.Join(extractDir, "env.properties")
		if copyErr := copyFile(srcEnv, destEnv); copyErr != nil {
			log.Printf("Failed to copy env.properties: %v\n", copyErr)
		}
	}

	fyne.Do(func() {
		progressDialog.Hide()
		dialog.ShowInformation("Update Complete", fmt.Sprintf("Successfully updated to version %s", targetInstallVersion), w)
		recomputeVersionList(w)
		latestInstalled = findLatestInstalled()
		checkForNewerVersion()
	})
}

func filterVersions(versions []string, curVersion string) []string {
	var filtered []string
	for _, version := range versions {
		if version != curVersion {
			filtered = append(filtered, version)
		}
	}
	return filtered
}

func copyFiles(srcDir, destDir string, alwaysCopy bool) error {
	var localDirModTime time.Time
	if !alwaysCopy {
		srcLocalDir := srcDir
		info, err := os.Stat(srcLocalDir)
		if err != nil {
			return err
		}
		localDirModTime = info.ModTime()
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destDir, relPath)

		if info.IsDir() {
			return shared.EnsureDir0755(destPath)
		}

		if !alwaysCopy {
			log.Printf("Comparing file timestamps: %s %s %s %s\n", path, info.ModTime(), destPath, localDirModTime)
			if info.ModTime().Before(localDirModTime) {
				// Skip copying if the file is older than the local directory timestamp
				return nil
			}
		}

		log.Printf("Copying file: %s to %s\n", path, destPath) // Log file names being copied

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		if err := shared.EnsureDir0755(filepath.Dir(destPath)); err != nil {
			return err
		}
		destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		return err
	})
}

// copyFile copies a single file from src to dest
func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// RefreshVersionList refreshes the version list display
func RefreshVersionList(w fyne.Window) {
	recomputeVersionList(w)
}

func recomputeVersionList(w fyne.Window) {
	log.Println("Reinitializing version list")
	versionContainer.Objects = nil // Clear the container
	newVersionList := createVersionList(w)

	numVersions := len(getAllInstalledVersions())
	versionScroll := container.NewVScroll(newVersionList)
	// Ensure the version scroll has enough height to display up to 4 rows
	versionScroll.SetMinSize(fyne.NewSize(0, computeVersionScrollHeight(numVersions)))
	center := container.NewStack(versionScroll)

	if numVersions == 0 {
		// Reset the tab to explanation mode so download UI is cleared first
		resetToExplainMode(w)
		// No version list to add — return now so we don't overwrite the explanation
		return
	} else {
		versionScroll.Show()
		versionContainer.Show()
	}

	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 8))
	content := container.NewVBox(spacer, center)

	versionContainer.Objects = nil
	versionContainer.Add(content)
	log.Println("Version list reinitialized")
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
	log.Printf("Updating explanation %d\n", len(x))
	if len(x) == 0 {
		downloadContainer.Remove(singleOrMultiVersionLabel)
		downloadContainer.Refresh()
	} else if len(x) == 1 {
		latestStable, stableErr := getMostRecentStableRelease()
		latestPrerelease, preErr := getMostRecentPrerelease()

		// Remove the label from the container first
		downloadContainer.Remove(singleOrMultiVersionLabel)

		if containsPreReleaseTag(x[0]) {
			if preErr == nil && x[0] == latestPrerelease {
				// It's the latest prerelease; do not re-insert the label
			} else {
				// Not the latest; re-insert singleOrMultiVersionLabel as second item
				if len(downloadContainer.Objects) > 0 {
					downloadContainer.Objects = append(downloadContainer.Objects[:1], append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects[1:]...)...)
				} else {
					downloadContainer.Objects = append([]fyne.CanvasObject{singleOrMultiVersionLabel}, downloadContainer.Objects...)
				}
				singleOrMultiVersionLabel.SetText("Use the Update button above to install the latest version. The current database will be copied to the new version, as well as local changes made to the configuration since the previous installation.")
			}
		} else {
			if stableErr == nil && x[0] == latestStable {
				// It's the latest stable; do not re-insert the label
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
