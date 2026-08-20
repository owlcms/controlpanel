package shared

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const videoConfigSubdir = "video_config"

// VideoConfigDir returns the shared ffmpeg config directory under the control panel.
// Layout: <controlpanel>/video_config/ffmpeg/ffmpeg.toml
func VideoConfigDir(versionDir string) string {
	_ = versionDir
	return filepath.Join(GetControlPanelInstallDir(), videoConfigSubdir, "ffmpeg")
}

// BuildVideoLaunchEnv builds the shared env passed to replays/cameras child processes.
func BuildVideoLaunchEnv(versionDir string) []string {
	configDir := VideoConfigDir(versionDir)
	env := NormalizeChildProcessEnv(os.Environ())
	env = UpsertEnv(env, "VIDEO_CONFIGDIR", configDir)
	env = UpsertEnv(env, "VIDEO_CONTROLPANEL_DIR", GetControlPanelInstallDir())
	env = UpsertEnv(env, "VIDEO_LAUNCHER", GetLauncherVersionSemver())
	env = UpsertEnv(env, "OWLCMS_CONTROLPANEL", GetLauncherVersionSemver())

	// Export the FFmpeg path so child processes find it directly.
	if ffmpegPath := FindFFmpeg(); ffmpegPath != "" {
		env = UpsertEnv(env, "VIDEO_FFMPEG_PATH", ffmpegPath)
		// For Linux shared builds, prepend the bundled lib/ to LD_LIBRARY_PATH.
		if GetGoos() == "linux" {
			libDir := filepath.Join(filepath.Dir(filepath.Dir(ffmpegPath)), "lib")
			if st, err := os.Stat(libDir); err == nil && st.IsDir() {
				if existing := os.Getenv("LD_LIBRARY_PATH"); existing != "" {
					env = UpsertEnv(env, "LD_LIBRARY_PATH", fmt.Sprintf("%s:%s", libDir, existing))
				} else {
					env = UpsertEnv(env, "LD_LIBRARY_PATH", libDir)
				}
			}
		}
	}

	parentEnvPath := filepath.Join(filepath.Dir(versionDir), "env.properties")
	releaseEnvPath := filepath.Join(versionDir, "env.properties")
	if props, err := MergeEnvironmentProperties(parentEnvPath, releaseEnvPath); err == nil {
		env = ApplyPropertiesToEnv(env, props, nil)
	}

	return env
}

// ShouldRunVideoExtract determines whether launchers should run --extractConfig preflight.
// app must be "replays" or "cameras".
// FFmpeg availability is handled separately by EnsureFFmpegPrerequisite.
// Always runs extract so that shared ffmpeg.toml is updated to the current embedded version on upgrade.
// Each app's extractor is idempotent and only creates per-instance config.toml when missing.
func ShouldRunVideoExtract(versionDir, app string) bool {
	// Always extract: ffmpeg.toml is refreshed when embedded content changes.
	// Per-instance config.toml remains app-owned and is written only if missing.
	_ = app
	return true
}

// RunVideoExtractBootstrap runs the app once with --extractConfig and shared launcher env.
// --configDir points to the version directory for per-instance configs;
// ffmpeg.toml is written to the shared VIDEO_CONFIGDIR set in the environment.
func RunVideoExtractBootstrap(exePath, versionDir string) error {
	cmd := exec.Command(exePath, "--extractConfig", "--configDir", versionDir)
	cmd.Dir = versionDir
	cmd.Env = BuildVideoLaunchEnv(versionDir)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract bootstrap failed: %w (%s)", err, string(output))
	}

	return nil
}

// ReadTopLevelTOMLValue reads a simple top-level TOML key without pulling in a full TOML parser.
func ReadTopLevelTOMLValue(filePath, key string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != key {
			continue
		}

		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		return value, nil
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", nil
}
