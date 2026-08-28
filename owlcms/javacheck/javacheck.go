package javacheck

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	customdialog "controlpanel/owlcms/dialog"
	"controlpanel/shared"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

var getTemurinVersionFunc func() string

func InitJavaCheck(installDir string, getVersionFunc func() string) {
	// No longer need to store installDir - we use shared location
	getTemurinVersionFunc = getVersionFunc
}

func FindLocalJava() (string, error) {
	// Get the Temurin version from config
	var temurinVersion string
	if getTemurinVersionFunc != nil {
		temurinVersion = getTemurinVersionFunc()
	} else {
		temurinVersion = "jdk-25" // fallback
	}

	return FindLocalJavaForVersion(temurinVersion)
}

// FindLocalJavaForVersion finds local Java installation for a specific Temurin version
func FindLocalJavaForVersion(temurinVersion string) (string, error) {
	return shared.FindLocalJavaForVersion(temurinVersion, shared.GetGoos)
}

// CheckJavaHeadless is a headless-friendly version of CheckJava that does not depend on Fyne.
func CheckJavaHeadless() error {
	javaPath, err := FindLocalJava()
	if err == nil {
		log.Printf("*** Found local Java at: %s\n", javaPath)
		return nil
	} else {
		log.Printf("*** Local Java not found at %s: %v\n", javaPath, err)
	}

	fmt.Println("Suitable Java not found. Downloading from Temurin...")
	log.Println("Downloading a local copy of the Java language runtime.")

	var temurinVersion string
	if getTemurinVersionFunc != nil {
		temurinVersion = getTemurinVersionFunc()
	} else {
		temurinVersion = "jdk-25" // fallback
	}

	javaDir := shared.GetSharedJavaDir(temurinVersion)

	if _, err := os.Stat(javaDir); err == nil {
		err := os.RemoveAll(javaDir)
		if err != nil {
			return fmt.Errorf("failed to delete existing java directory: %w", err)
		}
	}

	if err := shared.EnsureDir0755(javaDir); err != nil {
		return fmt.Errorf("creating java directory: %w", err)
	}

	downloadURL, err := shared.GetTemurinDownloadURL(temurinVersion, shared.GetGoos, "controlpanel")
	if err != nil {
		return fmt.Errorf("getting Temurin download URL: %w", err)
	}

	archivePath := filepath.Join(javaDir, "temurin")
	if shared.GetGoos() == "windows" && !shared.IsWSL() {
		archivePath += ".zip"
	} else {
		archivePath += ".tar.gz"
	}

	progressCallback := func(downloaded, total int64) {
		if total > 0 {
			percentage := float64(downloaded) / float64(total)
			log.Printf("Downloading Java... %.1f%%", percentage*100)
			fmt.Printf("\rDownloading Java... %.1f%%", percentage*100)
		}
	}

	if err := shared.DownloadArchive(downloadURL, archivePath, progressCallback, nil); err != nil {
		os.Remove(archivePath)
		return fmt.Errorf("error downloading Java: %w", err)
	}

	log.Println("Extracting files...")
	if shared.GetGoos() == "windows" && !shared.IsWSL() {
		if err := shared.ExtractZip(archivePath, javaDir, false); err != nil {
			return fmt.Errorf("error extracting Temurin zip: %w", err)
		}
	} else {
		if err := shared.ExtractTarGz(archivePath, javaDir); err != nil {
			return fmt.Errorf("extracting Temurin tar.gz: %w", err)
		}
	}

	log.Printf("Java downloaded and installed to %s\n", javaDir)
	fmt.Printf("\nJava downloaded and installed to %s\n", javaDir)
	return nil
}

// CheckJava checks for Java 17 or later and downloads/installs it if necessary.
func CheckJava(statusLabel *widget.Label) error {
	// First check for local Java installation
	javaPath, err := FindLocalJava()
	if err == nil {
		log.Printf("*** Found local Java at: %s\n", javaPath)
		return nil
	} else {
		log.Printf("*** Local Java not found at %s: %v\n", javaPath, err)
	}

	fmt.Println("Suitable Java not found. Downloading from Temurin...")
	statusLabel.SetText("Downloading a local copy of the Java language runtime.")
	statusLabel.Refresh()
	statusLabel.Show()

	// Create a cancel channel
	cancel := make(chan bool)

	// Show the progress dialog immediately
	progressDialog, progressBar := customdialog.NewDownloadDialog(
		"Installing Java",
		fyne.CurrentApp().Driver().AllWindows()[0],
		cancel)
	progressDialog.Show()
	progressBar.SetValue(0.01) // Set a small initial value to show activity

	// Get the Temurin version
	var temurinVersion string
	if getTemurinVersionFunc != nil {
		temurinVersion = getTemurinVersionFunc()
	} else {
		temurinVersion = "jdk-25" // fallback
	}

	// Use shared Java directory
	javaDir := shared.GetSharedJavaDir(temurinVersion)

	// Recursively delete the java directory if it exists
	if _, err := os.Stat(javaDir); err == nil {
		err := os.RemoveAll(javaDir)
		if err != nil {
			progressDialog.Hide()
			return fmt.Errorf("failed to delete existing java directory: %w", err)
		}
	}

	// Ensure the java directory exists
	if err := shared.EnsureDir0755(javaDir); err != nil {
		progressDialog.Hide()
		return fmt.Errorf("creating java directory: %w", err)
	}

	// Show activity while getting the download URL
	progressBar.SetValue(0.05)
	downloadURL, err := shared.GetTemurinDownloadURL(temurinVersion, shared.GetGoos, "controlpanel")
	if err != nil {
		progressDialog.Hide()
		return fmt.Errorf("getting Temurin download URL: %w", err)
	}

	archivePath := filepath.Join(javaDir, "temurin")
	if shared.GetGoos() == "windows" && !shared.IsWSL() {
		archivePath += ".zip"
	} else {
		archivePath += ".tar.gz"
	}

	progressCallback := func(downloaded, total int64) {
		if total > 0 {
			percentage := float64(downloaded) / float64(total)
			progressBar.SetValue(percentage)
		}
	}

	if err := shared.DownloadArchive(downloadURL, archivePath, progressCallback, cancel); err != nil {
		progressDialog.Hide()
		if err.Error() == "download cancelled" {
			// Handle cancellation
			log.Println("Java download cancelled by user")

			// Clean up the incomplete archive file
			os.Remove(archivePath)

			return nil
		}
		return fmt.Errorf("error downloading Java: %w", err)
	}

	// Show extraction progress
	progressBar.SetValue(0.9)
	if shared.GetGoos() == "windows" && !shared.IsWSL() {
		if err := shared.ExtractZip(archivePath, javaDir, false); err != nil {
			progressDialog.Hide()
			return fmt.Errorf("error extracting Temurin zip: %w", err)
		}
	} else {
		if err := shared.ExtractTarGz(archivePath, javaDir); err != nil {
			progressDialog.Hide()
			return fmt.Errorf("extracting Temurin tar.gz: %w", err)
		}
	}

	// Indicate completion
	progressBar.SetValue(1.0)
	progressDialog.Hide()

	// extract now removes the archive
	log.Printf("Java downloaded and installed to %s\n", javaDir)
	return nil
}
