package tracker

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"controlpanel/shared"
	"controlpanel/tracker/downloadutils"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ProcessLocalZipFile handles a ZIP file selected from the file system.
func ProcessLocalZipFile(zipPath string, w fyne.Window, trackerInstallDir string,
	updateExplanation func(),
	recomputeVersionList func(fyne.Window),
	checkForNewerVersion func()) {
	shared.ProcessLocalZipFile(zipPath, w, "1.2.3", func(zipPath, version string) {
		InstallLocalZipFile(zipPath, version, w, trackerInstallDir, updateExplanation, recomputeVersionList, checkForNewerVersion)
	})
}

// ZipCurrentSetup creates a ZIP file of a selected installed Tracker version
func ZipCurrentSetup(w fyne.Window, trackerInstallDir string,
	getAllInstalledVersions func() []string,
	selectSaveZip func(fyne.Window, string, func(string, error))) {
	versions := getAllInstalledVersions()
	if len(versions) == 0 {
		dialog.ShowError(fmt.Errorf("no versions installed to zip"), w)
		return
	}

	// Create a dialog to select which version to zip
	versionSelect := widget.NewSelect(versions, func(selected string) {})
	versionSelectContainer := container.NewGridWrap(fyne.NewSize(360, versionSelect.MinSize().Height), versionSelect)
	if len(versions) == 1 {
		versionSelect.Selected = versions[0]
	}

	dialog.ShowForm("Zip Current Setup",
		"Create ZIP",
		"Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Select version to zip", versionSelectContainer),
		},
		func(ok bool) {
			if !ok || versionSelect.Selected == "" {
				return
			}

			version := versionSelect.Selected
			sourceDir := filepath.Join(trackerInstallDir, version)

			// Check if directory exists
			if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
				dialog.ShowError(fmt.Errorf("version directory does not exist: %s", version), w)
				return
			}

			// Validate version name (allows Unicode in metadata)
			if err := shared.ValidateVersionName(version); err != nil {
				dialog.ShowError(fmt.Errorf("invalid version name: %w", err), w)
				return
			}

			baseVersion, existingMetadata := shared.ParseVersionWithBuild(version)
			existingMetadata = shared.NormalizeVersionMetadata(existingMetadata)

			// Preserve existing metadata and append the export timestamp.
			timestamp := time.Now().Format("2006-01-02T150405")
			metadata := timestamp
			if existingMetadata != "" {
				metadata = existingMetadata + "." + timestamp
			}
			zipFileName := fmt.Sprintf("owlcms-tracker_%s+%s.zip", baseVersion, metadata)

			// Ask user where to save the zip file using platform-specific dialog
			selectSaveZip(w, zipFileName, func(zipPath string, err error) {
				if err != nil {
					dialog.ShowError(fmt.Errorf("failed to select save location: %w", err), w)
					return
				}
				if zipPath == "" {
					// User cancelled
					return
				}

				// Create progress dialog
				progressBar := widget.NewProgressBar()
				messageLabel := widget.NewLabel(fmt.Sprintf("Creating ZIP file for version %s...", version))
				progressContent := container.NewVBox(messageLabel, progressBar)
				progressDialog := dialog.NewCustom(
					"Creating ZIP",
					"Please wait...",
					progressContent,
					w)
				progressDialog.Show()

				go func() {
					defer fyne.Do(progressDialog.Hide)

					// Create the zip file
					err := CreateZipArchive(sourceDir, zipPath, func(progress float64) {
						fyne.Do(func() {
							progressBar.SetValue(progress)
						})
					})

					if err != nil {
						fyne.Do(func() {
							dialog.ShowError(fmt.Errorf("failed to create ZIP file: %w", err), w)
						})
						return
					}

					fyne.Do(func() {
						dialog.ShowInformation("Success",
							fmt.Sprintf("Successfully created ZIP file:\n%s", zipPath), w)
					})
				}()
			})
		}, w)
}

// GetInstallationDirectoryName determines the directory name for installing a version,
// handling collisions by adding version suffixes to metadata.
func GetInstallationDirectoryName(baseVersion string, trackerInstallDir string) string {
	basePath := filepath.Join(trackerInstallDir, baseVersion)

	// Check if the base version directory already exists
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		// No collision, use base version as-is
		return baseVersion
	}

	// Collision detected - need to add version suffix
	// Determine if version has metadata
	var versionPrefix, metadata string
	if strings.Contains(baseVersion, "+") {
		// Has metadata: split on + and build pattern with .NN suffix on metadata
		parts := strings.Split(baseVersion, "+")
		versionPrefix = parts[0] + "+"
		metadata = parts[1]
	} else {
		// No metadata: we'll add +NN
		versionPrefix = baseVersion + "+"
		metadata = ""
	}

	// Scan for existing versioned directories
	maxSuffix := 0
	entries, err := os.ReadDir(trackerInstallDir)
	if err == nil {
		// Build pattern to match existing versioned directories
		var pattern *regexp.Regexp
		if metadata != "" {
			// Pattern: versionPrefix + metadata + .NN (e.g., "1.0.0-rc02+fhq.01")
			escapedMetadata := regexp.QuoteMeta(metadata)
			pattern = regexp.MustCompile(fmt.Sprintf(`^%s%s\.(\d{2})$`,
				regexp.QuoteMeta(versionPrefix), escapedMetadata))
		} else {
			// Pattern: versionPrefix + NN (e.g., "1.0.0+01")
			pattern = regexp.MustCompile(fmt.Sprintf(`^%s(\d{2})$`,
				regexp.QuoteMeta(versionPrefix)))
		}

		for _, entry := range entries {
			if entry.IsDir() {
				matches := pattern.FindStringSubmatch(entry.Name())
				if len(matches) == 2 {
					var suffix int
					if _, err := fmt.Sscanf(matches[1], "%d", &suffix); err == nil {
						if suffix > maxSuffix {
							maxSuffix = suffix
						}
					}
				}
			}
		}
	}

	nextSuffix := maxSuffix + 1
	if metadata != "" {
		return fmt.Sprintf("%s%s.%02d", versionPrefix, metadata, nextSuffix)
	}
	return fmt.Sprintf("%s%02d", versionPrefix, nextSuffix)
}

