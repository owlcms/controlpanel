package owlcms

import (
	"controlpanel/shared"
	"controlpanel/tracker"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/Masterminds/semver/v3"
	"github.com/magiconair/properties"
)

var (
	environment *properties.Properties
	installDir  = shared.GetOwlcmsInstallDir()
)

const (
	trackerConnectionEnv               = "OWLCMS_VIDEODATA"
	trackerConnectionURLSetting        = "CONTROLPANEL_TRACKER_URL"
	trackerConnectionPortSetting       = "CONTROLPANEL_TRACKER_PORT"
	trackerConnectionDefaultEnabledKey = "CONTROLPANEL_TRACKER_CONNECTION_ENABLED_BY_DEFAULT"
)

const (
	defaultTrackerConnectionURL  = "ws://localhost/ws"
	defaultTrackerConnectionPort = "8096"
)

// SetInstallDir overrides the OWLCMS installation directory for this process.
func SetInstallDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	installDir = dir
	environment = nil
	refreshRuntimePaths()
}

// GetLauncherVersion returns the current launcher version
func GetLauncherVersion() string {
	return shared.GetLauncherVersion()
}

// GetPort returns the configured port from env.properties, defaulting to "8080"
func GetPort() string {
	return getPortFromProperties(environment)
}

// GetTemurinVersion returns the configured Temurin version from env.properties
func GetTemurinVersion() string {
	return getTemurinVersionFromProperties(environment)
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
// It also syncs the setting to the tracker env.properties so both stay consistent.
func SetRunAsDaemon(enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}

	if err := SaveProperty(shared.RunAsDaemonEnv, value); err != nil {
		return err
	}

	// Cross-sync to tracker env.properties
	trackerEnv := filepath.Join(shared.GetTrackerInstallDir(), "env.properties")
	if err := shared.SavePropertyToFile(trackerEnv, shared.RunAsDaemonEnv, value); err != nil {
		log.Printf("Warning: failed to sync daemon setting to tracker: %v", err)
	}

	return shared.SetRunAsDaemonEnabled(enabled)
}

// GetPortForRelease returns the effective OWLCMS_PORT for a selected release,
// falling back to the shared env.properties value.
func GetPortForRelease(releaseVersion string) string {
	merged, err := loadEnvironmentForReleaseProps(releaseVersion)
	if err != nil || merged == nil {
		if err != nil {
			log.Printf("Falling back to shared OWLCMS port for %s: %v", strings.TrimSpace(releaseVersion), err)
		}
		return GetPort()
	}

	return getPortFromProperties(merged)
}

// GetReleaseEnvPath returns the version-specific env.properties path for the
// selected OWLCMS release.
func GetReleaseEnvPath(releaseVersion string) string {
	return filepath.Join(installDir, strings.TrimSpace(releaseVersion), "env.properties")
}

func trackerConnectionURL(baseURL, port string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s://%s:%s/ws", parsed.Scheme, parsed.Hostname(), strings.TrimSpace(port))
}

func trackerConnectionSettings(value string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Hostname() == "" || parsed.Path != "/ws" {
		return "", "", false
	}
	port := strings.TrimSpace(parsed.Port())
	if port == "" {
		return "", "", false
	}
	return fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Hostname(), parsed.Path), port, true
}

func trackerConnectionPort(value string) (string, bool) {
	_, port, ok := trackerConnectionSettings(value)
	return port, ok
}

