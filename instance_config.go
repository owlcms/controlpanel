package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"controlpanel/owlcms"
	"controlpanel/shared"
	"controlpanel/tracker"

	"github.com/magiconair/properties"
)

const controlPanelEnvFileName = "env.properties"

type cliOptions struct {
	instanceArg string
	runtimeArg  string
	init        bool
	mqtt        bool
	help        bool
}

type instancePaths struct {
	InstanceName    string
	ControlPanelDir string
	OwlcmsDir       string
	TrackerDir      string
}

const mainInstanceName = "owlcms"

func parseCLIOptions(args []string) cliOptions {
	var opts cliOptions

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--instance-dir", "--instance_dir":
			if i+1 < len(args) {
				i++
				opts.instanceArg = strings.TrimSpace(args[i])
			}
		case "--runtime-dir", "--runtime_dir":
			if i+1 < len(args) {
				i++
				opts.runtimeArg = strings.TrimSpace(args[i])
			}
		case "-i", "--instance":
			if i+1 < len(args) {
				i++
				opts.instanceArg = strings.TrimSpace(args[i])
			}
		case "-m", "--module", "--version", "--update-to", "--duplicate", "--from-version", "--to-version", "--remove", "--port", "--install-zip", "--create-zip":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		case "--install", "--local-tracker":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		case "--launch", "--stop", "--list", "--import", "--background":
		case "--owlcms", "--tracker":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		case "--init":
			opts.init = true
		case "--mqtt":
			opts.mqtt = true
		case "--help", "-h":
			opts.help = true
		default:
			if opts.instanceArg == "" && !strings.HasPrefix(args[i], "-") {
				opts.instanceArg = strings.TrimSpace(args[i])
			}
		}
	}

	return opts
}

