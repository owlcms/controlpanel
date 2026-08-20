package shared

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ErrFFmpegNotFound reports that no usable FFmpeg could be located or installed.
var ErrFFmpegNotFound = errors.New("ffmpeg not found")

const ffmpegDownloadPage = "https://ffmpeg.org/download.html"

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

// systemFFmpegSearchDirs lists well-known package manager install locations.
// GUI apps launched from Finder/Dock (macOS) or a desktop launcher (Linux) do
// not inherit the login shell PATH, so these must be probed explicitly.
func systemFFmpegSearchDirs() []string {
	switch GetGoos() {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	case "linux":
		return []string{"/usr/local/bin", "/usr/bin", "/snap/bin"}
	default:
		return nil
	}
}

func findFFmpegInSearchDirs() string {
	for _, dir := range systemFFmpegSearchDirs() {
		candidate := filepath.Join(dir, "ffmpeg")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}

// FindSystemFFmpeg locates an ffmpeg executable outside the control panel.
// On macOS the package manager directories are checked before the PATH, because
// the PATH of a Finder-launched app is the bare launchd one and never contains
// Homebrew.  Elsewhere the PATH wins so a user-chosen build takes precedence.
func FindSystemFFmpeg() string {
	if GetGoos() == "darwin" {
		if path := findFFmpegInSearchDirs(); path != "" {
			return path
		}
	}
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}
	return findFFmpegInSearchDirs()
}

// FindFFmpeg returns the bundled FFmpeg if installed, otherwise a system one.
func FindFFmpeg() string {
	if local := FindLocalFFmpeg(); local != "" {
		return local
	}
	return FindSystemFFmpeg()
}

// ffmpegMissingMarkdown explains how to install FFmpeg on platforms where the
// control panel cannot download a bundled build.
func ffmpegMissingMarkdown() string {
	if GetGoos() == "darwin" {
		return "### FFmpeg not found\n\n" +
			"The video modules need FFmpeg, which is not bundled on macOS. Install it either way:\n\n" +
			"- Use an installer from the [FFmpeg download page](" + ffmpegDownloadPage + ")\n" +
			"- Run `brew install ffmpeg` if you have [Homebrew](https://brew.sh)\n\n" +
			"Then restart the control panel.\n"
	}
	return "### FFmpeg not found\n\n" +
		"The video modules need FFmpeg. Install it either way:\n\n" +
		"- Use an installer from the [FFmpeg download page](" + ffmpegDownloadPage + ")\n" +
		"- Install the `ffmpeg` package with your package manager\n\n" +
		"Then restart the control panel.\n"
}

// ShowFFmpegError reports err to the user, using a rich dialog with clickable
// links when FFmpeg is simply missing.
func ShowFFmpegError(err error, w fyne.Window) {
	if !errors.Is(err, ErrFFmpegNotFound) {
		dialog.ShowError(err, w)
		return
	}

	body := widget.NewRichTextFromMarkdown(ffmpegMissingMarkdown())
	body.Wrapping = fyne.TextWrapWord

	d := dialog.NewCustom("FFmpeg Not Found", "Close", body, w)
	d.Resize(fyne.NewSize(520, 300))
	d.Show()
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
		if systemFFmpeg := FindSystemFFmpeg(); systemFFmpeg != "" {
			log.Printf("FFmpeg check: falling back to system FFmpeg at %s", systemFFmpeg)
			return systemFFmpeg, nil
		}
		log.Println("FFmpeg check: no system FFmpeg found either — giving up")
		return "", ErrFFmpegNotFound
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
		log.Printf("FFmpeg check: download/install failed (%v) — trying system FFmpeg as last resort", err)
		if systemFFmpeg := FindSystemFFmpeg(); systemFFmpeg != "" {
			log.Printf("FFmpeg check: falling back to system FFmpeg at %s", systemFFmpeg)
			return systemFFmpeg, nil
		}
		log.Println("FFmpeg check: no system FFmpeg found either — giving up")
		return "", fmt.Errorf("FFmpeg installation failed: %w", err)
	}

	log.Printf("FFmpeg check: successfully installed bundled FFmpeg at %s", path)
	return path, nil
}
