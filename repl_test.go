package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"controlpanel/owlcms"
)

func TestParseREPLInvocation(t *testing.T) {
	invocation, found, err := parseREPLInvocation([]string{"--instance", "records", "--repl"})
	if err != nil {
		t.Fatalf("parseREPLInvocation returned error: %v", err)
	}
	if !found || invocation.batchFile != "" || !invocation.interactive {
		t.Fatalf("unexpected invocation: %#v, found=%v", invocation, found)
	}
}

func TestParseREPLInvocationRejectsOldSubcommand(t *testing.T) {
	_, found, err := parseREPLInvocation([]string{"repl"})
	if !found || err == nil || !strings.Contains(err.Error(), "--repl") {
		t.Fatalf("expected --repl guidance, found=%v err=%v", found, err)
	}
}

func TestSplitREPLCommandSupportsQuotedPaths(t *testing.T) {
	args, err := splitREPLCommand(`owlcms install-zip "~/Downloads/competition build.zip" 66.0.0`)
	if err != nil {
		t.Fatalf("splitREPLCommand returned error: %v", err)
	}
	want := []string{"owlcms", "install-zip", "~/Downloads/competition build.zip", "66.0.0"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("expected %#v, got %#v", want, args)
	}
}

func TestBuildREPLModuleCommandStartUsesDefaultSelector(t *testing.T) {
	cmd, err := buildREPLModuleCommand("owlcms", "start", nil)
	if err != nil {
		t.Fatalf("buildREPLModuleCommand returned error: %v", err)
	}
	if cmd.Action != "launch" || !cmd.DaemonMode || cmd.Version != "A" || cmd.Port != "" {
		t.Fatalf("unexpected command: %#v", cmd)
	}
}

func TestOwlcmsVersionSettingsPersist(t *testing.T) {
	installDir := t.TempDir()
	versionDir := filepath.Join(installDir, "66.0.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("create version dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "env.properties"), []byte("OWLCMS_PORT=8080\n"), 0o644); err != nil {
		t.Fatalf("write version env: %v", err)
	}
	owlcms.SetInstallDir(installDir)
	t.Cleanup(resetInstallDirsForTest)

	var output bytes.Buffer
	commands := [][]string{
		{"A", "port", "18080"},
		{"A", "tracker", "on", "wss://tracker.example.org/ws", "18443"},
		{"A", "mqtt", "on", "1884"},
	}
	for _, command := range commands {
		if handled, err := executeREPLOwlcmsSetting(command, &output); !handled || err != nil {
			t.Fatalf("execute %#v: handled=%v err=%v", command, handled, err)
		}
	}

	content, err := os.ReadFile(filepath.Join(versionDir, "env.properties"))
	if err != nil {
		t.Fatalf("read version env: %v", err)
	}
	for _, expected := range []string{
		"OWLCMS_PORT = 18080",
		"OWLCMS_VIDEODATA = wss://tracker.example.org:18443/ws",
		"OWLCMS_ENABLEEMBEDDEDMQTT = true",
		"OWLCMS_MQTTPORT = 1884",
	} {
		if !strings.Contains(string(content), expected) {
			t.Errorf("expected %q in version env, got %q", expected, string(content))
		}
	}

	for _, command := range [][]string{{"A", "tracker", "off"}, {"A", "mqtt", "off"}} {
		if handled, err := executeREPLOwlcmsSetting(command, &output); !handled || err != nil {
			t.Fatalf("execute %#v: handled=%v err=%v", command, handled, err)
		}
	}
	content, err = os.ReadFile(filepath.Join(versionDir, "env.properties"))
	if err != nil {
		t.Fatalf("read disabled version env: %v", err)
	}
	if !strings.Contains(string(content), "OWLCMS_VIDEODATA = ") || !strings.Contains(string(content), "OWLCMS_ENABLEEMBEDDEDMQTT = false") {
		t.Fatalf("expected disabled settings in version env, got %q", string(content))
	}
}

func TestBuildREPLModuleCommandDuplicateUsesSelector(t *testing.T) {
	cmd, err := buildREPLModuleCommand("tracker", "duplicate", []string{"B", "practice"})
	if err != nil {
		t.Fatalf("buildREPLModuleCommand returned error: %v", err)
	}
	if cmd.Action != "duplicate" || cmd.FromVersion != "B" || cmd.DuplicateName != "practice" {
		t.Fatalf("unexpected command: %#v", cmd)
	}
}

