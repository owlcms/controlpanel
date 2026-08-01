package downloadutils

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractZipFlattensSingleMeaningfulRoot(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"owlcms_4.68.0/":        "",
		"owlcms_4.68.0/app.txt": "app",
		".DS_Store":       "metadata",
		"__MACOSX/._app":  "metadata",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest, nil); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "app.txt")); err != nil {
		t.Fatalf("flattened file was not extracted at root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "owlcms_4.68.0", "app.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive root was not flattened")
	}
}

func TestExtractZipPreservesMultipleMeaningfulRoots(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"owlcms_4.68.0/app.txt": "app",
		"config.ini":      "config",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest, nil); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "owlcms_4.68.0", "app.txt")); err != nil {
		t.Fatalf("meaningful archive root was unexpectedly flattened: %v", err)
	}
}

func TestExtractZipPreservesTrackerRootLayout(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"build/":                  "",
		"build/server.js":         "server",
		"extensions/":             "",
		"extensions/example.js":   "extension",
		"local/":                  "",
		"local/config.json":       "config",
		"logs/":                   "",
		"logs/tracker.log":        "log",
		"node_modules/":           "",
		"node_modules/package.json": "dependency",
		"package.json":            "package",
		"README.txt":              "readme",
		"src/":                    "",
		"src/index.js":            "source",
		"start-with-ws.js":        "startup",
		".DS_Store":               "metadata",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest, nil); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	for _, name := range []string{"build/server.js", "local/config.json", "package.json", "src/index.js", "start-with-ws.js"} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(name))); err != nil {
			t.Fatalf("tracker root entry %q was not preserved: %v", name, err)
		}
	}
}

func createTestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, contents := range files {
		var entry io.Writer
		if strings.HasSuffix(name, "/") {
			header := &zip.FileHeader{Name: name}
			header.SetMode(os.ModeDir | 0755)
			entry, err = writer.CreateHeader(header)
		} else {
			entry, err = writer.Create(name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
