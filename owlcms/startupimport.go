package owlcms

import (
	"errors"
	"os/exec"
	"strings"

	"controlpanel/shared"
)

// loadJSONEnv names an export (or a folder of exports) that OWLCMS loads at startup, replacing the database.
const loadJSONEnv = "OWLCMS_LOADJSON"

// exitRestartWithoutStartupImport is the exit code OWLCMS uses for a restart requested after a successful
// OWLCMS_LOADJSON load (Main.EXIT_RESTART_WITHOUT_STARTUP_IMPORT), for example after an import done in the UI:
// the restart must keep the current database instead of loading the startup file again.
const exitRestartWithoutStartupImport = 75

// launchesStartupImport reports whether the environment makes OWLCMS load an export at startup.
func launchesStartupImport(env []string) bool {
	prefix := loadJSONEnv + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) && strings.TrimSpace(entry[len(prefix):]) != "" {
			return true
		}
	}
	return false
}

// withoutStartupImport returns a copy of the environment without OWLCMS_LOADJSON.
func withoutStartupImport(env []string) []string {
	prefix := loadJSONEnv + "="
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// restartDecision decides whether OWLCMS is restarted after it exits, and with which environment.
//   - exit 75: OWLCMS asked for a restart after its startup load; restart without OWLCMS_LOADJSON, so this launch
//     and later ones keep the database.
//   - otherwise, a launch that loads an export is never restarted: a restart would load the file again and
//     replace the work done since.
//   - otherwise, the usual rules apply (shared.ShouldRestartProcess).
func restartDecision(env []string, waitErr error) (bool, []string) {
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == exitRestartWithoutStartupImport {
		return true, withoutStartupImport(env)
	}
	if launchesStartupImport(env) {
		return false, env
	}
	return shared.ShouldRestartProcess(waitErr), env
}
