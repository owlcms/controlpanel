package video

import (
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

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
	versionList *widget.List
)

func getAllInstalledVersions() []string {
	entries, err := os.ReadDir(installDir)
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
			baseVersion, _ := shared.ParseVersionWithBuild(entry.Name())
			v, err := semver.NewVersion(baseVersion)
			if err == nil {
				versions = append(versions, versionWithMeta{semver: v, original: entry.Name()})
			}
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[j].semver.LessThan(versions[i].semver)
	})

	var result []string
	for _, v := range versions {
		result = append(result, v.original)
	}
	return result
}

func findLatestInstalled() string {
	entries, err := os.ReadDir(installDir)
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
	var latest *semver.Version
	for _, dir := range getAllInstalledVersions() {
		v, err := semver.NewVersion(dir)
		if err == nil && !containsPreReleaseTag(dir) {
			if latest == nil || v.GreaterThan(latest) {
				latest = v
			}
		}
	}
	if latest != nil {
		return latest.String()
	}
	return ""
}

func findLatestPrereleaseInstalledVersion() string {
	var latest *semver.Version
	for _, dir := range getAllInstalledVersions() {
		v, err := semver.NewVersion(dir)
		if err == nil && containsPreReleaseTag(dir) {
			if latest == nil || v.GreaterThan(latest) {
				latest = v
			}
		}
	}
	if latest != nil {
		return latest.String()
	}
	return ""
}

func createVersionList(w fyne.Window) *widget.List {
	versions := getAllInstalledVersions()

	versionList = widget.NewList(
		func() int { return len(versions) },
		func() fyne.CanvasObject {
			label := widget.NewLabelWithStyle("LabelTemplate", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			videoBtn := widget.NewButton("ButtonTemplate", nil)
			videoBtn.Resize(fyne.NewSize(90, 25))
			videoBtn.Importance = widget.HighImportance
			buttonContainer := container.NewHBox(
				container.NewPadded(videoBtn),
				layout.NewSpacer(),
			)
			grid := container.New(layout.NewHBoxLayout(), container.NewGridWrap(fyne.NewSize(250, 25), label), buttonContainer)
			return grid
		},
		func(index widget.ListItemID, item fyne.CanvasObject) {
			version := versions[index]

			grid := item.(*fyne.Container)
			label := grid.Objects[0].(*fyne.Container).Objects[0].(*widget.Label)
			label.SetText(version)
			label.TextStyle = fyne.TextStyle{Bold: true}
			label.Refresh()

			buttonContainer := grid.Objects[1].(*fyne.Container)
			buttonContainer.RemoveAll()

			createVideoLaunchButton(w, version, buttonContainer)
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
			buttonContainer.Add(layout.NewSpacer())
			buttonContainer.Refresh()
		},
	)

	versionList.OnSelected = func(id widget.ListItemID) {
		if id < len(versions) {
			log.Printf("Selected video version: %s", versions[id])
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

// NewGreenButton creates a button styled with SuccessImportance (dark green)
func NewGreenButton(label string, tapped func()) *widget.Button {
	btn := widget.NewButton(label, tapped)
	btn.Importance = widget.SuccessImportance
	return btn
}

func createVideoLaunchButton(w fyne.Window, version string, buttonContainer *fyne.Container) {
	launchButton := NewGreenButton("Video", nil)
	launchButton.OnTapped = func() {
		if videoProcess != nil {
			dialog.ShowError(fmt.Errorf("video is already running"), w)
			return
		}
		log.Printf("Launching video %s", version)
		go func() {
			// Blocking download, must stay off the Fyne main goroutine.
			if _, err := shared.EnsureFFmpegPrerequisite(w); err != nil {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("FFmpeg prerequisite: %w", err), w)
				})
				return
			}
			fyne.Do(func() {
				if err := launchVideo(version, launchButton, w); err != nil {
					dialog.ShowError(err, w)
				}
			})
		}()
	}
	buttonContainer.Add(container.NewPadded(launchButton))
}

func createFilesButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	filesButton := widget.NewButton("Files", nil)
	filesButton.OnTapped = func() {
		versionDir := filepath.Join(installDir, version)
		if err := shared.OpenFileExplorer(versionDir); err != nil {
			dialog.ShowError(fmt.Errorf("failed to open directory: %w", err), w)
		}
	}
	buttonContainer.Add(container.NewPadded(filesButton))
}

func createUpdateButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	updateButton := widget.NewButton("Update", nil)

	latestStable, stableErr := getMostRecentStableRelease()
	latestPrerelease, preErr := getMostRecentPrerelease()
	latestStableInstalled := findLatestStableInstalledVersion()
	latestPrereleaseInstalled := findLatestPrereleaseInstalledVersion()

	if (!containsPreReleaseTag(latestStableInstalled) && stableErr == nil && latestStableInstalled == latestStable) ||
		(containsPreReleaseTag(latestPrereleaseInstalled) && preErr == nil && latestPrereleaseInstalled == latestPrerelease) {
		return
	}

	var mostRecent string
	var err error
	if !containsPreReleaseTag(version) {
		mostRecent, err = getMostRecentStableRelease()
	} else {
		mostRecent, err = getMostRecentPrerelease()
	}
	if err != nil {
		log.Printf("failed to get most recent release: %v", err)
		return
	}

	if shared.CompareVersions(mostRecent, version) {
		updateButton.SetText(fmt.Sprintf("Update to %s", mostRecent))
		updateButton.OnTapped = func() {
			go updateVersion(version, mostRecent, w)
		}
		updateButton.Refresh()
		buttonContainer.Add(container.NewPadded(updateButton))
	}
}

