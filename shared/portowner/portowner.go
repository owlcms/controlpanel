// Package portowner identifies the processes listening on a TCP port and their
// parents, and stops a confirmed process without elevation. It never acts on a
// PID whose identity (creation time) or port ownership has changed since it was
// inspected. The OWLCMS-specific parts (supervisors that restart OWLCMS, the
// HTTP stop endpoint) are used only by the OWLCMS port-freeing flow.
package portowner

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	psnet "github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
)

// Identity is a process as observed at one point in time.
type Identity struct {
	PID        int32
	Created    int64
	Parent     int32
	Executable string
	Name       string
	Cmdline    string
	// Args is the command line split into arguments; nil when unavailable.
	Args []string
	Cwd  string
}

// Listener is one listening socket on the inspected port.
type Listener struct {
	PID     int32
	Address string
}

// Classification hints, from OS-reported facts only.
const (
	KindMainWrapper    = "OWLCMS (MainWrapper supervisor)"
	KindOwlcmsMain     = "OWLCMS (app.owlcms.Main)"
	KindOwlcmsJar      = "OWLCMS (java -jar owlcms.jar)"
	KindOwlcmsProbably = "probably OWLCMS (Java in an owlcms directory)"
	KindControlPanel   = "Control Panel"
	KindTracker        = "probably Tracker (node)"
	KindUnrecognized   = "unrecognized"
)

// GOOS is a variable so tests can exercise the Windows branch.
var GOOS = runtime.GOOS

// FindListeners returns the TCP listeners on port, sorted by PID then address.
// A PID of 0 means the owner could not be determined.
func FindListeners(port int) ([]Listener, error) {
	conns, err := psnet.Connections("tcp")
	if err != nil {
		return nil, fmt.Errorf("list TCP connections: %w", err)
	}
	var result []Listener
	for _, c := range conns {
		if c.Laddr.Port == uint32(port) && strings.EqualFold(c.Status, "LISTEN") {
			result = append(result, Listener{PID: c.Pid, Address: net.JoinHostPort(c.Laddr.IP, strconv.Itoa(port))})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PID != result[j].PID {
			return result[i].PID < result[j].PID
		}
		return result[i].Address < result[j].Address
	})
	return result, nil
}

// ListeningPorts returns the TCP ports each process listens on, keyed by PID.
// PID 0 entries (owner unknown) are omitted.
func ListeningPorts() (map[int32][]int, error) {
	conns, err := psnet.Connections("tcp")
	if err != nil {
		return nil, fmt.Errorf("list TCP connections: %w", err)
	}
	result := make(map[int32][]int)
	for _, c := range conns {
		if c.Pid <= 0 || !strings.EqualFold(c.Status, "LISTEN") {
			continue
		}
		port := int(c.Laddr.Port)
		ports := result[c.Pid]
		seen := false
		for _, p := range ports {
			if p == port {
				seen = true
				break
			}
		}
		if !seen {
			result[c.Pid] = append(ports, port)
		}
	}
	return result, nil
}

// Inspect takes a fresh observation of pid.
func Inspect(pid int32) (Identity, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return Identity{}, fmt.Errorf("inspect PID %d: %w", pid, err)
	}
	created, err := p.CreateTime()
	if err != nil || created <= 0 {
		return Identity{}, fmt.Errorf("read creation time for PID %d: %v", pid, err)
	}
	parent, err := p.Ppid()
	if err != nil {
		return Identity{}, fmt.Errorf("read parent of PID %d: %w", pid, err)
	}
	executable, err := p.Exe()
	if err != nil {
		executable = fmt.Sprintf("(unavailable: %v)", err)
	} else if executable == "" {
		executable = "(unavailable)"
	}
	name, err := p.Name()
	if err != nil || name == "" {
		name = "(unavailable)"
	}
	cmdline, err := p.Cmdline()
	if err != nil || strings.TrimSpace(cmdline) == "" {
		cmdline = "(unavailable)"
	}
	args := commandArgs(p, cmdline)
	cwd, err := p.Cwd()
	if err != nil || cwd == "" {
		// Windows refuses the working directory of elevated processes
		cwd = "(unavailable)"
	}
	return Identity{PID: pid, Created: created, Parent: parent, Executable: executable,
		Name: name, Cmdline: cmdline, Args: args, Cwd: cwd}, nil
}

