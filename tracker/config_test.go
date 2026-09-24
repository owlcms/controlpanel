package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPortForReleaseUsesReleaseOverride(t *testing.T) {
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := os.WriteFile(filepath.Join(installDir, "env.properties"), []byte("TRACKER_PORT=8096\n"), 0o644); err != nil {
		t.Fatalf("write shared env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "2.3.0"), 0o755); err != nil {
		t.Fatalf("mkdir release dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "2.3.0", "env.properties"), []byte("TRACKER_PORT=18123\n"), 0o644); err != nil {
		t.Fatalf("write release env: %v", err)
	}

	if got := GetPortForRelease("2.3.0"); got != "18123" {
		t.Fatalf("expected release port 18123, got %q", got)
	}
	if got := GetPortForRelease("missing"); got != "8096" {
		t.Fatalf("expected fallback shared port 8096, got %q", got)
	}
}

func TestSharedKeySavedEncryptedAndInjected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(sharedKeyEnv, "")
	os.Unsetenv(sharedKeyEnv)
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := SaveSharedKeyForRelease("2.3.0", "s3cret"); err != nil {
		t.Fatalf("save key: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(installDir, "2.3.0", "env.properties"))
	if err != nil {
		t.Fatalf("read release env: %v", err)
	}
	if strings.Contains(string(content), "s3cret") || !strings.Contains(string(content), sharedKeyEnv+" = enc:v1:") {
		t.Fatalf("key must be stored encrypted, got:\n%s", content)
	}
	if got, err := GetSharedKeyForRelease("2.3.0"); err != nil || got != "s3cret" {
		t.Fatalf("GetSharedKeyForRelease = %q, %v", got, err)
	}

	merged, err := loadEnvironmentForReleaseProps("2.3.0")
	if err != nil {
		t.Fatalf("load env: %v", err)
	}
	env, warning := applySharedKeyToEnv(nil, merged, "2.3.0")
	if warning != "" || len(env) != 1 || env[0] != sharedKeyEnv+"=s3cret" {
		t.Fatalf("expected decrypted key in env, got %v warning %q", env, warning)
	}

	if err := SaveSharedKeyForRelease("2.3.0", "  "); err != nil {
		t.Fatalf("clear key: %v", err)
	}
	merged, err = loadEnvironmentForReleaseProps("2.3.0")
	if err != nil {
		t.Fatalf("load env: %v", err)
	}
	if value, ok := merged.Get(sharedKeyEnv); !ok || value != "" {
		t.Fatalf("cleared key must be saved as an empty value, got %q (present=%v)", value, ok)
	}
	if env, warning := applySharedKeyToEnv(nil, merged, "2.3.0"); len(env) != 0 || warning != "" {
		t.Fatalf("empty key must not be passed, got %v warning %q", env, warning)
	}

	if err := SaveDefaultSharedKey("fallback"); err != nil {
		t.Fatalf("save default key: %v", err)
	}
	if _, hasOwn, err := GetOwnSharedKeyForRelease("2.3.0"); err != nil || !hasOwn {
		t.Fatalf("empty key line must count as the version's own value, hasOwn=%v err=%v", hasOwn, err)
	}
	if got, _ := GetSharedKeyForRelease("2.3.0"); got != "" {
		t.Fatalf("empty own key must override the default, got %q", got)
	}
	if err := UseDefaultSharedKeyForRelease("2.3.0"); err != nil {
		t.Fatalf("use default: %v", err)
	}
	if _, hasOwn, err := GetOwnSharedKeyForRelease("2.3.0"); err != nil || hasOwn {
		t.Fatalf("key line must be removed, hasOwn=%v err=%v", hasOwn, err)
	}
	if got, _ := GetSharedKeyForRelease("2.3.0"); got != "fallback" {
		t.Fatalf("version without a key line must use the default, got %q", got)
	}
}

func TestNewVersionCopiesDefaultKeyOnlyWhenEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installDir := t.TempDir()
	previousDir := GetInstallDir()
	SetInstallDir(installDir)
	t.Cleanup(func() {
		SetInstallDir(previousDir)
	})

	if err := SaveDefaultSharedKey("fallback"); err != nil {
		t.Fatalf("save default key: %v", err)
	}
	if !GetDefaultSharedKeyEnabled() {
		t.Fatalf("unset setting must keep copying the default key")
	}
	if err := EnsureReleaseEnvFromParent("2.3.0"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, hasOwn, _ := GetOwnSharedKeyForRelease("2.3.0"); hasOwn {
		t.Fatalf("enabled: new version must have no key line")
	}
	if got, _ := GetSharedKeyForRelease("2.3.0"); got != "fallback" {
		t.Fatalf("enabled: new version key = %q, want fallback", got)
	}

	if err := SaveDefaultSharedKeyEnabled(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := EnsureReleaseEnvFromParent("2.4.0"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, hasOwn, _ := GetOwnSharedKeyForRelease("2.4.0"); !hasOwn {
		t.Fatalf("disabled: new version must get an explicit empty key line")
	}
	if got, _ := GetSharedKeyForRelease("2.4.0"); got != "" {
		t.Fatalf("disabled: new version key = %q, want empty", got)
	}
	if got, _ := GetDefaultSharedKey(); got != "fallback" {
		t.Fatalf("default key must be kept when disabled, got %q", got)
	}
}
