package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TemurinRelease represents a GitHub release for Temurin
type TemurinRelease struct {
	Name    string `json:"name"`
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// ExtractMajorVersion extracts the major version number from a temurin version string
// e.g., "jdk-17.0.15+6" -> 17, "jdk-25" -> 25
func ExtractMajorVersion(version string) (int, error) {
	// Remove "jdk-" prefix if present
	version = strings.TrimPrefix(version, "jdk-")
	// Split by "." and "+" to get the major version
	parts := strings.Split(strings.Split(version, "+")[0], ".")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid version string: %s", version)
	}
	majorVersion, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("parsing major version: %w", err)
	}
	return majorVersion, nil
}

// CompareJDKVersions compares two jdk directory names and returns true if a is more recent than b
func CompareJDKVersions(a, b string) bool {
	// Extract version numbers from directory names (e.g., "jdk-17.0.9+9" -> "17.0.9")
	aVersion := strings.TrimPrefix(a, "jdk-")
	bVersion := strings.TrimPrefix(b, "jdk-")

	// Split into components
	aParts := strings.Split(strings.Split(aVersion, "+")[0], ".")
	bParts := strings.Split(strings.Split(bVersion, "+")[0], ".")

	// Compare each component
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aNum, _ := strconv.Atoi(aParts[i])
		bNum, _ := strconv.Atoi(bParts[i])
		if aNum != bNum {
			return aNum > bNum
		}
	}
	return len(aParts) > len(bParts)
}

// FindLocalJavaForVersion finds local Java installation for a specific Temurin version
// It scans ALL Java installations in the control panel and returns the best match:
// - Any Java with major version >= required major version
// - Prefers the highest available version
// goosFunc should return the OS string (windows, darwin, linux)
func FindLocalJavaForVersion(temurinVersion string, goosFunc func() string) (string, error) {
	// Extract the required major version from temurinVersion
	requiredMajor, err := ExtractMajorVersion(temurinVersion)
	if err != nil {
		log.Printf("*** Warning: Could not extract major version from %s, using any available Java: %v\n", temurinVersion, err)
		requiredMajor = 0 // Use any version if we can't parse
	}

	// Scan all Java installations in the shared runtime directory
	runtimeDir := GetRuntimeDir()
	javaBaseDir := filepath.Join(runtimeDir, "java")

	if _, err := os.Stat(javaBaseDir); err != nil {
		log.Printf("*** Java base directory not found: %v\n", err)
		return "", fmt.Errorf("java base directory not found at %s", javaBaseDir)
	}

	// Get all version directories (jdk-17.0.15+6, jdk-25, etc.)
	versionDirs, err := os.ReadDir(javaBaseDir)
	if err != nil {
		log.Printf("*** Error reading java base directory: %v\n", err)
		return "", fmt.Errorf("reading java base directory: %w", err)
	}

	// Collect all available Java installations with their paths and versions
	type javaInstall struct {
		versionDir string
		jdkDir     string
		majorVer   int
		fullPath   string
	}
	var installations []javaInstall

	goos := goosFunc()
	var javaExe string
	if goos == "windows" && !IsWSL() {
		javaExe = "javaw.exe"
	} else {
		javaExe = "java"
	}

	for _, versionDir := range versionDirs {
		if !versionDir.IsDir() {
			continue
		}

		versionPath := filepath.Join(javaBaseDir, versionDir.Name())

		// Look for JDK directories inside this version directory
		entries, err := os.ReadDir(versionPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || (!strings.HasPrefix(entry.Name(), "jdk") && !strings.HasPrefix(entry.Name(), "jre")) {
				continue
			}

			// Extract major version from the JDK directory name
			majorVer, err := ExtractMajorVersion(entry.Name())
			if err != nil {
				continue
			}

			// Build the path to java executable
			var javaPath string
			if goos == "darwin" {
				javaPath = filepath.Join(versionPath, entry.Name(), "Contents", "Home", "bin", javaExe)
			} else {
				javaPath = filepath.Join(versionPath, entry.Name(), "bin", javaExe)
			}

			// Verify the executable exists
			if _, err := os.Stat(javaPath); err == nil {
				installations = append(installations, javaInstall{
					versionDir: versionDir.Name(),
					jdkDir:     entry.Name(),
					majorVer:   majorVer,
					fullPath:   javaPath,
				})
			}
		}
	}

	if len(installations) == 0 {
		log.Printf("*** No Java installations found in %s\n", javaBaseDir)
		return "", fmt.Errorf("no Java installations found in %s", javaBaseDir)
	}

	// Sort by major version (highest first), then by JDK version string
	sort.Slice(installations, func(i, j int) bool {
		if installations[i].majorVer != installations[j].majorVer {
			return installations[i].majorVer > installations[j].majorVer
		}
		return CompareJDKVersions(installations[i].jdkDir, installations[j].jdkDir)
	})

	// Find the best match: highest version that meets minimum requirement
	for _, install := range installations {
		if requiredMajor == 0 || install.majorVer >= requiredMajor {
			log.Printf("*** Found suitable Java %s (major version %d, >= required %d) at: %s\n",
				install.jdkDir, install.majorVer, requiredMajor, install.fullPath)
			return install.fullPath, nil
		}
	}

	// If we get here, no suitable version was found
	log.Printf("*** No Java installation found with major version >= %d (found: %d installations)\n", requiredMajor, len(installations))
	return "", fmt.Errorf("no Java installation found with major version >= %d", requiredMajor)
}

