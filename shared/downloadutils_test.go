package shared

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
		".DS_Store":             "metadata",
		"__MACOSX/._app":        "metadata",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "app.txt")); err != nil {
		t.Fatalf("flattened file was not extracted at root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "owlcms_4.68.0", "app.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive root was not flattened")
	}
}

func TestExtractZipCanPreserveSingleMeaningfulRoot(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"jdk-25.0.2+10-jre/":              "",
		"jdk-25.0.2+10-jre/bin/javaw.exe": "java",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest, false); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "jdk-25.0.2+10-jre", "bin", "javaw.exe")); err != nil {
		t.Fatalf("archive root was not preserved: %v", err)
	}
}

func TestExtractZipPreservesMultipleMeaningfulRoots(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"owlcms_4.68.0/app.txt": "app",
		"config.ini":            "config",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "owlcms_4.68.0", "app.txt")); err != nil {
		t.Fatalf("meaningful archive root was unexpectedly flattened: %v", err)
	}
}

func TestExtractZipPreservesOWLCMSRootLayout(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"database/":           "",
		"database/data.mv.db": "database",
		"env.properties":      "environment",
		"local/":              "",
		"local/config.json":   "config",
		"logs/":               "",
		"logs/owlcms.log":     "log",
		"mqttData/":           "",
		"mqttData/state.json": "state",
		"owlcms.jar":          "application",
		".DS_Store":           "metadata",
	})
	dest := t.TempDir()

	if err := ExtractZip(zipPath, dest); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	for _, name := range []string{
		"database/data.mv.db",
		"env.properties",
		"local/config.json",
		"logs/owlcms.log",
		"mqttData/state.json",
		"owlcms.jar",
	} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(name))); err != nil {
			t.Fatalf("OWLCMS root entry %q was not preserved: %v", name, err)
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
