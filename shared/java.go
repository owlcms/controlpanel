package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func defaultControlPanelInstallDir() string {
	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = "linux"
		if os.PathSeparator == '\\' {
			goos = "windows"
		} else if _, err := os.Stat("/Applications"); err == nil {
			goos = "darwin"
		}
	}

	switch goos {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "owlcms-controlpanel")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "owlcms-controlpanel")
	case "linux":
		return filepath.Join(os.Getenv("HOME"), ".local", "share", "owlcms-controlpanel")
	default:
		return "./owlcms-controlpanel"
	}
}

// DefaultControlPanelInstallDir returns the platform default control panel
// configuration directory without consulting environment overrides.
func DefaultControlPanelInstallDir() string {
	return defaultControlPanelInstallDir()
}

// GetControlPanelInstallDir returns the control panel configuration directory.
func GetControlPanelInstallDir() string {
	if dir := strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTALLDIR")); dir != "" {
		return dir
	}

	return defaultControlPanelInstallDir()
}

// GetRuntimeDir returns the shared runtime directory for Java, Node.js, and FFmpeg.
// When unset, it falls back to the control panel configuration directory.
func GetRuntimeDir() string {
	if dir := strings.TrimSpace(os.Getenv("RUNTIME_DIR")); dir != "" {
		return dir
	}

	return GetControlPanelInstallDir()
}

// GetSharedJavaDir returns the shared Java installation directory for a specific version
// The version string should be in format like "jdk-17.0.15+6"
func GetSharedJavaDir(temurinVersion string) string {
	controlPanelDir := GetRuntimeDir()
	// Store Java installations in a version-specific subdirectory
	// This allows multiple versions to coexist if needed
	return filepath.Join(controlPanelDir, "java", temurinVersion)
}

// CheckAndInstallJava centralizes the pattern of checking for and installing
// a Java runtime for a module. The concrete check/install work is performed
// by the provided callback `javaCheck`, which should encapsulate module-\
// specific behavior (install dir, version selection, etc.). `requiredVersion`
// is informational and currently unused by shared code but provided for
// future extensibility.
func CheckAndInstallJava(requiredVersion string, statusLabel *widget.Label, w fyne.Window, javaCheck func(*widget.Label) error) error {
	if javaCheck == nil {
		return fmt.Errorf("no java check function provided")
	}

	if err := javaCheck(statusLabel); err != nil {
		dialog.ShowError(fmt.Errorf("check/install of Java runtime failed: %w", err), w)
		return err
	}
	return nil
}

// DownloadAndInstallJava downloads and installs a specific Java version
func DownloadAndInstallJava(temurinVersion string, statusLabel *widget.Label, w fyne.Window, goosFunc func() string) error {
	if statusLabel != nil {
		statusLabel.SetText("Downloading required Java version...")
		statusLabel.Refresh()
		statusLabel.Show()
	}

	// Create a cancel channel
	cancel := make(chan bool)

	// Show the progress dialog if window provided
	var progressDialog dialog.Dialog
	var progressBar *widget.ProgressBar
	if w != nil {
		progressBar = widget.NewProgressBar()
		progressDialog = dialog.NewCustom("Installing Java", "Cancel", progressBar, w)
		progressDialog.Show()
		progressBar.SetValue(0.01)
	}

	// Use shared Java directory
	javaDir := GetSharedJavaDir(temurinVersion)

	// Recursively delete the java directory if it exists
	if _, err := os.Stat(javaDir); err == nil {
		if err := os.RemoveAll(javaDir); err != nil {
			if progressDialog != nil {
				progressDialog.Hide()
			}
			return fmt.Errorf("failed to delete existing java directory: %w", err)
		}
	}

	// Ensure the java directory exists
	if err := EnsureDir0755(javaDir); err != nil {
		if progressDialog != nil {
			progressDialog.Hide()
		}
		return fmt.Errorf("creating java directory: %w", err)
	}

	// Show activity while getting the download URL
	if progressBar != nil {
		progressBar.SetValue(0.05)
	}
	downloadURL, err := GetTemurinDownloadURL(temurinVersion, goosFunc, "controlpanel")
	if err != nil {
		if progressDialog != nil {
			progressDialog.Hide()
		}
		return fmt.Errorf("getting Temurin download URL: %w", err)
	}

	archivePath := filepath.Join(javaDir, "temurin")
	if goosFunc() == "windows" && !IsWSL() {
		archivePath += ".zip"
	} else {
		archivePath += ".tar.gz"
	}

	progressCallback := func(downloaded, total int64) {
		if total > 0 && progressBar != nil {
			percentage := float64(downloaded) / float64(total)
			progressBar.SetValue(percentage)
		}
	}

	if err := DownloadArchive(downloadURL, archivePath, progressCallback, cancel); err != nil {
		if progressDialog != nil {
			progressDialog.Hide()
		}
		if err.Error() == "download cancelled" {
			// Clean up the incomplete archive file
			os.Remove(archivePath)
			return nil
		}
		return fmt.Errorf("error downloading Java: %w", err)
	}

	// Show extraction progress
	if progressBar != nil {
		progressBar.SetValue(0.9)
	}
	if goosFunc() == "windows" && !IsWSL() {
		if err := ExtractZip(archivePath, javaDir); err != nil {
			if progressDialog != nil {
				progressDialog.Hide()
			}
			return fmt.Errorf("error extracting Temurin zip: %w", err)
		}
	} else {
		if err := ExtractTarGz(archivePath, javaDir); err != nil {
			if progressDialog != nil {
				progressDialog.Hide()
			}
			return fmt.Errorf("extracting Temurin tar.gz: %w", err)
		}
	}

	// Indicate completion
	if progressBar != nil {
		progressBar.SetValue(1.0)
	}
	if progressDialog != nil {
		progressDialog.Hide()
	}

	return nil
}

