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

// GetSharedFFmpegDir returns the shared FFmpeg installation directory
// under the control panel root, at the same level as java/ and node/.
func GetSharedFFmpegDir() string {
	return filepath.Join(GetRuntimeDir(), "ffmpeg")
}

// FindLocalFFmpeg searches for an FFmpeg executable in the shared control panel
// directory.  Returns the full path or empty string if not found.
func FindLocalFFmpeg() string {
	runtime, err := loadFFmpegRuntime(ffmpegManifestJSON, GetGoos(), GetGoarch())
	if err != nil {
		return ""
	}
	result, err := installedFFmpegRuntime(runtime, filepath.Join(GetSharedFFmpegDir(), runtime.Directory))
	if err != nil {
		return ""
	}
	return result
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
	runtime, err := loadFFmpegRuntime(ffmpegManifestJSON, GetGoos(), GetGoarch())
	if err == nil && !runtime.AllowSystemFallback {
		return ""
	}
	if err != nil && !errors.Is(err, errFFmpegSystemOnly) {
		return ""
	}
	return FindSystemFFmpeg()
}

// ffmpegMissingMarkdown explains how to install FFmpeg on platforms where the
// control panel cannot download a bundled build.
func ffmpegMissingMarkdown() string {
	if GetGoos() == "darwin" {
		return "### FFmpeg not found\n\n" +
			"The video modules need FFmpeg. Intel Macs use a system installation:\n\n" +
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
	runtime, err := loadFFmpegRuntime(ffmpegManifestJSON, GetGoos(), GetGoarch())
	if err != nil {
		return "", err
	}
	return runtime.Archives[0].URL, nil
}

// DownloadAndInstallFFmpeg downloads and installs FFmpeg to the shared
// directory.  Returns the path to the installed ffmpeg executable.
func DownloadAndInstallFFmpeg(progressCallback func(downloaded, total int64), cancel <-chan bool) (string, error) {
	runtime, err := loadFFmpegRuntime(ffmpegManifestJSON, GetGoos(), GetGoarch())
	if err != nil {
		return "", err
	}
	return installFFmpegRuntime(runtime, GetSharedFFmpegDir(), progressCallback, cancel, DownloadArchive)
}

// EnsureFFmpegPrerequisite checks for FFmpeg in the shared directory,
// downloads it if missing, and falls back to the system PATH only when
// the download is not possible (e.g. unsupported platform).
//
// This blocks on a network download and drives a progress dialog, so it must
// be called from a background goroutine, never from the Fyne main goroutine.
func EnsureFFmpegPrerequisite(w fyne.Window) (string, error) {
	runtime, manifestErr := loadFFmpegRuntime(ffmpegManifestJSON, GetGoos(), GetGoarch())
	if manifestErr != nil && !errors.Is(manifestErr, errFFmpegSystemOnly) {
		return "", manifestErr
	}
	log.Println("FFmpeg check: looking for bundled FFmpeg in shared directory")
	ffmpegDir := GetSharedFFmpegDir()
	log.Printf("FFmpeg check: shared directory is %s", ffmpegDir)

	// Already installed in the shared directory?
	if existing := FindLocalFFmpeg(); existing != "" {
		ffmpegInstallMu.Lock()
		cleanupFFmpegRuntimes(ffmpegDir, filepath.Join(ffmpegDir, runtime.Directory))
		ffmpegInstallMu.Unlock()
		log.Printf("FFmpeg check: already installed at %s — using it", existing)
		return existing, nil
	}
	log.Println("FFmpeg check: not found in shared directory")

	// Try to download our own copy first.
	downloadURL, urlErr := getFFmpegDownloadURL()
	if urlErr != nil {
		if !errors.Is(urlErr, errFFmpegSystemOnly) {
			return "", urlErr
		}
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

	cancel := make(chan bool, 1)
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
		if !runtime.AllowSystemFallback {
			return "", fmt.Errorf("FFmpeg installation failed: %w", err)
		}
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
