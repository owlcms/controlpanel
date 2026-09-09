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
		case "--repl":
		case "--batch":
			if i+1 < len(args) {
				i++
			}
		case "--init":
			opts.init = true
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

func validateCLIOptions(args []string) error {
	valueOptions := map[string]bool{
		"--instance-dir": true,
		"--instance_dir": true,
		"--runtime-dir":  true,
		"--runtime_dir":  true,
		"-i":             true,
		"--instance":     true,
		"--batch":        true,
	}
	flagOptions := map[string]bool{
		"--init": true,
		"--repl": true,
		"--help": true,
		"-h":     true,
	}

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if valueOptions[arg] {
			if index+1 >= len(args) {
				return fmt.Errorf("%s requires a value", arg)
			}
			index++
			continue
		}
		if flagOptions[arg] || !strings.HasPrefix(arg, "-") {
			continue
		}
		return fmt.Errorf("unsupported option %q", arg)
	}
	return nil
}

func printUsage() {
	fmt.Println("Usage: controlpanel [instance options]")
	fmt.Println("       controlpanel [instance options] --repl")
	fmt.Println("       controlpanel [instance options] --batch <file|->")
	fmt.Println("")
	fmt.Println("Without --repl or --batch, opens the graphical Control Panel.")
	fmt.Println("--repl opens the textual control loop; --batch runs its commands from a file or standard input.")
	fmt.Println("")
	fmt.Println("Instance options:")
	fmt.Println("  -i, --instance <name>                 Select a named sibling instance")
	fmt.Println("  --init                                 Initialize the selected instance and exit")
	fmt.Println("  -h, --help                             Show this help and exit")
	fmt.Println("")
	fmt.Println("Advanced directory overrides:")
	fmt.Println("  --instance-dir, --instance_dir <path> Use a nonstandard control panel directory")
	fmt.Println("  --runtime-dir, --runtime_dir <path>   Use a nonstandard shared runtime directory")
	fmt.Println("  Named instances normally choose both directories automatically.")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  controlpanel")
	fmt.Println("  controlpanel --instance records")
	fmt.Println("  controlpanel --instance records --init")
	fmt.Println("  controlpanel --repl")
	fmt.Println("  controlpanel --instance records --batch competition.commands")
	fmt.Println("")
	fmt.Println("Use 'help' inside the REPL for its command reference.")
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