// scanEnvPropertiesForJavaVersions scans all env.properties files in owlcms and firmata directories
// and returns all unique TEMURIN_VERSION values found
func scanEnvPropertiesForJavaVersions(owlcmsInstallDir, firmataInstallDir string) ([]string, error) {
	var versions []string
	seenVersions := make(map[string]bool)

	// Helper function to read TEMURIN_VERSION from a properties file
	readVersion := func(path string) (string, error) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "", nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		// Simple parsing - look for TEMURIN_VERSION=value
		lines := strings.SplitSeq(string(content), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "TEMURIN_VERSION="); ok {
				version := after
				return strings.TrimSpace(version), nil
			}
		}
		return "", nil
	}

	// Scan owlcms directory
	owlcmsEnvPath := filepath.Join(owlcmsInstallDir, "env.properties")
	if version, err := readVersion(owlcmsEnvPath); err == nil && version != "" {
		if !seenVersions[version] {
			versions = append(versions, version)
			seenVersions[version] = true
		}
	}

	// Scan owlcms version-specific directories
	if entries, err := os.ReadDir(owlcmsInstallDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				versionEnvPath := filepath.Join(owlcmsInstallDir, entry.Name(), "env.properties")
				if version, err := readVersion(versionEnvPath); err == nil && version != "" {
					if !seenVersions[version] {
						versions = append(versions, version)
						seenVersions[version] = true
					}
				}
			}
		}
	}

	// Scan firmata directory
	firmataEnvPath := filepath.Join(firmataInstallDir, "env.properties")
	if version, err := readVersion(firmataEnvPath); err == nil && version != "" {
		if !seenVersions[version] {
			versions = append(versions, version)
			seenVersions[version] = true
		}
	}

	// Scan firmata version-specific directories
	if entries, err := os.ReadDir(firmataInstallDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				versionEnvPath := filepath.Join(firmataInstallDir, entry.Name(), "env.properties")
				if version, err := readVersion(versionEnvPath); err == nil && version != "" {
					if !seenVersions[version] {
						versions = append(versions, version)
						seenVersions[version] = true
					}
				}
			}
		}
	}

	return versions, nil
}

