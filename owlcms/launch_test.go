package owlcms

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"controlpanel/shared"

	"github.com/magiconair/properties"
)

func TestShouldUseOwlcmsDaemonWrapperForDetachedDaemonMode(t *testing.T) {
	t.Setenv("CONTROLPANEL_RUN_AS_DAEMON", "true")
	t.Setenv("INVOCATION_ID", "")
	withOwlcmsGoos(t, "linux")

	if !shouldUseOwlcmsDaemonWrapper() {
		t.Fatalf("expected detached daemon launches to use MainWrapper")
	}
}

func TestShouldUseOwlcmsDaemonWrapperDisabledUnderSystemd(t *testing.T) {
	t.Setenv("CONTROLPANEL_RUN_AS_DAEMON", "true")
	t.Setenv("INVOCATION_ID", "systemd-123")
	withOwlcmsGoos(t, "linux")

	if shouldUseOwlcmsDaemonWrapper() {
		t.Fatalf("expected systemd launches to skip MainWrapper")
	}
}

func withOwlcmsGoos(t *testing.T, goos string) {
	t.Helper()
	previousGoos := owlcmsGoos
	owlcmsGoos = func() string { return goos }
	t.Cleanup(func() {
		owlcmsGoos = previousGoos
	})
}

func TestBuildOwlcmsCommandUsesMainWrapperForDaemonMode(t *testing.T) {
	params := &owlcmsLaunchParams{
		JavaPath: "/tmp/java",
		JarPath:  "/tmp/owlcms.jar",
	}

	cmd := buildOwlcmsCommand(params, true)

	want := []string{"/tmp/java", "-cp", "/tmp/owlcms.jar", "app.owlcms.MainWrapper"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected args length: got %v want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("unexpected arg %d: got %q want %q (all args: %v)", i, cmd.Args[i], want[i], cmd.Args)
		}
	}
}

func TestBuildOwlcmsCommandUsesJarEntrypointForNormalMode(t *testing.T) {
	params := &owlcmsLaunchParams{
		JavaPath: "/tmp/java",
		JarPath:  "/tmp/owlcms.jar",
	}

	cmd := buildOwlcmsCommand(params, false)

	want := []string{"/tmp/java", "-jar", "owlcms.jar"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected args length: got %v want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("unexpected arg %d: got %q want %q (all args: %v)", i, cmd.Args[i], want[i], want)
		}
	}
}

func TestAcquireJavaLockClearsStalePIDWithoutStoppingPortOwner(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on test port: %v", err)
	}
	defer listener.Close()

	previousEnvironment := environment
	previousPIDFilePath := pidFilePath
	previousLockFilePath := lockFilePath
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	environment = properties.NewProperties()
	environment.Set("OWLCMS_PORT", port)
	pidFilePath = filepath.Join(t.TempDir(), "java.pid")
	lockFilePath = filepath.Join(t.TempDir(), "java.lock")
	t.Cleanup(func() {
		environment = previousEnvironment
		pidFilePath = previousPIDFilePath
		lockFilePath = previousLockFilePath
	})

	stalePID := os.Getpid() + 100000
	for shared.IsProcessRunning(stalePID) {
		stalePID++
	}
	if err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(stalePID)), 0644); err != nil {
		t.Fatalf("write stale PID file: %v", err)
	}

	if _, err := acquireJavaLock(); err != nil {
		t.Fatalf("acquire Java lock: %v", err)
	}
	if _, err := os.Stat(pidFilePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale PID file to be removed, stat error: %v", err)
	}
	if err := shared.CheckPort(port); err != nil {
		t.Fatalf("stale PID cleanup stopped the listener on port %s: %v", port, err)
	}
}

func TestSetEnvValueAppendsWhenMissing(t *testing.T) {
	env := []string{"A=1", "B=2"}
	env = setEnvValue(env, embeddedMQTTEnv, "false")

	if got := env[len(env)-1]; got != embeddedMQTTEnv+"=false" {
		t.Fatalf("expected appended mqtt env, got %q", got)
	}
}

func TestSetEnvValueReplacesExistingValue(t *testing.T) {
	env := []string{"A=1", embeddedMQTTEnv + "=true", "B=2"}
	env = setEnvValue(env, embeddedMQTTEnv, "false")

	var matches int
	for _, entry := range env {
		if entry == embeddedMQTTEnv+"=false" {
			matches++
		}
		if entry == embeddedMQTTEnv+"=true" {
			t.Fatalf("expected existing mqtt env to be replaced, env=%v", env)
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly one mqtt env entry, got %d in %v", matches, env)
	}
}

func TestRecoveredInteractiveRuntimeIsClosable(t *testing.T) {
	previousProcess := currentProcess
	previousRuntime := activeRuntime
	currentProcess = nil
	activeRuntime = &shared.RuntimeMetadata{Daemon: false}
	t.Cleanup(func() {
		currentProcess = previousProcess
		activeRuntime = previousRuntime
	})

	if !IsLocalProcessRunning() {
		t.Fatal("expected recovered interactive runtime to be closable")
	}
	if IsRecoveredDaemonRunning() {
		t.Fatal("did not expect recovered interactive runtime to be treated as a daemon")
	}
}

func TestRecoveredDaemonRuntimeIsNotClosable(t *testing.T) {
	previousProcess := currentProcess
	previousRuntime := activeRuntime
	currentProcess = nil
	activeRuntime = &shared.RuntimeMetadata{Daemon: true}
	t.Cleanup(func() {
		currentProcess = previousProcess
		activeRuntime = previousRuntime
	})

	if IsLocalProcessRunning() {
		t.Fatal("did not expect recovered daemon runtime to be closable")
	}
	if !IsRecoveredDaemonRunning() {
		t.Fatal("expected recovered daemon runtime to be identified as a daemon")
	}
}

func TestApplyPropertiesToEnvClearsInheritedTrackerConnection(t *testing.T) {
	env := []string{trackerConnectionEnv + "=ws://127.0.0.1:18123/ws"}
	props := properties.NewProperties()
	props.Set(trackerConnectionEnv, "")

	env = applyOwlcmsPropertiesToEnv(env, props)

	for _, entry := range env {
		if strings.HasPrefix(entry, trackerConnectionEnv+"=") {
			t.Fatalf("expected tracker env to be removed, env=%v", env)
		}
	}
	if len(env) != 0 {
		t.Fatalf("expected tracker env slice to be empty after removal, got %v", env)
	}
}

func TestApplyPropertiesToEnvSkipsControlPanelTrackerSettings(t *testing.T) {
	props := properties.NewProperties()
	props.Set(trackerConnectionURLSetting, "wss://tracker.example.com/ws")
	props.Set(trackerConnectionPortSetting, "443")
	props.Set(trackerConnectionDefaultEnabledKey, "false")
	props.Set(checkpointFolderSetting, "/Users/me/checkpoints")

	env := applyOwlcmsPropertiesToEnv(nil, props)
	if len(env) != 0 {
		t.Fatalf("expected control-panel settings to stay out of OWLCMS environment, got %v", env)
	}
}