// commandArgs splits the command line. Linux and macOS report the arguments
// individually; this gopsutil version splits the Windows string on spaces,
// which breaks quoted paths, so Windows is parsed here.
func commandArgs(p *process.Process, cmdline string) []string {
	if cmdline == "(unavailable)" {
		return nil
	}
	if GOOS == "windows" {
		return SplitWindowsCommandLine(cmdline)
	}
	args, err := p.CmdlineSlice()
	if err != nil {
		return strings.Fields(cmdline)
	}
	return args
}

// SplitWindowsCommandLine splits a Windows command line on unquoted spaces,
// removing the double quotes around quoted arguments.
func SplitWindowsCommandLine(cmdline string) []string {
	var args []string
	var current strings.Builder
	inQuotes, inArg := false, false
	for _, r := range cmdline {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			inArg = true
		case (r == ' ' || r == '\t') && !inQuotes:
			if inArg {
				args = append(args, current.String())
				current.Reset()
				inArg = false
			}
		default:
			current.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		args = append(args, current.String())
	}
	return args
}

// Environ returns the initial environment of pid, or nil when the OS does not
// expose it (macOS with this gopsutil version) or access is denied. Variables a
// process sets on itself after starting are not visible on Linux.
func Environ(pid int32) []string {
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil
	}
	env, err := p.Environ()
	if err != nil {
		return nil
	}
	return env
}

// EnvValue returns the value of key in the initial environment of pid, or ""
// when absent or not readable. Reading another process's environment requires
// the same user on the same machine, which is the intended access control.
func EnvValue(pid int32, key string) string {
	for _, entry := range Environ(pid) {
		if value, ok := strings.CutPrefix(entry, key+"="); ok {
			return value
		}
	}
	if GOOS == "darwin" {
		return darwinEnvValue(pid, key)
	}
	return ""
}

// darwinEnvValue reads one variable with ps -E, which this gopsutil version
// does not offer on macOS. Values containing whitespace are not supported.
func darwinEnvValue(pid int32, key string) string {
	out, err := exec.Command("ps", "-E", "-o", "command=", "-p", strconv.Itoa(int(pid))).Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(key) + `=(\S+)`)
	m := re.FindSubmatch(out)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// ListProcesses returns a fresh observation of every inspectable process.
func ListProcesses() ([]Identity, error) {
	pids, err := process.Pids()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var result []Identity
	for _, pid := range pids {
		id, err := Inspect(pid)
		if err != nil {
			continue
		}
		result = append(result, id)
	}
	return result, nil
}

// TrimCommand returns a displayable command line: classpath and module path
// arguments are elided so the java invocation and main class stay visible, and
// the result is length-capped.
func TrimCommand(cmdline string) string {
	args := strings.Fields(cmdline)
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		out = append(out, args[i])
		if (args[i] == "-cp" || args[i] == "-classpath" || args[i] == "--class-path" ||
			args[i] == "-p" || args[i] == "--module-path") && i+1 < len(args) {
			out = append(out, "(elided)")
			i++
		}
	}
	trimmed := strings.Join(out, " ")
	const maxLen = 600
	if len(trimmed) > maxLen {
		trimmed = trimmed[:maxLen-len("…")] + "…"
	}
	return trimmed
}

// Classify guesses what a process is. It is a display hint, never a reason to
// stop anything.
func Classify(id Identity) string {
	exe := id.Executable
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		exe = exe[i+1:]
	}
	exe = strings.ToLower(exe)
	cmdline := strings.ToLower(id.Cmdline)
	cwd := strings.ToLower(id.Cwd)
	isJava := exe == "java" || exe == "java.exe" || exe == "javaw" || exe == "javaw.exe"
	switch {
	case isJava && strings.Contains(cmdline, "app.owlcms.mainwrapper"):
		return KindMainWrapper
	case isJava && strings.Contains(cmdline, "app.owlcms.main"):
		return KindOwlcmsMain
	case isJava && strings.Contains(cmdline, "owlcms.jar"):
		return KindOwlcmsJar
	case isJava && strings.Contains(cwd, "owlcms"):
		return KindOwlcmsProbably
	// Released Control Panels are named owlcms(.exe); development builds controlpanel(.exe).
	case exe == "controlpanel" || exe == "controlpanel.exe" || exe == "owlcms" || exe == "owlcms.exe" ||
		exe == "owlcms_controlpanel.exe":
		return KindControlPanel
	case (exe == "node" || exe == "node.exe") &&
		(strings.Contains(cmdline, "tracker") || strings.Contains(cwd, "tracker")):
		return KindTracker
	default:
		return KindUnrecognized
	}
}

