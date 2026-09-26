package owlcms

import (
	"bytes"
	"controlpanel/shared"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magiconair/properties"
)

func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestSecretRoundTrip(t *testing.T) {
	home := useTempHome(t)

	encrypted, err := shared.EncryptSecret("s3cret key")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(encrypted, "enc:v1:") || strings.Contains(encrypted, "s3cret") {
		t.Fatalf("unexpected encrypted value %q", encrypted)
	}
	info, err := os.Stat(filepath.Join(home, ".owlcms", "key"))
	if err != nil {
		t.Fatalf("installation key not created: %v", err)
	}
	if os.PathSeparator == '/' && info.Mode().Perm() != 0o600 {
		t.Fatalf("installation key permissions = %v, want 0600", info.Mode().Perm())
	}
	folder, err := os.Stat(filepath.Join(home, ".owlcms"))
	if err != nil || !folder.IsDir() {
		t.Fatalf("installation key folder not created: %v", err)
	}
	if os.PathSeparator == '/' && folder.Mode().Perm() != 0o700 {
		t.Fatalf("installation key folder permissions = %v, want 0700", folder.Mode().Perm())
	}

	plain, err := shared.DecryptSecret(encrypted)
	if err != nil || plain != "s3cret key" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
}

func TestSecretUsesLegacyInstallationKeyFile(t *testing.T) {
	home := useTempHome(t)
	legacyPath := filepath.Join(home, ".owlcms")
	legacyKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)) + "\n"
	if err := os.WriteFile(legacyPath, []byte(legacyKey), 0o600); err != nil {
		t.Fatalf("write legacy key: %v", err)
	}

	encrypted, err := shared.EncryptSecret("s3cret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	info, err := os.Stat(legacyPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("legacy key file must be kept as a file: %v", err)
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil || string(data) != legacyKey {
		t.Fatalf("legacy key file must be unchanged, got %q, %v", data, err)
	}
	plain, err := shared.DecryptSecret(encrypted)
	if err != nil || plain != "s3cret" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
}

func TestSecretPlainValuePassesThrough(t *testing.T) {
	useTempHome(t)
	plain, err := shared.DecryptSecret("typed-by-hand")
	if err != nil || plain != "typed-by-hand" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
}

func TestEmptySecretIsSavedAsNoValue(t *testing.T) {
	home := useTempHome(t)
	for _, blank := range []string{"", "   "} {
		encrypted, err := shared.EncryptSecret(blank)
		if err != nil || encrypted != "" {
			t.Fatalf("EncryptSecret(%q) = %q, %v; want empty value", blank, encrypted, err)
		}
		if shared.IsSecretSet(encrypted) {
			t.Fatalf("IsSecretSet(%q) = true for a blank key", encrypted)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".owlcms")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a blank key must not create the installation key, stat err = %v", err)
	}

	props := properties.NewProperties()
	props.Set(trackerConnectionKeyEnv, "")
	env, warning := applyTrackerConnectionKeyToEnv(nil, props, "67.0.0")
	if warning != "" || len(env) != 0 {
		t.Fatalf("blank key must not be passed, got env %v warning %q", env, warning)
	}
}

func TestSecretFromOtherInstallationRequiresReentry(t *testing.T) {
	useTempHome(t)
	encrypted, err := shared.EncryptSecret("s3cret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	useTempHome(t)
	if _, err := shared.DecryptSecret(encrypted); !errors.Is(err, shared.ErrSecretReentryRequired) {
		t.Fatalf("missing installation key: got %v, want ErrSecretReentryRequired", err)
	}

	if _, err := shared.EncryptSecret("other"); err != nil {
		t.Fatalf("encrypt with new installation key: %v", err)
	}
	if _, err := shared.DecryptSecret(encrypted); !errors.Is(err, shared.ErrSecretReentryRequired) {
		t.Fatalf("different installation key: got %v, want ErrSecretReentryRequired", err)
	}
}

func TestTrackerConnectionKeyValidation(t *testing.T) {
	if err := validateTrackerConnectionKey("ws://localhost/ws", ""); err != nil {
		t.Fatalf("empty key must be valid for ws://, got %v", err)
	}
	if err := validateTrackerConnectionKey("wss://my-comp.fly.dev/ws", "  "); err == nil {
		t.Fatal("empty key must be rejected for wss://")
	}
	if err := validateTrackerConnectionKey("wss://my-comp.fly.dev/ws", "s3cret"); err != nil {
		t.Fatalf("non-empty key must be valid for wss://, got %v", err)
	}
}

func TestTrackerConnectionKeyAppliedDecrypted(t *testing.T) {
	useTempHome(t)
	encrypted, err := shared.EncryptSecret("s3cret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	props := properties.NewProperties()
	props.Set(trackerConnectionKeyEnv, encrypted)

	env := applyOwlcmsPropertiesToEnv(nil, props)
	if len(env) != 0 {
		t.Fatalf("encrypted key must not be copied as-is, got %v", env)
	}
	env, warning := applyTrackerConnectionKeyToEnv(env, props, "67.0.0")
	if warning != "" {
		t.Fatalf("unexpected warning %q", warning)
	}
	if len(env) != 1 || env[0] != trackerConnectionKeyEnv+"=s3cret" {
		t.Fatalf("expected decrypted key in env, got %v", env)
	}

	useTempHome(t)
	env, warning = applyTrackerConnectionKeyToEnv(nil, props, "67.0.0")
	if warning == "" {
		t.Fatal("expected re-entry warning for key from another installation")
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, trackerConnectionKeyEnv+"=") {
			t.Fatalf("undecryptable key must not be passed, got %q", entry)
		}
	}
}