func createImportButton(versions []string, version string, w fyne.Window, buttonContainer *fyne.Container) {
	importButton := widget.NewButton("Import", nil)
	importButton.OnTapped = func() {
		sourceVersions := filterVersions(versions, version)
		sourceDropdown := widget.NewSelect(sourceVersions, func(selected string) {})
		label := widget.NewLabel("Copy configuration from a previous version")
		label.Wrapping = fyne.TextWrapWord
		selectContainer := container.NewGridWrap(fyne.NewSize(420, 35), sourceDropdown)
		content := container.NewVBox(label, selectContainer)

		d := dialog.NewCustomConfirm("Import Config", "Import", "Cancel", content, func(ok bool) {
			if !ok {
				return
			}
			sourceVersion := sourceDropdown.Selected
			if sourceVersion == "" {
				dialog.ShowError(fmt.Errorf("source version cannot be empty"), w)
				return
			}
			sourceDir := filepath.Join(installDir, sourceVersion)
			destDir := filepath.Join(installDir, version)
			if err := copyVersionConfigArtifacts(sourceDir, destDir); err != nil {
				log.Printf("No config to copy from %s: %v", sourceDir, err)
			}
			dialog.ShowInformation("Import Complete",
				fmt.Sprintf("Imported config from %s to %s", sourceVersion, version), w)
		}, w)
		d.Show()
	}
	buttonContainer.Add(container.NewPadded(importButton))
}

func createRemoveButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	removeButton := widget.NewButton("Remove", nil)
	removeButton.OnTapped = func() {
		dialog.ShowConfirm("Confirm Remove",
			fmt.Sprintf("Remove video tools version %s?", version),
			func(ok bool) {
				if !ok {
					return
				}
				versionDir := filepath.Join(installDir, version)
				if err := os.RemoveAll(versionDir); err != nil {
					dialog.ShowError(fmt.Errorf("failed to remove version: %w", err), w)
					return
				}
				recomputeVersionList(w)
				checkForNewerVersion()
			}, w)
	}
	buttonContainer.Add(container.NewPadded(removeButton))
}

func filterVersions(versions []string, current string) []string {
	var filtered []string
	for _, v := range versions {
		if v != current {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// copyVersionConfigArtifacts copies the per-version config owned by the video app.
// ffmpeg.toml lives in the shared control panel config directory and is not copied.
func copyVersionConfigArtifacts(srcDir, destDir string) error {
	copiedAny := false

	for _, name := range []string{"cameras.toml", "replays.toml", "auto.toml"} {
		copied, err := copyFileIfExists(filepath.Join(srcDir, name), filepath.Join(destDir, name))
		if err != nil {
			return err
		}
		if copied {
			copiedAny = true
		}
	}

	if copied, err := copyDirIfExists(filepath.Join(srcDir, "config"), filepath.Join(destDir, "config")); err != nil {
		return err
	} else if copied {
		copiedAny = true
	}

	if !copiedAny {
		return fmt.Errorf("no config artifacts found in %s", srcDir)
	}

	return nil
}

func copyFileIfExists(srcPath, destPath string) (bool, error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("expected file but found directory: %s", srcPath)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return false, err
	}
	defer srcFile.Close()

	if err := shared.EnsureDir0755(filepath.Dir(destPath)); err != nil {
		return false, err
	}

	destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return false, err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return false, err
	}

	return true, nil
}

func copyDirIfExists(srcDir, destDir string) (bool, error) {
	info, err := os.Stat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("expected directory but found file: %s", srcDir)
	}

	if err := copyFiles(srcDir, destDir, true); err != nil {
		return false, err
	}

	return true, nil
}

func copyFiles(srcDir, destDir string, alwaysCopy bool) error {
	var srcDirModTime time.Time
	if !alwaysCopy {
		info, err := os.Stat(srcDir)
		if err != nil {
			return err
		}
		srcDirModTime = info.ModTime()
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

		if !alwaysCopy && info.ModTime().Before(srcDirModTime) {
			return nil
		}

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

// RefreshVersionList refreshes the version list display
func RefreshVersionList(w fyne.Window) {
	recomputeVersionList(w)
}

func recomputeVersionList(w fyne.Window) {
	log.Println("Reinitializing video version list")
	versionContainer.Objects = nil
	newVersionList := createVersionList(w)

	numVersions := len(getAllInstalledVersions())
	versionScroll := container.NewVScroll(newVersionList)
	versionScroll.SetMinSize(fyne.NewSize(0, computeVersionScrollHeight(numVersions)))
	center := container.NewStack(versionScroll)

	if numVersions == 0 {
		resetToExplainMode(w)
		return
	}
	versionScroll.Show()
	versionContainer.Show()

	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 8))
	content := container.NewVBox(spacer, center)

	versionContainer.Objects = nil
	versionContainer.Add(content)
	updateExplanation()
	log.Println("Video version list reinitialized")
}

func computeVersionScrollHeight(numVersions int) float32 {
	minHeight := 140
	rowHeight := 50
	return float32(minHeight + (rowHeight * min(numVersions, 4)))
}

func uninstallAll() {
	dialog.ShowConfirm("Confirm Uninstall",
		"This will remove all video tools data and configurations.\nThis cannot be undone.",
		func(confirm bool) {
			if !confirm {
				return
			}
			if err := os.RemoveAll(installDir); err != nil {
				dialog.ShowError(fmt.Errorf("failed to remove data: %w", err), mainWindow)
				return
			}
			dialog.ShowInformation("Success", "All video tools data removed", mainWindow)
			recomputeVersionList(mainWindow)
		}, mainWindow)
}