func trackerConnectionEnabled(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func defaultTrackerConnectionSettings() (string, string) {
	return defaultTrackerConnectionURL, tracker.GetPort()
}

// SaveDefaultTrackerConnection saves the endpoint independently of whether new
// OWLCMS versions should use it by default.
func SaveDefaultTrackerConnection(baseURL, trackerPort string, enabled bool) error {
	baseURL = strings.TrimSpace(baseURL)
	trackerPort = strings.TrimSpace(trackerPort)
	if trackerConnectionURL(baseURL, trackerPort) == "" {
		return fmt.Errorf("tracker connection URL and port are required")
	}

	enabledValue := "false"
	if enabled {
		enabledValue = "true"
	}
	if err := SaveProperty(trackerConnectionURLSetting, baseURL); err != nil {
		return err
	}
	if err := SaveProperty(trackerConnectionPortSetting, trackerPort); err != nil {
		return err
	}
	if err := SaveProperty(trackerConnectionDefaultEnabledKey, enabledValue); err != nil {
		return err
	}

	connection := ""
	if enabled {
		connection = trackerConnectionURL(baseURL, trackerPort)
	}
	return SaveProperty(trackerConnectionEnv, connection)
}

// ConfigureTrackerConnectionForRelease stores the local tracker websocket URL in
// the same version-specific env.properties used by the interactive options menu.
func ConfigureTrackerConnectionForRelease(releaseVersion, trackerPort string) error {
	return ConfigureTrackerConnectionForReleaseURL(releaseVersion, defaultTrackerConnectionURL, trackerPort)
}

func ConfigureTrackerConnectionForReleaseURL(releaseVersion, baseURL, trackerPort string) error {
	trackerPort = strings.TrimSpace(trackerPort)
	if trackerPort == "" {
		return fmt.Errorf("tracker port is required")
	}
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Hostname() == "" || parsed.Path != "/ws" {
		return fmt.Errorf("tracker connection URL must be a ws or wss URL ending in /ws")
	}
	return SavePropertyForRelease(releaseVersion, trackerConnectionEnv, trackerConnectionURL(baseURL, trackerPort))
}

// DisableTrackerConnectionForRelease writes an explicit blank release override so
// the selected release clears any shared default tracker connection.
func DisableTrackerConnectionForRelease(releaseVersion string) error {
	return SavePropertyForRelease(releaseVersion, trackerConnectionEnv, "")
}

func loadReleaseProperties(releaseVersion string) (*properties.Properties, error) {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return nil, nil
	}

	releaseEnvPath := filepath.Join(installDir, releaseVersion, "env.properties")
	if _, err := os.Stat(releaseEnvPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to check release env.properties: %w", err)
	}

	return loadPropertiesFromFile(releaseEnvPath)
}

// GetTrackerConnectionPortForRelease returns the configured local tracker port
// from OWLCMS_VIDEODATA for the selected release, or empty when disabled.
func GetTrackerConnectionPortForRelease(releaseVersion string) string {
	connectionURL := GetTrackerConnectionURLForRelease(releaseVersion)
	port, ok := trackerConnectionPort(connectionURL)
	if !ok {
		return ""
	}
	return port
}

// GetTrackerConnectionURLForRelease returns the effective tracker WebSocket URL
// configured for a selected OWLCMS release, or an empty string when disabled.
func GetTrackerConnectionURLForRelease(releaseVersion string) string {
	releaseProps, err := loadReleaseProperties(releaseVersion)
	if err == nil && releaseProps != nil {
		if value, ok := releaseProps.Get(trackerConnectionEnv); ok {
			if _, _, valid := trackerConnectionSettings(value); !valid {
				return ""
			}
			return strings.TrimSpace(value)
		}
	}

	merged, err := loadEnvironmentForReleaseProps(releaseVersion)
	if err != nil || merged == nil {
		return ""
	}
	value, ok := merged.Get(trackerConnectionEnv)
	if !ok {
		return ""
	}
	if _, _, valid := trackerConnectionSettings(value); !valid {
		return ""
	}
	return strings.TrimSpace(value)
}

func loadPropertiesFromFile(envFilePath string) (*properties.Properties, error) {
	content, err := os.ReadFile(envFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read env.properties file: %w", err)
	}

	props := properties.NewProperties()
	if err := props.Load(content, properties.UTF8); err != nil {
		return nil, fmt.Errorf("failed to load env.properties file: %w", err)
	}

	log.Printf("Loaded env.properties file: %s", envFilePath)

	return props, nil
}

func getPropertyOrDefault(props *properties.Properties, key, defaultValue string) string {
	if props == nil {
		return defaultValue
	}

	value, ok := props.Get(key)
	if !ok || strings.TrimSpace(value) == "" {
		return defaultValue
	}

	return strings.TrimSpace(value)
}

func getPortFromProperties(props *properties.Properties) string {
	// to change the port to 80 this is where the change would be made
	// the port can be 80 when running on Windows, Pi and macOS; linux would stay at 8080
	return getPropertyOrDefault(props, "OWLCMS_PORT", "8080")
}

