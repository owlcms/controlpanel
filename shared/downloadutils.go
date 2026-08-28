package shared

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ProgressCallback is a function type that receives download progress updates
type ProgressCallback func(downloaded, total int64)

// DownloadArchive downloads a file and reports progress through the callback. It also accepts a cancel channel.
func DownloadArchive(url, destPath string, progress ProgressCallback, cancel <-chan bool) error {
	log.Printf("Attempting to download from URL: %s\n", url)

	client := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Call progress callback immediately to update UI before network request
	if progress != nil {
		progress(0, 100) // Use placeholder total size of 100
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned non-200 status: %s for %s", resp.Status, url)
	}

	// Update progress with actual total size now that we have the response
	if progress != nil && resp.ContentLength > 0 {
		progress(0, resp.ContentLength)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", destPath, err)
	}
	defer out.Close()

	// Create a proxy reader that will report progress
	counter := &WriteCounter{
		Total:    resp.ContentLength,
		Progress: progress,
		Cancel:   cancel, // Pass the cancel channel to the counter
	}

	_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	if err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	log.Printf("Successfully downloaded file to: %s\n", destPath)
	return nil
}

// WriteCounter counts bytes written and reports progress
type WriteCounter struct {
	Downloaded int64
	Total      int64
	Progress   ProgressCallback
	Cancel     <-chan bool // Add a cancel channel
	lastReport time.Time
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	select {
	case <-wc.Cancel:
		return 0, fmt.Errorf("download cancelled") // Check for cancellation
	default:
		n := len(p)
		wc.Downloaded += int64(n)

		// Report progress at most every 100ms to avoid overwhelming the UI
		if time.Since(wc.lastReport) > 100*time.Millisecond {
			if wc.Progress != nil {
				wc.Progress(wc.Downloaded, wc.Total)
			}
			wc.lastReport = time.Now()
		}

		return n, nil
	}
}

// GetDownloadURL returns the correct download URL based on the operating system.
func GetDownloadURL(baseURL string) string {
	var asset string
	goos := GetGoos()
	if IsWSL() {
		asset = "temurin.tar.gz"
	} else {
		switch goos {
		case "windows":
			asset = "temurin.zip"
		case "linux":
			asset = "temurin.tar.gz"
		default:
			asset = "temurin.zip"
		}
	}
	return fmt.Sprintf("%s/%s", baseURL, asset)
}

// ExtractZip extracts a zip archive to the specified destination directory.
// By default, a single top-level directory is stripped. Pass false to preserve it.
func ExtractZip(src, dest string, stripSingleRoot ...bool) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open zip file %s: %w", src, err)
	}
	shouldStripSingleRoot := len(stripSingleRoot) == 0 || stripSingleRoot[0]
	stripRoot := ""
	if shouldStripSingleRoot {
		stripRoot = singleZipRoot(r.File)
	}

	for _, f := range r.File {
		if isZipJunk(f.Name) || f.Name == "Procfile" || f.Name == "system.properties" {
			continue
		}

		entryName := stripZipRoot(f.Name, stripRoot)
		if entryName == "" {
			continue
		}
		fpath := filepath.Join(dest, filepath.FromSlash(entryName))

		if f.FileInfo().IsDir() {
			if err := EnsureDir0755(fpath); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		if err := EnsureDir0755(filepath.Dir(fpath)); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to open file for writing: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open file inside zip: %w", err)
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to copy file data from zip: %w", err)
		}

		// Restore file modification and creation times
		if err := os.Chtimes(fpath, f.Modified, f.Modified); err != nil {
			return fmt.Errorf("failed to change file times: %w", err)
		}
	}

	// Close the reader before removing the file; keeping it open blocks deletion on Windows.
	if err := r.Close(); err != nil {
		return fmt.Errorf("failed to close zip file %s: %w", src, err)
	}

	// Remove the downloaded ZIP file; if something holds the file (e.g., AV), log and continue
	if err := os.Remove(src); err != nil {
		log.Printf("warning: could not remove downloaded file %s: %v", src, err)
	}

	return nil
}

func singleZipRoot(files []*zip.File) string {
	var root string
	for _, f := range files {
		if isZipJunk(f.Name) {
			continue
		}
		name := strings.Trim(filepath.ToSlash(f.Name), "/")
		if name == "" {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) < 2 {
			if f.FileInfo().IsDir() {
				continue
			}
			return ""
		}
		if root == "" {
			root = parts[0]
		} else if root != parts[0] {
			return ""
		}
	}
	return root
}

func stripZipRoot(name, root string) string {
	name = strings.Trim(filepath.ToSlash(name), "/")
	if root != "" {
		name = strings.TrimPrefix(name, root+"/")
	}
	return name
}

func isZipJunk(name string) bool {
	for _, part := range strings.Split(strings.Trim(filepath.ToSlash(name), "/"), "/") {
		if part == ".DS_Store" || part == "__MACOSX" || part == ".AppleDouble" ||
			part == "Thumbs.db" || part == "desktop.ini" || strings.HasPrefix(part, "._") {
			return true
		}
	}
	return false
}

// ExtractTarGz extracts a tar.gz archive to the specified destination directory.
func ExtractTarGz(tarGzPath, dest string) error {
	r, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}

	gzr, err := gzip.NewReader(r)
	if err != nil {
		r.Close()
		return err
	}

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			gzr.Close()
			r.Close()
			return err
		}

		if header.Name == "Procfile" || header.Name == "system.properties" {
			continue
		}

		target := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := EnsureDir0755(target); err != nil {
				gzr.Close()
				r.Close()
				return err
			}
			if err := os.Chtimes(target, header.AccessTime, header.ModTime); err != nil {
				gzr.Close()
				r.Close()
				return err
			}
		case tar.TypeReg:
			// Ensure parent directories are 0755
			if err := EnsureDir0755(filepath.Dir(target)); err != nil {
				gzr.Close()
				r.Close()
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				gzr.Close()
				r.Close()
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				gzr.Close()
				r.Close()
				return err
			}
			if err := os.Chtimes(target, header.AccessTime, header.ModTime); err != nil {
				log.Printf("failed to change file times: %v  %s %s", err, header.AccessTime, header.ModTime)
				gzr.Close()
				r.Close()
				return err
			}
			outFile.Close()
		}
	}

	gzr.Close()
	r.Close() // Close the archive after extracting

	// Remove the downloaded tar.gz file
	if err := os.Remove(tarGzPath); err != nil {
		return fmt.Errorf("failed to remove downloaded file %s: %w", tarGzPath, err)
	}

	return nil
}

// ExtractTarXz extracts a tar.xz archive using the system tar command.
// The archive file is removed after successful extraction.
func ExtractTarXz(tarXzPath, dest string) error {
	cmd := exec.Command("tar", "-xJf", tarXzPath, "-C", dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to extract tar.xz %s: %w (%s)", tarXzPath, err, strings.TrimSpace(string(output)))
	}

	if err := os.Remove(tarXzPath); err != nil {
		log.Printf("warning: could not remove downloaded file %s: %v", tarXzPath, err)
	}

	return nil
}
