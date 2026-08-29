package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"controlpanel/owlcms"
	owlcmsinstallutils "controlpanel/owlcms/installutils"
	"controlpanel/shared"
	"controlpanel/tracker"
	trackerdownloadutils "controlpanel/tracker/downloadutils"
)

type moduleCLICommand struct {
	Module           string
	Action           string
	Version          string
	InstallVersion   string
	InstallZipPath   string
	CreateZipPath    string
	UpdateTo         string
	DuplicateName    string
	FromVersion      string
	ToVersion        string
	RemoveVersion    string
	Port             string
	LocalTrackerPort string
	DaemonMode       bool
	MQTT             bool
}

func moduleCommandRequiresExclusiveControlPanel(cmd moduleCLICommand) bool {
	return cmd.Action != "list" && cmd.Action != "stop" && cmd.Action != "launch"
}

func parseModuleCommand(args []string) (moduleCLICommand, bool, error) {
	var cmd moduleCLICommand
	var sawModule bool
	var sawModuleAction bool

	setAction := func(action string) error {
		if cmd.Action != "" && cmd.Action != action {
			return fmt.Errorf("only one module action can be specified (got %s and %s)", cmd.Action, action)
		}
		cmd.Action = action
		sawModuleAction = true
		return nil
	}

	valueAfter := func(index int, flag string) (string, int, error) {
		if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
			return "", index, fmt.Errorf("%s requires a value", flag)
		}
		return strings.TrimSpace(args[index+1]), index + 1, nil
	}

	optionalValueAfter := func(index int, fallback string) (string, int) {
		if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			return strings.TrimSpace(args[index+1]), index + 1
		}
		return fallback, index
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--owlcms", "--tracker":
			return cmd, true, fmt.Errorf("%s is not supported by the command-line guide; use --module %s with an explicit action", args[i], strings.TrimPrefix(args[i], "--"))
		case "-m", "--module":
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.Module = strings.ToLower(value)
			sawModule = true
			i = next
		case "--launch":
			if err := setAction("launch"); err != nil {
				return cmd, true, err
			}
		case "--stop":
			if err := setAction("stop"); err != nil {
				return cmd, true, err
			}
		case "--list":
			if err := setAction("list"); err != nil {
				return cmd, true, err
			}
		case "--install":
			if err := setAction("install"); err != nil {
				return cmd, true, err
			}
			cmd.InstallVersion, i = optionalValueAfter(i, "latest")
		case "--install-zip":
			if err := setAction("install-zip"); err != nil {
				return cmd, true, err
			}
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.InstallZipPath = value
			i = next
		case "--create-zip":
			if err := setAction("create-zip"); err != nil {
				return cmd, true, err
			}
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.CreateZipPath = value
			i = next
		case "--update-to":
			if err := setAction("update"); err != nil {
				return cmd, true, err
			}
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.UpdateTo = value
			i = next
		case "--duplicate":
			if err := setAction("duplicate"); err != nil {
				return cmd, true, err
			}
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.DuplicateName = value
			i = next
		case "--import":
			if err := setAction("import"); err != nil {
				return cmd, true, err
			}
		case "--remove":
			if err := setAction("remove"); err != nil {
				return cmd, true, err
			}
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.RemoveVersion = value
			i = next
		case "--version":
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.Version = value
			i = next
		case "--from-version":
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.FromVersion = value
			i = next
		case "--to-version":
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.ToVersion = value
			i = next
		case "--port":
			value, next, err := valueAfter(i, args[i])
			if err != nil {
				return cmd, true, err
			}
			cmd.Port = value
			i = next
		case "--local-tracker":
			cmd.LocalTrackerPort, i = optionalValueAfter(i, "8096")
		case "--background":
			cmd.DaemonMode = true
		case "--mqtt":
			cmd.MQTT = true
		}
	}

	if !sawModule && !sawModuleAction {
		return cmd, false, nil
	}
	if !sawModule {
		return cmd, true, fmt.Errorf("module actions require --module owlcms or --module tracker")
	}
	if cmd.Module != "owlcms" && cmd.Module != "tracker" {
		return cmd, true, fmt.Errorf("unsupported module %q", cmd.Module)
	}
	if cmd.Action == "" {
		return cmd, true, fmt.Errorf("--module %s requires an action", cmd.Module)
	}
	if cmd.LocalTrackerPort != "" && cmd.Module != "owlcms" {
		return cmd, true, fmt.Errorf("--local-tracker can only be used with --module owlcms")
	}

	return cmd, true, nil
}

func defaultVersion(requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "latest"
	}
	return requested
}

