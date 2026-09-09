package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"controlpanel/owlcms"
	"controlpanel/shared"
	"controlpanel/tracker"
)

type replInvocation struct {
	batchFile   string
	interactive bool
}

type replSession struct{}

func parseREPLInvocation(args []string) (replInvocation, bool, error) {
	var invocation replInvocation
	var found bool

	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "repl":
			return invocation, true, fmt.Errorf("use --repl instead of repl")
		case "--repl":
			if found {
				return invocation, true, fmt.Errorf("specify either --repl or --batch, not both")
			}
			invocation.interactive = true
			found = true
		case "--batch":
			if found {
				return invocation, true, fmt.Errorf("specify either --repl or --batch, not both")
			}
			if index+1 >= len(args) {
				return invocation, true, fmt.Errorf("--batch requires a file path or -")
			}
			index++
			invocation.batchFile = strings.TrimSpace(args[index])
			if invocation.batchFile == "" {
				return invocation, true, fmt.Errorf("--batch requires a file path or -")
			}
			found = true
		}
	}

	return invocation, found, nil
}

func runREPL(invocation replInvocation, session replSession, in io.Reader, out, errOut io.Writer) error {
	input := in
	if !invocation.interactive && invocation.batchFile != "-" {
		file, err := os.Open(invocation.batchFile)
		if err != nil {
			return fmt.Errorf("open batch file %s: %w", invocation.batchFile, err)
		}
		defer file.Close()
		input = file
	}

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	lineNumber := 0
	for {
		if invocation.interactive {
			fmt.Fprint(out, session.prompt())
		}
		if !scanner.Scan() {
			break
		}
		lineNumber++

		exit, err := executeREPLLine(scanner.Text(), &session, out)
		if err != nil {
			if !invocation.interactive {
				return fmt.Errorf("batch line %d: %w", lineNumber, err)
			}
			fmt.Fprintf(errOut, "error: %v\n", err)
			continue
		}
		if exit {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read commands: %w", err)
	}

	return nil
}

func (session replSession) prompt() string {
	instance := strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTANCE"))
	if instance == "" || isMainInstance(instance) {
		return "controlpanel> "
	}
	return fmt.Sprintf("controlpanel[%s]> ", instance)
}

func executeREPLLine(line string, session *replSession, out io.Writer) (bool, error) {
	args, err := splitREPLCommand(line)
	if err != nil {
		return false, err
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "#") {
		return false, nil
	}

	switch strings.ToLower(args[0]) {
	case "exit", "quit":
		if len(args) != 1 {
			return false, fmt.Errorf("%s takes no arguments", args[0])
		}
		return true, nil
	case "help":
		if len(args) != 1 {
			return false, fmt.Errorf("help takes no arguments; use 'owlcms help' or 'tracker help'")
		}
		writeREPLHelp(out)
		return false, nil
	case "context":
		if len(args) != 1 {
			return false, fmt.Errorf("context takes no arguments")
		}
		writeREPLContext(out)
		return false, nil
	case "owlcms", "tracker":
		return false, executeREPLModule(args[0], args[1:], session, out)
	default:
		return false, fmt.Errorf("unknown command %q; use help", args[0])
	}
}

func splitREPLCommand(line string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	escaped := false

	for _, character := range strings.TrimSpace(line) {
		if escaped {
			token.WriteRune(character)
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				token.WriteRune(character)
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == ' ' || character == '\t' {
			if token.Len() > 0 {
				args = append(args, token.String())
				token.Reset()
			}
			continue
		}
		token.WriteRune(character)
	}
	if escaped {
		return nil, fmt.Errorf("command ends with an escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted argument")
	}
	if token.Len() > 0 {
		args = append(args, token.String())
	}
	return args, nil
}

func writeREPLContext(out io.Writer) {
	instance := strings.TrimSpace(os.Getenv("CONTROLPANEL_INSTANCE"))
	if instance == "" {
		instance = mainInstanceName
	}
	fmt.Fprintf(out, "instance:          %s\n", instance)
	fmt.Fprintf(out, "control panel dir: %s\n", shared.GetControlPanelInstallDir())
	fmt.Fprintf(out, "owlcms dir:        %s\n", shared.GetOwlcmsInstallDir())
	fmt.Fprintf(out, "tracker dir:       %s\n", shared.GetTrackerInstallDir())
	fmt.Fprintf(out, "runtime dir:       %s\n", shared.GetRuntimeDir())
}

func writeREPLHelp(out io.Writer) {
	fmt.Fprintln(out, "Commands: context, help, exit, quit")
	fmt.Fprintln(out, "Use 'owlcms help' for OWLCMS commands.")
	fmt.Fprintln(out, "Use 'tracker help' for Tracker commands.")
}

func writeREPLOwlcmsHelp(out io.Writer) {
	fmt.Fprintln(out, "OWLCMS commands:")
	fmt.Fprintln(out, "  owlcms versions")
	fmt.Fprintln(out, "  owlcms start [selector]")
	fmt.Fprintln(out, "  owlcms run [selector]")
	fmt.Fprintln(out, "  owlcms stop | status")
	fmt.Fprintln(out, "  owlcms install [release]")
	fmt.Fprintln(out, "  owlcms install-zip <zip-path> [installed-version]")
	fmt.Fprintln(out, "  owlcms export [selector] <zip-path|directory>")
	fmt.Fprintln(out, "  owlcms update [selector] [release]")
	fmt.Fprintln(out, "  owlcms rename|duplicate [selector] <name>")
	fmt.Fprintln(out, "  owlcms import <source-selector> <target-selector>")
	fmt.Fprintln(out, "  owlcms remove [selector]")
	fmt.Fprintln(out, "Persistent version settings:")
	fmt.Fprintln(out, "  owlcms <selector> port <port>")
	fmt.Fprintln(out, "  owlcms <selector> tracker on [port]")
	fmt.Fprintln(out, "  owlcms <selector> tracker on <ws://host/ws> <port>")
	fmt.Fprintln(out, "  owlcms <selector> tracker off")
	fmt.Fprintln(out, "  owlcms <selector> mqtt on [port]")
	fmt.Fprintln(out, "  owlcms <selector> mqtt off")
}

func writeREPLTrackerHelp(out io.Writer) {
	fmt.Fprintln(out, "Tracker commands:")
	fmt.Fprintln(out, "  tracker versions")
	fmt.Fprintln(out, "  tracker start [selector] [port]")
	fmt.Fprintln(out, "  tracker run [selector] [port]")
	fmt.Fprintln(out, "  tracker stop | status")
	fmt.Fprintln(out, "  tracker install [release]")
	fmt.Fprintln(out, "  tracker install-zip <zip-path> [installed-version]")
	fmt.Fprintln(out, "  tracker export [selector] <zip-path|directory>")
	fmt.Fprintln(out, "  tracker update [selector] [release]")
	fmt.Fprintln(out, "  tracker rename|duplicate [selector] <name>")
	fmt.Fprintln(out, "  tracker import <source-selector> <target-selector>")
	fmt.Fprintln(out, "  tracker remove [selector]")
}

func executeREPLModule(module string, args []string, session *replSession, out io.Writer) error {
	module = strings.ToLower(module)
	if len(args) == 0 {
		return fmt.Errorf("%s requires an action", module)
	}
	if strings.EqualFold(args[0], "help") {
		if len(args) != 1 {
			return fmt.Errorf("%s help takes no arguments", module)
		}
		if module == "owlcms" {
			writeREPLOwlcmsHelp(out)
		} else {
			writeREPLTrackerHelp(out)
		}
		return nil
	}
	if module == "owlcms" {
		handled, err := executeREPLOwlcmsSetting(args, out)
		if handled {
			return err
		}
	}

	action := strings.ToLower(args[0])
	arguments := args[1:]
	cmd, err := buildREPLModuleCommand(module, action, arguments)
	if err != nil {
		return err
	}
	if cmd.Action == "list" {
		writeREPLAvailableVersions(out, module, installedVersionDirectories(moduleInstallDir(module)))
		return nil
	}
	if err := resolveREPLModuleVersions(&cmd); err != nil {
		return err
	}
	if moduleCommandRequiresExclusiveControlPanel(cmd) {
		if running, ok := runningControlPanelMetadata(); ok {
			return fmt.Errorf("another OWLCMS Control Panel is already running (%s); use that UI or close it before running module commands", describeControlPanelRuntime(running))
		}
	}

	err = executeModuleCommand(cmd, out)
	if err == nil {
		if replCommandChangesVersions(cmd.Action) {
			writeREPLAvailableVersions(out, module, installedVersionDirectories(moduleInstallDir(module)))
		}
	}
	return err
}

func replCommandChangesVersions(action string) bool {
	switch action {
	case "install", "install-zip", "update", "rename", "duplicate", "remove":
		return true
	default:
		return false
	}
}

func writeREPLAvailableVersions(out io.Writer, module string, versions []string) {
	fmt.Fprintf(out, "%s available versions:\n", module)
	if len(versions) == 0 {
		fmt.Fprintln(out, "  (none installed)")
		return
	}
	for index, version := range versions {
		latest := ""
		if index == 0 {
			latest = "  latest"
		}
		fmt.Fprintf(out, "  %-3s %-16s%s\n", replVersionSelector(index), version, latest)
	}
}

func buildREPLModuleCommand(module, action string, args []string) (moduleCLICommand, error) {
	cmd := moduleCLICommand{Module: module}
	defaultSelector := "A"

	withOptionalSelector := func(values []string) (string, string, error) {
		switch len(values) {
		case 1:
			return defaultSelector, values[0], nil
		case 2:
			return values[0], values[1], nil
		default:
			return "", "", fmt.Errorf("%s accepts a selector and one value", action)
		}
	}

	switch action {
	case "versions":
		if len(args) != 0 {
			return cmd, fmt.Errorf("%s versions takes no arguments", module)
		}
		cmd.Action = "list"
	case "start", "run":
		if module == "owlcms" {
			if len(args) > 1 {
				return cmd, fmt.Errorf("owlcms %s accepts only an optional selector; use 'owlcms <selector> port <port>' to configure its persistent port", action)
			}
			cmd.Action = "launch"
			cmd.DaemonMode = action == "start"
			cmd.Version = defaultSelector
			if len(args) == 1 {
				if isREPLPort(args[0]) {
					return cmd, fmt.Errorf("use 'owlcms <selector> port %s' before starting OWLCMS", args[0])
				}
				cmd.Version = args[0]
			}
			break
		}
		if len(args) > 2 {
			return cmd, fmt.Errorf("tracker %s accepts an optional selector and port", action)
		}
		cmd.Action = "launch"
		cmd.DaemonMode = action == "start"
		cmd.Version = defaultSelector
		if len(args) > 0 {
			if isREPLPort(args[0]) {
				cmd.Port = args[0]
			} else {
				cmd.Version = args[0]
			}
		}
		if len(args) == 2 {
			if !isREPLPort(args[1]) {
				return cmd, fmt.Errorf("%s %s port must be numeric", module, action)
			}
			cmd.Port = args[1]
		}
	case "stop", "status":
		if len(args) != 0 {
			return cmd, fmt.Errorf("%s %s takes no arguments", module, action)
		}
		cmd.Action = action
	case "install":
		if len(args) > 1 {
			return cmd, fmt.Errorf("%s install accepts at most one release", module)
		}
		cmd.Action = "install"
		cmd.InstallVersion = "latest"
		if len(args) == 1 {
			cmd.InstallVersion = args[0]
		}
	case "install-zip":
		if len(args) < 1 || len(args) > 2 {
			return cmd, fmt.Errorf("%s install-zip requires a ZIP path and optional installed version", module)
		}
		cmd.Action = "install-zip"
		cmd.InstallZipPath = args[0]
		if len(args) == 2 {
			cmd.Version = args[1]
		}
	case "export":
		selector, path, err := withOptionalSelector(args)
		if err != nil {
			return cmd, err
		}
		cmd.Action = "create-zip"
		cmd.Version = selector
		cmd.CreateZipPath = path
	case "update":
		cmd.Action = "update"
		switch len(args) {
		case 0:
			cmd.Version = defaultSelector
			cmd.UpdateTo = "latest"
		case 1:
			if _, isSelector := replSelectorIndex(args[0]); isSelector {
				cmd.Version = args[0]
				cmd.UpdateTo = "latest"
			} else {
				cmd.Version = defaultSelector
				cmd.UpdateTo = args[0]
			}
		case 2:
			cmd.Version = args[0]
			cmd.UpdateTo = args[1]
		default:
			return cmd, fmt.Errorf("%s update accepts an optional selector and release", module)
		}
	case "rename":
		selector, name, err := withOptionalSelector(args)
		if err != nil {
			return cmd, err
		}
		cmd.Action = "rename"
		cmd.Version = selector
		cmd.DuplicateName = name
	case "duplicate":
		selector, name, err := withOptionalSelector(args)
		if err != nil {
			return cmd, err
		}
		cmd.Action = "duplicate"
		cmd.FromVersion = selector
		cmd.DuplicateName = name
	case "import":
		if len(args) != 2 {
			return cmd, fmt.Errorf("%s import requires source and target selectors", module)
		}
		cmd.Action = "import"
		cmd.FromVersion = args[0]
		cmd.ToVersion = args[1]
	case "remove":
		if len(args) > 1 {
			return cmd, fmt.Errorf("%s remove accepts at most one selector", module)
		}
		cmd.Action = "remove"
		cmd.RemoveVersion = defaultSelector
		if len(args) == 1 {
			cmd.RemoveVersion = args[0]
		}
	default:
		return cmd, fmt.Errorf("unsupported %s action %q", module, action)
	}
	return cmd, nil
}

func isREPLPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

func resolveREPLModuleVersions(cmd *moduleCLICommand) error {
	resolve := func(value string) (string, error) {
		return resolveREPLVersionSelector(cmd.Module, value)
	}

	var err error
	switch cmd.Action {
	case "launch", "create-zip", "update", "rename":
		cmd.Version, err = resolve(cmd.Version)
	case "duplicate":
		cmd.FromVersion, err = resolve(cmd.FromVersion)
	case "import":
		cmd.FromVersion, err = resolve(cmd.FromVersion)
		if err == nil {
			cmd.ToVersion, err = resolve(cmd.ToVersion)
		}
	case "remove":
		cmd.RemoveVersion, err = resolve(cmd.RemoveVersion)
	}
	return err
}

func resolveREPLVersionSelector(module, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	versions := installedVersionDirectories(moduleInstallDir(module))
	if index, isSelector := replSelectorIndex(requested); isSelector {
		if index >= len(versions) {
			return "", fmt.Errorf("%s selector %q is not installed", module, requested)
		}
		return versions[index], nil
	}
	return resolveLocalModuleVersion(module, requested)
}

func moduleInstallDir(module string) string {
	if module == "owlcms" {
		return owlcms.GetInstallDir()
	}
	return tracker.GetInstallDir()
}

func replSelectorIndex(value string) (int, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return 0, false
	}
	index := 0
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return 0, false
		}
		index = index*26 + int(character-'A'+1)
	}
	return index - 1, true
}

