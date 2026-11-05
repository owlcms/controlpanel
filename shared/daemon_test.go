package shared

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCheckPortDetectsNonHTTPListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on test port: %v", err)
	}
	defer listener.Close()

	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err := CheckPort(port); err != nil {
		t.Fatalf("CheckPort(%s) did not detect TCP listener: %v", port, err)
	}
}

func TestCheckDaemonRunningDetectsRecordedProcess(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "runtime.json")
	metadata, err := WriteRuntimeMetadata(metadataPath, os.Getpid(), "test", "8080", true)
	if err != nil {
		t.Fatalf("write runtime metadata: %v", err)
	}

	got, running := CheckDaemonRunning(metadataPath)
	if !running {
		t.Fatal("CheckDaemonRunning did not detect the recorded running process")
	}
	if got == nil || got.PID != metadata.PID {
		t.Fatalf("CheckDaemonRunning returned metadata %+v, want PID %d", got, metadata.PID)
	}
}

func TestRuntimeMetadataNeverStoresTheToken(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "runtime.json")
	if _, err := WriteRuntimeMetadata(metadataPath, os.Getpid(), "test", "8080", false); err != nil {
		t.Fatalf("write runtime metadata: %v", err)
	}
	content, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(content)), "token") {
		t.Fatalf("runtime metadata must not contain a token field: %s", content)
	}
	got, _ := CheckDaemonRunning(metadataPath)
	if got == nil || got.Token != "" {
		t.Fatalf("metadata read back a token: %+v", got)
	}
}

func TestCheckDaemonRunningDoesNotAdoptForeignPortOwner(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on test port: %v", err)
	}
	defer listener.Close()

	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	metadataPath := filepath.Join(t.TempDir(), "runtime.json")
	stalePID := os.Getpid() + 100000
	for IsProcessRunning(stalePID) {
		stalePID++
	}
	metadata := &RuntimeMetadata{PID: stalePID, Version: "test", Port: port, Daemon: true}
	content, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal runtime metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, content, 0644); err != nil {
		t.Fatalf("write runtime metadata: %v", err)
	}

	if got, running := CheckDaemonRunning(metadataPath); running || got != nil {
		t.Fatalf("CheckDaemonRunning adopted foreign port owner: metadata=%+v running=%v", got, running)
	}
}
