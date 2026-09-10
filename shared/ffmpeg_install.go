package shared

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ffmpegDownloader func(string, string, ProgressCallback, <-chan bool) error

const ffmpegIdentityFile = ".ffmpeg-runtime.json"

var ffmpegInstallMu sync.Mutex

type ffmpegInstallation struct {
	Schema   int           `json:"schema"`
	Manifest ffmpegRuntime `json:"manifest"`
}

func installedFFmpegRuntime(runtime ffmpegRuntime, root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ffmpegIdentityFile))
	if err != nil {
		return "", fmt.Errorf("FFmpeg identity missing: %w", err)
	}
	var installed ffmpegInstallation
	if err := json.Unmarshal(data, &installed); err != nil {
		return "", fmt.Errorf("invalid FFmpeg identity: %w", err)
	}
	want, err := json.Marshal(runtime)
	if err != nil {
		return "", err
	}
	got, err := json.Marshal(installed.Manifest)
	if err != nil {
		return "", err
	}
	if installed.Schema != 1 || string(want) != string(got) {
		return "", fmt.Errorf("FFmpeg identity does not match manifest")
	}
	return validateFFmpegRuntime(runtime, root)
}

func installFFmpegRuntime(runtime ffmpegRuntime, root string, progress ProgressCallback, cancel <-chan bool, download ffmpegDownloader) (string, error) {
	ffmpegInstallMu.Lock()
	defer ffmpegInstallMu.Unlock()
	if err := EnsureDir0755(root); err != nil {
		return "", err
	}
	target := filepath.Join(root, runtime.Directory)
	if existing, err := installedFFmpegRuntime(runtime, target); err == nil {
		cleanupFFmpegRuntimes(root, target)
		return existing, nil
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".ffmpeg-stage-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload")
	for index, archive := range runtime.Archives {
		select {
		case <-cancel:
			return "", fmt.Errorf("FFmpeg installation cancelled")
		default:
		}
		archivePath := filepath.Join(stage, fmt.Sprintf("archive-%d.%s", index, archive.Format))
		log.Printf("Downloading FFmpeg %s: %s", runtime.Version, archive.URL)
		if err := download(archive.URL, archivePath, func(done, total int64) {
			if progress != nil && total > 0 {
				progress(int64(index)*1000+done*1000/total, int64(len(runtime.Archives))*1000)
			}
		}, cancel); err != nil {
			return "", err
		}
		if err := verifyFFmpegArchive(archivePath, archive.SHA256); err != nil {
			return "", err
		}
		destination := filepath.Join(payload, archive.Destination)
		if err := EnsureDir0755(destination); err != nil {
			return "", err
		}
		switch archive.Format {
		case "zip":
			if err := validateFFmpegZip(archivePath); err != nil {
				return "", err
			}
			err = ExtractZip(archivePath, destination, false)
		case "tar.xz":
			err = ExtractTarXz(archivePath, destination)
		default:
			return "", fmt.Errorf("unsupported FFmpeg archive format %q", archive.Format)
		}
		if err != nil {
			return "", err
		}
	}
	source := filepath.Join(payload, runtime.ArchiveRoot)
	if _, err := validateFFmpegRuntime(runtime, source); err != nil {
		return "", err
	}
	for _, name := range runtime.Executables {
		if err := os.Chmod(filepath.Join(source, "bin", name), 0755); err != nil {
			return "", err
		}
	}
	identity, err := json.MarshalIndent(ffmpegInstallation{Schema: 1, Manifest: runtime}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(source, ffmpegIdentityFile), identity, 0644); err != nil {
		return "", err
	}
	select {
	case <-cancel:
		return "", fmt.Errorf("FFmpeg installation cancelled")
	default:
	}
	retired := ""
	if _, err := os.Lstat(target); err == nil {
		retired, err = os.MkdirTemp(root, ".ffmpeg-retired-")
		if err != nil {
			return "", err
		}
		if err := os.Rename(target, filepath.Join(retired, "runtime")); err != nil {
			_ = os.Remove(retired)
			return "", fmt.Errorf("retiring stale FFmpeg: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(source, target); err != nil {
		if retired != "" {
			if restoreErr := os.Rename(filepath.Join(retired, "runtime"), target); restoreErr != nil {
				return "", fmt.Errorf("publishing FFmpeg: %v; restoring previous runtime from %s: %w", err, retired, restoreErr)
			}
			_ = os.Remove(retired)
		}
		return "", fmt.Errorf("publishing FFmpeg runtime: %w", err)
	}
	log.Printf("FFmpeg %s installed at %s", runtime.Version, target)
	result, err := installedFFmpegRuntime(runtime, target)
	if err != nil {
		return "", err
	}
	cleanupFFmpegRuntimes(root, target)
	return result, nil
}

func verifyFFmpegArchive(filename, expected string) error {
	if expected == "" {
		return nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	if !strings.EqualFold(fmt.Sprintf("%x", digest.Sum(nil)), expected) {
		return fmt.Errorf("FFmpeg archive SHA-256 mismatch: %s", filename)
	}
	return nil
}

func validateFFmpegZip(filename string) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if !validFFmpegRelativePath(entry.Name) || entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe FFmpeg archive entry %q", entry.Name)
		}
	}
	return nil
}

func validateFFmpegRuntime(runtime ffmpegRuntime, root string) (string, error) {
	for _, name := range runtime.Executables {
		info, err := os.Stat(filepath.Join(root, "bin", name))
		if err != nil {
			return "", fmt.Errorf("incomplete FFmpeg runtime: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("FFmpeg executable is not a regular file: %s", name)
		}
	}
	for _, directory := range append(append([]string{}, runtime.RequiredDirectories...), runtime.LibraryPathDirectories...) {
		info, err := os.Stat(filepath.Join(root, directory))
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("missing FFmpeg directory: %s", directory)
		}
	}
	return filepath.Join(root, "bin", runtime.Executables[0]), nil
}