func getTemurinVersionFromProperties(props *properties.Properties) string {
	return getPropertyOrDefault(props, "TEMURIN_VERSION", "jdk-25")
}

func ensureReleaseEnvFromCurrentParent(releaseVersion string) (*properties.Properties, error) {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return nil, nil
	}

	releaseEnvPath := filepath.Join(installDir, releaseVersion, "env.properties")
	if err := shared.EnsureDir0755(filepath.Dir(releaseEnvPath)); err != nil {
		return nil, fmt.Errorf("creating release env directory: %w", err)
	}

	var releaseProps *properties.Properties
	releaseExists := false
	if _, err := os.Stat(releaseEnvPath); err == nil {
		releaseExists = true
		loadedProps, loadErr := loadPropertiesFromFile(releaseEnvPath)
		if loadErr != nil {
			return nil, fmt.Errorf("failed to load release env.properties: %w", loadErr)
		}
		releaseProps = loadedProps
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check release env.properties: %w", err)
	} else {
		releaseProps = cloneProperties(environment)
	}

	defaults, defaultComments := defaultOwlcmsProperties()
	updated := !releaseExists
	for _, key := range defaults.Keys() {
		if _, ok := releaseProps.Get(key); !ok {
			value, _ := defaults.Get(key)
			releaseProps.Set(key, value)
			updated = true
		}
	}

	if current, _ := releaseProps.Get("TEMURIN_VERSION"); shouldUpgradeTemurinTo25(current) {
		releaseProps.Set("TEMURIN_VERSION", "jdk-25")
		updated = true
	}

	if !releaseExists {
		if _, ok := releaseProps.Get("TEMURIN_VERSION"); !ok {
			releaseProps.Set("TEMURIN_VERSION", "jdk-25")
		}
	}

	if !updated {
		return releaseProps, nil
	}

	commentBlock := defaultComments
	if releaseExists {
		if content, err := os.ReadFile(releaseEnvPath); err == nil {
			var comments []string
			lines := strings.SplitSeq(string(content), "\n")
			for line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "#") {
					comments = append(comments, line)
				}
			}
			if len(comments) > 0 {
				commentBlock = strings.Join(comments, "\n")
			}
		}
	}
	if commentBlock != "" && !releaseExists {
		lines := strings.Split(commentBlock, "\n")
		if len(lines) > 3 {
			commentBlock = strings.Join(lines[3:], "\n")
		}
	}

	file, err := os.Create(releaseEnvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create release env.properties: %w", err)
	}
	defer file.Close()

	if _, err := releaseProps.Write(file, properties.UTF8); err != nil {
		return nil, fmt.Errorf("failed to write release env.properties: %w", err)
	}
	if commentBlock != "" {
		if _, err := file.WriteString("\n" + commentBlock + "\n"); err != nil {
			log.Printf("Failed to write comments to release env.properties file: %v, but file is usable", err)
		}
	}

	return releaseProps, nil
}

func cloneProperties(src *properties.Properties) *properties.Properties {
	clone := properties.NewProperties()
	if src == nil {
		return clone
	}

	for _, key := range src.Keys() {
		value, _ := src.Get(key)
		clone.Set(key, value)
	}

	return clone
}

func loadEnvironmentForReleaseProps(releaseVersion string) (*properties.Properties, error) {
	processEnv := os.Environ()
	if err := EnsureParentEnvDefaults(); err != nil {
		return nil, err
	}

	merged := cloneProperties(environment)
	releaseProps, err := ensureReleaseEnvFromCurrentParent(releaseVersion)
	if err != nil {
		return nil, err
	}
	if releaseProps != nil {
		for _, key := range releaseProps.Keys() {
			value, _ := releaseProps.Get(key)
			merged.Set(key, value)
		}
	}
	for _, entry := range processEnv {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			if _, exists := merged.Get(key); exists {
				merged.Set(key, value)
			}
		}
	}

	return merged, nil
}

// LoadEnvironmentForRelease loads the shared env.properties and overlays any
// version-specific values from the selected release.
func LoadEnvironmentForRelease(releaseVersion string) error {
	merged, err := loadEnvironmentForReleaseProps(releaseVersion)
	if err != nil {
		return err
	}

	environment = merged
	return nil
}