// IsOwlcms reports whether the classification names an OWLCMS server process.
func IsOwlcms(id Identity) bool {
	switch Classify(id) {
	case KindOwlcmsMain, KindOwlcmsJar, KindOwlcmsProbably:
		return true
	}
	return false
}

// IsSupervisor reports whether a parent relaunches OWLCMS when it exits with a
// code in 1..127. Both the Go Control Panel and the Java MainWrapper do.
func IsSupervisor(id Identity) bool {
	switch Classify(id) {
	case KindControlPanel, KindMainWrapper:
		return true
	}
	return false
}

// ParentKind describes the relationship between a listener and its parent.
type ParentKind int

const (
	// ParentOrphan: the parent exited (or is init); nothing will restart the listener.
	ParentOrphan ParentKind = iota
	// ParentUnavailable: the parent exists but cannot be inspected.
	ParentUnavailable
	// ParentProtected: the parent must not be stopped (this program, a shell, a service manager).
	ParentProtected
	// ParentSupervisor: a Control Panel or MainWrapper that restarts OWLCMS on exit codes 1..127.
	ParentSupervisor
	// ParentOther: a live parent that is not a known supervisor.
	ParentOther
)

// Assessment is the listener and what is known about its parent.
type Assessment struct {
	Listener   Identity
	Parent     *Identity // set for ParentProtected, ParentSupervisor and ParentOther
	ParentKind ParentKind
	ParentErr  error // set for ParentUnavailable
	// SupervisorFirst is true when the supervisor must be stopped before the
	// listener: on Windows the only forced kill (taskkill /F) exits with code 1,
	// which both the Control Panel and MainWrapper restart. On Unix a SIGTERM
	// makes OWLCMS exit 0 and a SIGKILL is a signal death; neither is restarted.
	SupervisorFirst bool
}

// Assess inspects a listener PID and its parent. It refuses a PID that is this
// program or one of its ancestors.
func Assess(pid int32) (Assessment, error) {
	target, err := Inspect(pid)
	if err != nil {
		return Assessment{}, err
	}
	ancestors, err := OwnAncestors()
	if err != nil {
		return Assessment{}, fmt.Errorf("cannot safely identify this program's ancestors: %w", err)
	}
	if ancestors[target.PID] {
		return Assessment{}, fmt.Errorf("refusing to stop this program or its ancestors (PID %d)", target.PID)
	}
	a := Assessment{Listener: target}
	if target.Parent <= 1 {
		a.ParentKind = ParentOrphan
		return a, nil
	}
	p, parentErr := Inspect(target.Parent)
	switch {
	case errors.Is(parentErr, process.ErrorProcessNotRunning):
		a.ParentKind = ParentOrphan
	case parentErr != nil:
		a.ParentKind = ParentUnavailable
		a.ParentErr = parentErr
	case p.Created > target.Created:
		// Windows does not reparent orphans; the dead parent's PID was reused.
		a.ParentKind = ParentOrphan
	case ancestors[p.PID] || UnsafeParent(p.Executable):
		a.ParentKind = ParentProtected
		a.Parent = &p
	case IsSupervisor(p):
		a.ParentKind = ParentSupervisor
		a.Parent = &p
		a.SupervisorFirst = GOOS == "windows"
	default:
		a.ParentKind = ParentOther
		a.Parent = &p
	}
	return a, nil
}

// OwnAncestors returns this process and its live ancestors, plus PID 1.
func OwnAncestors() (map[int32]bool, error) {
	result := make(map[int32]bool)
	pid := int32(os.Getpid())
	var childCreated int64
	for pid > 1 {
		if result[pid] {
			return nil, fmt.Errorf("parent cycle at PID %d", pid)
		}
		p, err := process.NewProcess(pid)
		if errors.Is(err, process.ErrorProcessNotRunning) {
			// Windows does not reparent orphans: an ancestor can have exited.
			break
		}
		if err != nil {
			return nil, err
		}
		created, err := p.CreateTime()
		if err == nil && childCreated > 0 && created > childCreated {
			// The ancestor exited and its PID now belongs to a newer process.
			break
		}
		result[pid] = true
		childCreated = 0
		if err == nil {
			childCreated = created
		}
		pid, err = p.Ppid()
		if err != nil {
			return nil, err
		}
	}
	result[1] = true
	return result, nil
}

