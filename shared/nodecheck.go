package shared

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// NodeRelease represents a Node.js release from the official API
type NodeRelease struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Files   []string `json:"files"`
	LTS     any      `json:"lts"` // Can be false or a string like "Jod"
}

// ExtractNodeVersion parses a version string and returns major, minor, patch
// Handles formats like "v20.11.0" or "node-v20.11.0-linux-x64"
func ExtractNodeVersion(versionString string) (int, int, int, error) {
	// Remove common prefixes
	version := strings.TrimPrefix(versionString, "node-")
	version = strings.TrimPrefix(version, "v")

	// Remove platform/arch suffix if present (e.g., "-linux-x64")
	// Split by dash and take only the version part
	parts := strings.Split(version, "-")
	versionPart := parts[0]

	// Split version into components
	components := strings.Split(versionPart, ".")
	if len(components) < 1 {
		return 0, 0, 0, fmt.Errorf("invalid version format: %s", versionString)
	}

	major, err := strconv.Atoi(components[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parsing major version: %w", err)
	}

	minor := 0
	if len(components) > 1 {
		minor, _ = strconv.Atoi(components[1])
	}

	patch := 0
	if len(components) > 2 {
		patch, _ = strconv.Atoi(components[2])
	}

	return major, minor, patch, nil
}

// CompareNodeVersions returns true if a is newer than b
func CompareNodeVersions(a, b string) bool {
	aMajor, aMinor, aPatch, aErr := ExtractNodeVersion(a)
	bMajor, bMinor, bPatch, bErr := ExtractNodeVersion(b)

	if aErr != nil || bErr != nil {
		return a > b
	}

	if aMajor != bMajor {
		return aMajor > bMajor
	}
	if aMinor != bMinor {
		return aMinor > bMinor
	}
	return aPatch > bPatch
}

// findNodeExecutable recursively searches for the node executable in a directory
// Skips node_modules directories to avoid searching through npm packages
func findNodeExecutable(baseDir, nodeExe string) string {
	// Check if the executable exists at the current level
	nodePath := filepath.Join(baseDir, nodeExe)
	if _, err := os.Stat(nodePath); err == nil {
		return nodePath
	}

	// Recursively search subdirectories
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Skip node_modules directories
			if entry.Name() == "node_modules" {
				continue
			}

			found := findNodeExecutable(filepath.Join(baseDir, entry.Name()), nodeExe)
			if found != "" {
				return found
			}
		}
	}

	return ""
}

