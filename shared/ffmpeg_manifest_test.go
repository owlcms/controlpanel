package shared

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFFmpegManifestPlatforms(t *testing.T) {
	for _, platform := range []struct {
		goos, goarch, format string
		count                int
	}{
		{"windows", "amd64", "zip", 1},
		{"windows", "arm64", "zip", 1},
		{"linux", "amd64", "tar.xz", 1},
		{"linux", "arm64", "tar.xz", 1},
		{"darwin", "arm64", "zip", 3},
	} {
		t.Run(platform.goos+"-"+platform.goarch, func(t *testing.T) {
			runtime, err := loadFFmpegRuntime(ffmpegManifestJSON, platform.goos, platform.goarch)
			if err != nil {
				t.Fatal(err)
			}
			if len(runtime.Archives) != platform.count || runtime.Archives[0].Format != platform.format {
				t.Fatalf("unexpected runtime: %+v", runtime)
			}
		})
	}
	for _, platform := range []struct{ goos, goarch string }{{"darwin", "amd64"}, {"linux", "386"}} {
		if _, err := loadFFmpegRuntime(ffmpegManifestJSON, platform.goos, platform.goarch); !errors.Is(err, errFFmpegSystemOnly) {
			t.Fatalf("expected system-only for %+v, got %v", platform, err)
		}
	}
}

func TestFFmpegManifestRejectsInvalidEntries(t *testing.T) {
	for _, replacement := range []struct{ old, new string }{
		{"\"destination\": \"bin\"", "\"destination\": \"../outside\""},
		{"\"format\": \"zip\"", "\"format\": \"rar\""},
		{"64f1ad87071be6883ba8ba3fe01412baa9ae1891a50f28449e14a1280132deda", "bad"},
	} {
		data := strings.ReplaceAll(string(ffmpegManifestJSON), replacement.old, replacement.new)
		if _, err := loadFFmpegRuntime([]byte(data), "darwin", "arm64"); err == nil {
			t.Fatalf("accepted invalid replacement %s", replacement.new)
		}
	}
}

