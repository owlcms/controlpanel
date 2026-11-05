package firmata

import (
	"path/filepath"
	"testing"
)

func TestVersionFromWorkingDir(t *testing.T) {
	previous := installDir
	installDir = filepath.Join(t.TempDir(), "owlcms-firmata")
	t.Cleanup(func() { installDir = previous })

	if got := versionFromWorkingDir(filepath.Join(installDir, "2.8.1")); got != "2.8.1" {
		t.Errorf("version directory of this instance: got %q, want 2.8.1", got)
	}
	other := filepath.Join(filepath.Dir(installDir), "records-firmata", "2.8.1")
	if got := versionFromWorkingDir(other); got != "" {
		t.Errorf("another instance's directory must not match, got %q", got)
	}
	if got := versionFromWorkingDir("(unavailable)"); got != "" {
		t.Errorf("unavailable working directory must not match, got %q", got)
	}
}
