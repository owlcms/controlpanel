package shared

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	// Windows FFmpeg from BtbN (shared build, same repo as Linux)
	ffmpegWindowsBuild = "ffmpeg-master-latest-win64-gpl-shared"
	ffmpegWindowsURL   = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/" + ffmpegWindowsBuild + ".zip"

	// Linux FFmpeg from BtbN (shared builds with libraries)
	ffmpegLinuxAmd64URL = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl-shared.tar.xz"
	ffmpegLinuxArm64URL = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linuxarm64-gpl-shared.tar.xz"
)

// GetSharedFFmpegDir returns the shared FFmpeg installation directory
// under the control panel root, at the same level as java/ and node/.
func GetSharedFFmpegDir() string {
	return filepath.Join(GetRuntimeDir(), "ffmpeg")
}

// FindLocalFFmpeg searches for an FFmpeg executable in the shared control panel
// directory.  Returns the full path or empty string if not found.
func FindLocalFFmpeg() string {
	ffmpegDir := GetSharedFFmpegDir()
	if _, err := os.Stat(ffmpegDir); err != nil {
		return ""
	}

	var exeName string
	if GetGoos() == "windows" {
		exeName = "ffmpeg.exe"
	} else {
		exeName = "ffmpeg"
	}

	// Archives extract into a named subdirectory (e.g. ffmpeg-7.1-full_build/).
	// Scan for <subdir>/bin/<exeName>.
	entries, err := os.ReadDir(ffmpegDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(ffmpegDir, entry.Name(), "bin", exeName)
		if _, err := os.Stat(candidate); err == nil {
			// For Linux shared builds verify lib/ exists next to bin/
			if GetGoos() == "linux" {
				libDir := filepath.Join(ffmpegDir, entry.Name(), "lib")
				if st, err := os.Stat(libDir); err != nil || !st.IsDir() {
					continue
				}
			}
			log.Printf("Found shared FFmpeg at: %s", candidate)
			return candidate
		}
	}

	// Also check bin/ directly under ffmpegDir (flat layout)
	directCandidate := filepath.Join(ffmpegDir, "bin", exeName)
	if _, err := os.Stat(directCandidate); err == nil {
		return directCandidate
	}

	return ""
}

// getFFmpegDownloadURL returns the download URL for the current platform.
func getFFmpegDownloadURL() (string, error) {
	goos := GetGoos()
	goarch := GetGoarch()

	switch goos {
	case "windows":
		return ffmpegWindowsURL, nil
	case "linux":
		switch goarch {
		case "amd64":
			return ffmpegLinuxAmd64URL, nil
		case "arm64":
			return ffmpegLinuxArm64URL, nil
		default:
			return "", fmt.Errorf("unsupported Linux architecture for FFmpeg: %s", goarch)
		}
	default:
		return "", fmt.Errorf("unsupported OS for bundled FFmpeg: %s", goos)
	}
}

// DownloadAndInstallFFmpeg downloads and installs FFmpeg to the shared
// directory.  Returns the path to the installed ffmpeg executable.
func DownloadAndInstallFFmpeg(progressCallback func(downloaded, total int64), cancel <-chan bool) (string, error) {
	downloadURL, err := getFFmpegDownloadURL()
	if err != nil {
		return "", err
	}

	ffmpegDir := GetSharedFFmpegDir()
	if err := EnsureDir0755(ffmpegDir); err != nil {
		return "", fmt.Errorf("creating ffmpeg directory: %w", err)
	}

	goos := GetGoos()
	var archivePath string
	if goos == "windows" {
		archivePath = filepath.Join(ffmpegDir, "ffmpeg.zip")
	} else {
		archivePath = filepath.Join(ffmpegDir, "ffmpeg.tar.xz")
	}

	log.Printf("Downloading FFmpeg from: %s", downloadURL)
	if err := DownloadArchive(downloadURL, archivePath, progressCallback, cancel); err != nil {
		return "", fmt.Errorf("downloading FFmpeg: %w", err)
	}

	log.Printf("Extracting FFmpeg to: %s", ffmpegDir)
	if goos == "windows" {
		if err := ExtractZip(archivePath, ffmpegDir); err != nil {
			return "", fmt.Errorf("extracting FFmpeg zip: %w", err)
		}
	} else {
		if err := ExtractTarXz(archivePath, ffmpegDir); err != nil {
			return "", fmt.Errorf("extracting FFmpeg tar.xz: %w", err)
		}
	}

	result := FindLocalFFmpeg()
	if result == "" {
		return "", fmt.Errorf("FFmpeg executable not found after extraction in %s", ffmpegDir)
	}

	// Make executables +x on non-Windows
	if goos != "windows" {
		binDir := filepath.Dir(result)
		for _, name := range []string{"ffmpeg", "ffprobe", "ffplay"} {
			p := filepath.Join(binDir, name)
			if _, statErr := os.Stat(p); statErr == nil {
				os.Chmod(p, 0755)
			}
		}
	}

	log.Printf("FFmpeg installed successfully at: %s", result)
	return result, nil
}

