package tracker

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"controlpanel/shared"

	"fyne.io/fyne/v2/test"
)

func TestLatestLocalConnectionUsesLatestReleaseSettings(t *testing.T) {
	dir := t.TempDir()
	previousDir := installDir
	SetInstallDir(dir)
	t.Cleanup(func() { SetInstallDir(previousDir) })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	if old, ok := os.LookupEnv(sharedKeyEnv); ok {
		t.Cleanup(func() { _ = os.Setenv(sharedKeyEnv, old) })
	}
	if err := os.Unsetenv(sharedKeyEnv); err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{"2.1.0", "2.2.0"} {
		if err := os.MkdirAll(filepath.Join(dir, version), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := SavePropertyForRelease("2.1.0", "TRACKER_PORT", "18001"); err != nil {
		t.Fatal(err)
	}
	if err := SavePropertyForRelease("2.2.0", "TRACKER_PORT", "18002"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSharedKeyForRelease("2.2.0", "latest-key"); err != nil {
		t.Fatal(err)
	}
	config, err := LatestLocalConnection()
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != "2.2.0" || config.Port != "18002" || config.Key != "latest-key" {
		t.Fatalf("unexpected latest connection: version=%s port=%s key matches=%t", config.Version, config.Port, config.Key == "latest-key")
	}
	if err := SavePropertyForRelease("2.2.0", "TRACKER_PORT", "invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestLocalConnection(); err == nil {
		t.Fatal("expected invalid Tracker port to be rejected")
	}
}

func TestAutomaticTrackerReusesAndStopsRunningLatest(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	dir := t.TempDir()
	previousDir, previousProcess, previousRuntime := installDir, currentProcess, activeRuntime
	SetInstallDir(dir)
	t.Cleanup(func() {
		currentProcess, activeRuntime = previousProcess, previousRuntime
		SetInstallDir(previousDir)
	})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv(sharedKeyEnv, "")
	for _, version := range []string{"2.1.0", "2.2.0"} {
		if err := os.MkdirAll(filepath.Join(dir, version), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := SavePropertyForRelease("2.2.0", "TRACKER_PORT", "invalid-after-start"); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAutomaticTrackerHelper$")
	cmd.Env = shared.UpsertEnv(os.Environ(), "CONTROLPANEL_TEST_TRACKER", "1")
	cmd.Env = shared.UpsertEnv(cmd.Env, sharedKeyEnv, "running-key")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		if shared.IsProcessRunning(cmd.Process.Pid) {
			_ = cmd.Process.Kill()
		}
		<-done
	})
	port, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	port = strings.TrimSpace(port)
	currentProcess = cmd
	if _, err := shared.WriteRuntimeMetadata(runtimeMetadataPath(), cmd.Process.Pid, "2.1.0", port, false); err != nil {
		t.Fatal(err)
	}
	if _, err := StartLatestForOwlcms(); err == nil || !strings.Contains(err.Error(), "2.1.0") {
		t.Fatalf("expected a conflict with older Tracker: %v", err)
	}
	if _, err := shared.WriteRuntimeMetadata(runtimeMetadataPath(), cmd.Process.Pid, "2.2.0", port, false); err != nil {
		t.Fatal(err)
	}
	connection, err := StartLatestForOwlcms()
	if err != nil {
		t.Fatal(err)
	}
	if connection.Port != port || connection.Key != "running-key" || connection.Version != "2.2.0" {
		t.Fatalf("running Tracker configuration was not used: version=%s port=%s key matches=%t", connection.Version, connection.Port, connection.Key == "running-key")
	}
	if err := connection.Stop(); err != nil {
		t.Fatalf("stop reused Tracker: %v", err)
	}
	if shared.IsProcessRunning(cmd.Process.Pid) {
		t.Fatal("Tracker is still running after stop")
	}
}

func TestAutomaticTrackerHelper(t *testing.T) {
	if os.Getenv("CONTROLPANEL_TEST_TRACKER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(listener.Addr().(*net.TCPAddr).Port)
	for {
		conn, err := listener.Accept()
		if err != nil {
			os.Exit(1)
		}
		conn.Close()
	}
}
