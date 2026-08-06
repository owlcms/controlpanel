package firmata

import (
	"controlpanel/shared"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	customdialog "controlpanel/firmata/dialog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/magiconair/properties"
)

var (
	launcherVersion = "1.0.0"              // Default launcher version
	buildVersion    = "_TAG_"              // Placeholder for build version
	environment     *properties.Properties // Global variable to hold the environment properties
)

func initConfig() {
	if buildVersion != ("_" + "TAG" + "_") {
		// not running in a development environment
		launcherVersion = buildVersion
	}
}

// GetPort returns the configured port from env.properties, defaulting to "8090"
func GetPort() string {
	if environment == nil {
		return "8090"
	}
	port, ok := environment.Get("FIRMATA_PORT")
	if !ok {
		return "8090"
	}
	return port
}

// GetTemurinVersion returns the configured Temurin version from env.properties
func GetTemurinVersion() string {
	if environment == nil {
		return "jdk-25"
	}
	version, ok := environment.Get("TEMURIN_VERSION")
	if !ok {
		return "jdk-25"
	}
	return version
}

// GetPortForRelease returns the effective FIRMATA_PORT for a specific release,
// falling back to the shared env.properties value.
func GetPortForRelease(releaseVersion string) string {
	merged, err := shared.MergeEnvironmentProperties(
		filepath.Join(installDir, "env.properties"),
		filepath.Join(installDir, strings.TrimSpace(releaseVersion), "env.properties"),
	)
	if err != nil {
		return GetPort()
	}

	if port, ok := merged.Get("FIRMATA_PORT"); ok {
		trimmed := strings.TrimSpace(port)
		if trimmed != "" {
			return trimmed
		}
	}

	return GetPort()
}

// GetTemurinVersionForRelease returns the Temurin version for a specific release.
// It first checks the version-specific env.properties file, then falls back to
// the shared env.properties file, and finally to the default value.
func GetTemurinVersionForRelease(releaseVersion string) string {
	// First, try to load version-specific env.properties
	versionEnvPath := filepath.Join(installDir, releaseVersion, "env.properties")
	if _, err := os.Stat(versionEnvPath); err == nil {
		// Version-specific env.properties exists, try to load it
		versionProps := properties.NewProperties()
		content, err := os.ReadFile(versionEnvPath)
		if err == nil {
			if err := versionProps.Load(content, properties.UTF8); err == nil {
				if temurinVersion, ok := versionProps.Get("TEMURIN_VERSION"); ok {
					log.Printf("Using version-specific Temurin %s for firmata %s", temurinVersion, releaseVersion)
					return temurinVersion
				}
			}
		}
	}

	// Fall back to shared env.properties
	temurinVersion := GetTemurinVersion()
	log.Printf("Using shared Temurin %s for firmata %s", temurinVersion, releaseVersion)
	return temurinVersion
}

// EnsureParentEnvDefaults ensures the shared env.properties exists and contains
// required defaults. TEMURIN_VERSION is forced to jdk-25.
// This delegates to InitEnv which handles all the enforcement logic.
func EnsureParentEnvDefaults() error {
	return InitEnv()
}

// EnsureEnvWithDialog ensures env.properties exists and shows a wide error dialog on failure.
// Returns true if env.properties was successfully created/verified, false otherwise.
func EnsureEnvWithDialog(w fyne.Window) bool {
	if err := EnsureParentEnvDefaults(); err != nil {
		log.Printf("Failed to initialize env.properties: %v", err)
		errorMsg := fmt.Errorf("installation completed but failed to create configuration file: %w\n\nYou may need to check permissions on the installation directory", err)
		customdialog.ShowWideError(errorMsg, w)
		return false
	}
	return true
}

// SaveProperty persists a shared Firmata setting in the parent env.properties file.
func SaveProperty(key, value string) error {
	if err := InitEnv(); err != nil {
		return fmt.Errorf("failed to initialize environment: %w", err)
	}
	if key == "FIRMATA_PORT" {
		for _, releaseVersion := range getAllInstalledVersions() {
			if err := EnsureReleaseEnvFromParent(releaseVersion); err != nil {
				return fmt.Errorf("preserving configuration for version %s: %w", releaseVersion, err)
			}
		}
	}

	environment.Set(key, value)
	envFilePath := filepath.Join(installDir, "env.properties")
	file, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open env.properties for writing: %w", err)
	}
	defer file.Close()

	if _, err := environment.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("failed to write env.properties: %w", err)
	}

	log.Printf("Saved property %s = %s to %s", key, value, envFilePath)
	return nil
}

// EnsureReleaseEnvFromParent snapshots shared settings for a newly installed release.
func EnsureReleaseEnvFromParent(releaseVersion string) error {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return fmt.Errorf("release version is required")
	}
	if err := InitEnv(); err != nil {
		return fmt.Errorf("initializing shared environment: %w", err)
	}

	releaseEnvPath := filepath.Join(installDir, releaseVersion, "env.properties")
	if _, err := os.Stat(releaseEnvPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", releaseEnvPath, err)
	}
	if err := shared.EnsureDir0755(filepath.Dir(releaseEnvPath)); err != nil {
		return fmt.Errorf("creating release env directory: %w", err)
	}

	props := properties.NewProperties()
	for _, propKey := range environment.Keys() {
		propValue, _ := environment.Get(propKey)
		props.Set(propKey, propValue)
	}
	file, err := os.Create(releaseEnvPath)
	if err != nil {
		return fmt.Errorf("creating release env.properties: %w", err)
	}
	defer file.Close()
	if _, err := props.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("writing release env.properties: %w", err)
	}

	return nil
}

