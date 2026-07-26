package owlcms

import (
	"path/filepath"
	"testing"
)

func TestIsDirectoryEntry(t *testing.T) {
	if !isDirectoryEntry("templates" + string(filepath.Separator)) {
		t.Fatal("expected trailing-separator path to be a directory entry")
	}
	if isDirectoryEntry(filepath.Join("templates", "start.html")) {
		t.Fatal("expected file path not to be a directory entry")
	}
}
