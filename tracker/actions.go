package tracker

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"controlpanel/shared"
	"controlpanel/tracker/downloadutils"
)

// ActionResult describes the installed tracker version affected by a non-UI action.
type ActionResult struct {
	Version          string
	Path             string
	LocalFilesCopied bool
}

func ensureReleaseCatalog() ([]string, error) {
	if len(allReleases) == 0 {
		releases, err := fetchReleases()
		if err != nil {
			return nil, err
		}
		allReleases = releases
	}
	return allReleases, nil
}

// ResolveInstallRelease resolves a GitHub release selector for a clean install.
func ResolveInstallRelease(selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" || strings.EqualFold(selector, "latest") {
		if _, err := ensureReleaseCatalog(); err != nil {
			return "", err
		}
		return getMostRecentStableRelease()
	}
	return selector, nil
}

// ResolveUpdateRelease resolves a GitHub release selector for an update target.
func ResolveUpdateRelease(selector, fromVersion string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector != "" && !strings.EqualFold(selector, "latest") {
		return selector, nil
	}

	if _, err := ensureReleaseCatalog(); err != nil {
		return "", err
	}
	if containsPreReleaseTag(fromVersion) {
		return getMostRecentPrerelease()
	}
	return getMostRecentStableRelease()
}

func releaseAssetURL(version string) (string, string, error) {
	assetNames := getAssetNames(version)
	for _, name := range assetNames {
		assetURL := fmt.Sprintf("https://github.com/owlcms/owlcms-tracker/releases/download/%s/%s", version, name)
		if checkAssetExists(assetURL) {
			return assetURL, name, nil
		}
	}
	return "", "", fmt.Errorf("no tracker release asset found for version %s (tried: %v)", version, assetNames)
}

// InstallRelease downloads and extracts a clean Tracker release.
func InstallRelease(downloadVersion, installVersion string, downloadProgress, extractProgress func(int64, int64)) (ActionResult, error) {
	downloadVersion = strings.TrimSpace(downloadVersion)
	installVersion = strings.TrimSpace(installVersion)
	if downloadVersion == "" {
		return ActionResult{}, fmt.Errorf("download version is required")
	}
	if installVersion == "" {
		installVersion = downloadVersion
	}

	zipURL, assetName, err := releaseAssetURL(downloadVersion)
	if err != nil {
		return ActionResult{}, err
	}
	if err := shared.EnsureDir0755(installDir); err != nil {
		return ActionResult{}, fmt.Errorf("creating tracker directory: %w", err)
	}

	zipPath := filepath.Join(installDir, assetName)
	extractPath := filepath.Join(installDir, installVersion)
	if _, err := os.Stat(extractPath); err == nil {
		return ActionResult{}, fmt.Errorf("tracker version %q already exists", installVersion)
	} else if !os.IsNotExist(err) {
		return ActionResult{}, fmt.Errorf("checking install directory %s: %w", extractPath, err)
	}

	if err := downloadutils.DownloadArchive(zipURL, zipPath, downloadProgress, nil); err != nil {
		_ = os.Remove(zipPath)
		return ActionResult{}, fmt.Errorf("download failed: %w", err)
	}
	if err := downloadutils.ExtractZip(zipPath, extractPath, extractProgress); err != nil {
		_ = os.RemoveAll(extractPath)
		return ActionResult{}, fmt.Errorf("extraction failed: %w", err)
	}
	if err := EnsureReleaseEnvFromParent(installVersion); err != nil {
		_ = os.RemoveAll(extractPath)
		return ActionResult{}, fmt.Errorf("creating release env.properties: %w", err)
	}

	return ActionResult{Version: installVersion, Path: extractPath}, nil
}