// GetTemurinVersionForRelease returns the Temurin version for a specific release.
// It first checks the version-specific env.properties file, then falls back to
// the shared env.properties file, and finally to the default value.
func GetTemurinVersionForRelease(releaseVersion string) string {
	if versionProps, err := loadReleaseProperties(releaseVersion); err == nil && versionProps != nil {
		if temurinVersion, ok := versionProps.Get("TEMURIN_VERSION"); ok {
			trimmed := strings.TrimSpace(temurinVersion)
			log.Printf("Using version-specific Temurin %s for owlcms %s", trimmed, releaseVersion)
			return trimmed
		}
	}

	// Fall back to shared env.properties
	temurinVersion := GetTemurinVersion()
	log.Printf("Using shared Temurin %s for owlcms %s", temurinVersion, releaseVersion)
	return temurinVersion
}

func defaultOwlcmsProperties() (*properties.Properties, string) {
	props := properties.NewProperties()
	props.Set("TEMURIN_VERSION", "jdk-25")
	props.Set(shared.RunAsDaemonEnv, "false")

	rawString := `
# Environment variables defined in this file are copied to the installed versions where they
# can be used to configure a given version.

# All the OWLCMS and Java environment variables can be set here
# These are typical only - adjust as needed for your installation
# Remove the leading # to uncomment and set the variable.

#OWLCMS_PORT=8080

#OWLCMS_INITIALDATA=LARGEGROUP_DEMO
#OWLCMS_RESETMODE=true
#OWLCMS_MEMORYMODE=true

# this overrides all the feature toggles in the database (remove the leading # to uncomment)
#OWLCMS_FEATURESWITCHES=interimScores

# java options can be set with this variable (remove the leading # to uncomment)
#JAVA_OPTIONS=-Xmx512m -Xmx512m`

	return props, rawString
}

func shouldUpgradeTemurinTo25(version string) bool {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return true
	}

	major, err := shared.ExtractMajorVersion(trimmed)
	if err != nil {
		return false
	}

	return major < 25
}

// EnsureParentEnvDefaults ensures the shared env.properties exists and contains
// required defaults.
func EnsureParentEnvDefaults() error {
	envFilePath := filepath.Join(installDir, "env.properties")
	if err := InitEnv(); err != nil {
		return err
	}

	defaults, defaultComments := defaultOwlcmsProperties()
	updated := false
	for _, key := range defaults.Keys() {
		if _, ok := environment.Get(key); !ok {
			value, _ := defaults.Get(key)
			environment.Set(key, value)
			updated = true
		}
	}

	if current, _ := environment.Get("TEMURIN_VERSION"); shouldUpgradeTemurinTo25(current) {
		environment.Set("TEMURIN_VERSION", "jdk-25")
		updated = true
	}

	if !updated {
		return nil
	}

	// Preserve existing comment lines if present
	commentBlock := ""
	if content, err := os.ReadFile(envFilePath); err == nil {
		var comments []string
		lines := strings.SplitSeq(string(content), "\n")
		for line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				comments = append(comments, line)
			}
		}
		if len(comments) > 0 {
			commentBlock = strings.Join(comments, "\n")
		}
	}
	if commentBlock == "" {
		commentBlock = defaultComments
	}

	file, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open env.properties for writing: %w", err)
	}
	defer file.Close()

	if _, err := environment.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("failed to write env.properties: %w", err)
	}
	if commentBlock != "" {
		if _, err := file.WriteString("\n" + commentBlock + "\n"); err != nil {
			log.Printf("Failed to write comments to env.properties file: %v, but file is usable", err)
		}
	}

	return nil
}

// EnsureReleaseEnvFromParent ensures a version-specific env.properties exists.
// Existing files are preserved and only missing keys are added.
func EnsureReleaseEnvFromParent(releaseVersion string) error {
	if err := EnsureParentEnvDefaults(); err != nil {
		return err
	}
	_, err := ensureReleaseEnvFromCurrentParent(releaseVersion)
	return err
}

// GetInstallDir returns the installation directory
func GetInstallDir() string {
	return installDir
}

