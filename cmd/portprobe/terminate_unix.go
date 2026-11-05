//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// errNoSoftStop is never returned on Unix: SIGTERM is a real soft stop that
// OWLCMS catches and turns into exit 0. It exists so stopProcess can test for
// it on every platform.
var errNoSoftStop = errors.New("no soft stop available")

// terminate sends SIGTERM (force=false) or SIGKILL (force=true) to one PID.
func terminate(pid int32, force bool) error {
	p, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}
	if force {
		return p.Signal(syscall.SIGKILL)
	}
	return p.Signal(syscall.SIGTERM)
}