func TestFFmpegInstallLayouts(t *testing.T) {
	for _, layout := range []string{"flat-zips", "nested-zip", "nested-tar.xz"} {
		t.Run(layout, func(t *testing.T) {
			fixture := t.TempDir()
			runtime := ffmpegRuntime{Version: "test", Directory: "test-runtime", ArchiveRoot: ".", Executables: []string{"ffmpeg", "ffprobe", "ffplay"}}
			archives := make(map[string]string)
			if layout == "flat-zips" {
				for _, name := range runtime.Executables {
					filename := filepath.Join(fixture, name+".zip")
					writeFFmpegTestZip(t, filename, []string{name})
					runtime.Archives = append(runtime.Archives, ffmpegArchive{URL: name, Format: "zip", Destination: "bin", SHA256: ffmpegTestHash(t, filename)})
					archives[name] = filename
				}
			} else {
				runtime.ArchiveRoot = "provider-root"
				format := "zip"
				filename := filepath.Join(fixture, "runtime.zip")
				if layout == "nested-zip" {
					writeFFmpegTestZip(t, filename, []string{"provider-root/bin/ffmpeg", "provider-root/bin/ffprobe", "provider-root/bin/ffplay", "provider-root/bin/runtime.dll"})
				} else {
					if _, err := exec.LookPath("tar"); err != nil {
						t.Skip("tar is unavailable")
					}
					format = "tar.xz"
					filename = filepath.Join(fixture, "runtime.tar.xz")
					for _, name := range []string{"bin/ffmpeg", "bin/ffprobe", "bin/ffplay", "lib/libavcodec.so"} {
						full := filepath.Join(fixture, "provider-root", name)
						if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(full, []byte(name), 0644); err != nil {
							t.Fatal(err)
						}
					}
					if output, err := exec.Command("tar", "-cJf", filename, "-C", fixture, "provider-root").CombinedOutput(); err != nil {
						t.Fatalf("tar fixture: %v: %s", err, output)
					}
					runtime.RequiredDirectories = []string{"lib"}
				}
				runtime.Archives = []ffmpegArchive{{URL: "runtime", Format: format, Destination: ".", SHA256: ffmpegTestHash(t, filename)}}
				archives["runtime"] = filename
			}
			root := filepath.Join(t.TempDir(), "ffmpeg")
			download := func(url, destination string, progress ProgressCallback, cancel <-chan bool) error {
				data, err := os.ReadFile(archives[url])
				if err != nil {
					return err
				}
				return os.WriteFile(destination, data, 0644)
			}
			result, err := installFFmpegRuntime(runtime, root, nil, nil, download)
			if err != nil {
				t.Fatal(err)
			}
			if result != filepath.Join(root, "test-runtime", "bin", "ffmpeg") {
				t.Fatalf("unexpected executable: %s", result)
			}
			if layout == "nested-zip" {
				if _, err := os.Stat(filepath.Join(root, "test-runtime", "bin", "runtime.dll")); err != nil {
					t.Fatal(err)
				}
			}
			if layout == "nested-tar.xz" {
				if _, err := os.Stat(filepath.Join(root, "test-runtime", "lib", "libavcodec.so")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFFmpegInstallRejectsIncompleteOrCorruptArchives(t *testing.T) {
	for _, scenario := range []string{"checksum", "missing-tool", "download", "traversal", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "fixture.zip")
			entries := []string{"ffmpeg"}
			if scenario == "cancel" {
				entries = append(entries, "ffprobe")
			}
			if scenario == "traversal" {
				entries = append(entries, "../escaped")
			}
			writeFFmpegTestZip(t, filename, entries)
			digest := ffmpegTestHash(t, filename)
			if scenario == "checksum" {
				digest = strings.Repeat("0", 64)
			}
			runtime := ffmpegRuntime{Directory: "test-runtime", ArchiveRoot: ".", Executables: []string{"ffmpeg", "ffprobe"}, Archives: []ffmpegArchive{{Format: "zip", Destination: "bin", SHA256: digest}}}
			root := filepath.Join(t.TempDir(), "ffmpeg")
			cancellation := make(chan bool, 1)
			_, err := installFFmpegRuntime(runtime, root, nil, cancellation, func(url, destination string, progress ProgressCallback, cancel <-chan bool) error {
				if scenario == "cancel" {
					cancellation <- true
				}
				if scenario == "download" {
					return fmt.Errorf("simulated failure")
				}
				data, err := os.ReadFile(filename)
				if err != nil {
					return err
				}
				return os.WriteFile(destination, data, 0644)
			})
			if err == nil {
				t.Fatal("expected installation failure")
			}
			if _, err := os.Stat(filepath.Join(root, runtime.Directory)); !os.IsNotExist(err) {
				t.Fatalf("partial runtime published: %v", err)
			}
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(root), ".ffmpeg-stage-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("staging not cleaned: %v %v", stages, err)
			}
		})
	}
}

func writeFFmpegTestZip(t *testing.T, filename string, names []string) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func ffmpegTestHash(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestFFmpegIdentityMigration(t *testing.T) {
	for _, scenario := range []string{"missing", "invalid", "revision", "matching", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "ffmpeg")
			fixture := filepath.Join(t.TempDir(), "runtime.zip")
			writeFFmpegTestZip(t, fixture, []string{"ffmpeg", "ffprobe", "ffplay"})
			runtime := ffmpegRuntime{Version: "test", Revision: "1", Directory: "current", ArchiveRoot: ".", Executables: []string{"ffmpeg", "ffprobe", "ffplay"}, Archives: []ffmpegArchive{{Format: "zip", Destination: "bin", SHA256: ffmpegTestHash(t, fixture)}}}
			downloads := 0
			download := func(url, destination string, progress ProgressCallback, cancel <-chan bool) error {
				downloads++
				data, err := os.ReadFile(fixture)
				if err != nil {
					return err
				}
				return os.WriteFile(destination, data, 0644)
			}
			if _, err := installFFmpegRuntime(runtime, root, nil, nil, download); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, runtime.Directory)
			identity := filepath.Join(target, ffmpegIdentityFile)
			switch scenario {
			case "missing", "failure":
				if err := os.Remove(identity); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.WriteFile(identity, []byte("invalid json"), 0644); err != nil {
					t.Fatal(err)
				}
			case "revision":
				runtime.Revision = "2"
			}
			legacy := filepath.Join(root, "legacy", "bin", "ffmpeg")
			if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacy, []byte("legacy"), 0755); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(root, "notes", "keep.txt")
			if err := os.MkdirAll(filepath.Dir(unrelated), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(unrelated, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			if scenario == "failure" {
				download = func(string, string, ProgressCallback, <-chan bool) error { return fmt.Errorf("offline") }
			}
			_, err := installFFmpegRuntime(runtime, root, nil, nil, download)
			if scenario == "failure" {
				if err == nil {
					t.Fatal("expected failure")
				}
				if _, err := os.Stat(legacy); err != nil {
					t.Fatal("legacy removed after failure", err)
				}
				if _, err := os.Stat(filepath.Join(target, "bin", "ffmpeg")); err != nil {
					t.Fatal("previous runtime removed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := installedFFmpegRuntime(runtime, target); err != nil {
				t.Fatal(err)
			}
			wantDownloads := 2
			if scenario == "matching" {
				wantDownloads = 1
			}
			if downloads != wantDownloads {
				t.Fatalf("downloads=%d, want %d", downloads, wantDownloads)
			}
			if _, err := os.Stat(legacy); !os.IsNotExist(err) {
				t.Fatal("legacy not cleaned", err)
			}
			if _, err := os.Stat(unrelated); err != nil {
				t.Fatal("unrelated file removed", err)
			}
			retired, err := filepath.Glob(filepath.Join(root, ".ffmpeg-retired-*"))
			if err != nil || len(retired) != 0 {
				t.Fatalf("retired runtimes remain: %v %v", retired, err)
			}
		})
	}
}

func TestFFmpegManifestLibraryPaths(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		runtime, err := loadFFmpegRuntime(ffmpegManifestJSON, "linux", arch)
		if err != nil {
			t.Fatal(err)
		}
		if len(runtime.LibraryPathDirectories) != 1 || runtime.LibraryPathDirectories[0] != "lib" {
			t.Fatal("missing Linux library path")
		}
	}
	data := strings.ReplaceAll(string(ffmpegManifestJSON), `"libraryPathDirectories": ["lib"]`, `"libraryPathDirectories": ["../outside"]`)
	if _, err := loadFFmpegRuntime([]byte(data), "linux", "amd64"); err == nil {
		t.Fatal("accepted unsafe library path")
	}
	data = strings.ReplaceAll(string(ffmpegManifestJSON), `"revision": "1"`, `"revision": ""`)
	if _, err := loadFFmpegRuntime([]byte(data), "linux", "amd64"); err == nil {
		t.Fatal("accepted missing revision")
	}
}
