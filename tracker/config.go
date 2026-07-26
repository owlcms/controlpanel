package tracker

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"controlpanel/shared"

	"github.com/magiconair/properties"
)

var (
	environment *properties.Properties
)

// SetInstallDir overrides the tracker installation directory for this process.
func SetInstallDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	installDir = dir
	environment = nil
	lockFilePath = filepath.Join(installDir, "tracker.lock")
	pidFilePath = filepath.Join(installDir, "tracker.pid")
}

// GetPort returns the configured port for tracker, defaulting to "8096"
func GetPort() string {
	if environment == nil {
		return "8096"
	}
	port, ok := environment.Get("TRACKER_PORT")
	if !ok {
		return "8096"
	}
	return port
}

func loadEnvironmentForReleaseProps(releaseVersion string) (*properties.Properties, error) {
	if err := InitEnv(); err != nil {
		return nil, err
	}

	return shared.MergeEnvironmentProperties(
		filepath.Join(installDir, "env.properties"),
		filepath.Join(installDir, strings.TrimSpace(releaseVersion), "env.properties"),
	)
}

// GetPortForRelease returns the effective TRACKER_PORT for a selected release,
// falling back to the shared env.properties value.
func GetPortForRelease(releaseVersion string) string {
	merged, err := loadEnvironmentForReleaseProps(releaseVersion)
	if err != nil || merged == nil {
		if err != nil {
			log.Printf("Falling back to shared tracker port for %s: %v", strings.TrimSpace(releaseVersion), err)
		}
		return GetPort()
	}

	port, ok := merged.Get("TRACKER_PORT")
	if !ok || strings.TrimSpace(port) == "" {
		return "8096"
	}
	return strings.TrimSpace(port)
}

// GetRunAsDaemon returns true if the control panel should leave OWLCMS and Tracker
// running after window or terminal closure on Linux.
func GetRunAsDaemon() bool {
	if environment == nil {
		return shared.IsRunAsDaemonEnabled()
	}

	value, ok := environment.Get(shared.RunAsDaemonEnv)
	if !ok {
		return shared.IsRunAsDaemonEnabled()
	}

	trimmed := strings.TrimSpace(strings.ToLower(value))
	return trimmed == "1" || trimmed == "true" || trimmed == "yes" || trimmed == "on"
}

// SetRunAsDaemon persists the daemon setting and updates the current process environment.
// It also syncs the setting to the owlcms env.properties so both stay consistent.
func SetRunAsDaemon(enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}

	if err := SaveProperty(shared.RunAsDaemonEnv, value); err != nil {
		return err
	}

	// Cross-sync to owlcms env.properties
	owlcmsEnv := filepath.Join(shared.GetOwlcmsInstallDir(), "env.properties")
	if err := shared.SavePropertyToFile(owlcmsEnv, shared.RunAsDaemonEnv, value); err != nil {
		log.Printf("Warning: failed to sync daemon setting to owlcms: %v", err)
	}

	return shared.SetRunAsDaemonEnabled(enabled)
}

// SaveProperty saves a key-value pair to env.properties and reloads the environment
func SaveProperty(key, value string) error {
	envFilePath := filepath.Join(installDir, "env.properties")

	if environment == nil {
		if err := InitEnv(); err != nil {
			return fmt.Errorf("failed to initialize environment: %w", err)
		}
	}

	environment.Set(key, value)

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

// SavePropertyForRelease saves a key-value pair to a version-specific env.properties file.
func SavePropertyForRelease(releaseVersion, key, value string) error {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return fmt.Errorf("release version is required")
	}

	releaseEnvPath := filepath.Join(installDir, releaseVersion, "env.properties")
	if err := shared.EnsureDir0755(filepath.Dir(releaseEnvPath)); err != nil {
		return fmt.Errorf("creating release env directory: %w", err)
	}

	props := properties.NewProperties()
	if _, err := os.Stat(releaseEnvPath); err == nil {
		loaded, err := properties.LoadFile(releaseEnvPath, properties.UTF8)
		if err != nil {
			return fmt.Errorf("loading %s: %w", releaseEnvPath, err)
		}
		props = loaded
	} else if os.IsNotExist(err) {
		if environment == nil {
			if err := InitEnv(); err != nil {
				return fmt.Errorf("failed to initialize environment: %w", err)
			}
		}
		for _, propKey := range environment.Keys() {
			propValue, _ := environment.Get(propKey)
			props.Set(propKey, propValue)
		}
	} else {
		return fmt.Errorf("checking %s: %w", releaseEnvPath, err)
	}

	props.Set(key, value)
	file, err := os.Create(releaseEnvPath)
	if err != nil {
		return fmt.Errorf("opening %s for writing: %w", releaseEnvPath, err)
	}
	defer file.Close()
	if _, err := props.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("writing %s: %w", releaseEnvPath, err)
	}

	log.Printf("Saved property %s = %s to %s", key, value, releaseEnvPath)
	return nil
}

// InitEnv initializes the tracker environment from env.properties
func InitEnv() error {
	log.Println("Initializing tracker environment")
	// Check for the presence of env.properties file in the installDir
	envFilePath := filepath.Join(installDir, "env.properties")
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		log.Printf("env.properties file not found at %s, creating with default values", envFilePath)

		// Ensure the directory exists before creating the file
		if err := shared.EnsureDir0755(installDir); err != nil {
			return err
		}

		// Create env.properties file with default entry using properties library
		props := properties.NewProperties()
		props.Set("TRACKER_PORT", "8096")
		props.Set(shared.RunAsDaemonEnv, "false")

		file, err := os.Create(envFilePath)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := props.Write(file, properties.UTF8); err != nil {
			return err
		}

		log.Printf("Successfully created env.properties file at %s", envFilePath)
	}

	// Load environment from env.properties
	props, err := properties.LoadFile(envFilePath, properties.UTF8)
	if err != nil {
		return err
	}
	environment = props

	// Log the properties for debugging
	log.Printf("Loaded properties from %s:", envFilePath)
	for _, key := range environment.Keys() {
		value, _ := environment.Get(key)
		log.Printf("  %s = %s", key, value)
	}

	// Sync the daemon setting to the process environment
	if err := shared.SetRunAsDaemonEnabled(GetRunAsDaemon()); err != nil {
		log.Printf("Failed to sync daemon setting from tracker env.properties: %v", err)
	}

	return nil
}

// LoadEnvironmentForRelease loads the shared tracker env.properties and overlays
// any version-specific env.properties for the selected release.
func LoadEnvironmentForRelease(releaseVersion string) error {
	merged, err := loadEnvironmentForReleaseProps(releaseVersion)
	if err != nil {
		return err
	}

	environment = merged
	return nil
}

func getInstallDir() string {
	return shared.GetTrackerInstallDir()
}

// GetInstallDir returns the installation directory used by the tracker package
func GetInstallDir() string {
	return getInstallDir()
}