// FindLocalNodeForVersion finds a local Node.js installation
// If version is specified, finds a version that meets the minimum requirement
// If version is empty, returns the highest available version
func FindLocalNodeForVersion(version string, goosFunc func() string) (string, error) {
	goos := goosFunc()

	// Extract required version if specified
	var requiredMajor, requiredMinor, requiredPatch int
	var err error
	if version != "" {
		requiredMajor, requiredMinor, requiredPatch, err = ExtractNodeVersion(version)
		if err != nil {
			log.Printf("*** Warning: Could not extract version from %s, using any available Node: %v\n", version, err)
			requiredMajor = 0
		}
	}

	runtimeDir := GetRuntimeDir()
	nodeBaseDir := filepath.Join(runtimeDir, "node")

	if _, err := os.Stat(nodeBaseDir); err != nil {
		log.Printf("*** Node base directory not found: %v\n", err)
		return "", fmt.Errorf("node base directory not found at %s", nodeBaseDir)
	}

	// Get all version directories
	versionDirs, err := os.ReadDir(nodeBaseDir)
	if err != nil {
		log.Printf("*** Error reading node base directory: %v\n", err)
		return "", fmt.Errorf("reading node base directory: %w", err)
	}

	var nodeInstalls []struct {
		versionDir string
		nodePath   string
		fullPath   string
		major      int
		minor      int
		patch      int
	}

	var nodeExe string
	if goos == "windows" {
		nodeExe = "node.exe"
	} else {
		nodeExe = "node"
	}

	for _, versionDir := range versionDirs {
		if !versionDir.IsDir() {
			continue
		}

		versionPath := filepath.Join(nodeBaseDir, versionDir.Name())

		// Look for node-v*-platform-arch directories
		entries, err := os.ReadDir(versionPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "node-") {
				continue
			}

			// Extract version from directory name
			major, minor, patch, err := ExtractNodeVersion(versionDir.Name())
			if err != nil {
				continue
			}

			// Search for node executable recursively in this directory
			extractedPath := filepath.Join(versionPath, entry.Name())
			nodePath := findNodeExecutable(extractedPath, nodeExe)

			if nodePath != "" {
				nodeInstalls = append(nodeInstalls, struct {
					versionDir string
					nodePath   string
					fullPath   string
					major      int
					minor      int
					patch      int
				}{
					versionDir: versionDir.Name(),
					nodePath:   entry.Name(),
					fullPath:   nodePath,
					major:      major,
					minor:      minor,
					patch:      patch,
				})
			}
		}
	}

	if len(nodeInstalls) == 0 {
		log.Printf("*** No Node.js installations found in %s\n", nodeBaseDir)
		return "", fmt.Errorf("no Node.js installations found in %s", nodeBaseDir)
	}

	// Sort by version (highest first)
	sort.Slice(nodeInstalls, func(i, j int) bool {
		return CompareNodeVersions(nodeInstalls[i].versionDir, nodeInstalls[j].versionDir)
	})

	// Find the best match: highest version that meets minimum requirement
	for _, install := range nodeInstalls {
		if requiredMajor == 0 ||
			install.major > requiredMajor ||
			(install.major == requiredMajor && install.minor > requiredMinor) ||
			(install.major == requiredMajor && install.minor == requiredMinor && install.patch >= requiredPatch) {

			log.Printf("*** Found suitable Node.js %s (>= required %s) at: %s\n",
				install.versionDir, version, install.fullPath)
			return install.fullPath, nil
		}
	}

	// If we get here, no suitable version was found
	if requiredMajor > 0 {
		log.Printf("*** No Node.js installation found with version >= %s (found: %d installations)\n", version, len(nodeInstalls))
		return "", fmt.Errorf("no Node.js installation found with version >= %s", version)
	}

	// If no version specified, return the highest available
	log.Printf("*** Found Node.js at: %s\n", nodeInstalls[0].fullPath)
	return nodeInstalls[0].fullPath, nil
}

// FindLatestNodeRelease fetches the latest Node.js LTS release from the official API
func FindLatestNodeRelease(version string) (string, error) {
	url := "https://nodejs.org/dist/index.json"

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Node.js releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unexpected status %d from Node.js API", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var releases []NodeRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("invalid response format: %w", err)
	}

	if len(releases) == 0 {
		return "", fmt.Errorf("no Node.js releases found")
	}

	// Find the latest LTS version
	for _, release := range releases {
		if release.LTS != false && release.LTS != nil {
			log.Printf("Found latest Node.js LTS: %s\n", release.Version)
			return release.Version, nil
		}
	}

	// If no LTS found, return the latest stable
	log.Printf("No LTS found, using latest: %s\n", releases[0].Version)
	return releases[0].Version, nil
}

// scanEnvPropertiesForNodeVersions scans all env.properties files in tracker directories
// and returns all unique NODE_VERSION values found
func scanEnvPropertiesForNodeVersions(trackerInstallDir string) ([]string, error) {
	var versions []string
	seenVersions := make(map[string]bool)

	// Helper function to read NODE_VERSION from a properties file
	readVersion := func(path string) (string, error) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "", nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		// Simple parsing - look for NODE_VERSION=value
		lines := strings.SplitSeq(string(content), "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "NODE_VERSION="); ok {
				version := after
				return strings.TrimSpace(version), nil
			}
		}
		return "", nil
	}

	// Scan tracker directory
	trackerEnvPath := filepath.Join(trackerInstallDir, "env.properties")
	if version, err := readVersion(trackerEnvPath); err == nil && version != "" {
		if !seenVersions[version] {
			versions = append(versions, version)
			seenVersions[version] = true
		}
	}

	// Scan tracker version-specific directories
	if entries, err := os.ReadDir(trackerInstallDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				versionEnvPath := filepath.Join(trackerInstallDir, entry.Name(), "env.properties")
				if version, err := readVersion(versionEnvPath); err == nil && version != "" {
					if !seenVersions[version] {
						versions = append(versions, version)
						seenVersions[version] = true
					}
				}
			}
		}
	}

	return versions, nil
}