// UpdateRelease downloads targetVersion and migrates local data from existingVersion.
func UpdateRelease(existingVersion, targetVersion string, downloadProgress, extractProgress func(int64, int64)) (ActionResult, error) {
	existingVersion = strings.TrimSpace(existingVersion)
	targetVersion = strings.TrimSpace(targetVersion)
	if existingVersion == "" {
		return ActionResult{}, fmt.Errorf("source version is required")
	}
	if targetVersion == "" {
		return ActionResult{}, fmt.Errorf("target version is required")
	}

	currentVersionDir := filepath.Join(installDir, existingVersion)
	if info, err := os.Stat(currentVersionDir); err != nil {
		return ActionResult{}, fmt.Errorf("source version %q not found: %w", existingVersion, err)
	} else if !info.IsDir() {
		return ActionResult{}, fmt.Errorf("source version %q is not a directory", existingVersion)
	}
	if _, isCustom := readCustomBuildPlugins(currentVersionDir); isCustom {
		return ActionResult{}, fmt.Errorf("%s", updateCustomBuildBlockMessage(existingVersion, targetVersion))
	}

	targetBaseVersion, _ := shared.ParseVersionWithBuild(targetVersion)
	existingBuild := shared.GetCurrentBuildString(existingVersion)
	targetInstallVersion := targetVersion
	if existingBuild != "" {
		resolvedBuild := shared.ResolveCollisionForBuild(installDir, targetBaseVersion, existingBuild)
		targetInstallVersion = fmt.Sprintf("%s+%s", targetBaseVersion, resolvedBuild)
	}

	extractDir := filepath.Join(installDir, targetInstallVersion)
	if _, err := os.Stat(extractDir); err == nil {
		return ActionResult{}, fmt.Errorf("target install version %q already exists", targetInstallVersion)
	} else if !os.IsNotExist(err) {
		return ActionResult{}, fmt.Errorf("checking target install directory: %w", err)
	}

	zipURL, assetName, err := releaseAssetURL(targetVersion)
	if err != nil {
		return ActionResult{}, err
	}
	zipPath := filepath.Join(installDir, assetName)

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(extractDir)
		}
	}()

	if err := downloadutils.DownloadArchive(zipURL, zipPath, downloadProgress, nil); err != nil {
		_ = os.Remove(zipPath)
		return ActionResult{}, fmt.Errorf("download failed: %w", err)
	}
	if err := downloadutils.ExtractZip(zipPath, extractDir, extractProgress); err != nil {
		return ActionResult{}, fmt.Errorf("extraction failed: %w", err)
	}
	if err := removeCustomBuildMarker(extractDir); err != nil {
		return ActionResult{}, fmt.Errorf("failed to remove custom build marker: %w", err)
	}

	result := ActionResult{Version: targetInstallVersion, Path: extractDir}
	if err := copyFiles(filepath.Join(currentVersionDir, "local"), filepath.Join(extractDir, "local"), true); err != nil {
		log.Printf("No local files to copy from %s: %v", currentVersionDir, err)
	} else {
		result.LocalFilesCopied = true
	}

	success = true
	return result, nil
}

func validateImportCompatibility(sourceVersion, targetVersion, sourceDir, destDir string, allowMismatchedCustomBuilds bool) error {
	sourcePlugins, sourceIsCustom := readCustomBuildPlugins(sourceDir)
	destPlugins, destIsCustom := readCustomBuildPlugins(destDir)
	if sourceIsCustom && destIsCustom {
		if customBuildPluginsEqual(sourcePlugins, destPlugins) || allowMismatchedCustomBuilds {
			return nil
		}
		return fmt.Errorf("%s", importBlockMessage(sourceVersion, targetVersion, sourcePlugins, sourceIsCustom, destPlugins, destIsCustom))
	}
	if sourceIsCustom || destIsCustom {
		return fmt.Errorf("%s", importBlockMessage(sourceVersion, targetVersion, sourcePlugins, sourceIsCustom, destPlugins, destIsCustom))
	}
	return nil
}

// ImportDataAndConfig migrates local data/config from sourceVersion to destVersion.
func ImportDataAndConfig(sourceVersion, destVersion string) (ActionResult, error) {
	return importDataAndConfig(sourceVersion, destVersion, false)
}

func importDataAndConfig(sourceVersion, destVersion string, allowMismatchedCustomBuilds bool) (ActionResult, error) {
	sourceVersion = strings.TrimSpace(sourceVersion)
	destVersion = strings.TrimSpace(destVersion)
	sourceDir := filepath.Join(installDir, sourceVersion)
	destDir := filepath.Join(installDir, destVersion)
	if info, err := os.Stat(sourceDir); err != nil {
		return ActionResult{}, fmt.Errorf("source version %q does not exist: %w", sourceVersion, err)
	} else if !info.IsDir() {
		return ActionResult{}, fmt.Errorf("source version %q is not a directory", sourceVersion)
	}
	if info, err := os.Stat(destDir); err != nil {
		return ActionResult{}, fmt.Errorf("destination version %q does not exist: %w", destVersion, err)
	} else if !info.IsDir() {
		return ActionResult{}, fmt.Errorf("destination version %q is not a directory", destVersion)
	}
	if err := validateImportCompatibility(sourceVersion, destVersion, sourceDir, destDir, allowMismatchedCustomBuilds); err != nil {
		return ActionResult{}, err
	}

	result := ActionResult{Version: destVersion, Path: destDir}
	if err := copyFiles(filepath.Join(sourceDir, "local"), filepath.Join(destDir, "local"), true); err != nil {
		log.Printf("No local files to copy from %s: %v", sourceDir, err)
	} else {
		result.LocalFilesCopied = true
	}
	return result, nil
}

// DuplicateInstalledVersion copies an installed Tracker version to an exact new directory name.
func DuplicateInstalledVersion(fromVersion, newVersion string) (ActionResult, error) {
	created, err := shared.DuplicateVersionDirectory(installDir, fromVersion, newVersion)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Version: created, Path: filepath.Join(installDir, created)}, nil
}

// RemoveInstalledVersion removes an installed Tracker version directory.
func RemoveInstalledVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	dir := filepath.Join(installDir, version)
	if info, err := os.Stat(dir); err != nil {
		return fmt.Errorf("tracker version %q is not installed: %w", version, err)
	} else if !info.IsDir() {
		return fmt.Errorf("tracker version %q is not a directory", version)
	}
	return os.RemoveAll(dir)
}
