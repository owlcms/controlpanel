package owlcms

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	customdialog "controlpanel/owlcms/dialog"
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

// GetAllInstalledVersions returns all installed versions sorted by semver descending.
func GetAllInstalledVersions() []string {
	return getAllInstalledVersions()
}

func findLatestInstalled() string {
	owlcmsDir := installDir
	entries, err := os.ReadDir(owlcmsDir)
	if err != nil {
		return ""
	}

	// Accept any valid semver directory name
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

func findLatestStableInstalled() string {
	owlcmsDir := installDir
	entries, err := os.ReadDir(owlcmsDir)
	if err != nil {
		return ""
	}

	var versions []*semver.Version
	for _, entry := range entries {
		if entry.IsDir() {
			v, err := semver.NewVersion(entry.Name())
			if err == nil && v.Prerelease() == "" {
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

func findLatestPrereleaseInstalled() string {
	owlcmsDir := installDir
	entries, err := os.ReadDir(owlcmsDir)
	if err != nil {
		return ""
	}

	// Accept any valid semver with a pre-release tag
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

func createVersionList(w fyne.Window, stopBtn *widget.Button) *widget.List {
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

			launchButton := createLaunchButton(w, version, stopBtn, buttonContainer)
			createVersionOptionsButton(w, version, launchButton, stopBtn, buttonContainer)
			createFilesButton(version, w, buttonContainer)
			if len(allReleases) > 0 {
				createUpdateButton(version, w, buttonContainer)
			}
			createImportButton(versions, version, w, buttonContainer)
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

func createVersionOptionsButton(w fyne.Window, version string, launchButton, stopBtn *widget.Button, buttonContainer *fyne.Container) {
	menuItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Port Number", func() {
			showPortNumberDialogForVersion(w, version)
		}),
		fyne.NewMenuItem("Tracker Connection", func() {
			showTrackerConnectionDialogForVersion(w, version)
		}),
	}
	// offered only while the checkpoints folder holds an export, and only for versions that can start from it
	if folder := GetCheckpointFolder(); folder != "" && canLaunchFromCheckpoint(version) {
		if export, err := checkpointExport(folder); err == nil {
			menuItems = append(menuItems, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Launch from Checkpoint", func() {
				confirmLaunchFromCheckpoint(w, version, folder, export, launchButton, stopBtn)
			}))
		}
	}

	buttonContainer.Add(container.NewPadded(shared.CreateMenuButton("Options", menuItems)))
}

func createImportButton(versions []string, version string, w fyne.Window, buttonContainer *fyne.Container) {
	importButton := widget.NewButton("Import", nil)
	importButton.Show()
	importButton.OnTapped = func() {
		extractOption := fmt.Sprintf("%s (extract from distribution)", version)
		sourceVersions := append([]string{extractOption}, filterVersions(versions, version)...)
		sourceVersionDropdown := widget.NewSelect(sourceVersions, func(selected string) {})
		// Wrap in a fixed-size container to ensure adequate width
		selectContainer := container.NewGridWrap(fyne.NewSize(380, 35), sourceVersionDropdown)
		dryRunCheck := widget.NewCheck("Create a dry-run report (no files changed)", nil)

		dialog.ShowForm("Import Data and Config",
			"Import",
			"Cancel",
			[]*widget.FormItem{
				widget.NewFormItem("Copy from version", selectContainer),
				widget.NewFormItem("", dryRunCheck),
			},
			func(ok bool) {
				if !ok {
					return
				}

				selected := sourceVersionDropdown.Selected
				if selected == "" {
					dialog.ShowError(fmt.Errorf("source version cannot be empty"), w)
					return
				}

				if selected == extractOption {
					if dryRunCheck.Checked {
						go func() {
							reportPath, err := DryRunExtractLocalFromDistribution(version)
							fyne.Do(func() {
								if err != nil {
									log.Printf("Error while generating dry-run extraction report: %v\n", err)
									dialog.ShowError(fmt.Errorf("failed to create dry-run report: %w", err), w)
									return
								}
								showMigrationReportDialog(
									"Dry-Run Extraction Report",
									fmt.Sprintf("Dry-run report saved (no files were modified):\n\n%s", reportPath),
									reportPath,
									w,
								)
							})
						}()
						return
					}

					confirmMsg := fmt.Sprintf("This will extract locally customizable files from version %s into its local/ folder.\n\n"+
						"Existing files in local/ will NOT be overwritten, and your database and env.properties will not be changed.\n\n"+
						"Do you want to continue?", version)
					dialog.ShowConfirm("Extract from Distribution",
						confirmMsg,
						func(confirmed bool) {
							if !confirmed {
								return
							}
							go func() {
								reportPath, err := ExtractLocalFromDistribution(version)
								fyne.Do(func() {
									if err != nil {
										log.Printf("Error while extracting distribution files: %v\n", err)
										dialog.ShowError(fmt.Errorf("failed to extract distribution files: %w", err), w)
										return
									}
									msg := fmt.Sprintf("Successfully extracted customizable files from version %s", version)
									if reportPath != "" {
										showMigrationReportDialog("Extraction Complete", msg, reportPath, w)
										return
									}
									dialog.ShowInformation("Extraction Complete", msg, w)
								})
							}()
						},
						w)
					return
				}

				sourceVersion := selected
				sourceDir := filepath.Join(installDir, sourceVersion)

				if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
					dialog.ShowError(fmt.Errorf("source version %s does not exist", sourceVersion), w)
					return
				}

				if dryRunCheck.Checked {
					go func() {
						reportPath, err := DryRunImportLocalFiles(sourceVersion, version)
						fyne.Do(func() {
							if err != nil {
								log.Printf("Error while generating dry-run migration report: %v\n", err)
								dialog.ShowError(fmt.Errorf("failed to create dry-run report: %w", err), w)
								return
							}
							showMigrationReportDialog(
								"Dry-Run Migration Report",
								fmt.Sprintf("Dry-run report saved (no files were modified):\n\n%s", reportPath),
								reportPath,
								w,
							)
						})
					}()
					return
				}

				// Show warning about destructive operation
				warningMsg := fmt.Sprintf("⚠️ WARNING: Import Process\n\n"+
					"This will:\n"+
					"1. Replace any local files in version %s (any changes you made directly in version %s will be LOST)\n"+
					"2. Copy local additions and modifications from version %s (skipping files that match the distribution)\n\n"+
					"Do you want to continue?", version, version, sourceVersion)

				dialog.ShowConfirm("Confirm Import",
					warningMsg,
					func(confirmed bool) {
						if !confirmed {
							return
						}

						var runImport func()
						runImport = func() {
							go func() {
								res, err := ImportDataAndConfig(sourceVersion, version)
								fyne.Do(func() {
									if err != nil {
										if showFileInUseRecovery(err, w, runImport) {
											return
										}
										log.Printf("Error while processing local files: %v\n", err)
										dialog.ShowError(fmt.Errorf("failed to process local files: %w", err), w)
										return
									}
									msg := fmt.Sprintf("Successfully imported data and config from version %s to version %s", sourceVersion, version)
									if res.MigrationReportPath != "" {
										showMigrationReportDialog("Import Complete", msg, res.MigrationReportPath, w)
										return
									}
									dialog.ShowInformation("Import Complete", msg, w)
								})
							}()
						}
						runImport()
					},
					w)
			},
			w)
	}
	buttonContainer.Add(container.NewPadded(importButton))
}

func showMigrationReportDialog(title, message, reportPath string, w fyne.Window) {
	openReportLink := widget.NewHyperlink("Open report", nil)
	openReportLink.OnTapped = func() {
		if err := shared.OpenFile(reportPath); err != nil {
			dialog.ShowError(err, w)
		}
	}
	openFolderLink := widget.NewHyperlink("Open folder", nil)
	openFolderLink.OnTapped = func() {
		if err := shared.OpenFileExplorer(filepath.Dir(reportPath)); err != nil {
			dialog.ShowError(err, w)
		}
	}
	content := container.NewVBox(
		widget.NewLabel(message),
		container.NewHBox(openReportLink, openFolderLink),
	)
	dialog.NewCustom(title, "OK", content, w).Show()
}

func createUpdateButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	updateButton := widget.NewButton("Update", nil)
	var mostRecent string
	var err error

	latestStable, stableErr := getMostRecentStableRelease()
	latestPrerelease, preErr := getMostRecentPrerelease()
	latestStableInstalled := findLatestStableInstalled()
	latestPrereleaseInstalled := findLatestPrereleaseInstalled()

	if (!containsPreReleaseTag(latestStableInstalled) && stableErr == nil && latestStableInstalled == latestStable) ||
		(containsPreReleaseTag(latestPrereleaseInstalled) && preErr == nil && latestPrereleaseInstalled == latestPrerelease) {
		// here is no point in updating since the most recent version is already installed
		return
	}

	// Check if the current version is stable or a prerelease
	shouldShow := false
	if !containsPreReleaseTag(version) {
		mostRecent, err = getMostRecentStableRelease()
		if err == nil {
			shouldShow = adjustUpdateButton(mostRecent, version, updateButton, buttonContainer, w)
		} else {
			log.Printf("failed to get most recent stable release: %v", err)
		}
	} else {
		mostRecent, err = getMostRecentPrerelease()
		if err == nil {
			shouldShow = adjustUpdateButton(mostRecent, version, updateButton, buttonContainer, w)
		} else {
			log.Printf("failed to get most recent prerelease: %v", err)
		}
	}
	if shouldShow {
		buttonContainer.Add(container.NewPadded(updateButton))
	}
}

