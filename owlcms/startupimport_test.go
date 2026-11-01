package owlcms

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func exitWith(t *testing.T, code string) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh to produce exit codes")
	}
	err := exec.Command("sh", "-c", "exit "+code).Run()
	if err == nil {
		t.Fatalf("sh exited 0, expected %s", code)
	}
	return err
}

func TestRestartAfterStartupLoadDropsTheStartupImport(t *testing.T) {
	env := []string{"PATH=/bin", loadJSONEnv + "=/data/checkpoints", "OWLCMS_PORT=8080"}
	restart, next := restartDecision(env, exitWith(t, "75"))
	if !restart {
		t.Fatal("exit 75 must restart")
	}
	if launchesStartupImport(next) {
		t.Fatalf("restart environment still loads the startup file: %v", next)
	}
	if len(next) != 2 || next[0] != "PATH=/bin" || next[1] != "OWLCMS_PORT=8080" {
		t.Fatalf("other variables must be kept: %v", next)
	}
	if !launchesStartupImport(env) {
		t.Fatal("the original environment must not be modified")
	}
}

func TestStartupLoadLaunchIsNotRestartedOtherwise(t *testing.T) {
	env := []string{loadJSONEnv + "=/data/export.json"}
	for _, code := range []string{"1", "78"} {
		if restart, _ := restartDecision(env, exitWith(t, code)); restart {
			t.Fatalf("a launch loading an export must not restart on exit %s", code)
		}
	}
}

func TestOrdinaryLaunchKeepsTheUsualRules(t *testing.T) {
	env := []string{"OWLCMS_PORT=8080", loadJSONEnv + "=  "}
	if restart, _ := restartDecision(env, exitWith(t, "1")); !restart {
		t.Fatal("exit 1 restarts an ordinary launch")
	}
	if restart, _ := restartDecision(env, nil); restart {
		t.Fatal("exit 0 does not restart")
	}
	if restart, _ := restartDecision(env, exitWith(t, "75")); !restart {
		t.Fatal("exit 75 restarts an ordinary launch too")
	}
}

func TestCheckpointExportIsTheNewestJsonDirectlyInTheFolder(t *testing.T) {
	folder := t.TempDir()
	if _, err := checkpointExport(folder); err == nil {
		t.Fatal("an empty folder must not be offered")
	}
	if _, err := checkpointExport(""); err == nil {
		t.Fatal("no folder must not be offered")
	}
	if _, err := checkpointExport(filepath.Join(folder, "missing")); err == nil {
		t.Fatal("a missing folder must not be offered")
	}

	write := func(name string, at time.Time) {
		path := filepath.Join(folder, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	write("zips/2026-11-01T13-56-23.385Z.zip", now)
	write("logs/owlcms.log", now)
	if _, err := checkpointExport(folder); err == nil {
		t.Fatal("zips and logs alone must not be offered")
	}
	write("database_v2_2026-11-01_08h43.json", now.Add(-time.Hour))
	write("database_v2_2026-11-01_08h56.json", now)
	write("logs/nested.json", now.Add(time.Hour))

	export, err := checkpointExport(folder)
	if err != nil || export != "database_v2_2026-11-01_08h56.json" {
		t.Fatalf("expected the newest top-level export, got %q, %v", export, err)
	}
}

func TestOnlyOwlcms69AndLaterLaunchFromACheckpoint(t *testing.T) {
	for _, version := range []string{"69.0.0", "69.0.0-beta01", "69.1.2-alpha03", "70.0.0", "69.0.0+build7"} {
		if !canLaunchFromCheckpoint(version) {
			t.Errorf("%s must offer Launch from Checkpoint", version)
		}
	}
	for _, version := range []string{"68.2.1", "68.0.0-rc01", "4.33.0", "not-a-version"} {
		if canLaunchFromCheckpoint(version) {
			t.Errorf("%s must not offer Launch from Checkpoint", version)
		}
	}
}