func resolveLocalModuleVersion(module, requested string) (string, error) {
	requested = defaultVersion(requested)
	switch module {
	case "owlcms":
		return resolveLocalVersionSelector("owlcms", requested, owlcms.GetAllInstalledVersions(), owlcms.GetInstallDir())
	case "tracker":
		return resolveLocalVersionSelector("tracker", requested, tracker.GetAllInstalledVersions(), tracker.GetInstallDir())
	default:
		return "", fmt.Errorf("unsupported module %q", module)
	}
}

func resolveLocalVersionSelector(label, requested string, allVersions []string, installDir string) (string, error) {
	requested = defaultVersion(requested)
	if strings.EqualFold(requested, "latest") {
		if len(allVersions) == 0 {
			return "", fmt.Errorf("no installed %s versions found", label)
		}
		return allVersions[0], nil
	}
	if strings.EqualFold(requested, "previous") {
		if len(allVersions) < 2 {
			return "", fmt.Errorf("no previous installed %s version found", label)
		}
		return allVersions[1], nil
	}

	dir := filepath.Join(installDir, requested)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s version %q is not installed (directory %s not found)", label, requested, dir)
		}
		return "", fmt.Errorf("checking %s version %q: %w", label, requested, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s version %q is not a directory", label, requested)
	}
	return requested, nil
}

func installedVersionDirectories(installDir string) []string {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return nil
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return shared.CompareVersions(versions[i], versions[j])
	})
	return versions
}

func executeModuleCommand(cmd moduleCLICommand, out io.Writer) error {
	switch cmd.Action {
	case "list":
		if cmd.Module == "owlcms" {
			writeAvailableVersions(out, "owlcms", installedVersionDirectories(owlcms.GetInstallDir()))
		} else {
			writeAvailableVersions(out, "tracker", installedVersionDirectories(tracker.GetInstallDir()))
		}
		return nil
	case "stop":
		stopHeadlessDaemons(cmd.Module == "owlcms", cmd.Module == "tracker")
		return nil
	case "launch":
		return executeModuleLaunch(cmd, out)
	case "install":
		return executeModuleInstall(cmd, out)
	case "install-zip":
		return executeModuleInstallZip(cmd, out)
	case "create-zip":
		return executeModuleCreateZip(cmd, out)
	case "update":
		return executeModuleUpdate(cmd, out)
	case "duplicate":
		return executeModuleDuplicate(cmd, out)
	case "import":
		return executeModuleImport(cmd, out)
	case "remove":
		return executeModuleRemove(cmd, out)
	default:
		return fmt.Errorf("unsupported action %q", cmd.Action)
	}
}

func executeModuleLaunch(cmd moduleCLICommand, out io.Writer) error {
	version, err := resolveLocalModuleVersion(cmd.Module, cmd.Version)
	if err != nil {
		return err
	}
	if cmd.Port != "" {
		if cmd.Module == "owlcms" {
			if err := owlcms.SavePropertyForRelease(version, "OWLCMS_PORT", cmd.Port); err != nil {
				return err
			}
		} else if err := tracker.SavePropertyForRelease(version, "TRACKER_PORT", cmd.Port); err != nil {
			return err
		}
	}
	if cmd.LocalTrackerPort != "" {
		if err := owlcms.ConfigureTrackerConnectionForRelease(version, cmd.LocalTrackerPort); err != nil {
			return err
		}
	}

	if cmd.DaemonMode {
		if err := shared.SetRunAsDaemonEnabled(true); err != nil {
			return err
		}
		if cmd.Module == "owlcms" {
			if err := owlcms.LaunchDaemon(version, cmd.MQTT); err != nil {
				return err
			}
		} else if err := tracker.LaunchDaemon(version); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %s started successfully\n", cmd.Module, version)
		return nil
	}

	if cmd.Module == "owlcms" {
		return owlcms.LaunchForeground(version, cmd.MQTT)
	}
	return tracker.LaunchForeground(version)
}

func executeModuleInstall(cmd moduleCLICommand, out io.Writer) error {
	if cmd.Module == "owlcms" {
		target, err := owlcms.ResolveInstallRelease(cmd.InstallVersion)
		if err != nil {
			return err
		}
		result, err := owlcms.InstallRelease(target, target, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "owlcms %s installed at %s\n", result.Version, result.Path)
		return nil
	}

	target, err := tracker.ResolveInstallRelease(cmd.InstallVersion)
	if err != nil {
		return err
	}
	result, err := tracker.InstallRelease(target, target, nil, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "tracker %s installed at %s\n", result.Version, result.Path)
	return nil
}