func printUsage() {
	fmt.Println("Usage: controlpanel [instance options] --module <owlcms|tracker> <action> [action options]")
	fmt.Println("")
	fmt.Println("Most common cases:")
	fmt.Println("  Start OWLCMS in the foreground:")
	fmt.Println("    controlpanel --module owlcms --launch")
	fmt.Println("  Start OWLCMS in the background:")
	fmt.Println("    controlpanel --module owlcms --launch --background")
	fmt.Println("  Start OWLCMS and connect it to a local tracker:")
	fmt.Println("    controlpanel --module owlcms --launch --local-tracker")
	fmt.Println("  Start Tracker in the foreground:")
	fmt.Println("    controlpanel --module tracker --launch")
	fmt.Println("  Start Tracker in the background:")
	fmt.Println("    controlpanel --module tracker --launch --background")
	fmt.Println("  Stop OWLCMS:")
	fmt.Println("    controlpanel --module owlcms --stop")
	fmt.Println("  Stop Tracker:")
	fmt.Println("    controlpanel --module tracker --stop")
	fmt.Println("")
	fmt.Println("Version, update, and import commands:")
	fmt.Println("  List installed versions:")
	fmt.Println("    controlpanel --module owlcms --list")
	fmt.Println("    controlpanel --module tracker --list")
	fmt.Println("  Install a new downloaded version:")
	fmt.Println("    controlpanel --module owlcms --install latest")
	fmt.Println("    controlpanel --module tracker --install latest")
	fmt.Println("  Install from or create a local ZIP:")
	fmt.Println("    controlpanel --module owlcms --install-zip C:/Downloads/owlcms_66.0.0.zip")
	fmt.Println("    controlpanel --module tracker --create-zip C:/Backups/tracker.zip --version 3.4.0")
	fmt.Println("  Update by copying data/config from an installed source version:")
	fmt.Println("    controlpanel --module owlcms --version latest --update-to latest")
	fmt.Println("    controlpanel --module tracker --version 3.3.0 --update-to 3.4.0")
	fmt.Println("  Import data/config between installed local versions:")
	fmt.Println("    controlpanel --module owlcms --import --from-version 65.0.0 --to-version 66.0.0")
	fmt.Println("    controlpanel --module tracker --import --from-version 3.3.0 --to-version 3.4.0")
	fmt.Println("  Duplicate or remove an installed version:")
	fmt.Println("    controlpanel --module owlcms --duplicate practice-copy --from-version 66.0.0")
	fmt.Println("    controlpanel --module tracker --remove 3.3.0")
	fmt.Println("")
	fmt.Println("Switch reference:")
	fmt.Println("  Module selection:")
	fmt.Println("    -m, --module <owlcms|tracker>        Selects the module to manage")
	fmt.Println("  Actions:")
	fmt.Println("    --launch                             Starts the selected module")
	fmt.Println("    --stop                               Stops the selected running module")
	fmt.Println("    --list                               Lists installed local versions")
	fmt.Println("    --install [latest|<github-version>]  Downloads a clean new version")
	fmt.Println("    --install-zip <zip-file>             Installs a local ZIP file, often from a federation")
	fmt.Println("    --create-zip <zip-file|directory>    Creates a ZIP from the version selected by --version")
	fmt.Println("                                        Uses a .zip path exactly, or creates a timestamped file in an existing directory")
	fmt.Println("    --update-to <latest|github-version>  Updates using --version as local source")
	fmt.Println("    --import                             Imports data/config between installed versions")
	fmt.Println("    --duplicate <new-name>               Copies --from-version to a new directory")
	fmt.Println("    --remove <local-version>             Removes an installed version directory")
	fmt.Println("  Launch options:")
	fmt.Println("    --version <latest|previous|version>  Local version selector; default: latest")
	fmt.Println("    --background                         Runs detached and returns the terminal")
	fmt.Println("    --port <port>                        Stores a version-specific launch port")
	fmt.Println("    --local-tracker [port]               OWLCMS only; default tracker port 8096")
	fmt.Println("    --mqtt                               OWLCMS only; enables embedded MQTT")
	fmt.Println("  Version-copy options:")
	fmt.Println("    --from-version <local-version>       Source version for import/duplicate")
	fmt.Println("    --to-version <local-version>         Destination version for import")
	fmt.Println("  General:")
	fmt.Println("    --help, -h                           Shows this help and exits")
	fmt.Println("")
	fmt.Println("Interactive control panel:")
	fmt.Println("    controlpanel")
	fmt.Println("    controlpanel --instance records")
	fmt.Println("")
	fmt.Println("Multiple instances:")
	fmt.Println("  Select an instance by name:")
	fmt.Println("    controlpanel --instance records --module owlcms --launch --background --port 8180 --local-tracker 8196")
	fmt.Println("    controlpanel --instance records --module tracker --launch --background --port 8196")
	fmt.Println("  Positional instance shorthand is also accepted:")
	fmt.Println("    controlpanel records --module owlcms --stop")
	fmt.Println("  Instance switches:")
	fmt.Println("    -i, --instance <name>                 Selects a named sibling instance")
	fmt.Println("    --instance-dir, --instance_dir <path> Uses an explicit control panel directory")
	fmt.Println("    --runtime-dir, --runtime_dir <path>   Uses an explicit Java/Node/FFmpeg runtime directory")
	fmt.Println("")
	fmt.Println("Initialize instance directories:")
	fmt.Println("    controlpanel --instance records --init")
	fmt.Println("    controlpanel --instance records --runtime-dir runtime-records --init")
	fmt.Println("    controlpanel --instance-dir C:/owlcms/controlpanel-records --runtime-dir C:/owlcms/runtime --init")
	fmt.Println("")
}

func maybeApplyImplicitInstanceForHeadless(opts cliOptions, owlcmsVersion, trackerVersion string) (string, string, error) {
	if strings.TrimSpace(opts.instanceArg) != "" {
		return owlcmsVersion, trackerVersion, nil
	}
	if strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTANCE")) != "" {
		return owlcmsVersion, trackerVersion, nil
	}

	candidate, err := inferImplicitInstanceName(owlcmsVersion, trackerVersion)
	if err != nil || candidate == "" {
		return owlcmsVersion, trackerVersion, err
	}

	if err := applyCLIInstanceOptions(cliOptions{instanceArg: candidate}); err != nil {
		return owlcmsVersion, trackerVersion, err
	}

	if strings.EqualFold(strings.TrimSpace(owlcmsVersion), candidate) {
		owlcmsVersion = "latest"
	}
	if strings.EqualFold(strings.TrimSpace(trackerVersion), candidate) {
		trackerVersion = "latest"
	}

	return owlcmsVersion, trackerVersion, nil
}

func inferImplicitInstanceName(owlcmsVersion, trackerVersion string) (string, error) {
	owlcmsCandidate := implicitInstanceCandidate(owlcmsVersion, shared.GetOwlcmsInstallDir())
	trackerCandidate := implicitInstanceCandidate(trackerVersion, shared.GetTrackerInstallDir())

	switch {
	case owlcmsCandidate == "" && trackerCandidate == "":
		return "", nil
	case owlcmsCandidate == "":
		return trackerCandidate, nil
	case trackerCandidate == "":
		return owlcmsCandidate, nil
	case strings.EqualFold(owlcmsCandidate, trackerCandidate):
		return owlcmsCandidate, nil
	default:
		return "", fmt.Errorf("headless launch is ambiguous: %q looks like instance %q and %q; specify --instance-dir explicitly", strings.TrimSpace(owlcmsVersion)+"/"+strings.TrimSpace(trackerVersion), owlcmsCandidate, trackerCandidate)
	}
}

