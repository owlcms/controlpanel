package shared

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	psnet "github.com/shirou/gopsutil/net"
)

// RuntimeMetadata stores the minimum process identity needed to reconnect
// to a previously launched background service safely.
type RuntimeMetadata struct {
	PID               int    `json:"pid"`
	Version           string `json:"version"`
	Port              string `json:"port"`
	Daemon            bool   `json:"daemon"`
	ProcessStartTicks uint64 `json:"processStartTicks"`
	StartedAt         string `json:"startedAt"`
}

const RunAsDaemonEnv = "CONTROLPANEL_RUN_AS_DAEMON"

// IsRunningUnderSystemd returns true when the current process was started by systemd.
// systemd sets INVOCATION_ID for service processes, which makes this check cheap and reliable.
func IsRunningUnderSystemd() bool {
	return strings.TrimSpace(os.Getenv("INVOCATION_ID")) != ""
}

// IsRunAsDaemonEnabled returns true when daemon mode is enabled for this control panel process.
func IsRunAsDaemonEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(RunAsDaemonEnv)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

// SetRunAsDaemonEnabled updates the process environment so runtime behavior can change immediately.
func SetRunAsDaemonEnabled(enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	return os.Setenv(RunAsDaemonEnv, value)
}

// LoadRuntimeMetadata reads a runtime metadata file.
func LoadRuntimeMetadata(filePath string) (*RuntimeMetadata, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var metadata RuntimeMetadata
	if err := json.Unmarshal(content, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal runtime metadata: %w", err)
	}

	return &metadata, nil
}

// WriteRuntimeMetadata writes runtime metadata atomically.
func WriteRuntimeMetadata(filePath string, pid int, version, port string, daemon bool) (*RuntimeMetadata, error) {
	if err := EnsureDir0755(filepath.Dir(filePath)); err != nil {
		return nil, fmt.Errorf("creating runtime metadata directory: %w", err)
	}

	startTicks, err := ReadProcessStartTicks(pid)
	if err != nil {
		return nil, err
	}

	metadata := &RuntimeMetadata{
		PID:               pid,
		Version:           version,
		Port:              strings.TrimSpace(port),
		Daemon:            daemon,
		ProcessStartTicks: startTicks,
		StartedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	content, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal runtime metadata: %w", err)
	}

	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		return nil, fmt.Errorf("write runtime metadata temp file: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("rename runtime metadata temp file: %w", err)
	}

	return metadata, nil
}

// ClearRuntimeMetadata removes the runtime metadata file if present.
func ClearRuntimeMetadata(filePath string) error {
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsProcessRunning returns true when a PID still exists.
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	if GetGoos() == "windows" {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false
		}
		return strings.Contains(string(output), strconv.Itoa(pid))
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// ReadProcessStartTicks returns the Linux /proc start time ticks for a PID.
// On non-Linux platforms it returns 0 with no error so callers can degrade gracefully.
func ReadProcessStartTicks(pid int) (uint64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid PID %d", pid)
	}

	if GetGoos() != "linux" {
		return 0, nil
	}

	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("read /proc stat for pid %d: %w", pid, err)
	}

	statLine := strings.TrimSpace(string(content))
	idx := strings.LastIndex(statLine, ")")
	if idx == -1 || idx+2 >= len(statLine) {
		return 0, fmt.Errorf("unexpected /proc stat format for pid %d", pid)
	}

	fields := strings.Fields(statLine[idx+2:])
	if len(fields) <= 19 {
		return 0, fmt.Errorf("missing start time in /proc stat for pid %d", pid)
	}

	startTicks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse start ticks for pid %d: %w", pid, err)
	}

	return startTicks, nil
}

// PIDMatchesStartTicks validates that the current process for a PID is the same one
// that was originally recorded in runtime metadata.
func PIDMatchesStartTicks(pid int, expected uint64) bool {
	if !IsProcessRunning(pid) {
		return false
	}

	if expected == 0 {
		return true
	}

	actual, err := ReadProcessStartTicks(pid)
	if err != nil {
		return false
	}

	return actual == expected
}

