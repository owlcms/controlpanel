package video

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"controlpanel/shared"

	"github.com/magiconair/properties"
)

func getInstallDir() string {
	switch shared.GetGoos() {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "owlcms-video")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "owlcms-video")
	case "linux":
		return filepath.Join(os.Getenv("HOME"), ".local", "share", "owlcms-video")
	default:
		return "./owlcms-video"
	}
}

// GetInstallDir returns the video installation directory
func GetInstallDir() string {
	return getInstallDir()
}

// InitEnv creates or loads env.properties in the video install directory
func InitEnv() error {
	log.Println("Initializing video environment")
	envFilePath := filepath.Join(installDir, "env.properties")
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		log.Printf("env.properties not found at %s, creating with defaults", envFilePath)

		if err := shared.EnsureDir0755(installDir); err != nil {
			return err
		}

		props := properties.NewProperties()
		props.Set("VIDEO_CONFIGDIR", filepath.Join(shared.GetControlPanelInstallDir(), "video_config", "ffmpeg"))

		file, err := os.Create(envFilePath)
		if err != nil {
			return err
		}
		defer file.Close()

		if _, err := props.Write(file, properties.UTF8); err != nil {
			return err
		}
		log.Printf("Created env.properties at %s", envFilePath)
	}

	if _, err := properties.LoadFile(envFilePath, properties.UTF8); err != nil {
		return err
	}
	return nil
}

// getPortForRelease reads the replays web port from the version's replays.toml.
func getPortForRelease(version string) string {
	if strings.TrimSpace(version) == "" {
		return ""
	}

	configPath := filepath.Join(installDir, version, "replays.toml")
	value, err := shared.ReadTopLevelTOMLValue(configPath, "port")
	if err != nil {
		log.Printf("Failed to read Video port from %s: %v", configPath, err)
		return ""
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	portNum, err := strconv.Atoi(value)
	if err != nil || portNum < 1 || portNum > 65535 {
		log.Printf("Invalid Video port %q in %s", value, configPath)
		return ""
	}

	return strconv.Itoa(portNum)
}

func runtimeVideoPort() string {
	if port := getPortForRelease(videoVersion); port != "" {
		return port
	}

	seen := make(map[string]struct{})
	for _, version := range getAllInstalledVersions() {
		port := getPortForRelease(version)
		if port == "" {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}

		portNum, err := strconv.Atoi(port)
		if err != nil {
			continue
		}
		pid, err := shared.FindPIDByPort(portNum)
		if err == nil && pid > 0 {
			return port
		}
	}

	return ""
}