func implicitInstanceCandidate(requested, installDir string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" || strings.EqualFold(requested, "latest") || strings.EqualFold(requested, "stop") || strings.EqualFold(requested, "list") {
		return ""
	}

	if _, err := os.Stat(filepath.Join(installDir, requested)); err == nil {
		return ""
	}

	paths, err := resolveInstancePaths(requested)
	if err != nil {
		return ""
	}

	if _, err := os.Stat(controlPanelEnvPath(paths.ControlPanelDir)); err == nil {
		return requested
	}

	return ""
}

func applyCLIInstanceOptions(opts cliOptions) error {
	instanceArg := strings.TrimSpace(opts.instanceArg)
	if instanceArg == "" && opts.init {
		instanceArg = mainInstanceName
	}

	if instanceArg == "" {
		return nil
	}

	paths, err := resolveInstancePaths(instanceArg)
	if err != nil {
		return err
	}

	runtimeDir, err := resolveRequestedRuntimeDir(paths.ControlPanelDir, paths.InstanceName, opts.runtimeArg, opts.init)
	if err != nil {
		return err
	}

	if err := os.Setenv("CONTROLPANEL_INSTALLDIR", paths.ControlPanelDir); err != nil {
		return err
	}
	if err := os.Setenv("OWLCMS_INSTALLDIR", paths.OwlcmsDir); err != nil {
		return err
	}
	if err := os.Setenv("TRACKER_INSTALLDIR", paths.TrackerDir); err != nil {
		return err
	}
	if err := os.Setenv("RUNTIME_DIR", runtimeDir); err != nil {
		return err
	}
	if err := os.Setenv("CONTROLPANEL_INSTANCE", paths.InstanceName); err != nil {
		return err
	}

	owlcms.SetInstallDir(paths.OwlcmsDir)
	tracker.SetInstallDir(paths.TrackerDir)

	if !opts.init {
		if _, err := os.Stat(controlPanelEnvPath(paths.ControlPanelDir)); err != nil {
			if os.IsNotExist(err) {
				if isMainInstance(paths.InstanceName) {
					return nil
				}
				return fmt.Errorf("instance %q is not initialized; run with --instance-dir %s --init first", paths.InstanceName, paths.InstanceName)
			}
			return err
		}
		return nil
	}

	return initializeInstanceLayout(paths, runtimeDir)
}

func initializeInstanceLayout(paths *instancePaths, runtimeDir string) error {
	if err := shared.EnsureDir0755(runtimeDir); err != nil {
		return fmt.Errorf("create runtime dir %s: %w", runtimeDir, err)
	}

	for _, dir := range []struct {
		label string
		path  string
	}{
		{label: "control panel", path: paths.ControlPanelDir},
		{label: "owlcms", path: paths.OwlcmsDir},
		{label: "tracker", path: paths.TrackerDir},
	} {
		if err := shared.EnsureDir0755(dir.path); err != nil {
			return fmt.Errorf("create %s dir %s: %w", dir.label, dir.path, err)
		}
	}

	if err := writeControlPanelEnv(paths.ControlPanelDir, runtimeDir, paths.InstanceName); err != nil {
		return err
	}
	if err := owlcms.InitEnv(); err != nil {
		return err
	}
	if err := tracker.InitEnv(); err != nil {
		return err
	}

	return nil
}

func resolveInstancePaths(spec string) (*instancePaths, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty instance dir")
	}

	defaultControlPanelDir := shared.DefaultControlPanelInstallDir()
	baseDir := filepath.Dir(defaultControlPanelDir)

	if filepath.IsAbs(spec) {
		controlPanelDir := filepath.Clean(spec)
		instanceName := deriveInstanceName(filepath.Base(controlPanelDir))
		if instanceName == "" {
			return nil, fmt.Errorf("could not derive instance name from %q", spec)
		}
		parent := filepath.Dir(controlPanelDir)
		return &instancePaths{
			InstanceName:    instanceName,
			ControlPanelDir: controlPanelDir,
			OwlcmsDir:       resolveOwlcmsDir(parent, instanceName),
			TrackerDir:      filepath.Join(parent, trackerDirName(instanceName)),
		}, nil
	}

	if strings.Contains(spec, string(os.PathSeparator)) {
		return nil, fmt.Errorf("relative instance dir %q must be a simple name", spec)
	}

	instanceName := spec
	return &instancePaths{
		InstanceName:    instanceName,
		ControlPanelDir: filepath.Join(baseDir, controlPanelDirName(instanceName)),
		OwlcmsDir:       resolveOwlcmsDir(baseDir, instanceName),
		TrackerDir:      filepath.Join(baseDir, trackerDirName(instanceName)),
	}, nil
}