func createRemoveButton(version string, w fyne.Window, buttonContainer *fyne.Container) {
	removeButton := widget.NewButton("Remove", nil)
	removeButton.OnTapped = func() {
		dialog.ShowConfirm("Confirm Remove",
			fmt.Sprintf("Do you want to remove OWLCMS version %s?", version),
			func(ok bool) {
				if !ok {
					return
				}

				if err := RemoveInstalledVersion(version); err != nil {
					dialog.ShowError(fmt.Errorf("failed to remove OWLCMS %s: %w", version, err), w)
					return
				}

				// Recompute the version list
				recomputeVersionList(w)

				// Check if a more recent version is available
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

func createLaunchButton(w fyne.Window, version string, stopBtn *widget.Button, buttonContainer *fyne.Container) *widget.Button {
	launchButton := widget.NewButton("Launch", nil)
	launchButton.Resize(fyne.NewSize(80, 25))
	launchButton.Importance = widget.HighImportance
	launchButton.SetText("Launch")
	launchButton.OnTapped = func() {
		if currentProcess != nil {
			dialog.ShowError(fmt.Errorf("OWLCMS is already running"), w)
			return
		}

		doLaunch := func() {
			log.Printf("Launching version %s\n", version)
			// Get version-specific Temurin version
			ver := GetTemurinVersionForRelease(version)
			if err := shared.CheckAndInstallJava(ver, statusLabel, w, checkJava); err != nil {
				return
			}

			if err := launchOwlcms(version, launchButton, stopBtn); err != nil {
				dialog.ShowError(err, w)
				return
			}
		}

		doLaunch()
	}
	buttonContainer.Add(container.NewPadded(launchButton))
	return launchButton
}

// confirmLaunchFromCheckpoint launches OWLCMS with its database replaced by the newest export in the checkpoints
// folder. Meant for when the machine that was running OWLCMS is lost.
func confirmLaunchFromCheckpoint(w fyne.Window, version, folder, export string, launchButton, stopBtn *widget.Button) {
	if currentProcess != nil {
		dialog.ShowError(fmt.Errorf("OWLCMS is already running"), w)
		return
	}
	message := fmt.Sprintf("Launch OWLCMS %s from the checkpoints folder\n%s ?\n\n"+
		"The database will be replaced by %s.\n"+
		"Use this only when the machine that was running OWLCMS is no longer available.", version, folder, export)
	dialog.ShowConfirm("Launch from Checkpoint", message, func(confirmed bool) {
		if !confirmed {
			return
		}
		log.Printf("Launching version %s from checkpoints folder %s\n", version, folder)
		ver := GetTemurinVersionForRelease(version)
		if err := shared.CheckAndInstallJava(ver, statusLabel, w, checkJava); err != nil {
			return
		}
		if err := launchOwlcmsLoading(version, folder, launchButton, stopBtn); err != nil {
			dialog.ShowError(err, w)
		}
	}, w)
}

func adjustUpdateButton(mostRecent string, version string, updateButton *widget.Button, buttonContainer *fyne.Container, w fyne.Window) bool {
	if shared.CompareVersions(mostRecent, version) {
		// If the natural update target (same build suffix on the new base version)
		// is already installed, the update has effectively been done; don't propose it.
		targetBaseVersion, _ := shared.ParseVersionWithBuild(mostRecent)
		existingBuild := shared.GetCurrentBuildString(version)
		naturalTarget := targetBaseVersion
		if existingBuild != "" {
			naturalTarget = fmt.Sprintf("%s+%s", targetBaseVersion, existingBuild)
		}
		if _, err := os.Stat(filepath.Join(installDir, naturalTarget)); err == nil {
			return false
		}

		targetInstallVersion := computeUpdateTargetVersion(version, mostRecent)
		updateButton.SetText(fmt.Sprintf("Update to %s", targetInstallVersion))
		updateButton.OnTapped = func() {
			if running, err := FindRunningOwlcms(); err != nil {
				log.Printf("Cannot determine whether OWLCMS is running before update: %v", err)
			} else if running != nil {
				dialog.ShowError(fmt.Errorf("OWLCMS is already running with PID %d on port %s.\nStop it first. You can use the 'Processes' menu to stop it before updating",
					running.Assessment.Listener.PID, running.Port), w)
				return
			}

			confirmDialog := dialog.NewConfirm("Backup Suggestion",
				"The update process keeps your current version intact so you can revert if needed.\n\nBut we nevertheless suggest that you take a backup of your current database using the 'Export Database' button of the 'Prepare Competition' page.",
				func(confirm bool) {
					if confirm {
						updateVersion(version, mostRecent, w)
					}
				},
				w,
			)
			confirmDialog.SetConfirmText("Perform Update")
			confirmDialog.SetDismissText("Cancel Update")
			confirmDialog.Show()
		}
		updateButton.Refresh()
		return true
	}
	buttonContainer.Refresh()
	return false
}

func computeUpdateTargetVersion(existingVersion string, targetVersion string) string {
	targetBaseVersion, _ := shared.ParseVersionWithBuild(targetVersion)
	existingBuild := shared.GetCurrentBuildString(existingVersion)
	if existingBuild == "" {
		return targetBaseVersion
	}

	resolvedBuild := shared.ResolveCollisionForBuild(installDir, targetBaseVersion, existingBuild)
	return fmt.Sprintf("%s+%s", targetBaseVersion, resolvedBuild)
}

func updateVersion(existingVersion string, targetVersion string, w fyne.Window) {
	actionCancel := make(chan bool)
	actionProgressDialog, actionProgressBar := customdialog.NewDownloadDialog(
		"Updating OWLCMS",
		w,
		actionCancel)
	actionProgressDialog.Show()

	actionProgressCallback := func(downloaded, total int64) {
		if total > 0 {
			progress := float64(downloaded) / float64(total)
			fyne.Do(func() {
				actionProgressBar.SetValue(progress)
			})
		}
	}

	go func() {
		actionResult, actionErr := UpdateRelease(existingVersion, targetVersion, actionProgressCallback, actionCancel)
		fyne.Do(func() {
			actionProgressDialog.Hide()
			if actionErr != nil {
				if showFileInUseRecovery(actionErr, w, func() {
					updateVersion(existingVersion, targetVersion, w)
				}) {
					return
				}
				dialog.ShowError(actionErr, w)
				return
			}

			actionSuccessMessage := fmt.Sprintf("Successfully updated to version %s\n", actionResult.Version)
			if actionResult.DatabaseCopied {
				actionSuccessMessage += "\n✓ Database files have been copied"
			}
			if actionResult.LocalFilesCopied {
				actionSuccessMessage += "\n✓ Local configuration files have been processed"
			}
			actionSuccessContent := container.NewVBox(widget.NewLabel(actionSuccessMessage))
			if actionResult.MigrationReportPath != "" {
				reportPath := actionResult.MigrationReportPath
				openReportLink := widget.NewHyperlink("Open migration report", nil)
				openReportLink.OnTapped = func() {
					if err := shared.OpenFile(reportPath); err != nil {
						dialog.ShowError(err, w)
					}
				}
				openFolderLink := widget.NewHyperlink("Open folder", nil)
				openFolderLink.OnTapped = func() {
					if err := shared.OpenFileExplorer(filepath.Dir(reportPath)); err != nil {
						dialog.ShowError(err, w)
					}
				}
				actionSuccessContent.Add(container.NewHBox(openReportLink, openFolderLink))
			}
			actionSuccessDialog := dialog.NewCustom(
				"Update Complete",
				"OK",
				actionSuccessContent,
				w,
			)
			actionSuccessDialog.SetOnClosed(func() {
				recomputeVersionList(w)
				checkForNewerVersion()
				w.Content().Refresh()
			})
			actionSuccessDialog.Show()
		})
	}()
}

func showFileInUseRecovery(err error, w fyne.Window, retry func()) bool {
	var fileInUse *shared.FileInUseError
	if !errors.As(err, &fileInUse) || len(fileInUse.Processes) == 0 {
		return false
	}

	processes := make([]string, 0, len(fileInUse.Processes))
	for _, process := range fileInUse.Processes {
		processes = append(processes, fmt.Sprintf("%s (PID %d)", process.DisplayName(), process.PID))
	}

	message := fmt.Sprintf(
		"%s is currently in use by:\n\n%s\n\nStopping these process(es) can discard unsaved competition changes. Stop them and retry?",
		fileInUse.FileName(),
		strings.Join(processes, "\n"),
	)
	confirmDialog := dialog.NewConfirm("Database File In Use", message, func(stop bool) {
		if !stop {
			return
		}

		go func() {
			var stopErrors []string
			for _, process := range fileInUse.Processes {
				if stopErr := shared.GracefullyStopPID(process.PID); stopErr != nil {
					stopErrors = append(stopErrors, fmt.Sprintf("%s (PID %d): %v", process.DisplayName(), process.PID, stopErr))
				}
			}

			fyne.Do(func() {
				if len(stopErrors) > 0 {
					dialog.ShowError(fmt.Errorf("could not stop process(es):\n%s", strings.Join(stopErrors, "\n")), w)
					return
				}
				retry()
			})
		}()
	}, w)
	confirmDialog.SetConfirmText("Stop Process and Retry")
	confirmDialog.SetDismissText("Cancel")
	confirmDialog.Show()
	return true
}

// retrieveNewVersionDirs lists top-level local/ directories where subtrees
// that differ from oldJar keep previously identical files updated from newJar
// rather than pruning them against newJar.
var retrieveNewVersionDirs = []string{"templates", "agegroups"}

// customizableDistributionDirs and customizableDistributionFiles mirror the
// resource directories and root files shipped as locally customizable in owlcms.jar.
var customizableDistributionDirs = []string{
	"ageFactors",
	"agefactors",
	"agegroups",
	"css",
	"flags",
	"gamx",
	"i18n",
	"iwf",
	"logos",
	"mappings",
	"pictures",
	"qpoints",
	"records",
	"robi",
	"sinclair",
	"sounds",
	"templates",
	"timing",
}

var customizableDistributionFiles = []string{
	"logback.xml",
}

func isCustomizableDistributionEntry(slashPath string) bool {
	if slices.Contains(customizableDistributionFiles, slashPath) {
		return true
	}
	for _, dir := range customizableDistributionDirs {
		if strings.HasPrefix(slashPath, dir+"/") {
			return true
		}
	}
	return false
}

// extractDistributionLocalFiles extracts customizable files from versionDir/owlcms.jar
// into versionDir/local without overwriting files that already exist on disk.
func extractDistributionLocalFiles(versionDir string, dryRun bool) (string, error) {
	jarPath := filepath.Join(versionDir, "owlcms.jar")
	localDir := filepath.Join(versionDir, "local")
	versionName := filepath.Base(versionDir)

	var reportBuf strings.Builder
	logBoth := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		reportBuf.WriteString(msg)
		log.Print(msg)
	}

	if dryRun {
		logBoth("=== Starting Dry-Run Extraction from Distribution (%s — no files will be modified) ===\n", versionName)
	} else {
		logBoth("\n=== Extracting Customizable Files from Distribution (%s) ===\n", versionName)
	}
	logBoth("Target: %s\n", localDir)
	logBoth("============================================================\n\n")

	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return reportBuf.String(), fmt.Errorf("failed to open %s: %w", jarPath, err)
	}
	defer r.Close()

	var toExtract []*zip.File
	var skippedExisting []string
	for _, f := range r.File {
		if f.FileInfo().IsDir() || isIgnoredLocalMetadataFile(f.Name) {
			continue
		}
		if !isCustomizableDistributionEntry(f.Name) {
			continue
		}
		targetPath := filepath.Join(localDir, filepath.FromSlash(f.Name))
		if _, err := os.Stat(targetPath); err == nil {
			skippedExisting = append(skippedExisting, f.Name)
			continue
		} else if !os.IsNotExist(err) {
			return reportBuf.String(), fmt.Errorf("failed to inspect %s: %w", targetPath, err)
		}
		toExtract = append(toExtract, f)
	}

	sort.Slice(toExtract, func(i, j int) bool { return toExtract[i].Name < toExtract[j].Name })
	sort.Strings(skippedExisting)

	if dryRun {
		logBoth("Would extract from %s: %d files\n", versionName, len(toExtract))
		for _, f := range toExtract {
			logBoth("  + Would extract: %s\n", f.Name)
		}
	} else {
		logBoth("Extracting from %s: %d files\n", versionName, len(toExtract))
		for _, f := range toExtract {
			targetPath := filepath.Join(localDir, filepath.FromSlash(f.Name))
			if err := shared.EnsureDir0755(filepath.Dir(targetPath)); err != nil {
				return reportBuf.String(), err
			}
			rc, err := f.Open()
			if err != nil {
				return reportBuf.String(), err
			}
			outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				rc.Close()
				return reportBuf.String(), err
			}
			_, copyErr := io.Copy(outFile, rc)
			closeErr := outFile.Close()
			rc.Close()
			if copyErr != nil {
				return reportBuf.String(), copyErr
			}
			if closeErr != nil {
				return reportBuf.String(), closeErr
			}
			logBoth("  + Extracted from %s: %s\n", versionName, f.Name)
		}
	}

	if len(skippedExisting) > 0 {
		logBoth("\nKept existing local files (not overwritten): %d\n", len(skippedExisting))
		for _, f := range skippedExisting {
			logBoth("  = Kept existing: %s\n", f)
		}
	}

	if dryRun {
		logBoth("\n=== Dry-Run Complete (no files were modified) ===\n")
	} else {
		logBoth("\n=== Extraction Complete ===\n")
	}
	return reportBuf.String(), nil
}

// restoreLocalFilesFromPreviousVersion migrates user customizations from oldDir/local
// to newDir/local in two steps:
//  1. Compare oldDir/local against oldDir/owlcms.jar and write additions/modifications
//     into newDir/local (plus newJar versions of previously identical files inside
//     differing subtrees of retrieveNewVersionDirs).
//  2. Clean up standard paths in newDir/local that already match newDir/owlcms.jar.
func restoreLocalFilesFromPreviousVersion(newDir, oldDir string) error {
	_, err := migrateLocalFiles(newDir, oldDir, false)
	return err
}

// restoreLocalFilesFromPreviousVersionWithReport migrates local/ and writes a timestamped
// report file under newDir/logs/.
func restoreLocalFilesFromPreviousVersionWithReport(newDir, oldDir string) (string, error) {
	report, err := migrateLocalFiles(newDir, oldDir, false)
	if err != nil {
		return "", err
	}
	reportPath, writeErr := writeMigrationReport(
		filepath.Join(newDir, "logs"),
		"migration",
		filepath.Base(oldDir),
		filepath.Base(newDir),
		report,
	)
	if writeErr != nil {
		log.Printf("Warning: failed to write migration report: %v", writeErr)
		return "", nil
	}
	return reportPath, nil
}

func defaultDownloadsDir(fallbackDir string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		downloads := filepath.Join(home, "Downloads")
		if info, statErr := os.Stat(downloads); statErr == nil && info.IsDir() {
			return downloads
		}
		return home
	}
	return fallbackDir
}

