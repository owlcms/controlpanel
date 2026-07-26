package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildVideoLaunchEnvPrefersReleaseEnvProperties(t *testing.T) {
	controlPanelDir := t.TempDir()
	previousControlPanelDir := os.Getenv("CONTROLPANEL_INSTALLDIR")
	if err := os.Setenv("CONTROLPANEL_INSTALLDIR", controlPanelDir); err != nil {
		t.Fatalf("set CONTROLPANEL_INSTALLDIR: %v", err)
	}
	t.Cleanup(func() {
		if previousControlPanelDir == "" {
			_ = os.Unsetenv("CONTROLPANEL_INSTALLDIR")
			return
		}
		_ = os.Setenv("CONTROLPANEL_INSTALLDIR", previousControlPanelDir)
	})

	installDir := t.TempDir()
	versionDir := filepath.Join(installDir, "1.2.3")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(installDir, "env.properties"), []byte("VIDEO_CONFIGDIR=parent-config\nPARENT_ONLY=shared\n"), 0o644); err != nil {
		t.Fatalf("write parent env.properties: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "env.properties"), []byte("VIDEO_CONFIGDIR=release-config\nRELEASE_ONLY=local\n"), 0o644); err != nil {
		t.Fatalf("write release env.properties: %v", err)
	}

	env := BuildVideoLaunchEnv(versionDir)

	if got := lookupEnvValue(env, "VIDEO_CONFIGDIR"); got != "release-config" {
		t.Fatalf("expected release VIDEO_CONFIGDIR override, got %q", got)
	}
	if got := lookupEnvValue(env, "PARENT_ONLY"); got != "shared" {
		t.Fatalf("expected parent env property to be preserved, got %q", got)
	}
	if got := lookupEnvValue(env, "RELEASE_ONLY"); got != "local" {
		t.Fatalf("expected release-only env property to be present, got %q", got)
	}
	if got := lookupEnvValue(env, "VIDEO_CONTROLPANEL_DIR"); got != controlPanelDir {
		t.Fatalf("expected VIDEO_CONTROLPANEL_DIR=%q, got %q", controlPanelDir, got)
	}
}

func lookupEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if after, ok := strings.CutPrefix(entry, prefix); ok {
			return after
		}
	}
	return ""
}