// CleanupObsoleteJavaVersions scans env.properties files in the control panel,
// finds the highest required Java version, ensures it's installed,
// removes older control panel Java versions, and removes legacy bundled Java
func CleanupObsoleteJavaVersions(owlcmsInstallDir, firmataInstallDir string, statusLabel *widget.Label, w fyne.Window) ([]string, error) {
	runtimeDir := GetRuntimeDir()
	javaBaseDir := filepath.Join(runtimeDir, "java")

	var removed []string

	// Step 1: Find all required Java versions from env.properties files in control panel structure
	controlPanelOwlcmsDir := owlcmsInstallDir
	controlPanelFirmataDir := firmataInstallDir
	requiredVersions, err := scanEnvPropertiesForJavaVersions(controlPanelOwlcmsDir, controlPanelFirmataDir)
	if err != nil {
		return nil, fmt.Errorf("scanning env.properties files: %w", err)
	}

	// Step 2: Determine the highest major version required
	highestMajor := 0
	for _, version := range requiredVersions {
		majorVer, err := ExtractMajorVersion(version)
		if err != nil {
			continue
		}
		if majorVer > highestMajor {
			highestMajor = majorVer
		}
	}

	// If no versions found in env.properties, default to jdk-25
	if highestMajor == 0 {
		highestMajor = 25
	}

	// Step 3: Check if the required Java version (or newer) exists in control panel
	if _, err := os.Stat(javaBaseDir); os.IsNotExist(err) {
		// No Java directory exists, need to install
		if err := EnsureDir0755(javaBaseDir); err != nil {
			return nil, fmt.Errorf("creating java directory: %w", err)
		}
	}

	// Check for any Java version >= required major version
	hasRequiredJava := false
	if entries, err := os.ReadDir(javaBaseDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				majorVer, err := ExtractMajorVersion(entry.Name())
				if err == nil && majorVer >= highestMajor {
					hasRequiredJava = true
					break
				}
			}
		}
	}

	// If required Java not found, attempt to download it
	if !hasRequiredJava {
		// Determine the version string to download
		versionToDownload := fmt.Sprintf("jdk-%d", highestMajor)

		// Try to download and install
		if statusLabel != nil {
			statusLabel.SetText(fmt.Sprintf("Java %d required but not found. Downloading...", highestMajor))
			statusLabel.Refresh()
		}

		err := DownloadAndInstallJava(versionToDownload, statusLabel, w, func() string {
			if os.PathSeparator == '\\' {
				return "windows"
			} else if _, err := os.Stat("/Applications"); err == nil {
				return "darwin"
			}
			return "linux"
		})

		if err != nil {
			removed = append(removed, fmt.Sprintf("Required Java %d will be downloaded on next launch (%v)", highestMajor, err))
		} else {
			removed = append(removed, fmt.Sprintf("Downloaded and installed Java %d", highestMajor))
		}
	}

	// Step 4: Remove all Java versions older than the highest major version
	if entries, err := os.ReadDir(javaBaseDir); err == nil {
		versionsByMajor := make(map[int][]string)

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			versionName := entry.Name()
			majorVer, err := ExtractMajorVersion(versionName)
			if err != nil {
				continue
			}
			versionsByMajor[majorVer] = append(versionsByMajor[majorVer], versionName)
		}

		// Remove all versions of older major versions
		for majorVer, versions := range versionsByMajor {
			if majorVer < highestMajor {
				for _, version := range versions {
					versionPath := filepath.Join(javaBaseDir, version)
					if err := os.RemoveAll(versionPath); err != nil {
						return removed, fmt.Errorf("removing %s: %w", version, err)
					}
					removed = append(removed, fmt.Sprintf("%s (superseded by Java %d)", version, highestMajor))
				}
			}
		}

		// For the highest major version, keep only the latest patch version
		if versions, exists := versionsByMajor[highestMajor]; exists && len(versions) > 1 {
			// Sort to find the latest version
			sortedVersions := make([]string, len(versions))
			copy(sortedVersions, versions)

			// Sort using CompareJDKVersions (latest first)
			for i := range sortedVersions {
				for j := i + 1; j < len(sortedVersions); j++ {
					if CompareJDKVersions(sortedVersions[j], sortedVersions[i]) {
						sortedVersions[i], sortedVersions[j] = sortedVersions[j], sortedVersions[i]
					}
				}
			}

			latestVersion := sortedVersions[0]

			// Remove all older patch versions
			for _, version := range sortedVersions[1:] {
				versionPath := filepath.Join(javaBaseDir, version)
				if err := os.RemoveAll(versionPath); err != nil {
					return removed, fmt.Errorf("removing %s: %w", version, err)
				}
				removed = append(removed, fmt.Sprintf("%s (kept latest: %s)", version, latestVersion))
			}
		}
	}

	// Step 5: Remove legacy Java from owlcms and firmata directories
	owlcmsJavaDirs := []string{
		filepath.Join(owlcmsInstallDir, "java17"),
		filepath.Join(owlcmsInstallDir, "java"),
	}
	for _, javaDir := range owlcmsJavaDirs {
		if _, err := os.Stat(javaDir); err == nil {
			if err := os.RemoveAll(javaDir); err != nil {
				return removed, fmt.Errorf("removing legacy owlcms java: %w", err)
			}
			removed = append(removed, fmt.Sprintf("Legacy Java from owlcms (%s)", filepath.Base(javaDir)))
		}
	}

	firmataJavaDirs := []string{
		filepath.Join(firmataInstallDir, "java17"),
		filepath.Join(firmataInstallDir, "java"),
	}
	for _, javaDir := range firmataJavaDirs {
		if _, err := os.Stat(javaDir); err == nil {
			if err := os.RemoveAll(javaDir); err != nil {
				return removed, fmt.Errorf("removing legacy firmata java: %w", err)
			}
			removed = append(removed, fmt.Sprintf("Legacy Java from firmata (%s)", filepath.Base(javaDir)))
		}
	}

	return removed, nil
}
