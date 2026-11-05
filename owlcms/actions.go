package owlcms

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"controlpanel/shared"
)

// ActionResult describes the installed version affected by a non-UI action.
type ActionResult struct {
	Version          string
	Path             string
	DatabaseCopied   bool
	EnvCopied        bool
	LocalFilesCopied bool
}

func ensureReleaseCatalog(includePrereleases bool) ([]string, error) {
	if len(allReleases) == 0 || (includePrereleases && !releaseCatalogHasPrerelease(allReleases)) {
		releases, err := fetchReleasesForCatalog(includePrereleases)
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
		if _, err := ensureReleaseCatalog(false); err != nil {
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

	includePrereleases := containsPreReleaseTag(fromVersion)
	if _, err := ensureReleaseCatalog(includePrereleases); err != nil {
		return "", err
	}
	if includePrereleases {
		return getMostRecentPrerelease()
	}
	return getMostRecentStableRelease()
}

// InstallRelease downloads and extracts a clean OWLCMS release.
func InstallRelease(downloadVersion, installVersion string, progress shared.ProgressCallback) (ActionResult, error) {
	downloadVersion = strings.TrimSpace(downloadVersion)
	installVersion = strings.TrimSpace(installVersion)
	if downloadVersion == "" {
		return ActionResult{}, fmt.Errorf("download version is required")
	}
	if installVersion == "" {
		installVersion = downloadVersion
	}

	var urlPrefix string
	if containsPreReleaseTag(downloadVersion) {
		urlPrefix = "https://github.com/owlcms/prereleases/releases/download"
	} else {
		urlPrefix = "https://github.com/owlcms/releases/releases/download"
	}
	fileName := fmt.Sprintf("owlcms_%s.zip", downloadVersion)
	zipURL := fmt.Sprintf("%s/%s/%s", urlPrefix, downloadVersion, fileName)

	if err := shared.EnsureDir0755(installDir); err != nil {
		return ActionResult{}, fmt.Errorf("creating owlcms directory: %w", err)
	}

	zipPath := filepath.Join(installDir, fileName)
	extractPath := filepath.Join(installDir, installVersion)
	if _, err := os.Stat(extractPath); err == nil {
		return ActionResult{}, fmt.Errorf("OWLCMS version %q already exists", installVersion)
	} else if !os.IsNotExist(err) {
		return ActionResult{}, fmt.Errorf("checking install directory %s: %w", extractPath, err)
	}

	if err := shared.DownloadArchive(zipURL, zipPath, progress, nil); err != nil {
		_ = os.Remove(zipPath)
		return ActionResult{}, fmt.Errorf("download failed: %w", err)
	}
	if err := shared.ExtractZip(zipPath, extractPath); err != nil {
		_ = os.RemoveAll(extractPath)
		return ActionResult{}, fmt.Errorf("extraction failed: %w", err)
	}
	if err := normalizeExtractedDir(extractPath); err != nil {
		_ = os.RemoveAll(extractPath)
		return ActionResult{}, fmt.Errorf("failed to finalize install directory: %w", err)
	}
	if err := EnsureReleaseEnvFromParent(installVersion); err != nil {
		_ = os.RemoveAll(extractPath)
		return ActionResult{}, fmt.Errorf("failed to create release env.properties: %w", err)
	}

	return ActionResult{Version: installVersion, Path: extractPath}, nil
}

// UpdateRelease downloads targetVersion and migrates data/config from existingVersion.
func UpdateRelease(existingVersion, targetVersion string, progress shared.ProgressCallback, cancel <-chan bool) (ActionResult, error) {
	existingVersion = strings.TrimSpace(existingVersion)
	targetVersion = strings.TrimSpace(targetVersion)
	if existingVersion == "" {
		return ActionResult{}, fmt.Errorf("source version is required")
	}
	if targetVersion == "" {
		return ActionResult{}, fmt.Errorf("target version is required")
	}

	existingVersionDir := filepath.Join(installDir, existingVersion)
	if info, err := os.Stat(existingVersionDir); err != nil {
		return ActionResult{}, fmt.Errorf("source version %q not found: %w", existingVersion, err)
	} else if !info.IsDir() {
		return ActionResult{}, fmt.Errorf("source version %q is not a directory", existingVersion)
	}

	targetInstallVersion := computeUpdateTargetVersion(existingVersion, targetVersion)
	newVersionDir := filepath.Join(installDir, targetInstallVersion)
	if _, err := os.Stat(newVersionDir); err == nil {
		return ActionResult{}, fmt.Errorf("target install version %q already exists", targetInstallVersion)
	} else if !os.IsNotExist(err) {
		return ActionResult{}, fmt.Errorf("checking target install directory: %w", err)
	}

	var urlPrefix string
	if containsPreReleaseTag(targetVersion) {
		urlPrefix = "https://github.com/owlcms/prereleases/releases/download"
	} else {
		urlPrefix = "https://github.com/owlcms/releases/releases/download"
	}
	fileName := fmt.Sprintf("owlcms_%s.zip", targetVersion)
	zipURL := fmt.Sprintf("%s/%s/%s", urlPrefix, targetVersion, fileName)
	zipPath := filepath.Join(installDir, fileName)

	if err := shared.DownloadArchive(zipURL, zipPath, progress, cancel); err != nil {
		_ = os.Remove(zipPath)
		return ActionResult{}, fmt.Errorf("download failed: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(newVersionDir)
		}
	}()

	if err := shared.ExtractZip(zipPath, newVersionDir); err != nil {
		return ActionResult{}, fmt.Errorf("extraction failed: %w", err)
	}

	result := ActionResult{Version: targetInstallVersion, Path: newVersionDir}
	existingDatabaseDir := filepath.Join(existingVersionDir, "database")
	if _, statErr := os.Stat(existingDatabaseDir); !os.IsNotExist(statErr) {
		if err := copyFiles(existingDatabaseDir, filepath.Join(newVersionDir, "database"), true); err != nil {
			return ActionResult{}, fmt.Errorf("failed to copy database: %w", err)
		}
		result.DatabaseCopied = true
	} else {
		log.Printf("Database directory does not exist in %s", existingDatabaseDir)
	}

	srcEnv := filepath.Join(existingVersionDir, "env.properties")
	if _, statErr := os.Stat(srcEnv); statErr == nil {
		if err := copyFile(srcEnv, filepath.Join(newVersionDir, "env.properties")); err != nil {
			return ActionResult{}, fmt.Errorf("failed to copy env.properties: %w", err)
		}
		result.EnvCopied = true
	}

	if err := restoreLocalFilesFromPreviousVersion(newVersionDir, existingVersionDir); err != nil {
		return ActionResult{}, fmt.Errorf("failed to restore local files: %w", err)
	}
	result.LocalFilesCopied = true
	success = true
	return result, nil
}

// ImportDataAndConfig migrates local data/config from sourceVersion to destVersion.
func ImportDataAndConfig(sourceVersion, destVersion string) (ActionResult, error) {
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

	result := ActionResult{Version: destVersion, Path: destDir}
	if err := copyFiles(filepath.Join(sourceDir, "database"), filepath.Join(destDir, "database"), true); err != nil {
		log.Printf("No database files to copy from %s: %v", sourceDir, err)
	} else {
		result.DatabaseCopied = true
	}

	srcEnv := filepath.Join(sourceDir, "env.properties")
	if _, err := os.Stat(srcEnv); err == nil {
		if err := copyFile(srcEnv, filepath.Join(destDir, "env.properties")); err != nil {
			return ActionResult{}, fmt.Errorf("failed to copy env.properties: %w", err)
		}
		result.EnvCopied = true
	}

	if err := restoreLocalFilesFromPreviousVersion(destDir, sourceDir); err != nil {
		return ActionResult{}, fmt.Errorf("failed to process local files: %w", err)
	}
	result.LocalFilesCopied = true
	return result, nil
}

// DuplicateInstalledVersion copies an installed OWLCMS version to an exact new directory name.
func DuplicateInstalledVersion(fromVersion, newVersion string) (ActionResult, error) {
	created, err := shared.DuplicateVersionDirectory(installDir, fromVersion, newVersion)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Version: created, Path: filepath.Join(installDir, created)}, nil
}

// RemoveInstalledVersion removes an installed OWLCMS version directory.
func RemoveInstalledVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	dir := filepath.Join(installDir, version)
	if info, err := os.Stat(dir); err != nil {
		return fmt.Errorf("OWLCMS version %q is not installed: %w", version, err)
	} else if !info.IsDir() {
		return fmt.Errorf("OWLCMS version %q is not a directory", version)
	}
	return os.RemoveAll(dir)
}