func deriveInstanceName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}

	for _, suffix := range []string{"-controlpanel", "-owlcms", "-tracker"} {
		if before, ok := strings.CutSuffix(base, suffix); ok {
			trimmed := strings.TrimSpace(before)
			if trimmed != "" {
				return trimmed
			}
		}
	}

	for _, baseName := range []string{"owlcms-controlpanel", "owlcms", "owlcms-owlcms", "owlcms-tracker"} {
		if strings.EqualFold(base, baseName) {
			return mainInstanceName
		}
	}
	return base
}

func controlPanelDirName(instanceName string) string {
	if isMainInstance(instanceName) {
		return "owlcms-controlpanel"
	}
	return instanceName + "-controlpanel"
}

func trackerDirName(instanceName string) string {
	if isMainInstance(instanceName) {
		return "owlcms-tracker"
	}
	return instanceName + "-tracker"
}

func resolveOwlcmsDir(parentDir, instanceName string) string {
	defaultName := owlcmsDirName(instanceName)
	if !isMainInstance(instanceName) {
		return filepath.Join(parentDir, defaultName)
	}

	for _, name := range []string{"owlcms", "owlcms-owlcms"} {
		candidate := filepath.Join(parentDir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return filepath.Join(parentDir, defaultName)
}

func owlcmsDirName(instanceName string) string {
	if isMainInstance(instanceName) {
		return "owlcms"
	}
	return instanceName + "-owlcms"
}

func isMainInstance(instanceName string) bool {
	return strings.EqualFold(strings.TrimSpace(instanceName), mainInstanceName)
}

func resolveRequestedRuntimeDir(controlPanelDir, instanceName, runtimeArg string, init bool) (string, error) {
	runtimeArg = strings.TrimSpace(runtimeArg)
	if runtimeArg != "" {
		return resolveRuntimeDir(runtimeArg), nil
	}

	stored, err := loadStoredRuntimeDir(controlPanelDir)
	if err != nil {
		return "", err
	}
	if stored != "" {
		return stored, nil
	}

	if isMainInstance(instanceName) {
		return shared.DefaultControlPanelInstallDir(), nil
	}

	if init {
		return shared.DefaultControlPanelInstallDir(), nil
	}

	return "", fmt.Errorf("instance %q has no stored runtime dir; run with --init first", instanceName)
}

func resolveRuntimeDir(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return shared.DefaultControlPanelInstallDir()
	}
	if filepath.IsAbs(spec) {
		return filepath.Clean(spec)
	}
	return filepath.Join(filepath.Dir(shared.DefaultControlPanelInstallDir()), spec)
}

func controlPanelEnvPath(controlPanelDir string) string {
	return filepath.Join(controlPanelDir, controlPanelEnvFileName)
}

func loadStoredRuntimeDir(controlPanelDir string) (string, error) {
	props, err := loadControlPanelEnv(controlPanelDir)
	if err != nil || props == nil {
		return "", err
	}
	value, ok := props.Get("RUNTIME_DIR")
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}

func loadControlPanelEnv(controlPanelDir string) (*properties.Properties, error) {
	envPath := controlPanelEnvPath(controlPanelDir)
	content, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", envPath, err)
	}

	props := properties.NewProperties()
	if err := props.Load(content, properties.UTF8); err != nil {
		return nil, fmt.Errorf("load %s: %w", envPath, err)
	}
	return props, nil
}

func writeControlPanelEnv(controlPanelDir, runtimeDir, instanceName string) error {
	props, err := loadControlPanelEnv(controlPanelDir)
	if err != nil {
		return err
	}
	if props == nil {
		props = properties.NewProperties()
	}
	props.Set("RUNTIME_DIR", runtimeDir)
	props.Set("CONTROLPANEL_INSTANCE", instanceName)

	envPath := controlPanelEnvPath(controlPanelDir)
	file, err := os.Create(envPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", envPath, err)
	}
	defer file.Close()

	if _, err := props.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("write %s: %w", envPath, err)
	}
	return nil
}