// FindLatestTemurinRelease fetches the tag name for a specific Temurin version from GitHub
// If version is empty, it returns the latest release of Temurin 25
// Supports both "jdk-17" style (major only) and "jdk-17.0.15+6" (full version)
func FindLatestTemurinRelease(version string, userAgent string) (string, error) {
	return FindLatestTemurinReleaseContext(context.Background(), version, userAgent)
}

func FindLatestTemurinReleaseContext(ctx context.Context, version string, userAgent string) (string, error) {
	// Extract major version to determine which repo to use
	majorVersion := 25 // Default to 25
	if version != "" {
		extracted, err := ExtractMajorVersion(version)
		if err == nil {
			majorVersion = extracted
		}
	}

	// Determine the API endpoint based on major version and whether a specific version is requested
	var apiURL string
	repoName := fmt.Sprintf("temurin%d-binaries", majorVersion)

	// Check if this is a major-only version (like "jdk-25") or a full version (like "jdk-25.0.1+9")
	isMajorOnly := version != "" && strings.TrimPrefix(version, "jdk-") == strconv.Itoa(majorVersion)

	if version == "" || isMajorOnly {
		// Get latest release for this major version
		apiURL = fmt.Sprintf("https://api.github.com/repos/adoptium/%s/releases/latest", repoName)
	} else {
		// Get specific version release info from API - URL encode the version tag
		encodedVersion := url.QueryEscape(version)
		apiURL = fmt.Sprintf("https://api.github.com/repos/adoptium/%s/releases/tags/%s", repoName, encodedVersion)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to fetch latest release: %v", err)
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s\nBody: %s", resp.StatusCode, resp.Status, string(body))
	}

	var release TemurinRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("Failed to parse release: %v", err)
		return "", fmt.Errorf("failed to parse release: %w", err)
	}

	log.Printf("Latest Temurin release: %s\n", release.TagName)
	return release.TagName, nil
}

// GetTemurinDownloadURL returns the download URL for a specific Temurin version
// goosFunc should return the OS string (windows, darwin, linux)
func GetTemurinDownloadURL(temurinVersion string, goosFunc func() string, userAgent string) (string, error) {
	return GetTemurinDownloadURLContext(context.Background(), temurinVersion, goosFunc, userAgent)
}