// InstallLocalZipFile installs Tracker from a local ZIP file.
func InstallLocalZipFile(zipPath, version string, w fyne.Window, trackerInstallDir string,
	updateExplanation func(),
	recomputeVersionList func(fyne.Window),
	checkForNewerVersion func()) {
	shared.PromptForInstallVersionName(trackerInstallDir, version, w, func(finalVersionName string) {
		progressBar := widget.NewProgressBar()
		progressBar.SetValue(0.01)
		messageLabel := widget.NewLabel(fmt.Sprintf("Installing Tracker %s from local file...", finalVersionName))
		content := container.NewVBox(messageLabel, progressBar)
		progressDialog := dialog.NewCustom(
			"Installing Tracker",
			"Please wait...",
			content,
			w)
		progressDialog.Show()

		// Ensure the tracker directory exists
		trackerDir := trackerInstallDir
		if _, err := os.Stat(trackerDir); os.IsNotExist(err) {
			if err := shared.EnsureDir0755(trackerDir); err != nil {
				progressDialog.Hide()
				dialog.ShowError(fmt.Errorf("creating tracker directory: %w", err), w)
				return
			}
		}

		originalFileName := filepath.Base(zipPath)
		destOriginalPath := filepath.Join(trackerDir, originalFileName)

		messageLabel.SetText("Copying ZIP file...")
		messageLabel.Refresh()

		progressBar.SetValue(0.02)

		// Preserve a copy of the ZIP file in the installation directory for reference
		if zipPath != destOriginalPath {
			err := copyFile(zipPath, destOriginalPath)
			if err != nil {
				progressDialog.Hide()
				dialog.ShowError(fmt.Errorf("failed to copy ZIP file: %w", err), w)
				return
			}
		}

		go func() {
			finalExtractPath := filepath.Join(trackerDir, finalVersionName)

			fyne.Do(func() {
				messageLabel.SetText("Extracting files...")
				messageLabel.Refresh()
			})

			log.Printf("Extracting ZIP file to: %s\n", finalExtractPath)
			extractProgress := func(extracted, total int64) {
				if total > 0 {
					progress := float64(extracted) / float64(total)
					fyne.Do(func() {
						progressBar.SetValue(progress)
					})
				}
			}
			err := downloadutils.ExtractZip(destOriginalPath, finalExtractPath, extractProgress)
			if err != nil {
				fyne.Do(func() {
					progressDialog.Hide()
					dialog.ShowError(fmt.Errorf("extraction failed: %w", err), w)
				})
				return
			}
			if err := EnsureReleaseEnvFromParent(finalVersionName); err != nil {
				fyne.Do(func() {
					progressDialog.Hide()
					dialog.ShowError(fmt.Errorf("creating release env.properties: %w", err), w)
				})
				return
			}

			fyne.Do(func() {
				progressBar.SetValue(1.0)
			})
			log.Println("Extraction completed")

			message := fmt.Sprintf(
				"Successfully installed Tracker version %s\n\n"+
					"Location: %s\n\n"+
					"The program files have been extracted to the above directory.",
				finalVersionName, finalExtractPath)

			fyne.Do(func() {
				// Ensure the tab UI is initialized so download UI widgets exist
				initializeTab(w)
				updateExplanation()
				progressDialog.Hide()
				dialog.ShowInformation("Installation Complete", message, w)
				recomputeVersionList(w)
				checkForNewerVersion()
			})
		}()
	})
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

// CreateZipArchive creates a zip file from a directory
func CreateZipArchive(sourceDir, zipPath string, progressCallback func(float64)) error {
	// Count total files first for progress tracking
	var totalFiles int
	filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			totalFiles++
		}
		return nil
	})

	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	var processedFiles int
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Create zip header
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		// Use forward slashes in zip paths
		header.Name = filepath.ToSlash(relPath)

		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(writer, file)
			if err != nil {
				return err
			}

			processedFiles++
			if progressCallback != nil && totalFiles > 0 {
				progressCallback(float64(processedFiles) / float64(totalFiles))
			}
		}

		return nil
	})

	return err
}