// SavePropertyForRelease saves a key-value pair in a release-specific env.properties file.
func SavePropertyForRelease(releaseVersion, key, value string) error {
	if err := EnsureReleaseEnvFromParent(releaseVersion); err != nil {
		return err
	}

	releaseEnvPath := filepath.Join(installDir, strings.TrimSpace(releaseVersion), "env.properties")
	props, err := properties.LoadFile(releaseEnvPath, properties.UTF8)
	if err != nil {
		return fmt.Errorf("loading %s: %w", releaseEnvPath, err)
	}
	props.Set(key, value)
	file, err := os.Create(releaseEnvPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", releaseEnvPath, err)
	}
	defer file.Close()
	if _, err := props.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("writing %s: %w", releaseEnvPath, err)
	}

	return nil
}

// InitEnv initializes the environment properties from env.properties file
func InitEnv() error {
	log.Println("Initializing environment properties")
	// Check for the presence of env.properties file in the installDir
	props := properties.NewProperties()
	envFilePath := filepath.Join(installDir, "env.properties")
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		// Create env.properties file with entry "FIRMATA_PORT=8090"
		if err := shared.EnsureDir0755(installDir); err != nil {
			return fmt.Errorf("failed to create Firmata installation directory: %w", err)
		}
		props.Set("FIRMATA_PORT", "8090")
		props.Set("TEMURIN_VERSION", "jdk-25")
		file, err := os.Create(envFilePath)
		if err != nil {
			return fmt.Errorf("failed to create env.properties file: %w", err)
		}
		defer file.Close()
		if _, err := props.Write(file, properties.UTF8); err != nil {
			return fmt.Errorf("failed to write env.properties file: %w", err)
		}
		// Add commented-out entries
		rawString := `# Add any environment variable you need. (remove the leading # to uncomment)
# java options can be set with this variable (remove the leading # to uncomment)
#JAVA_OPTIONS=-Xmx512m -Xmx512m`

		if _, err := file.WriteString(rawString); err != nil {
			return fmt.Errorf("failed to write comment to env.properties file: %w", err)
		}
	}

	// Load the properties into the global variable environment
	if err := loadProperties(envFilePath); err != nil {
		return err
	}

	// Ensure required defaults are enforced even on existing files
	updated := false
	if current, _ := environment.Get("TEMURIN_VERSION"); current != "jdk-25" {
		log.Printf("Updating TEMURIN_VERSION from %s to jdk-25", current)
		environment.Set("TEMURIN_VERSION", "jdk-25")
		updated = true
	}
	if _, ok := environment.Get("FIRMATA_PORT"); !ok {
		environment.Set("FIRMATA_PORT", "8090")
		updated = true
	}

	if !updated {
		return nil
	}

	log.Printf("Writing updated env.properties to %s", envFilePath)
	file, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open env.properties for writing: %w", err)
	}
	defer file.Close()

	if _, err := environment.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("failed to write env.properties: %w", err)
	}

	rawString := `# Add any environment variable you need. (remove the leading # to uncomment)
# java options can be set with this variable (remove the leading # to uncomment)
#JAVA_OPTIONS=-Xmx512m -Xmx512m`
	if _, err := file.WriteString("\n" + rawString + "\n"); err != nil {
		log.Printf("Failed to write comments to env.properties file: %v, but file is usable", err)
	}

	return nil
}

func loadProperties(envFilePath string) error {
	environment = properties.NewProperties()
	file, err := os.Open(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open env.properties file: %w", err)
	}
	defer file.Close()
	content, err := os.ReadFile(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to read env.properties file: %w", err)
	}
	if err := environment.Load(content, properties.UTF8); err != nil {
		return fmt.Errorf("failed to load env.properties file: %w", err)
	}
	return nil
}

// LoadEnvironmentForRelease loads the shared env.properties and overlays any
// version-specific env.properties for the selected release.
func LoadEnvironmentForRelease(releaseVersion string) error {
	if err := EnsureParentEnvDefaults(); err != nil {
		return err
	}

	merged, err := shared.MergeEnvironmentProperties(
		filepath.Join(installDir, "env.properties"),
		filepath.Join(installDir, strings.TrimSpace(releaseVersion), "env.properties"),
	)
	if err != nil {
		return err
	}

	environment = merged
	return nil
}

// CheckForUpdates checks for updates to the firmata control panel
func CheckForUpdates(win fyne.Window) {
	const repoURL = "https://api.github.com/repos/firmata/firmata-controlpanel/releases/latest"
	resp, err := http.Get(repoURL)
	if err != nil {
		log.Printf("Failed to check for updates: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to check for updates: received status code %d", resp.StatusCode)
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("Failed to parse update information: %v", err)
		return
	}

	if release.TagName > launcherVersion {
		url, err := url.Parse(release.HTMLURL)
		if err != nil {
			log.Printf("Failed to parse release URL: %v", err)
			return
		}
		link := widget.NewHyperlink("Release Notes and Installer", url)
		content := container.NewVBox(
			widget.NewLabel(fmt.Sprintf("A new version (%s) is available. You are currently using version %s.\nYou can simply download the new installer and install over the current version.", release.TagName, launcherVersion)),
			link,
		)
		dialog.ShowCustom("Update Available", "Close", content, win)
	} else {
		dialog.ShowInformation("No Updates", "The latest version is installed.", win)
	}
}
