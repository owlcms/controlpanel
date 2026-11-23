package owlcms

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"controlpanel/shared"
)

// extractLocalOnInitialInstall controls whether official release installs unwrap
// the bundled local/ directory from the downloaded release ZIP.
const extractLocalOnInitialInstall = false

func isLocalZipEntry(entryName string) bool {
	clean := strings.TrimPrefix(filepath.ToSlash(entryName), "./")
	return clean == "local" || clean == "local/" || strings.HasPrefix(clean, "local/")
}

func extractOfficialReleaseZip(zipPath, destDir string) error {
	if extractLocalOnInitialInstall {
		return shared.ExtractZip(zipPath, destDir)
	}
	return shared.ExtractZipFiltered(zipPath, destDir, true, isLocalZipEntry)
}

// ActionResult describes the installed version affected by a non-UI action.
type ActionResult struct {
	Version             string
	Path                string
	DatabaseCopied      bool
	EnvCopied           bool
	LocalFilesCopied    bool
	MigrationReportPath string
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
	return installRelease(downloadVersion, installVersion, progress, nil)
}

func installRelease(downloadVersion, installVersion string, progress shared.ProgressCallback, cancel <-chan bool) (ActionResult, error) {
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

	if err := shared.DownloadArchive(zipURL, zipPath, progress, cancel); err != nil {
		_ = os.Remove(zipPath)
		return ActionResult{}, fmt.Errorf("download failed: %w", err)
	}
	if err := extractOfficialReleaseZip(zipPath, extractPath); err != nil {
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

// UpdateRelease installs targetVersion and imports data/config from existingVersion.
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
	installed, err := installRelease(targetVersion, targetInstallVersion, progress, cancel)
	if err != nil {
		return ActionResult{}, err
	}

	result, err := ImportDataAndConfig(existingVersion, installed.Version)
	if err != nil {
		_ = os.RemoveAll(installed.Path)
		return ActionResult{}, err
	}
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
	sourceDatabaseDir := filepath.Join(sourceDir, "database")
	if _, statErr := os.Stat(sourceDatabaseDir); !os.IsNotExist(statErr) {
		if err := copyFiles(sourceDatabaseDir, filepath.Join(destDir, "database"), true); err != nil {
			return ActionResult{}, fmt.Errorf("failed to copy database: %w", err)
		}
		result.DatabaseCopied = true
	} else {
		log.Printf("Database directory does not exist in %s", sourceDatabaseDir)
	}

	srcEnv := filepath.Join(sourceDir, "env.properties")
	if _, err := os.Stat(srcEnv); err == nil {
		if err := copyFile(srcEnv, filepath.Join(destDir, "env.properties")); err != nil {
			return ActionResult{}, fmt.Errorf("failed to copy env.properties: %w", err)
		}
		result.EnvCopied = true
	}

	reportPath, err := restoreLocalFilesFromPreviousVersionWithReport(destDir, sourceDir)
	if err != nil {
		return ActionResult{}, fmt.Errorf("failed to process local files: %w", err)
	}
	result.LocalFilesCopied = true
	result.MigrationReportPath = reportPath
	return result, nil
}

// DryRunImportLocalFiles analyzes migrating local/ from sourceVersion to destVersion
// without modifying any files, and saves a timestamped report to the user's Downloads folder.
func DryRunImportLocalFiles(sourceVersion, destVersion string) (string, error) {
	sourceVersion = strings.TrimSpace(sourceVersion)
	destVersion = strings.TrimSpace(destVersion)
	sourceDir := filepath.Join(installDir, sourceVersion)
	destDir := filepath.Join(installDir, destVersion)
	if info, err := os.Stat(sourceDir); err != nil {
		return "", fmt.Errorf("source version %q does not exist: %w", sourceVersion, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("source version %q is not a directory", sourceVersion)
	}
	if info, err := os.Stat(destDir); err != nil {
		return "", fmt.Errorf("destination version %q does not exist: %w", destVersion, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("destination version %q is not a directory", destVersion)
	}

	report, err := migrateLocalFiles(destDir, sourceDir, true)
	if err != nil {
		return "", fmt.Errorf("failed to analyze local files: %w", err)
	}
	return writeMigrationReport(defaultDownloadsDir(filepath.Join(destDir, "logs")), "migration-dry-run", sourceVersion, destVersion, report)
}

// ExtractLocalFromDistribution extracts customizable distribution files from version/owlcms.jar
// into version/local without overwriting existing files, and saves a report under version/logs/.
func ExtractLocalFromDistribution(version string) (string, error) {
	version = strings.TrimSpace(version)
	versionDir := filepath.Join(installDir, version)
	if info, err := os.Stat(versionDir); err != nil {
		return "", fmt.Errorf("version %q does not exist: %w", version, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("version %q is not a directory", version)
	}

	report, err := extractDistributionLocalFiles(versionDir, false)
	if err != nil {
		return "", fmt.Errorf("failed to extract distribution files: %w", err)
	}
	reportPath, writeErr := writeMigrationReport(
		filepath.Join(versionDir, "logs"),
		"extract",
		version,
		version,
		report,
	)
	if writeErr != nil {
		log.Printf("Warning: failed to write extraction report: %v", writeErr)
		return "", nil
	}
	return reportPath, nil
}

// DryRunExtractLocalFromDistribution analyzes which customizable files from version/owlcms.jar
// would be extracted into version/local without modifying any files, saving the report to Downloads.
func DryRunExtractLocalFromDistribution(version string) (string, error) {
	version = strings.TrimSpace(version)
	versionDir := filepath.Join(installDir, version)
	if info, err := os.Stat(versionDir); err != nil {
		return "", fmt.Errorf("version %q does not exist: %w", version, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("version %q is not a directory", version)
	}

	report, err := extractDistributionLocalFiles(versionDir, true)
	if err != nil {
		return "", fmt.Errorf("failed to analyze distribution files: %w", err)
	}
	return writeMigrationReport(
		defaultDownloadsDir(filepath.Join(versionDir, "logs")),
		"extract-dry-run",
		version,
		version,
		report,
	)
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
