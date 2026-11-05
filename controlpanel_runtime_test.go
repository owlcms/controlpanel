package main

import (
	"os"
	"path/filepath"
	"testing"

	"controlpanel/shared"
	"controlpanel/shared/portowner"
)

func TestControlPanelInstanceDir(t *testing.T) {
	t.Setenv("CONTROLPANEL_INSTALLDIR", "")
	defaultDir := shared.DefaultControlPanelInstallDir()
	records, err := resolveInstancePaths("records")
	if err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(t.TempDir(), "custom-controlpanel")

	for _, tc := range []struct {
		name    string
		args    []string
		environ []string
		want    string
	}{
		{"main", []string{`C:\Program Files\owlcms\owlcms.exe`}, nil, defaultDir},
		{"instance flag", []string{"controlpanel", "--instance", "records", "--repl"}, nil, records.ControlPanelDir},
		{"positional instance", []string{"controlpanel", "records", "--batch", "x.commands"}, nil, records.ControlPanelDir},
		{"env", []string{"controlpanel"}, []string{"HOME=/x", "CONTROLPANEL_INSTALLDIR=" + custom}, custom},
		{"flag beats env", []string{"controlpanel", "--instance", "records"}, []string{"CONTROLPANEL_INSTALLDIR=" + custom}, records.ControlPanelDir},
		{"no command line", nil, nil, ""},
	} {
		if got := controlPanelInstanceDir(tc.args, tc.environ); got != tc.want {
			t.Errorf("%s: controlPanelInstanceDir = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSplitWindowsCommandLine(t *testing.T) {
	got := portowner.SplitWindowsCommandLine(`"C:\Users\x\AppData\Roaming\owlcms Control Panel\owlcms.exe" --instance-dir "C:\My Instances\records"  --repl`)
	want := []string{`C:\Users\x\AppData\Roaming\owlcms Control Panel\owlcms.exe`, "--instance-dir", `C:\My Instances\records`, "--repl"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFindRunningControlPanelsExcludesSelf(t *testing.T) {
	found, err := findRunningControlPanels()
	if err != nil {
		t.Fatal(err)
	}
	for _, cp := range found {
		if cp.Identity.PID == int32(os.Getpid()) {
			t.Fatal("the current process must not be reported as another Control Panel")
		}
	}
}
