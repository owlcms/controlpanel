package owlcms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPortForReleaseUsesLocalPort(t *testing.T) {
	t.Setenv("OWLCMS_PORT", "19090")
	t.Setenv("CONTROLPANEL_RUN_AS_DAEMON", "true")
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := os.WriteFile(filepath.Join(installDir, "env.properties"), []byte("OWLCMS_PORT=8080\nTEMURIN_VERSION=jdk-25\nCONTROLPANEL_RUN_AS_DAEMON=false\n"), 0o644); err != nil {
		t.Fatalf("write shared env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "65.0.0"), 0o755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "65.0.0", "env.properties"), []byte("OWLCMS_PORT=18080\nTEMURIN_VERSION=jdk-25\nCONTROLPANEL_RUN_AS_DAEMON=true\nOWLCMS_INITIALDATA=TESTDATA\n"), 0o644); err != nil {
		t.Fatalf("write release env: %v", err)
	}

	if got := GetPortForRelease("65.0.0"); got != "19090" {
		t.Fatalf("expected environment port 19090, got %q", got)
	}
	if got := GetPortForRelease("missing"); got != "19090" {
		t.Fatalf("expected environment port 19090, got %q", got)
	}

	t.Setenv("CONTROLPANEL_RUN_AS_DAEMON", "true")
	if err := LoadEnvironmentForRelease("65.0.0"); err != nil {
		t.Fatalf("load release environment: %v", err)
	}
	if got := GetPort(); got != "19090" {
		t.Fatalf("expected loaded environment to use environment port 19090, got %q", got)
	}
	if !GetRunAsDaemon() {
		t.Fatal("expected loaded environment to use release daemon override")
	}
	if got, ok := GetEnvironment().Get("OWLCMS_INITIALDATA"); !ok || got != "TESTDATA" {
		t.Fatalf("expected release-only property to be preserved, got value=%q ok=%v", got, ok)
	}
}

func TestEnsureReleaseEnvDoesNotCreatePortWithoutMenuOverride(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := EnsureReleaseEnvFromParent("65.0.0"); err != nil {
		t.Fatalf("create release environment: %v", err)
	}

	releaseProps, err := loadReleaseProperties("65.0.0")
	if err != nil {
		t.Fatalf("load release environment: %v", err)
	}
	if _, ok := releaseProps.Get("OWLCMS_PORT"); ok {
		t.Fatalf("expected no release port without a menu override")
	}
}

func TestEnsureReleaseEnvCopiesPortAfterMenuOverride(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := SaveProperty("OWLCMS_PORT", "19090"); err != nil {
		t.Fatalf("save menu port: %v", err)
	}
	if err := EnsureReleaseEnvFromParent("65.0.0"); err != nil {
		t.Fatalf("create release environment: %v", err)
	}

	releaseProps, err := loadReleaseProperties("65.0.0")
	if err != nil {
		t.Fatalf("load release environment: %v", err)
	}
	if got, ok := releaseProps.Get("OWLCMS_PORT"); !ok || got != "19090" {
		t.Fatalf("expected menu port in release environment, got %q (present=%v)", got, ok)
	}
}

func TestGetTrackerConnectionPortForReleaseReadsStoredURL(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := os.WriteFile(filepath.Join(installDir, "env.properties"), []byte("OWLCMS_PORT=8080\nTEMURIN_VERSION=jdk-25\n"), 0o644); err != nil {
		t.Fatalf("write shared env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "65.0.0"), 0o755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "65.0.0", "env.properties"), []byte("OWLCMS_VIDEODATA=ws://127.0.0.1:18123/ws\n"), 0o644); err != nil {
		t.Fatalf("write release env: %v", err)
	}

	if got := GetTrackerConnectionPortForRelease("65.0.0"); got != "18123" {
		t.Fatalf("expected tracker port 18123, got %q", got)
	}
	if got := GetTrackerConnectionURLForRelease("65.0.0"); got != "ws://127.0.0.1:18123/ws" {
		t.Fatalf("expected tracker URL ws://127.0.0.1:18123/ws, got %q", got)
	}
	if !GetTrackerConnectionEnabledForRelease("65.0.0") {
		t.Fatal("expected tracker connection to be enabled for release")
	}
}

func TestGetTrackerConnectionPortForReleaseBlankReleaseOverrideClearsSharedDefault(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := os.WriteFile(filepath.Join(installDir, "env.properties"), []byte("OWLCMS_PORT=8080\nTEMURIN_VERSION=jdk-25\nOWLCMS_VIDEODATA=ws://127.0.0.1:18123/ws\n"), 0o644); err != nil {
		t.Fatalf("write shared env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "65.0.0"), 0o755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "65.0.0", "env.properties"), []byte("OWLCMS_VIDEODATA=\n"), 0o644); err != nil {
		t.Fatalf("write release env: %v", err)
	}

	if got := GetTrackerConnectionPortForRelease("65.0.0"); got != "" {
		t.Fatalf("expected tracker connection to be cleared, got %q", got)
	}
	if GetTrackerConnectionEnabledForRelease("65.0.0") {
		t.Fatal("expected tracker connection to be disabled for release")
	}
}

func TestDisableTrackerConnectionForReleaseWritesBlankOverride(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := os.WriteFile(filepath.Join(installDir, "env.properties"), []byte("OWLCMS_PORT=8080\nTEMURIN_VERSION=jdk-25\nOWLCMS_VIDEODATA=ws://127.0.0.1:18123/ws\n"), 0o644); err != nil {
		t.Fatalf("write shared env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "65.0.0"), 0o755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}

	if err := DisableTrackerConnectionForRelease("65.0.0"); err != nil {
		t.Fatalf("disable tracker connection: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(installDir, "65.0.0", "env.properties"))
	if err != nil {
		t.Fatalf("read release env: %v", err)
	}
	if string(content) == "" || !strings.Contains(string(content), "OWLCMS_VIDEODATA = ") {
		t.Fatalf("expected blank tracker override to be written, got %q", string(content))
	}
	if got := GetTrackerConnectionPortForRelease("65.0.0"); got != "" {
		t.Fatalf("expected tracker connection to be disabled after blank override, got %q", got)
	}
}

func TestTrackerConnectionPortAcceptsExternalTrackerURL(t *testing.T) {
	if port, ok := trackerConnectionPort("ws://example.com:18123/ws"); !ok || port != "18123" {
		t.Fatalf("expected external URL to be accepted, got port=%q ok=%v", port, ok)
	}
	if port, ok := trackerConnectionPort("wss://tracker.example.com:443/ws"); !ok || port != "443" {
		t.Fatalf("expected secure external URL to be accepted, got port=%q ok=%v", port, ok)
	}
}

func TestDefaultTrackerConnectionRetainsEndpointWhenDisabled(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := SaveDefaultTrackerConnection("wss://tracker.example.com/ws", "443", false); err != nil {
		t.Fatalf("save disabled default tracker connection: %v", err)
	}

	url, port, enabled := GetTrackerConnectionSettings()
	if url != "wss://tracker.example.com/ws" || port != "443" || enabled {
		t.Fatalf("unexpected default tracker settings: url=%q port=%q enabled=%v", url, port, enabled)
	}
	if GetTrackerConnectionEnabled() {
		t.Fatal("expected default tracker connection to remain disabled")
	}

	content, err := os.ReadFile(filepath.Join(installDir, "env.properties"))
	if err != nil {
		t.Fatalf("read shared env: %v", err)
	}
	if !strings.Contains(string(content), "OWLCMS_VIDEODATA = ") {
		t.Fatalf("expected disabled default to clear OWLCMS_VIDEODATA, got %q", string(content))
	}
}
