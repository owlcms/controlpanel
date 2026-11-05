//go:build windows

package portowner

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

// errNoSoftStop is returned for the soft step: Windows has no SIGTERM, and a
// non-forced taskkill only posts WM_CLOSE to the target's windows. OWLCMS is a
// windowless Java process, so that request never reaches it.
var errNoSoftStop = errors.New("no soft stop on Windows for a windowless process")

// createNoWindow keeps taskkill from flashing a console window.
const createNoWindow = 0x08000000

// terminate ends a process. force=false reports errNoSoftStop; force=true
// runs taskkill /F, which calls TerminateProcess with exit code 1. No /T: only
// the confirmed PID is killed, not its descendants. taskkill output is not
// shown: its language and code page depend on the Windows installation.
func terminate(pid int32, force bool) error {
	if !force {
		return errNoSoftStop
	}
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.FormatInt(int64(pid), 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("taskkill /F failed: %w", err)
	}
	return nil
}