// EnsureFFmpegPrerequisite checks for FFmpeg in the shared directory,
// downloads it if missing, and falls back to the system PATH only when
// the download is not possible (e.g. unsupported platform).
//
// This blocks on a network download and drives a progress dialog, so it must
// be called from a background goroutine, never from the Fyne main goroutine.
func EnsureFFmpegPrerequisite(w fyne.Window) (string, error) {
	log.Println("FFmpeg check: looking for bundled FFmpeg in shared directory")
	ffmpegDir := GetSharedFFmpegDir()
	log.Printf("FFmpeg check: shared directory is %s", ffmpegDir)

	// Already installed in the shared directory?
	if existing := FindLocalFFmpeg(); existing != "" {
		log.Printf("FFmpeg check: already installed at %s — using it", existing)
		return existing, nil
	}
	log.Println("FFmpeg check: not found in shared directory")

	// Try to download our own copy first.
	downloadURL, urlErr := getFFmpegDownloadURL()
	if urlErr != nil {
		// Platform not supported for bundled FFmpeg — fall back to system PATH.
		log.Printf("FFmpeg check: cannot determine download URL (%v) — platform not supported for bundled FFmpeg", urlErr)
		if systemFFmpeg, err := exec.LookPath("ffmpeg"); err == nil {
			log.Printf("FFmpeg check: falling back to system PATH FFmpeg at %s", systemFFmpeg)
			return systemFFmpeg, nil
		}
		log.Println("FFmpeg check: not found on system PATH either — giving up")
		return "", fmt.Errorf("FFmpeg not available: %v and not found on system PATH", urlErr)
	}

	log.Printf("FFmpeg check: will download bundled FFmpeg from %s", downloadURL)

	cancel := make(chan bool)
	var progressBar *widget.ProgressBar
	var progressDialog dialog.Dialog
	fyne.DoAndWait(func() {
		progressBar = widget.NewProgressBar()
		progressDialog = dialog.NewCustom("Installing FFmpeg", "Cancel", progressBar, w)
		progressDialog.SetOnClosed(func() {
			select {
			case cancel <- true:
			default:
			}
		})
		progressDialog.Show()
		progressBar.SetValue(0.01)
	})

	path, err := DownloadAndInstallFFmpeg(func(downloaded, total int64) {
		if total > 0 {
			progress := float64(downloaded) / float64(total)
			fyne.Do(func() {
				progressBar.SetValue(progress)
			})
		}
	}, cancel)

	fyne.DoAndWait(func() {
		progressBar.SetValue(1.0)
		progressDialog.Hide()
	})

	if err != nil {
		// Download failed — try system PATH as last resort.
		log.Printf("FFmpeg check: download/install failed (%v) — trying system PATH as last resort", err)
		if systemFFmpeg, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			log.Printf("FFmpeg check: falling back to system PATH FFmpeg at %s", systemFFmpeg)
			return systemFFmpeg, nil
		}
		log.Println("FFmpeg check: not found on system PATH either — giving up")
		return "", fmt.Errorf("FFmpeg installation failed: %w", err)
	}

	log.Printf("FFmpeg check: successfully installed bundled FFmpeg at %s", path)
	return path, nil
}