// GracefullyStopPID attempts to stop a process while allowing normal shutdown hooks to run.
//
// On Unix: sends SIGTERM first.  Java installs a SIGTERM handler that runs
// shutdown hooks and then exits, so SIGTERM is the cleanest stop signal.
// SIGINT is skipped — it produces exit code 130 (128+SIGINT) which systemd's
// Restart=on-failure would treat as a failure, triggering an unwanted restart.
// Under systemd with SuccessExitStatus=SIGTERM, a SIGTERM death is treated
// as a clean exit.
//
// On Windows: first tries os.Interrupt when available, then non-forced
// taskkill. Both are gentler than taskkill /F and give Java a chance to run
// shutdown hooks and choose its own exit code.
func GracefullyStopPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}

	if GetGoos() == "windows" {
		return windowsGracefullyStopPID(pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	// Unix (Linux/macOS): SIGTERM triggers Java shutdown hooks.
	// Java exits cleanly after running hooks, or is killed by the
	// kernel if the signal is not caught (same end result).
	if err := process.Signal(syscall.SIGTERM); err == nil {
		if waitForProcessExit(pid, 10*time.Second) {
			return nil
		}
	}

	return ForcefullyKillPID(pid)
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return !IsProcessRunning(pid)
}

// SignalStopPID asks a process to exit using only catchable or non-forced
// mechanisms. It deliberately does not escalate to force-kill because a forced
// kill prevents the child from choosing its own clean exit code.
func SignalStopPID(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}

	if GetGoos() == "windows" {
		process, err := os.FindProcess(pid)
		if err == nil {
			if err := process.Signal(os.Interrupt); err == nil {
				if waitForProcessExit(pid, timeout) {
					return nil
				}
			} else {
				log.Printf("SignalStopPID(%d): os.Interrupt not available or not accepted: %v", pid, err)
			}
		} else {
			log.Printf("SignalStopPID(%d): cannot open handle for os.Interrupt: %v", pid, err)
		}

		if err := windowsGentleTaskkill(pid); err != nil {
			return err
		}
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal SIGTERM to pid %d: %w", pid, err)
	}
	if timeout > 0 && !waitForProcessExit(pid, timeout) {
		return fmt.Errorf("pid %d did not exit within %s after SIGTERM", pid, timeout)
	}
	return nil
}

// StopOwnedProcess stops a child process that this control panel instance
// started and still owns through exec.Cmd. Use this before PID-file or port
// lookup so interactive stops target the exact child process handle.
func StopOwnedProcess(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("missing owned process handle")
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return fmt.Errorf("invalid owned process PID %d", pid)
	}

	if GetGoos() == "windows" {
		if err := cmd.Process.Kill(); err != nil {
			if !IsProcessRunning(pid) {
				return nil
			}
			return fmt.Errorf("kill owned process %d: %w", pid, err)
		}
	} else {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			if !IsProcessRunning(pid) {
				return nil
			}
			log.Printf("StopOwnedProcess(%d): SIGTERM failed: %v", pid, err)
			if err := cmd.Process.Kill(); err != nil {
				if !IsProcessRunning(pid) {
					return nil
				}
				return fmt.Errorf("kill owned process %d after SIGTERM failed: %w", pid, err)
			}
		}
	}

	if timeout > 0 && !waitForProcessExit(pid, timeout) {
		return fmt.Errorf("owned process %d did not exit within %s", pid, timeout)
	}

	return nil
}

func windowsGracefullyStopPID(pid int) error {
	process, err := os.FindProcess(pid)
	if err == nil {
		if err := process.Signal(os.Interrupt); err == nil {
			if waitForProcessExit(pid, 10*time.Second) {
				return nil
			}
		} else {
			log.Printf("GracefullyStopPID(%d): os.Interrupt not available or not accepted: %v", pid, err)
		}
	} else {
		log.Printf("GracefullyStopPID(%d): cannot open handle for os.Interrupt: %v", pid, err)
	}

	if err := windowsGentleTaskkill(pid); err == nil {
		return nil
	} else {
		log.Printf("GracefullyStopPID(%d): gentle taskkill failed: %v", pid, err)
	}

	return ForcefullyKillPID(pid)
}

func windowsGentleTaskkill(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid))
	out, err := cmd.CombinedOutput()
	if waitForProcessExit(pid, 10*time.Second) {
		return nil
	}

	combined := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("gentle taskkill pid %d: %w (%s)", pid, err, combined)
	}
	return fmt.Errorf("gentle taskkill pid %d: process still running (%s)", pid, combined)
}