func sanitizeFilenamePart(s string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return replacer.Replace(strings.TrimSpace(s))
}

func writeMigrationReport(dir, prefix, fromVersion, toVersion, content string) (string, error) {
	if err := shared.EnsureDir0755(dir); err != nil {
		return "", fmt.Errorf("failed to create report directory: %w", err)
	}
	stamp := time.Now().Format("2006-01-02_150405")
	fileName := fmt.Sprintf("%s-%s-to-%s-%s.txt",
		prefix,
		sanitizeFilenamePart(fromVersion),
		sanitizeFilenamePart(toVersion),
		stamp,
	)
	reportPath := filepath.Join(dir, fileName)
	if err := os.WriteFile(reportPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write report file %s: %w", reportPath, err)
	}
	return reportPath, nil
}

func migrateLocalFiles(newDir, oldDir string, dryRun bool) (string, error) {
	newLocal := filepath.Join(newDir, "local")
	oldLocal := filepath.Join(oldDir, "local")
	oldJar := filepath.Join(oldDir, "owlcms.jar")
	newJar := filepath.Join(newDir, "owlcms.jar")

	var reportBuf strings.Builder
	logBoth := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		reportBuf.WriteString(msg)
		log.Print(msg)
	}

	if dryRun {
		logBoth("=== Starting Dry-Run Migration Report (no files will be modified) ===\n")
	} else {
		logBoth("\n=== Starting Import Process ===\n")
	}
	logBoth("From: %s\n", oldDir)
	logBoth("To: %s\n", newDir)
	logBoth("============================\n\n")

	// Start with a clean destination local/ directory (missing newLocal is normal).
	if !dryRun {
		if _, err := os.Stat(newLocal); err == nil {
			logBoth("  - Removing existing local directory: %s\n", newLocal)
			if err := os.RemoveAll(newLocal); err != nil {
				return reportBuf.String(), fmt.Errorf("failed to remove existing local directory: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return reportBuf.String(), fmt.Errorf("failed to inspect target local directory: %w", err)
		}
	}

	// If the previous version has no local/ directory, there is nothing to migrate.
	oldLocalInfo, err := os.Stat(oldLocal)
	if os.IsNotExist(err) {
		logBoth("  - Source version has no local/ directory; nothing to migrate.\n")
		logBoth("\n=== Import Complete ===\n")
		return reportBuf.String(), nil
	}
	if err != nil {
		return reportBuf.String(), fmt.Errorf("failed to inspect old local directory: %w", err)
	}
	if !oldLocalInfo.IsDir() {
		return reportBuf.String(), fmt.Errorf("old local path is not a directory: %s", oldLocal)
	}

	// Step 1: Analyze oldDir/local against oldDir/owlcms.jar.
	logBoth("Phase 1: Analyzing oldDir/local against oldDir/owlcms.jar...\n")
	oldLocalFiles, err := collectLocalFiles(oldLocal)
	if err != nil {
		return reportBuf.String(), fmt.Errorf("failed to read old local files: %w", err)
	}
	if len(oldLocalFiles) == 0 {
		logBoth("  - Source local/ directory contains no files; nothing to migrate.\n")
		logBoth("\n=== Import Complete ===\n")
		return reportBuf.String(), nil
	}

	anyRetrieveDirPresent := false
	for _, dir := range retrieveNewVersionDirs {
		if hasSubtreeFiles(oldLocalFiles, dir) {
			anyRetrieveDirPresent = true
			break
		}
	}

	wantedOldJarFiles := make(map[string]struct{}, len(oldLocalFiles))
	for relPath := range oldLocalFiles {
		wantedOldJarFiles[relPath] = struct{}{}
	}
	var wantedRetrievePrefixes []string
	if anyRetrieveDirPresent {
		wantedRetrievePrefixes = make([]string, 0, len(retrieveNewVersionDirs))
		for _, dir := range retrieveNewVersionDirs {
			wantedRetrievePrefixes = append(wantedRetrievePrefixes, dir+"/")
		}
	}

	// Single scan of oldJar: hashes only files present in oldLocal (plus retrieve-new-version
	// prefixes if present in oldLocal).
	oldJarFiles, err := readJarEntryChecksums(oldJar, wantedOldJarFiles, wantedRetrievePrefixes)
	if err != nil {
		return reportBuf.String(), fmt.Errorf("failed to read old jar files: %w", err)
	}

	var standardAdded []string
	var standardModified []string
	var standardUnchanged []string

	oldLocalPaths := make([]string, 0, len(oldLocalFiles))
	for relPath := range oldLocalFiles {
		oldLocalPaths = append(oldLocalPaths, relPath)
	}
	sort.Strings(oldLocalPaths)

	for _, relPath := range oldLocalPaths {
		if isRetrieveNewVersionPath(relPath) {
			continue
		}
		oldJarSum, inOldJar := oldJarFiles[relPath]
		if !inOldJar {
			standardAdded = append(standardAdded, relPath)
		} else if oldLocalFiles[relPath] != oldJarSum {
			standardModified = append(standardModified, relPath)
		} else {
			standardUnchanged = append(standardUnchanged, relPath)
		}
	}

	standardCandidates := make([]string, 0, len(standardAdded)+len(standardModified))
	standardCandidates = append(standardCandidates, standardAdded...)
	standardCandidates = append(standardCandidates, standardModified...)
	sort.Strings(standardCandidates)

	// Single open + scan of newJar: hashes only standardCandidates and indexes
	// retrieve-new-version entries (without decompressing them) for classification and extraction.
	newJarStandard := make(map[string]string)
	var newJarRetrievePaths []string
	newJarRetrieveEntries := make(map[string]*zip.File)
	var newJarReader *zip.ReadCloser
	if len(standardCandidates) > 0 || anyRetrieveDirPresent {
		wantedInNewJar := make(map[string]struct{}, len(standardCandidates))
		for _, f := range standardCandidates {
			wantedInNewJar[f] = struct{}{}
		}
		newJarReader, err = zip.OpenReader(newJar)
		if err != nil {
			return reportBuf.String(), fmt.Errorf("failed to open new jar: %w", err)
		}
		defer newJarReader.Close()

		for _, f := range newJarReader.File {
			if f.FileInfo().IsDir() || isIgnoredLocalMetadataFile(f.Name) {
				continue
			}
			if _, ok := wantedInNewJar[f.Name]; ok {
				rc, openErr := f.Open()
				if openErr != nil {
					return reportBuf.String(), fmt.Errorf("failed to read %s from new jar: %w", f.Name, openErr)
				}
				sum, sumErr := streamChecksum(rc)
				rc.Close()
				if sumErr != nil {
					return reportBuf.String(), fmt.Errorf("failed to checksum %s in new jar: %w", f.Name, sumErr)
				}
				newJarStandard[f.Name] = sum
			}
			if anyRetrieveDirPresent {
				for _, prefix := range wantedRetrievePrefixes {
					if strings.HasPrefix(f.Name, prefix) {
						newJarRetrievePaths = append(newJarRetrievePaths, f.Name)
						newJarRetrieveEntries[f.Name] = f
						break
					}
				}
			}
		}
	}

	var retrieveReport retrieveClassification
	if anyRetrieveDirPresent {
		newJarPathSet := make(map[string]struct{}, len(newJarRetrievePaths))
		for _, p := range newJarRetrievePaths {
			newJarPathSet[p] = struct{}{}
		}
		for _, dir := range retrieveNewVersionDirs {
			classifyRetrieveNewVersionSubtree(
				dir,
				oldLocalFiles,
				oldJarFiles,
				newJarRetrievePaths,
				newJarPathSet,
				&retrieveReport,
			)
		}
	}
	sort.Strings(retrieveReport.UntouchedDirs)
	sort.Strings(retrieveReport.NewJarDirs)
	sort.Strings(retrieveReport.CopyFromOld)
	sort.Strings(retrieveReport.ExtractFromNewJar)
	sort.Strings(retrieveReport.CopyLegacyFromOld)

	oldVersion := filepath.Base(oldDir)
	newVersion := filepath.Base(newDir)

	logBoth("\n=== Import Analysis: %s -> %s ===\n", oldVersion, newVersion)
	logBoth("Standard paths (outside %s): copied=%d modified=%d unchanged=%d\n",
		strings.Join(retrieveNewVersionDirs, ", "), len(standardAdded), len(standardModified), len(standardUnchanged))
	for _, f := range standardAdded {
		if newJarSum, inNewJar := newJarStandard[f]; inNewJar {
			if oldLocalFiles[f] == newJarSum {
				logBoth("  + Copied from %s (matches %s — will be pruned): %s\n", oldVersion, newVersion, f)
			} else {
				logBoth("  ! Copied from %s (also in %s distribution with different content — please check): %s\n", oldVersion, newVersion, f)
			}
		} else {
			logBoth("  + Copied from %s: %s\n", oldVersion, f)
		}
	}
	for _, f := range standardModified {
		newJarSum, inNewJar := newJarStandard[f]
		switch {
		case inNewJar && oldLocalFiles[f] == newJarSum:
			logBoth("  * Modified (copied from %s; matches %s — will be pruned): %s\n", oldVersion, newVersion, f)
		case inNewJar && newJarSum == oldJarFiles[f]:
			logBoth("  * Modified (copied from %s; %s distribution unchanged — likely OK): %s\n", oldVersion, newVersion, f)
		case inNewJar:
			logBoth("  ! Modified (copied from %s; %s distribution also changed — please check): %s\n", oldVersion, newVersion, f)
		default:
			logBoth("  ! Modified (copied from %s; removed from %s distribution — please check): %s\n", oldVersion, newVersion, f)
		}
	}

	logBoth("\nLocal-only paths (%s):\n", strings.Join(retrieveNewVersionDirs, ", "))
	logBoth("  Untouched folders (extracted from %s): %d\n", newVersion, len(retrieveReport.UntouchedDirs))
	for _, d := range retrieveReport.UntouchedDirs {
		logBoth("    = %s\n", d)
	}
	if len(retrieveReport.NewJarDirs) > 0 {
		logBoth("  Brand-new folders in %s (extracted from %s): %d\n", newVersion, newVersion, len(retrieveReport.NewJarDirs))
		for _, d := range retrieveReport.NewJarDirs {
			logBoth("    + %s\n", d)
		}
	}
	logBoth("  Touched folders: %d\n", len(retrieveReport.TouchedFolders))
	for _, tf := range retrieveReport.TouchedFolders {
		logBoth("    * %s/ (copied=%d modified=%d from-%s=%d legacy=%d new-in-%s=%d)\n",
			tf.Dir,
			len(tf.Added),
			len(tf.Modified),
			newVersion,
			len(tf.UpdatedFromNewJar),
			len(tf.LegacyFromOld),
			newVersion,
			len(tf.NewInNewJar),
		)
		for _, f := range tf.Added {
			logBoth("        + Copied from %s: %s\n", oldVersion, f)
		}
		for _, f := range tf.Modified {
			logBoth("        * Modified (copied from %s): %s\n", oldVersion, f)
		}
		for _, f := range tf.UpdatedFromNewJar {
			logBoth("        ~ Extracted from %s: %s\n", newVersion, f)
		}
		for _, f := range tf.LegacyFromOld {
			logBoth("        + Legacy (copied from %s; not in %s): %s\n", oldVersion, newVersion, f)
		}
		for _, f := range tf.NewInNewJar {
			logBoth("        ! New in %s (not copied): %s\n", newVersion, f)
		}
	}
	logBoth("=== End Import Analysis ===\n\n")

	if dryRun {
		if len(standardCandidates) > 0 {
			logBoth("Phase 3 (dry-run): Standard files matching newDir/owlcms.jar (would be pruned):\n")
			prunedCount := 0
			for _, f := range standardCandidates {
				if newJarSum, ok := newJarStandard[f]; ok && oldLocalFiles[f] == newJarSum {
					logBoth("  - Would prune (matches new jar): %s\n", f)
					prunedCount++
				}
			}
			if prunedCount == 0 {
				logBoth("  (none)\n")
			}
		}
		logBoth("\n=== Dry-Run Complete (no files were modified) ===\n")
		return reportBuf.String(), nil
	}

	// Copy additions, modifications, and legacy files from oldDir/local -> newDir/local.
	logBoth("Phase 2: Copying additions and modifications to newDir/local...\n")
	toCopy := make([]string, 0, len(standardAdded)+len(standardModified)+len(retrieveReport.CopyFromOld)+len(retrieveReport.CopyLegacyFromOld))
	toCopy = append(toCopy, standardAdded...)
	toCopy = append(toCopy, standardModified...)
	toCopy = append(toCopy, retrieveReport.CopyFromOld...)
	toCopy = append(toCopy, retrieveReport.CopyLegacyFromOld...)
	for _, relPath := range toCopy {
		srcPath := filepath.Join(oldLocal, filepath.FromSlash(relPath))
		dstPath := filepath.Join(newLocal, filepath.FromSlash(relPath))
		if err := copyFile(srcPath, dstPath); err != nil {
			return reportBuf.String(), fmt.Errorf("failed to copy %s: %w", relPath, err)
		}
	}

	// Pull updated versions from newJar for untouched directories, brand-new kit subtrees,
	// and previously identical files in touched directories using the already-indexed entries.
	if len(retrieveReport.ExtractFromNewJar) > 0 || len(retrieveReport.NewJarDirs) > 0 {
		logBoth("Phase 2b: Extracting updated versions from newDir/owlcms.jar (%d files, %d new dirs)...\n",
			len(retrieveReport.ExtractFromNewJar), len(retrieveReport.NewJarDirs))
		wantedFromNewJar := make(map[string]struct{}, len(retrieveReport.ExtractFromNewJar))
		for _, relPath := range retrieveReport.ExtractFromNewJar {
			wantedFromNewJar[relPath] = struct{}{}
		}
		extracted, missing, err := extractIndexedJarEntries(newJarRetrieveEntries, newLocal, wantedFromNewJar, retrieveReport.NewJarDirs)
		if err != nil {
			return reportBuf.String(), fmt.Errorf("failed to extract updated files from new jar: %w", err)
		}
		for _, f := range extracted {
			logBoth("  ~ Extracted from new jar: %s\n", f)
		}
		for _, f := range missing {
			srcPath := filepath.Join(oldLocal, filepath.FromSlash(f))
			dstPath := filepath.Join(newLocal, filepath.FromSlash(f))
			if err := copyFile(srcPath, dstPath); err != nil {
				return reportBuf.String(), fmt.Errorf("failed to copy legacy file %s: %w", f, err)
			}
			logBoth("  + Copied legacy file (removed from new jar): %s\n", f)
		}
	}

	// Step 2: Clean up standard paths in newDir/local that already match newDir/owlcms.jar
	// using the checksums already collected in the single newJar scan.
	logBoth("Phase 3: Cleaning up newDir/local files matching newDir/owlcms.jar...\n")
	if err := pruneStandardLocalMatchingChecksums(newLocal, standardCandidates, oldLocalFiles, newJarStandard, logBoth); err != nil {
		return reportBuf.String(), fmt.Errorf("failed to clean up new local against new jar: %w", err)
	}

	logBoth("\n=== Import Complete ===\n")
	logBoth("Successfully migrated customizations from %s to %s\n", oldDir, newDir)
	logBoth("=======================\n")
	return reportBuf.String(), nil
}

type touchedFolderReport struct {
	Dir               string
	Added             []string
	Modified          []string
	UpdatedFromNewJar []string
	LegacyFromOld     []string
	NewInNewJar       []string
}

type retrieveClassification struct {
	UntouchedDirs     []string
	NewJarDirs        []string
	TouchedFolders    []touchedFolderReport
	CopyFromOld       []string
	ExtractFromNewJar []string
	CopyLegacyFromOld []string
}

// classifyRetrieveNewVersionSubtree recursively classifies each directory under
// retrieveNewVersionDirs present in oldLocal. Whether a folder is touched is determined
// solely by its direct files on disk versus oldJar (absent subfolders do not mark a
// parent folder as touched):
//   - Untouched direct files (localDirect == jarDirect): extract all direct files of dir
//     from newJar (updates and new additions), keeping the folder untouched for future updates.
//   - Touched direct files (localDirect != jarDirect): copy added/modified direct files from
//     oldLocal, extract previously identical direct files from newJar (or copy from oldLocal
//     as legacy if absent from newJar), and leave absent files / newJar additions out.
//   - Child subdirectories present in oldLocal are always recursed into; child subdirectories
//     absent from oldLocal are never extracted unless they are brand-new in newJar inside a
//     folder with untouched files or untouched sibling folders.
func classifyRetrieveNewVersionSubtree(
	dir string,
	oldLocalFiles map[string]string,
	oldJarFiles map[string]string,
	newJarPaths []string,
	newJarPathSet map[string]struct{},
	report *retrieveClassification,
) bool {
	prefix := dir + "/"
	localSub := subtreeMap(oldLocalFiles, dir)
	if len(localSub) == 0 {
		return false
	}

	jarSub := subtreeMap(oldJarFiles, dir)

	localDirect := make(map[string]string)
	localChildDirsSet := make(map[string]struct{})
	for relPath, sum := range localSub {
		rest := strings.TrimPrefix(relPath, prefix)
		if child, _, hasSlash := strings.Cut(rest, "/"); hasSlash {
			localChildDirsSet[child] = struct{}{}
		} else {
			localDirect[relPath] = sum
		}
	}

	jarDirect := make(map[string]string)
	for relPath, sum := range jarSub {
		rest := strings.TrimPrefix(relPath, prefix)
		if !strings.Contains(rest, "/") {
			jarDirect[relPath] = sum
		}
	}

	directUntouched := mapsEqual(localDirect, jarDirect)
	hasUntouchedDirectFiles := directUntouched && len(localDirect) > 0

	if hasUntouchedDirectFiles {
		report.UntouchedDirs = append(report.UntouchedDirs, prefix)
		for _, p := range newJarPaths {
			if !strings.HasPrefix(p, prefix) {
				continue
			}
			rest := strings.TrimPrefix(p, prefix)
			if !strings.Contains(rest, "/") {
				report.ExtractFromNewJar = append(report.ExtractFromNewJar, p)
			}
		}
	} else if !directUntouched {
		directPaths := make([]string, 0, len(localDirect))
		for relPath := range localDirect {
			directPaths = append(directPaths, relPath)
		}
		sort.Strings(directPaths)

		folderReport := touchedFolderReport{Dir: dir}
		for _, relPath := range directPaths {
			baseName := strings.TrimPrefix(relPath, prefix)
			jarSum, inOldJar := jarDirect[relPath]
			if !inOldJar {
				folderReport.Added = append(folderReport.Added, baseName)
				report.CopyFromOld = append(report.CopyFromOld, relPath)
			} else if localDirect[relPath] != jarSum {
				folderReport.Modified = append(folderReport.Modified, baseName)
				report.CopyFromOld = append(report.CopyFromOld, relPath)
			} else if _, inNewJar := newJarPathSet[relPath]; inNewJar {
				folderReport.UpdatedFromNewJar = append(folderReport.UpdatedFromNewJar, baseName)
				report.ExtractFromNewJar = append(report.ExtractFromNewJar, relPath)
			} else {
				folderReport.LegacyFromOld = append(folderReport.LegacyFromOld, baseName)
				report.CopyLegacyFromOld = append(report.CopyLegacyFromOld, relPath)
			}
		}

		for _, p := range newJarPaths {
			if !strings.HasPrefix(p, prefix) {
				continue
			}
			rest := strings.TrimPrefix(p, prefix)
			if strings.Contains(rest, "/") {
				continue
			}
			if _, inOldJar := jarDirect[p]; !inOldJar {
				if _, inOldLocal := localDirect[p]; !inOldLocal {
					folderReport.NewInNewJar = append(folderReport.NewInNewJar, rest)
				}
			}
		}

		sort.Strings(folderReport.NewInNewJar)
		report.TouchedFolders = append(report.TouchedFolders, folderReport)
	}

	childDirs := make([]string, 0, len(localChildDirsSet))
	for child := range localChildDirsSet {
		childDirs = append(childDirs, child)
	}
	sort.Strings(childDirs)

	hadUntouchedChild := false
	for _, child := range childDirs {
		childDir := dir + "/" + child
		if classifyRetrieveNewVersionSubtree(childDir, oldLocalFiles, oldJarFiles, newJarPaths, newJarPathSet, report) {
			hadUntouchedChild = true
		}
	}

	// For a kit directory (which has untouched direct files or untouched sibling folders),
	// also track brand-new subdirectories introduced in newJar that did not exist in oldJar.
	if hasUntouchedDirectFiles || hadUntouchedChild {
		newChildDirs := make(map[string]struct{})
		for _, p := range newJarPaths {
			if !strings.HasPrefix(p, prefix) {
				continue
			}
			rest := strings.TrimPrefix(p, prefix)
			if child, _, hasSlash := strings.Cut(rest, "/"); hasSlash {
				childDir := dir + "/" + child
				if !hasSubtreeFiles(oldJarFiles, childDir) && !hasSubtreeFiles(oldLocalFiles, childDir) {
					newChildDirs[childDir] = struct{}{}
				}
			}
		}
		sortedNewChildren := make([]string, 0, len(newChildDirs))
		for childDir := range newChildDirs {
			sortedNewChildren = append(sortedNewChildren, childDir)
		}
		sort.Strings(sortedNewChildren)
		for _, childDir := range sortedNewChildren {
			report.NewJarDirs = append(report.NewJarDirs, childDir+"/")
		}
	}

	return hasUntouchedDirectFiles || hadUntouchedChild
}

func subtreeMap(files map[string]string, dir string) map[string]string {
	prefix := dir + "/"
	sub := make(map[string]string)
	for relPath, sum := range files {
		if strings.HasPrefix(relPath, prefix) {
			sub[relPath] = sum
		}
	}
	return sub
}

func hasSubtreeFiles(files map[string]string, dir string) bool {
	prefix := dir + "/"
	for relPath := range files {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

// pruneStandardLocalMatchingChecksums removes standard-path files in localDir
// whose SHA-256 matches the corresponding entry in newJarChecksums, then prunes empty directories.
func pruneStandardLocalMatchingChecksums(
	localDir string,
	standardPaths []string,
	localChecksums map[string]string,
	newJarChecksums map[string]string,
	logBoth func(string, ...any),
) error {
	if _, err := os.Stat(localDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if len(standardPaths) == 0 {
		return removeEmptyDirsUpTo(localDir, localDir, logBoth)
	}

	dirsToCheck := make(map[string]struct{})
	for _, relPath := range standardPaths {
		newJarSum, inNewJar := newJarChecksums[relPath]
		if inNewJar && localChecksums[relPath] == newJarSum {
			targetPath := filepath.Join(localDir, filepath.FromSlash(relPath))
			logBoth("  - Removing (matches new jar): %s\n", relPath)
			if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			dirsToCheck[filepath.Dir(targetPath)] = struct{}{}
		}
	}

	dirs := make([]string, 0, len(dirsToCheck))
	for dir := range dirsToCheck {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if err := removeEmptyDirsUpTo(dir, localDir, logBoth); err != nil {
			return err
		}
	}
	return nil
}

func isRetrieveNewVersionPath(slashPath string) bool {
	for _, dir := range retrieveNewVersionDirs {
		if slashPath == dir || strings.HasPrefix(slashPath, dir+"/") {
			return true
		}
	}
	return false
}

func isIgnoredLocalMetadataFile(name string) bool {
	base := filepath.Base(name)
	return slices.Contains([]string{".DS_Store", "Thumbs.db"}, base)
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// collectLocalFiles walks dir and returns a map of slash-separated relative file paths
// to their SHA-256 checksums, skipping OS metadata files.
func collectLocalFiles(dir string) (map[string]string, error) {
	result := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if isIgnoredLocalMetadataFile(d.Name()) {
			return nil
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum, err := fileChecksum(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relPath)] = sum
		return nil
	})
	return result, err
}

// readJarEntryChecksums returns SHA-256 checksums for entries in jarPath whose path is
// in wantedFiles or starts with one of wantedPrefixes.
func readJarEntryChecksums(jarPath string, wantedFiles map[string]struct{}, wantedPrefixes []string) (map[string]string, error) {
	result := make(map[string]string)
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.FileInfo().IsDir() || isIgnoredLocalMetadataFile(f.Name) {
			continue
		}
		matched := false
		if _, ok := wantedFiles[f.Name]; ok {
			matched = true
		} else {
			for _, prefix := range wantedPrefixes {
				if strings.HasPrefix(f.Name, prefix) {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		sum, err := streamChecksum(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		result[f.Name] = sum
	}
	return result, nil
}

// extractIndexedJarEntries extracts requested exact slash-separated file paths and
// prefix trees from already-indexed *zip.File entries into destDir without reopening the JAR.
func extractIndexedJarEntries(entries map[string]*zip.File, destDir string, wantedFiles map[string]struct{}, wantedPrefixes []string) ([]string, []string, error) {
	found := make(map[string]bool, len(wantedFiles))
	var selected []*zip.File
	for name, f := range entries {
		matched := false
		if _, ok := wantedFiles[name]; ok {
			matched = true
		} else {
			for _, prefix := range wantedPrefixes {
				if strings.HasPrefix(name, prefix) {
					matched = true
					break
				}
			}
		}
		if matched {
			selected = append(selected, f)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })

	var extracted []string
	for _, f := range selected {
		targetPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if err := shared.EnsureDir0755(filepath.Dir(targetPath)); err != nil {
			return nil, nil, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			rc.Close()
			return nil, nil, err
		}
		_, copyErr := io.Copy(outFile, rc)
		closeErr := outFile.Close()
		rc.Close()
		if copyErr != nil {
			return nil, nil, copyErr
		}
		if closeErr != nil {
			return nil, nil, closeErr
		}

		found[f.Name] = true
		extracted = append(extracted, f.Name)
	}

	var missing []string
	for path := range wantedFiles {
		if !found[path] {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	return extracted, missing, nil
}

// fileChecksum returns the SHA256 checksum of a file.
func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return streamChecksum(f)
}

// streamChecksum returns the SHA256 checksum of the data read from r.
func streamChecksum(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// copyFile copies a file from src to dst, overwriting dst if it exists.
func copyFile(src, dst string) error {
	if err := shared.EnsureDir0755(filepath.Dir(dst)); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// removeEmptyDirsUpTo removes dirPath and its empty ancestors up to and including basePath.
func removeEmptyDirsUpTo(dirPath, basePath string, logBoth func(string, ...any)) error {
	cleanBase := filepath.Clean(basePath)
	cur := filepath.Clean(dirPath)
	for {
		relPath, err := filepath.Rel(cleanBase, cur)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return nil
		}
		info, err := os.Stat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || !info.IsDir() {
			return err
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return nil
		}
		if relPath == "." {
			logBoth("  - Removing empty local directory: %s\n", cur)
		} else {
			logBoth("  - Removing empty directory: %s\n", relPath)
		}
		if err := os.Remove(cur); err != nil {
			return err
		}
		if cur == cleanBase {
			return nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil
		}
		cur = parent
	}
}

func filterVersions(versions []string, currentVersion string) []string {
	var filtered []string
	for _, version := range versions {
		if version != currentVersion {
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
			if info.ModTime().Before(localDirModTime) {
				// Skip copying if the file is older than the local directory timestamp
				return nil
			}
		}

		log.Printf("Copying file: %s to %s\n", path, destPath) // Log file names being copied

		srcFile, err := os.Open(path)
		if err != nil {
			return shared.WrapFileInUseError(path, err)
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
	installed := getAllInstalledVersions()
	log.Printf("recomputeVersionList called; installed versions from disk: %v", installed)
	versionContainer.Objects = nil // Clear the container
	newVersionList := createVersionList(w, stopButton)

	numVersions := len(installed)
	versionScroll := container.NewVScroll(newVersionList)
	// Ensure the version scroll has enough height to display up to 4 rows
	versionScroll.SetMinSize(fyne.NewSize(0, computeVersionScrollHeight(numVersions)))
	center := container.NewStack(versionScroll)

	if numVersions == 0 {
		// Delegate UI mode switching to the centralized mode functions.
		setOwlcmsTabModeUninstalled(w)
		return
	}

	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 8))
	content := container.NewVBox(spacer, center)

	versionContainer.Objects = nil
	versionContainer.Add(content)
	versionContainer.Refresh()
	log.Println("Version list reinitialized")
}