func replVersionSelector(index int) string {
	index++
	var selector []byte
	for index > 0 {
		index--
		selector = append([]byte{byte('A' + index%26)}, selector...)
		index /= 26
	}
	return string(selector)
}

func executeREPLOwlcmsSetting(args []string, out io.Writer) (bool, error) {
	if len(args) < 2 {
		return false, nil
	}
	setting := strings.ToLower(args[1])
	if setting != "port" && setting != "tracker" && setting != "mqtt" {
		return false, nil
	}

	version, err := resolveREPLVersionSelector("owlcms", args[0])
	if err != nil {
		return true, err
	}
	values := args[2:]

	if setting == "port" {
		if len(values) != 1 || !isREPLPort(values[0]) {
			return true, fmt.Errorf("owlcms <selector> port requires a port number between 1 and 65535")
		}
		if err := owlcms.SavePropertyForRelease(version, "OWLCMS_PORT", values[0]); err != nil {
			return true, err
		}
		fmt.Fprintf(out, "owlcms %s port set to %s (persistent)\n", version, values[0])
		return true, nil
	}
	if setting == "mqtt" {
		if len(values) == 1 && strings.EqualFold(values[0], "off") {
			if err := owlcms.SavePropertyForRelease(version, "OWLCMS_ENABLEEMBEDDEDMQTT", "false"); err != nil {
				return true, err
			}
			fmt.Fprintf(out, "owlcms %s embedded MQTT disabled (persistent)\n", version)
			return true, nil
		}
		if len(values) < 1 || len(values) > 2 || !strings.EqualFold(values[0], "on") {
			return true, fmt.Errorf("owlcms <selector> mqtt requires 'on [port]' or 'off'")
		}
		if len(values) == 2 && !isREPLPort(values[1]) {
			return true, fmt.Errorf("MQTT port must be a number between 1 and 65535")
		}
		if err := owlcms.SavePropertyForRelease(version, "OWLCMS_ENABLEEMBEDDEDMQTT", "true"); err != nil {
			return true, err
		}
		if len(values) == 2 {
			if err := owlcms.SavePropertyForRelease(version, "OWLCMS_MQTTPORT", values[1]); err != nil {
				return true, err
			}
			fmt.Fprintf(out, "owlcms %s embedded MQTT enabled on port %s (persistent)\n", version, values[1])
			return true, nil
		}
		fmt.Fprintf(out, "owlcms %s embedded MQTT enabled (persistent)\n", version)
		return true, nil
	}

	if len(values) == 1 && strings.EqualFold(values[0], "off") {
		if err := owlcms.DisableTrackerConnectionForRelease(version); err != nil {
			return true, err
		}
		fmt.Fprintf(out, "owlcms %s tracker connection disabled (persistent)\n", version)
		return true, nil
	}
	if len(values) < 1 || !strings.EqualFold(values[0], "on") || len(values) > 3 {
		return true, fmt.Errorf("owlcms <selector> tracker requires 'on [port]', 'on <url> <port>', or 'off'")
	}

	baseURL, trackerPort, _ := owlcms.GetTrackerConnectionSettings()
	if len(values) == 2 {
		trackerPort = values[1]
	}
	if len(values) == 3 {
		baseURL = values[1]
		trackerPort = values[2]
	}
	if !isREPLPort(trackerPort) {
		return true, fmt.Errorf("tracker port must be a number between 1 and 65535")
	}
	if err := owlcms.ConfigureTrackerConnectionForReleaseURL(version, baseURL, trackerPort); err != nil {
		return true, err
	}
	fmt.Fprintf(out, "owlcms %s tracker connection set to %s on port %s (persistent)\n", version, baseURL, trackerPort)
	return true, nil
}