// ForcefullyKillPID terminates a process immediately.
func ForcefullyKillPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}

	// On Windows, jump straight to taskkill /F /T when we can't get a
	// process handle (typically denied for elevated processes). taskkill
	// runs as its own process and can sometimes succeed where the direct
	// OpenProcess from this process is denied.
	process, err := os.FindProcess(pid)
	if err != nil {
		if GetGoos() == "windows" {
			return windowsTaskkill(pid)
		}
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if err := process.Kill(); err != nil && GetGoos() != "windows" {
		return fmt.Errorf("kill pid %d: %w", pid, err)
	}

	time.Sleep(500 * time.Millisecond)
	if !IsProcessRunning(pid) {
		return nil
	}

	if GetGoos() == "windows" {
		return windowsTaskkill(pid)
	}

	if err := process.Kill(); err != nil {
		return fmt.Errorf("kill pid %d after retry: %w", pid, err)
	}

	return nil
}

// windowsTaskkill runs `taskkill /F /T /PID <pid>` and reports success when
// the process is no longer running afterwards (taskkill sometimes returns
// non-zero with "Access is denied" yet still succeeds on the next attempt,
// or the process exits between the check and the call). When the regular
// invocation is denied (typically because the target process is running at
// a higher integrity level / elevated), a second attempt is made through
// UAC ("Run as administrator"), which pops a consent prompt to the user.
func windowsTaskkill(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	out, err := cmd.CombinedOutput()
	time.Sleep(500 * time.Millisecond)
	if !IsProcessRunning(pid) {
		return nil
	}

	combined := strings.TrimSpace(string(out))
	if err == nil {
		return fmt.Errorf("force taskkill pid %d: process still running (%s)", pid, combined)
	}

	// Access denied (typical exit status 255 with "could not be terminated"
	// or "Access is denied") usually means the target is elevated. Retry
	// via UAC. This will trigger a consent prompt; user can decline.
	lower := strings.ToLower(combined)
	if strings.Contains(lower, "access is denied") || strings.Contains(lower, "could not be terminated") {
		log.Printf("taskkill pid %d denied; retrying with elevation (UAC prompt)", pid)
		if elevErr := windowsElevatedTaskkill(pid); elevErr == nil {
			return nil
		} else {
			return fmt.Errorf("force taskkill pid %d: %w (%s); elevated retry: %v", pid, err, combined, elevErr)
		}
	}

	return fmt.Errorf("force taskkill pid %d: %w (%s)", pid, err, combined)
}