func TestBuildREPLModuleCommandUpdateSelectorDefaultsReleaseToLatest(t *testing.T) {
	cmd, err := buildREPLModuleCommand("owlcms", "update", []string{"A"})
	if err != nil {
		t.Fatalf("buildREPLModuleCommand returned error: %v", err)
	}
	if cmd.Action != "update" || cmd.Version != "A" || cmd.UpdateTo != "latest" {
		t.Fatalf("unexpected command: %#v", cmd)
	}
}

func TestREPLPromptUsesStartupSelectedInstance(t *testing.T) {
	t.Setenv("CONTROLPANEL_INSTANCE", "records")
	if got := (replSession{}).prompt(); got != "controlpanel[records]> " {
		t.Fatalf("expected records prompt, got %q", got)
	}
}

func TestREPLCommandChangesVersions(t *testing.T) {
	for _, action := range []string{"install", "install-zip", "update", "rename", "duplicate", "remove"} {
		if !replCommandChangesVersions(action) {
			t.Errorf("expected %q to refresh the version list", action)
		}
	}
	for _, action := range []string{"list", "launch", "stop", "status", "create-zip", "import"} {
		if replCommandChangesVersions(action) {
			t.Errorf("expected %q not to refresh the version list", action)
		}
	}
}

func TestREPLSelectorLabelsUseSpreadsheetOrder(t *testing.T) {
	for index, want := range []string{"A", "Z", "AA", "AB"} {
		actualIndex := []int{0, 25, 26, 27}[index]
		if got := replVersionSelector(actualIndex); got != want {
			t.Errorf("selector %d: expected %q, got %q", actualIndex, want, got)
		}
		if got, ok := replSelectorIndex(want); !ok || got != actualIndex {
			t.Errorf("index %q: expected %d, got %d (ok=%v)", want, actualIndex, got, ok)
		}
	}
}

func TestBatchStopsAtFirstCommandError(t *testing.T) {
	var output bytes.Buffer
	err := runREPL(replInvocation{batchFile: "-"}, replSession{}, strings.NewReader("# setup\nunknown\ncontext\n"), &output, &output)
	if err == nil || !strings.Contains(err.Error(), "batch line 2") {
		t.Fatalf("expected line 2 batch error, got %v", err)
	}
}

func TestInteractiveREPLPrintsPromptBeforeCommands(t *testing.T) {
	var output bytes.Buffer
	err := runREPL(replInvocation{interactive: true}, replSession{}, strings.NewReader("help\nexit\n"), &output, &output)
	if err != nil {
		t.Fatalf("runREPL returned error: %v", err)
	}
	if !strings.HasPrefix(output.String(), "controlpanel> ") {
		t.Fatalf("expected output to start with prompt, got %q", output.String())
	}
}

func TestREPLHelpPointsToModuleHelp(t *testing.T) {
	var output bytes.Buffer
	_, err := executeREPLLine("help", &replSession{}, &output)
	if err != nil {
		t.Fatalf("help returned error: %v", err)
	}
	if !strings.Contains(output.String(), "owlcms help") || !strings.Contains(output.String(), "tracker help") {
		t.Fatalf("expected module help directions, got %q", output.String())
	}
}

func TestREPLHelpRejectsCommandArgument(t *testing.T) {
	var output bytes.Buffer
	_, err := executeREPLLine("help tracker", &replSession{}, &output)
	if err == nil || !strings.Contains(err.Error(), "tracker help") {
		t.Fatalf("expected tracker help guidance, got %v", err)
	}
}

func TestREPLModuleHelpIsSeparated(t *testing.T) {
	for _, test := range []struct {
		command   string
		want      string
		doNotWant string
	}{
		{command: "owlcms help", want: "OWLCMS commands:", doNotWant: "Tracker commands:"},
		{command: "tracker help", want: "Tracker commands:", doNotWant: "OWLCMS commands:"},
	} {
		var output bytes.Buffer
		if _, err := executeREPLLine(test.command, &replSession{}, &output); err != nil {
			t.Fatalf("%s returned error: %v", test.command, err)
		}
		if !strings.Contains(output.String(), test.want) || strings.Contains(output.String(), test.doNotWant) {
			t.Fatalf("unexpected %s output: %q", test.command, output.String())
		}
	}
}