// GetEnvironment returns the environment properties
func GetEnvironment() *properties.Properties {
	return environment
}

// InitEnv initializes the environment properties from env.properties
func InitEnv() error {
	envFilePath := filepath.Join(installDir, "env.properties")
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		log.Printf("env.properties file not found at %s, creating with default values", envFilePath)

		if err := shared.EnsureDir0755(installDir); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", installDir, err)
		}

		props := properties.NewProperties()
		props.Set("TEMURIN_VERSION", "jdk-25")
		props.Set(shared.RunAsDaemonEnv, "false")

		file, err := os.Create(envFilePath)
		if err != nil {
			return fmt.Errorf("failed to create env.properties file: %w", err)
		}
		defer file.Close()

		if _, err := props.Write(file, properties.UTF8); err != nil {
			return fmt.Errorf("failed to write env.properties file: %w", err)
		}

		rawString := `# Add any environment variable you need. (remove the leading # to uncomment)
#OWLCMS_INITIALDATA=LARGEGROUP_DEMO
#OWLCMS_RESETMODE=true
#OWLCMS_MEMORYMODE=true

# this overrides all the feature toggles in the database (remove the leading # to uncomment)
#OWLCMS_FEATURESWITCHES=interimScores

# java options can be set with this variable (remove the leading # to uncomment)
#JAVA_OPTIONS=-Xmx512m -Xmx512m

# set to true on Linux to leave OWLCMS and Tracker running after the control panel exits
#CONTROLPANEL_RUN_AS_DAEMON=false`

		if _, err := file.WriteString(rawString); err != nil {
			log.Printf("Failed to write comments to env.properties file: %v, but file is usable", err)
		}

		log.Printf("Successfully created env.properties file at %s", envFilePath)
	}

	return loadProperties(envFilePath)
}

func loadProperties(envFilePath string) error {
	loaded, err := loadPropertiesFromFile(envFilePath)
	if err != nil {
		return err
	}
	environment = loaded

	if err := shared.SetRunAsDaemonEnabled(GetRunAsDaemon()); err != nil {
		return fmt.Errorf("failed to sync daemon setting to process environment: %w", err)
	}

	return nil
}

// SaveProperty saves a key-value pair to env.properties and reloads the environment
func SaveProperty(key, value string) error {
	envFilePath := filepath.Join(installDir, "env.properties")

	// Ensure environment is loaded
	if environment == nil {
		if err := InitEnv(); err != nil {
			return fmt.Errorf("failed to initialize environment: %w", err)
		}
	}
	if key == "OWLCMS_PORT" {
		for _, releaseVersion := range getAllInstalledVersions() {
			if err := EnsureReleaseEnvFromParent(releaseVersion); err != nil {
				return fmt.Errorf("preserving configuration for version %s: %w", releaseVersion, err)
			}
		}
	}

	// Set the property
	environment.Set(key, value)

	// Write back to file
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

// SavePropertyForRelease saves a key-value pair to a version-specific
// env.properties file, creating it from parent defaults if needed.
func SavePropertyForRelease(releaseVersion, key, value string) error {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return fmt.Errorf("release version is required")
	}

	if err := EnsureReleaseEnvFromParent(releaseVersion); err != nil {
		return fmt.Errorf("failed to initialize release env.properties: %w", err)
	}

	envFilePath := filepath.Join(installDir, releaseVersion, "env.properties")
	props := properties.NewProperties()
	content, err := os.ReadFile(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to read release env.properties: %w", err)
	}

	if err := props.Load(content, properties.UTF8); err != nil {
		return fmt.Errorf("failed to load release env.properties: %w", err)
	}

	props.Set(key, value)

	file, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open release env.properties for writing: %w", err)
	}
	defer file.Close()

	if _, err := props.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("failed to write release env.properties: %w", err)
	}

	log.Printf("Saved property %s = %s to %s", key, value, envFilePath)
	return nil
}