// DownloadAndInstallNode downloads and installs a Node.js version
func DownloadAndInstallNode(version string, progressCallback func(downloaded, total int64)) (string, error) {
	goos := GetGoos()
	goarch := GetGoarch()

	// Normalize version (ensure it starts with 'v')
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	// Map to Node.js platform names
	var platform string
	switch goos {
	case "windows":
		platform = "win"
	case "darwin":
		platform = "darwin"
	case "linux":
		platform = "linux"
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}

	// Map architecture
	var arch string
	switch goarch {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	case "arm":
		arch = "armv7l"
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}

	// Determine file extension
	var ext string
	if goos == "windows" {
		ext = "zip"
	} else {
		ext = "tar.gz"
	}

	nodeDirName := fmt.Sprintf("node-%s-%s-%s", version, platform, arch)
	downloadURL := fmt.Sprintf("https://nodejs.org/dist/%s/%s.%s", version, nodeDirName, ext)

	log.Printf("Downloading Node.js from: %s\n", downloadURL)

	// Prepare directories
	runtimeDir := GetRuntimeDir()
	nodeBaseDir := filepath.Join(runtimeDir, "node", version)

	if err := os.MkdirAll(nodeBaseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create node directory: %w", err)
	}

	// Download the archive
	archivePath := filepath.Join(nodeBaseDir, fmt.Sprintf("node.%s", ext))
	if err := DownloadArchive(downloadURL, archivePath, progressCallback, nil); err != nil {
		return "", fmt.Errorf("failed to download Node.js: %w", err)
	}

	log.Printf("Downloaded Node.js to: %s\n", archivePath)

	// Extract the archive
	if ext == "zip" {
		if err := ExtractZip(archivePath, nodeBaseDir); err != nil {
			return "", fmt.Errorf("failed to extract Node.js: %w", err)
		}
	} else {
		if err := ExtractTarGz(archivePath, nodeBaseDir); err != nil {
			return "", fmt.Errorf("failed to extract Node.js: %w", err)
		}
	}

	// Remove the archive
	os.Remove(archivePath)

	// Find the node executable recursively in the extracted directory
	var nodeExe string
	if goos == "windows" {
		nodeExe = "node.exe"
	} else {
		nodeExe = "node"
	}

	extractedDir := filepath.Join(nodeBaseDir, nodeDirName)
	nodePath := findNodeExecutable(extractedDir, nodeExe)

	if nodePath == "" {
		return "", fmt.Errorf("no Node.js executable found in extracted directory: %s", extractedDir)
	}

	// Make it executable on Unix systems
	if goos != "windows" {
		os.Chmod(nodePath, 0755)
	}

	log.Printf("Successfully installed Node.js at: %s\n", nodePath)
	return nodePath, nil
}