func executeModuleInstallZip(cmd moduleCLICommand, out io.Writer) error {
	zipPath := strings.TrimSpace(cmd.InstallZipPath)
	if zipPath == "" {
		return fmt.Errorf("--install-zip requires a ZIP file path")
	}

	version, err := installZipVersion(zipPath, cmd.Version)
	if err != nil {
		return err
	}

	if cmd.Module == "owlcms" {
		finalVersion := owlcmsinstallutils.GetInstallationDirectoryName(version, owlcms.GetInstallDir())
		extractPath := filepath.Join(owlcms.GetInstallDir(), finalVersion)
		if err := extractLocalZipArchive(zipPath, extractPath, owlcms.GetInstallDir(), func(src, dest string) error {
			return shared.ExtractZip(src, dest)
		}); err != nil {
			return err
		}
		if err := owlcms.EnsureReleaseEnvFromParent(finalVersion); err != nil {
			_ = os.RemoveAll(extractPath)
			return fmt.Errorf("failed to create release env.properties: %w", err)
		}
		fmt.Fprintf(out, "owlcms %s installed from %s at %s\n", finalVersion, zipPath, extractPath)
		return nil
	}

	finalVersion := tracker.GetInstallationDirectoryName(version, tracker.GetInstallDir())
	extractPath := filepath.Join(tracker.GetInstallDir(), finalVersion)
	if err := extractLocalZipArchive(zipPath, extractPath, tracker.GetInstallDir(), func(src, dest string) error {
		return trackerdownloadutils.ExtractZip(src, dest, nil)
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "tracker %s installed from %s at %s\n", finalVersion, zipPath, extractPath)
	return nil
}

func installZipVersion(zipPath, requestedVersion string) (string, error) {
	version := strings.TrimSpace(requestedVersion)
	if version == "" {
		inferred, err := shared.ExtractVersionFromFilename(filepath.Base(zipPath))
		if err != nil {
			return "", fmt.Errorf("--install-zip requires --version when the ZIP filename does not contain a semantic version: %w", err)
		}
		version = inferred
	}
	if strings.EqualFold(version, "latest") || strings.EqualFold(version, "previous") {
		return "", fmt.Errorf("--install-zip --version must be the installed version name, not %q", version)
	}
	if err := shared.ValidateVersionName(version); err != nil {
		return "", fmt.Errorf("invalid install ZIP version %q: %w", version, err)
	}
	return version, nil
}

func extractLocalZipArchive(zipPath, extractPath, scratchDir string, extract func(string, string) error) error {
	if info, err := os.Stat(zipPath); err != nil {
		return fmt.Errorf("checking ZIP file %s: %w", zipPath, err)
	} else if info.IsDir() {
		return fmt.Errorf("ZIP file path is a directory: %s", zipPath)
	}
	if !strings.EqualFold(filepath.Ext(zipPath), ".zip") {
		return fmt.Errorf("ZIP file must have .zip extension: %s", zipPath)
	}
	if err := shared.EnsureDir0755(scratchDir); err != nil {
		return fmt.Errorf("creating install directory: %w", err)
	}
	if err := shared.EnsureDir0755(filepath.Dir(extractPath)); err != nil {
		return fmt.Errorf("creating install parent directory: %w", err)
	}

	tempZipPath := filepath.Join(scratchDir, fmt.Sprintf(".controlpanel-install-%d-%s", time.Now().UnixNano(), filepath.Base(zipPath)))
	if err := copyFilePath(zipPath, tempZipPath); err != nil {
		return fmt.Errorf("copying ZIP file: %w", err)
	}
	if err := extract(tempZipPath, extractPath); err != nil {
		_ = os.Remove(tempZipPath)
		_ = os.RemoveAll(extractPath)
		return fmt.Errorf("extraction failed: %w", err)
	}
	return nil
}

func executeModuleCreateZip(cmd moduleCLICommand, out io.Writer) error {
	zipPath := strings.TrimSpace(cmd.CreateZipPath)
	if zipPath == "" {
		return fmt.Errorf("--create-zip requires an output ZIP path")
	}

	version, err := resolveLocalModuleVersion(cmd.Module, cmd.Version)
	if err != nil {
		return err
	}
	if err := shared.ValidateVersionName(version); err != nil {
		return fmt.Errorf("invalid version name %q: %w", version, err)
	}

	var sourceDir string
	if cmd.Module == "owlcms" {
		sourceDir = filepath.Join(owlcms.GetInstallDir(), version)
	} else {
		sourceDir = filepath.Join(tracker.GetInstallDir(), version)
	}
	if info, err := os.Stat(sourceDir); err != nil {
		return fmt.Errorf("checking version directory %s: %w", sourceDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("version path is not a directory: %s", sourceDir)
	}

	zipPath, err = resolveCreateZipPath(cmd.Module, version, zipPath)
	if err != nil {
		return err
	}
	if err := shared.EnsureDir0755(filepath.Dir(zipPath)); err != nil {
		return fmt.Errorf("creating ZIP output directory: %w", err)
	}

	if cmd.Module == "owlcms" {
		if err := owlcmsinstallutils.CreateZipArchive(sourceDir, zipPath, nil); err != nil {
			return err
		}
	} else if err := tracker.CreateZipArchive(sourceDir, zipPath, nil); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s %s ZIP created at %s\n", cmd.Module, version, zipPath)
	return nil
}

func resolveCreateZipPath(module, version, requestedPath string) (string, error) {
	if info, err := os.Stat(requestedPath); err == nil && info.IsDir() {
		return filepath.Join(requestedPath, defaultZipFileName(module, version)), nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("checking ZIP output path %s: %w", requestedPath, err)
	}
	if !strings.EqualFold(filepath.Ext(requestedPath), ".zip") {
		return "", fmt.Errorf("--create-zip output must be a .zip file or an existing directory: %s", requestedPath)
	}
	return requestedPath, nil
}

func defaultZipFileName(module, version string) string {
	prefix := "owlcms"
	if module == "tracker" {
		prefix = "owlcms-tracker"
	}
	timestamp := time.Now().Format("2006-01-02T150405")
	return fmt.Sprintf("%s_%s+%s.zip", prefix, shared.StripMetadata(version), timestamp)
}

func copyFilePath(src, dst string) error {
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

func executeModuleUpdate(cmd moduleCLICommand, out io.Writer) error {
	fromVersion, err := resolveLocalModuleVersion(cmd.Module, cmd.Version)
	if err != nil {
		return err
	}
	if cmd.Module == "owlcms" {
		target, err := owlcms.ResolveUpdateRelease(cmd.UpdateTo, fromVersion)
		if err != nil {
			return err
		}
		result, err := owlcms.UpdateRelease(fromVersion, target, nil, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "owlcms %s updated from %s at %s\n", result.Version, fromVersion, result.Path)
		return nil
	}

	target, err := tracker.ResolveUpdateRelease(cmd.UpdateTo, fromVersion)
	if err != nil {
		return err
	}
	result, err := tracker.UpdateRelease(fromVersion, target, nil, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "tracker %s updated from %s at %s\n", result.Version, fromVersion, result.Path)
	return nil
}

func executeModuleDuplicate(cmd moduleCLICommand, out io.Writer) error {
	if strings.TrimSpace(cmd.FromVersion) == "" {
		return fmt.Errorf("--duplicate requires --from-version")
	}
	fromVersion, err := resolveLocalModuleVersion(cmd.Module, cmd.FromVersion)
	if err != nil {
		return err
	}
	if cmd.Module == "owlcms" {
		result, err := owlcms.DuplicateInstalledVersion(fromVersion, cmd.DuplicateName)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "owlcms %s duplicated to %s\n", fromVersion, result.Version)
		return nil
	}
	result, err := tracker.DuplicateInstalledVersion(fromVersion, cmd.DuplicateName)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "tracker %s duplicated to %s\n", fromVersion, result.Version)
	return nil
}

func executeModuleImport(cmd moduleCLICommand, out io.Writer) error {
	if strings.TrimSpace(cmd.FromVersion) == "" || strings.TrimSpace(cmd.ToVersion) == "" {
		return fmt.Errorf("--import requires --from-version and --to-version")
	}
	fromVersion, err := resolveLocalModuleVersion(cmd.Module, cmd.FromVersion)
	if err != nil {
		return err
	}
	toVersion, err := resolveLocalModuleVersion(cmd.Module, cmd.ToVersion)
	if err != nil {
		return err
	}
	if cmd.Module == "owlcms" {
		if _, err := owlcms.ImportDataAndConfig(fromVersion, toVersion); err != nil {
			return err
		}
	} else if _, err := tracker.ImportDataAndConfig(fromVersion, toVersion); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s data/config imported from %s to %s\n", cmd.Module, fromVersion, toVersion)
	return nil
}

func executeModuleRemove(cmd moduleCLICommand, out io.Writer) error {
	version, err := resolveLocalModuleVersion(cmd.Module, cmd.RemoveVersion)
	if err != nil {
		return err
	}
	if cmd.Module == "owlcms" {
		if err := owlcms.RemoveInstalledVersion(version); err != nil {
			return err
		}
	} else if err := tracker.RemoveInstalledVersion(version); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s removed\n", cmd.Module, version)
	return nil
}