// UnsafeParent reports executables that must never be stopped as a parent.
func UnsafeParent(executable string) bool {
	name := strings.ToLower(filepath.Base(executable))
	switch name {
	case "", "(unavailable)", "systemd", "launchd", "init", "bash", "sh", "zsh", "fish", "dash",
		"cmd.exe", "powershell.exe", "pwsh.exe", "explorer.exe", "services.exe", "svchost.exe",
		"wininit.exe", "winlogon.exe", "code.exe":
		return true
	}
	return strings.HasPrefix(executable, "(unavailable:")
}

// SameProcess reports whether id is still alive with the same creation time.
// An error means the state is unknown, or the PID was reused.
func SameProcess(id Identity) (bool, error) {
	exists, err := process.PidExists(id.PID)
	if err != nil || !exists {
		return false, err
	}
	p, err := process.NewProcess(id.PID)
	var created int64
	if err == nil {
		created, err = p.CreateTime()
		if err == nil && created <= 0 {
			err = fmt.Errorf("invalid creation time for PID %d", id.PID)
		}
	}
	if err != nil {
		exists, checkErr := process.PidExists(id.PID)
		if checkErr != nil {
			return false, fmt.Errorf("inspect PID %d: %v; recheck existence: %w", id.PID, err, checkErr)
		}
		if !exists {
			return false, nil
		}
		return false, fmt.Errorf("inspect PID %d: %w", id.PID, err)
	}
	if created != id.Created {
		return false, fmt.Errorf("PID %d was reused; refusing to act", id.PID)
	}
	return true, nil
}

// ValidateAlive checks that id is unchanged and still running. Use it instead
// of ValidateListener once the process has released its port, for example after
// an accepted stop request, when it may still be running its shutdown hooks.
func ValidateAlive(id Identity) error {
	alive, err := SameProcess(id)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("PID %d no longer exists", id.PID)
	}
	return nil
}

// ValidateListener checks that id is unchanged and still listens on port.
func ValidateListener(id Identity, port int) error {
	alive, err := SameProcess(id)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("PID %d no longer exists", id.PID)
	}
	listeners, err := FindListeners(port)
	if err != nil {
		return err
	}
	for _, l := range listeners {
		if l.PID == id.PID {
			return nil
		}
	}
	return fmt.Errorf("PID %d no longer appears as a listener on port %d; refusing to act", id.PID, port)
}

// ValidateParent checks that parent is unchanged and is still child's parent.
func ValidateParent(child, parent Identity) error {
	alive, err := SameProcess(parent)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("parent PID %d has exited", parent.PID)
	}
	current, err := Inspect(child.PID)
	if err != nil {
		return err
	}
	if current.Created != child.Created || current.Parent != parent.PID {
		return fmt.Errorf("listener identity or parent has changed")
	}
	return nil
}

// Stop ends id: soft termination where the OS offers one that reaches the
// process, then forced termination. validate runs before each signal and must
// fail if the process or its role has changed. report receives progress lines.
func Stop(id Identity, validate func() error, report func(string)) error {
	if err := validate(); err != nil {
		return err
	}
	report(fmt.Sprintf("Requesting soft termination of PID %d", id.PID))
	softErr := terminate(id.PID, false)
	if errors.Is(softErr, errNoSoftStop) {
		report(fmt.Sprintf("No soft termination is available on %s; using forced termination", GOOS))
	} else {
		if softErr != nil {
			report(fmt.Sprintf("Soft termination failed: %v", softErr))
		}
		exited, err := WaitForExit(id, 5*time.Second)
		if err != nil || exited {
			return err
		}
		if err := validate(); err != nil {
			return err
		}
	}
	report(fmt.Sprintf("Requesting forced termination of PID %d", id.PID))
	if err := terminate(id.PID, true); err != nil {
		return fmt.Errorf("forced termination failed: %w", err)
	}
	exited, err := WaitForExit(id, 5*time.Second)
	if err != nil {
		return err
	}
	if !exited {
		return fmt.Errorf("PID %d is still running after forced termination", id.PID)
	}
	return nil
}

// WaitForExit polls until id has exited, the timeout expires, or its state
// becomes unknown.
func WaitForExit(id Identity, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		alive, err := SameProcess(id)
		if err != nil || !alive {
			return !alive && err == nil, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}