// CleanupObsoleteNodeVersions scans for NODE_VERSION requirements, ensures they're met,
// then removes older control panel Node versions and bundled Node from tracker releases
func CleanupObsoleteNodeVersions(statusLabel *widget.Label, w fyne.Window) ([]string, error) {
	var removed []string

	// Step 1: Find all required Node versions from env.properties files in control panel structure
	controlPanelTrackerDir := GetTrackerInstallDir()
	requiredVersions, err := scanEnvPropertiesForNodeVersions(controlPanelTrackerDir)
	if err != nil {
		return nil, fmt.Errorf("scanning env.properties files: %w", err)
	}

	// Step 2: Determine the highest major.minor.patch version required
	var highestMajor, highestMinor, highestPatch int
	for _, version := range requiredVersions {
		major, minor, patch, err := ExtractNodeVersion(version)
		if err != nil {
			continue
		}
		if major > highestMajor ||
			(major == highestMajor && minor > highestMinor) ||
			(major == highestMajor && minor == highestMinor && patch > highestPatch) {
			highestMajor = major
			highestMinor = minor
			highestPatch = patch
		}
	}

	// Step 3: Check if we have a Node version that meets requirements in control panel
	nodeBaseDir := filepath.Join(GetRuntimeDir(), "node")
	hasRequiredNode := false
	if _, err := os.Stat(nodeBaseDir); err == nil {
		if entries, err := os.ReadDir(nodeBaseDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					major, minor, patch, err := ExtractNodeVersion(entry.Name())
					if err == nil &&
						(major > highestMajor ||
							(major == highestMajor && minor > highestMinor) ||
							(major == highestMajor && minor == highestMinor && patch >= highestPatch)) {
						hasRequiredNode = true
						break
					}
				}
			}
		}
	}

	// Step 4: If we don't have required Node, download the latest LTS
	if !hasRequiredNode && (highestMajor > 0 || len(requiredVersions) == 0) {
		if statusLabel != nil {
			statusLabel.SetText("Downloading required Node.js version...")
			statusLabel.Refresh()
		}

		var targetVersion string
		if highestMajor > 0 {
			targetVersion = fmt.Sprintf("v%d.%d.%d", highestMajor, highestMinor, highestPatch)
		} else {
			// No requirements found, get latest LTS
			targetVersion, err = FindLatestNodeRelease("")
			if err != nil {
				return nil, fmt.Errorf("finding latest Node.js release: %w", err)
			}
		}

		// Download the required version
		_, err = DownloadAndInstallNode(targetVersion, nil)
		if err != nil {
			return nil, fmt.Errorf("downloading Node.js %s: %w", targetVersion, err)
		}
	}

	// Step 5: Clean up versioned Node installations in control panel
	if _, err := os.Stat(nodeBaseDir); err == nil {
		versionDirs, err := os.ReadDir(nodeBaseDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read node directory: %w", err)
		}

		if len(versionDirs) > 1 {
			// Collect versions
			var versions []string
			for _, dir := range versionDirs {
				if dir.IsDir() {
					versions = append(versions, dir.Name())
				}
			}

			// Sort to find the latest
			sort.Slice(versions, func(i, j int) bool {
				return CompareNodeVersions(versions[i], versions[j])
			})

			// Keep the first (latest), remove the rest
			for _, version := range versions[1:] {
				versionPath := filepath.Join(nodeBaseDir, version)

				if statusLabel != nil {
					statusLabel.SetText(fmt.Sprintf("Removing Node.js %s...", version))
					statusLabel.Refresh()
				}

				log.Printf("Removing Node.js version: %s\n", version)
				if err := os.RemoveAll(versionPath); err != nil {
					log.Printf("Warning: failed to remove %s: %v\n", version, err)
				} else {
					removed = append(removed, fmt.Sprintf("Node.js %s", version))
				}
			}
		}
	}

	// Second, remove bundled node.exe/node files from tracker release directories
	trackerBaseDir := GetTrackerInstallDir()
	if _, err := os.Stat(trackerBaseDir); err == nil {
		trackerVersions, err := os.ReadDir(trackerBaseDir)
		if err == nil {
			goos := GetGoos()
			var nodeExe string
			if goos == "windows" {
				nodeExe = "node.exe"
			} else {
				nodeExe = "node"
			}

			for _, dir := range trackerVersions {
				if !dir.IsDir() {
					continue
				}

				versionPath := filepath.Join(trackerBaseDir, dir.Name())

				// Use defensive search to find any bundled Node executable
				nodePath := findNodeExecutable(versionPath, nodeExe)
				if nodePath != "" {
					if statusLabel != nil {
						statusLabel.SetText(fmt.Sprintf("Removing bundled Node from tracker %s...", dir.Name()))
						statusLabel.Refresh()
					}

					log.Printf("Removing bundled %s from tracker %s at: %s\n", nodeExe, dir.Name(), nodePath)
					if err := os.Remove(nodePath); err != nil {
						log.Printf("Warning: failed to remove %s: %v\n", nodePath, err)
					} else {
						removed = append(removed, fmt.Sprintf("Bundled Node from tracker %s", dir.Name()))
					}
				}
			}
		}
	}

	return removed, nil
}
