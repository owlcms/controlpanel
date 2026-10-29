package owlcms

import (
	"os/exec"
	"runtime"
	"testing"
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