// DeletePropertyForRelease removes a key from a version-specific env.properties
// file, creating the file from parent defaults if needed.
func DeletePropertyForRelease(releaseVersion, key string) error {
	releaseVersion = strings.TrimSpace(releaseVersion)
	if releaseVersion == "" {
		return fmt.Errorf("release version is required")
	}

	if err := EnsureReleaseEnvFromParent(releaseVersion); err != nil {
		return fmt.Errorf("failed to initialize release env.properties: %w", err)
	}

	envFilePath := filepath.Join(installDir, releaseVersion, "env.properties")
	props := properties.NewProperties()
	content, err := os.ReadFile(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to read release env.properties: %w", err)
	}

	if err := props.Load(content, properties.UTF8); err != nil {
		return fmt.Errorf("failed to load release env.properties: %w", err)
	}

	props.Delete(key)

	file, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open release env.properties for writing: %w", err)
	}
	defer file.Close()

	if _, err := props.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("failed to write release env.properties: %w", err)
	}

	log.Printf("Deleted property %s from %s", key, envFilePath)
	return nil
}

// DeleteProperty removes a key from env.properties and reloads the environment
func DeleteProperty(key string) error {
	envFilePath := filepath.Join(installDir, "env.properties")

	// Ensure environment is loaded
	if environment == nil {
		if err := InitEnv(); err != nil {
			return fmt.Errorf("failed to initialize environment: %w", err)
		}
	}

	// Delete the property
	environment.Delete(key)

	// Write back to file
	file, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open env.properties for writing: %w", err)
	}
	defer file.Close()

	if _, err := environment.Write(file, properties.UTF8); err != nil {
		return fmt.Errorf("failed to write env.properties: %w", err)
	}

	log.Printf("Deleted property %s from %s", key, envFilePath)
	return nil
}

// GetTrackerConnectionEnabled returns whether new OWLCMS versions use the default endpoint.
func GetTrackerConnectionEnabled() bool {
	_, _, enabled := GetTrackerConnectionSettings()
	return enabled
}

// GetTrackerConnectionPort returns the configured default tracker port.
func GetTrackerConnectionPort() string {
	_, port, _ := GetTrackerConnectionSettings()
	return port
}

// GetTrackerConnectionSettings returns the configured default tracker URL and
// port, plus whether new OWLCMS versions use that endpoint by default.
func GetTrackerConnectionSettings() (string, string, bool) {
	defaultURL, defaultPort := defaultTrackerConnectionSettings()
	if environment == nil {
		return defaultURL, defaultPort, false
	}

	url := getPropertyOrDefault(environment, trackerConnectionURLSetting, defaultURL)
	port := getPropertyOrDefault(environment, trackerConnectionPortSetting, defaultPort)
	if value, ok := environment.Get(trackerConnectionDefaultEnabledKey); ok {
		return url, port, trackerConnectionEnabled(value)
	}

	// Preserve existing installations where OWLCMS_VIDEODATA was both the
	// configured default endpoint and the enable switch.
	if value, ok := environment.Get(trackerConnectionEnv); ok {
		if legacyURL, legacyPort, valid := trackerConnectionSettings(value); valid {
			return legacyURL, legacyPort, true
		}
	}

	return url, port, false
}

// GetTrackerConnectionEnabledForRelease returns true if the version-specific
// effective environment points OWLCMS_VIDEODATA to a local tracker websocket.
func GetTrackerConnectionEnabledForRelease(releaseVersion string) bool {
	return GetTrackerConnectionPortForRelease(releaseVersion) != ""
}