func GetTemurinDownloadURLContext(ctx context.Context, temurinVersion string, goosFunc func() string, userAgent string) (string, error) {
	url, err := FindTemurinDownloadURLFromRecentReleasesContext(ctx, temurinVersion, goosFunc, userAgent, 10)
	if err != nil {
		log.Printf("Failed to find download URL in recent releases: %v", err)
		return "", err
	}
	return url, nil
}

// FindTemurinDownloadURLFromRecentReleases scans the most recent releases and returns
// a matching JRE/JDK asset URL. It logs each asset name being inspected.
func FindTemurinDownloadURLFromRecentReleases(temurinVersion string, goosFunc func() string, userAgent string, perPage int) (string, error) {
	return FindTemurinDownloadURLFromRecentReleasesContext(
		context.Background(), temurinVersion, goosFunc, userAgent, perPage)
}

func FindTemurinDownloadURLFromRecentReleasesContext(
	ctx context.Context,
	temurinVersion string,
	goosFunc func() string,
	userAgent string,
	perPage int,
) (string, error) {
	// Extract major version to determine which repo to use
	majorVersion := 25 // Default
	if temurinVersion != "" {
		extracted, err := ExtractMajorVersion(temurinVersion)
		if err == nil {
			majorVersion = extracted
		}
	}
	repoName := fmt.Sprintf("temurin%d-binaries", majorVersion)
	if perPage <= 0 {
		perPage = 10
	}

	listURL := fmt.Sprintf("https://api.github.com/repos/adoptium/%s/releases?per_page=%d&page=1", repoName, perPage)
	req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to fetch releases list: %v", err)
		return "", fmt.Errorf("failed to fetch releases list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s\nBody: %s", resp.StatusCode, resp.Status, string(body))
	}

	var releases []TemurinRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		log.Printf("Failed to parse releases list: %v", err)
		return "", fmt.Errorf("failed to parse releases list: %w", err)
	}

	goos := goosFunc()
	log.Printf("Running on: OS=%s, ARCH=%s, WSL=%v\n", goos, runtime.GOARCH, IsWSL())

	for _, release := range releases {
		if release.TagName == "" {
			continue
		}
		majorOnly := temurinVersion == "" ||
			strings.TrimPrefix(temurinVersion, "jdk-") == strconv.Itoa(majorVersion)
		if !majorOnly && release.TagName != temurinVersion {
			continue
		}
		log.Printf("Checking release tag: %s\n", release.TagName)
		version := strings.TrimPrefix(release.TagName, "jdk-")
		version = strings.ReplaceAll(version, "+", "_")

		pattern, fallbackPattern, err := buildTemurinAssetPatterns(majorVersion, version, goos)
		if err != nil {
			return "", err
		}
		log.Printf("Looking for asset: %s\n", pattern)

		for _, asset := range release.Assets {
			if asset.Name == pattern {
				log.Printf("Found matching JRE: %s\n", asset.Name)
				log.Printf("Matching JRE URL: %s\n", asset.BrowserDownloadURL)
				return asset.BrowserDownloadURL, nil
			}
		}

		if fallbackPattern != "" {
			log.Printf("No matching JRE found, trying JDK asset: %s\n", fallbackPattern)
			for _, asset := range release.Assets {
				if asset.Name == fallbackPattern {
					log.Printf("Found matching JDK: %s\n", asset.Name)
					log.Printf("Matching JDK URL: %s\n", asset.BrowserDownloadURL)
					return asset.BrowserDownloadURL, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no matching JRE found in first %d releases", perPage)
}

func buildTemurinAssetPatterns(majorVersion int, version string, goos string) (string, string, error) {
	var pattern string
	var fallbackPattern string
	openJDKPrefix := fmt.Sprintf("OpenJDK%dU", majorVersion)
	if goos == "darwin" {
		switch runtime.GOARCH {
		case "amd64":
			pattern = fmt.Sprintf("%s-jre_x64_mac_hotspot_%s.tar.gz", openJDKPrefix, version)
			fallbackPattern = fmt.Sprintf("%s-jdk_x64_mac_hotspot_%s.tar.gz", openJDKPrefix, version)
		case "arm64":
			pattern = fmt.Sprintf("%s-jre_aarch64_mac_hotspot_%s.tar.gz", openJDKPrefix, version)
			fallbackPattern = fmt.Sprintf("%s-jdk_aarch64_mac_hotspot_%s.tar.gz", openJDKPrefix, version)
		default:
			return "", "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
		}
	} else if IsWSL() || goos == "linux" {
		switch runtime.GOARCH {
		case "amd64":
			pattern = fmt.Sprintf("%s-jre_x64_linux_hotspot_%s.tar.gz", openJDKPrefix, version)
			fallbackPattern = fmt.Sprintf("%s-jdk_x64_linux_hotspot_%s.tar.gz", openJDKPrefix, version)
		case "arm64":
			pattern = fmt.Sprintf("%s-jre_aarch64_linux_hotspot_%s.tar.gz", openJDKPrefix, version)
			fallbackPattern = fmt.Sprintf("%s-jdk_aarch64_linux_hotspot_%s.tar.gz", openJDKPrefix, version)
		default:
			return "", "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
		}
	} else if goos == "windows" {
		switch runtime.GOARCH {
		case "amd64":
			pattern = fmt.Sprintf("%s-jre_x64_windows_hotspot_%s.zip", openJDKPrefix, version)
			fallbackPattern = fmt.Sprintf("%s-jdk_x64_windows_hotspot_%s.zip", openJDKPrefix, version)
		case "arm64":
			pattern = fmt.Sprintf("%s-jre_aarch64_windows_hotspot_%s.zip", openJDKPrefix, version)
			fallbackPattern = fmt.Sprintf("%s-jdk_aarch64_windows_hotspot_%s.zip", openJDKPrefix, version)
		default:
			return "", "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
		}
	} else {
		return "", "", fmt.Errorf("unsupported OS: %s", goos)
	}

	return pattern, fallbackPattern, nil
}

// FindSystemJava finds Java in the system (JAVA_HOME or PATH)
// goosFunc should return the OS string (windows, darwin, linux)
func FindSystemJava(goosFunc func() string) (string, error) {
	javaHome := os.Getenv("JAVA_HOME")
	javaCommand := "java"
	goos := goosFunc()
	if goos == "windows" && !IsWSL() {
		javaCommand = "javaw"
	}
	if javaHome != "" {
		javaExecutable := filepath.Join(javaHome, "bin", javaCommand)
		if _, err := os.Stat(javaExecutable); err == nil {
			return javaExecutable, nil
		}
	}

	javaPath, err := exec.LookPath(javaCommand)
	if err == nil {
		return javaPath, nil
	}
	return "", fmt.Errorf("java executable not found")
}

// GetJavaVersion parses the version from java -version output
func GetJavaVersion(javaPath string) (int, error) {
	cmd := exec.Command(javaPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("getting java version: %w", err)
	}

	versionRegex := regexp.MustCompile(`(?:java|openjdk) version "(?:(\d+)\.)?(?:(\d+)\.)?(\d+)"`)
	matches := versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		majorVersionStr := matches[1]
		if majorVersionStr == "" {
			majorVersionStr = matches[2]
			if majorVersionStr == "" {
				majorVersionStr = matches[3]
			}
		}
		majorVersion, err := strconv.Atoi(majorVersionStr)
		if err != nil {
			return 0, fmt.Errorf("parsing java version: %w", err)
		}
		return majorVersion, nil
	}

	versionRegex = regexp.MustCompile(`(?:java|openjdk) (\d+)`)
	matches = versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		majorVersion, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("parsing java version: %w", err)
		}
		return majorVersion, nil
	}

	return 0, fmt.Errorf("could not parse java version from output: %s", string(output))
}