// windowsElevatedTaskkill uses PowerShell's Start-Process -Verb RunAs to
// re-invoke taskkill at administrator integrity. The user sees a UAC
// consent dialog. Returns nil if the process is gone afterwards.
func windowsElevatedTaskkill(pid int) error {
	psCmd := fmt.Sprintf(
		`Start-Process -FilePath taskkill -ArgumentList '/F','/T','/PID','%d' -Verb RunAs -WindowStyle Hidden -Wait`,
		pid,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	out, err := cmd.CombinedOutput()
	time.Sleep(500 * time.Millisecond)
	if !IsProcessRunning(pid) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("elevated taskkill: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return fmt.Errorf("elevated taskkill: process still running (%s)", strings.TrimSpace(string(out)))
}

// ResolvePIDFromFileOrPort returns a running PID using the standard lookup order:
// first the PID file, then the TCP port listener.
func ResolvePIDFromFileOrPort(pidFilePath, port string) (int, string, error) {
	trimmedPIDFile := strings.TrimSpace(pidFilePath)
	trimmedPort := strings.TrimSpace(port)

	if trimmedPIDFile != "" {
		content, err := os.ReadFile(trimmedPIDFile)
		switch {
		case err == nil:
			pidText := strings.TrimSpace(string(content))
			if pidText != "" {
				pid, parseErr := strconv.Atoi(pidText)
				if parseErr == nil && pid > 0 {
					if IsProcessRunning(pid) {
						return pid, "pid file", nil
					}
					log.Printf("PID file %s is stale (PID %d not running)", trimmedPIDFile, pid)
				} else {
					log.Printf("PID file %s is invalid (%q)", trimmedPIDFile, pidText)
				}
			}
		case os.IsNotExist(err):
			// Fall through to port lookup.
		default:
			return 0, "", fmt.Errorf("read PID file %s: %w", trimmedPIDFile, err)
		}
	}

	if trimmedPort == "" {
		return 0, "", nil
	}

	portNum, err := strconv.Atoi(trimmedPort)
	if err != nil || portNum < 1 || portNum > 65535 {
		return 0, "", fmt.Errorf("invalid port %q", port)
	}

	pid, err := FindPIDByPort(portNum)
	if err != nil {
		return 0, "", err
	}
	if pid > 0 {
		return pid, "port", nil
	}

	return 0, "", nil
}

// StopPIDFileOrPortProcess resolves a process using the PID file first and the
// port second, then applies the normal graceful-stop escalation until the port
// is free or the process is gone.
func StopPIDFileOrPortProcess(pidFilePath, port string) error {
	trimmedPort := strings.TrimSpace(port)
	deadline := time.Now().Add(5 * time.Second)

	for {
		pid, source, err := ResolvePIDFromFileOrPort(pidFilePath, trimmedPort)
		if err != nil {
			return err
		}
		if pid == 0 {
			return nil
		}

		log.Printf("Stopping PID %d resolved from %s", pid, source)
		if err := GracefullyStopPID(pid); err != nil {
			log.Printf("GracefullyStopPID(%d): %v", pid, err)
		}

		if trimmedPort == "" {
			if !IsProcessRunning(pid) {
				return nil
			}
		} else if CheckPort(trimmedPort) != nil {
			return nil
		}

		if time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	pid, source, err := ResolvePIDFromFileOrPort(pidFilePath, trimmedPort)
	if err != nil {
		return err
	}
	if pid != 0 {
		if source == "" {
			source = "process lookup"
		}
		if trimmedPort != "" {
			return fmt.Errorf("port %s is still in use by PID %d resolved from %s", trimmedPort, pid, source)
		}
		return fmt.Errorf("PID %d resolved from %s is still running", pid, source)
	}

	return nil
}

// FindPIDByPort returns the PID of the process listening on the given TCP port.
// Returns 0 with no error if no listener is found.
func FindPIDByPort(port int) (int, error) {
	conns, err := psnet.Connections("tcp")
	if err != nil {
		return 0, fmt.Errorf("list TCP connections: %w", err)
	}
	for _, c := range conns {
		if c.Laddr.Port == uint32(port) && c.Status == "LISTEN" {
			return int(c.Pid), nil
		}
	}
	return 0, nil
}

// EnsurePortFree finds whatever process is listening on the given port and
// stops it (SIGINT → SIGTERM → SIGKILL), then verifies the port is free.
// This is the single stop primitive used by both interactive and headless paths.
func EnsurePortFree(port string) error {
	return StopPIDFileOrPortProcess("", port)
}

// CheckPort tries to connect to localhost:port and returns nil if a TCP server is listening.
// This answers "is the port occupied", not "is the application usable": a server may accept
// connections long before it is able to serve requests. Use CheckPortReady for readiness.
func CheckPort(port string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", strings.TrimSpace(port)), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

// CheckPortReady issues an HTTP request to localhost:port and returns nil once the
// application answers. Unlike CheckPort, this goes through the servlet/handler layer,
// so applications that hold requests until they are initialized (OWLCMS blocks the
// bootstrap page until the database is ready) only answer when they are actually usable.
// The timeout is generous because the request is expected to be held during startup.
func CheckPortReady(port string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%s", strings.TrimSpace(port)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// CheckDaemonRunning loads runtime metadata and checks whether the recorded
// daemon is still alive by validating its PID (with start-ticks on Linux).
// It does not adopt an arbitrary process listening on the recorded port.
func CheckDaemonRunning(metadataPath string) (*RuntimeMetadata, bool) {
	metadata, err := LoadRuntimeMetadata(metadataPath)
	if err != nil || metadata == nil {
		return nil, false
	}
	if strings.TrimSpace(metadata.Version) == "" || strings.TrimSpace(metadata.Port) == "" {
		return nil, false
	}

	// Primary: PID check (with start-ticks validation on Linux)
	if metadata.PID > 0 && PIDMatchesStartTicks(metadata.PID, metadata.ProcessStartTicks) {
		return metadata, true
	}

	return nil, false
}