// CheckForUpdates checks for updates to the control panel itself
func CheckForUpdates(win fyne.Window, showConfirmation bool) {
	currentVersion := shared.GetLauncherVersion()
	if len(currentVersion) > 0 && currentVersion[0] == 'v' {
		currentVersion = currentVersion[1:]
	}
	// Normalize SNAPSHOT to lowercase so it sorts after rc/alpha/beta in semver comparison
	currentVersion = strings.ReplaceAll(currentVersion, "-SNAPSHOT", "-snapshot")

	currentSemver, currentErr := semver.NewVersion(currentVersion)
	if currentErr != nil {
		log.Printf("Failed to parse current version string: %v", currentErr)
		return
	}

	isCurrentPrerelease := currentSemver.Prerelease() != ""

	// If current is a prerelease, check all releases; otherwise just check latest stable
	var repoURL string
	if isCurrentPrerelease {
		repoURL = "https://api.github.com/repos/owlcms/owlcms-controlpanel/releases"
	} else {
		repoURL = "https://api.github.com/repos/owlcms/owlcms-controlpanel/releases/latest"
	}
	log.Println("Checking for updates from:", repoURL)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, repoURL, nil)
	if err != nil {
		log.Printf("Failed to build update request: %v", err)
		return
	}
	req.Header.Set("User-Agent", "controlpanel")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to check for updates: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to check for updates: received status code %d", resp.StatusCode)
		if resp.StatusCode == http.StatusForbidden {
			log.Printf("GitHub rate limit: limit=%s remaining=%s reset=%s", resp.Header.Get("X-RateLimit-Limit"), resp.Header.Get("X-RateLimit-Remaining"), resp.Header.Get("X-RateLimit-Reset"))
		}
		return
	}

	type releaseInfo struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	var newerRelease *releaseInfo
	var newerSemver *semver.Version

	if isCurrentPrerelease {
		// Parse array of releases and find the newest one (stable or prerelease) that's newer than current
		var releases []releaseInfo
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			log.Printf("Failed to parse update information: %v", err)
			return
		}

		for _, release := range releases {
			remoteVersion := release.TagName
			if len(remoteVersion) > 0 && remoteVersion[0] == 'v' {
				remoteVersion = remoteVersion[1:]
			}
			// Normalize SNAPSHOT to lowercase so it sorts after rc/alpha/beta in semver comparison
			remoteVersion = strings.ReplaceAll(remoteVersion, "-SNAPSHOT", "-snapshot")

			remoteSemver, remoteErr := semver.NewVersion(remoteVersion)
			if remoteErr != nil {
				continue
			}

			if remoteSemver.GreaterThan(currentSemver) {
				if newerSemver == nil || remoteSemver.GreaterThan(newerSemver) {
					newerSemver = remoteSemver
					releaseCopy := release
					newerRelease = &releaseCopy
				}
			}
		}
	} else {
		// Just check latest stable release
		var release releaseInfo
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			log.Printf("Failed to parse update information: %v", err)
			return
		}

		remoteVersion := release.TagName
		if len(remoteVersion) > 0 && remoteVersion[0] == 'v' {
			remoteVersion = remoteVersion[1:]
		}
		// Normalize SNAPSHOT to lowercase so it sorts after rc/alpha/beta in semver comparison
		remoteVersion = strings.ReplaceAll(remoteVersion, "-SNAPSHOT", "-snapshot")

		remoteSemver, remoteErr := semver.NewVersion(remoteVersion)
		if remoteErr != nil {
			log.Printf("Failed to parse version strings for comparison: %v", remoteErr)
			return
		}

		if remoteSemver.GreaterThan(currentSemver) {
			newerSemver = remoteSemver
			newerRelease = &release
		}
	}

	// Check if we found a newer version
	if newerRelease != nil && newerSemver != nil {
		log.Printf("Update available: %s -> %s", currentSemver, newerSemver)

		parsedURL, err := url.Parse(newerRelease.HTMLURL)
		if err != nil {
			log.Printf("Failed to parse release URL: %v", err)
			return
		}
		releaseNotesLink := widget.NewHyperlink("Release Notes", parsedURL)
		installerLink := widget.NewHyperlink("Download the Installer", parsedURL)
		links := container.NewHBox(installerLink, releaseNotesLink)
		content := container.NewVBox(
			widget.NewLabel(fmt.Sprintf("A new version (%s) is available. You are currently using version %s.\nYou can simply download the new installer and install over the current version.", newerRelease.TagName, shared.GetLauncherVersion())),
			links,
		)
		dialog.ShowCustom("Update Available", "Close", content, win)
	} else {
		log.Println("No updates available - the latest version is installed.")
		if showConfirmation {
			releasesURL, _ := url.Parse("https://github.com/owlcms/owlcms-controlpanel/releases")
			releasesLink := widget.NewHyperlink("GitHub Releases", releasesURL)
			content := container.NewVBox(
				widget.NewLabel(fmt.Sprintf("The latest version is installed (%s)", shared.GetLauncherVersion())),
				releasesLink,
			)
			dialog.ShowCustom("No Updates", "Close", content, win)
		}
	}
}
