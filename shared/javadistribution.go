package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// JavaRelease is a Java runtime archive that can be installed on the current platform.
type JavaRelease struct {
	Distribution string
	Tag          string
	// Version is comparable with JAVA_VERSION from the runtime's release file.
	Version     string
	AssetName   string
	DownloadURL string
}

// javaPlatform uses Go names: OS is windows, darwin or linux; Arch is amd64 or arm64.
type javaPlatform struct {
	OS   string
	Arch string
}

func currentJavaPlatform(goosFunc func() string) javaPlatform {
	goos := goosFunc()
	if IsWSL() {
		goos = "linux"
	}
	return javaPlatform{OS: goos, Arch: runtime.GOARCH}
}

func (p javaPlatform) archiveExtension() string {
	if p.OS == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// javaDistribution publishes Java runtimes as GitHub release assets.
type javaDistribution interface {
	name() string
	// findRelease returns the newest release of the major version that has a
	// runtime for the platform, skipping releases that lack one.
	findRelease(ctx context.Context, major int, platform javaPlatform, userAgent string) (JavaRelease, error)
}

// javaDistributions is in order of preference.
var javaDistributions = []javaDistribution{libericaDistribution{}, temurinDistribution{}}

// FindJavaRelease finds a downloadable runtime for a requested version such as
// "jdk-25" (newest Java 25 from the first distribution that has one) or a
// pinned Temurin tag such as "jdk-25.0.1+9".
func FindJavaRelease(ctx context.Context, requested string, goosFunc func() string, userAgent string) (JavaRelease, error) {
	platform := currentJavaPlatform(goosFunc)
	log.Printf("Looking for Java %s for OS=%s, ARCH=%s, WSL=%v\n", requested, platform.OS, platform.Arch, IsWSL())

	major, pinned, err := parseRequestedJava(requested)
	if err != nil {
		return JavaRelease{}, err
	}
	if pinned {
		release, err := findTemurinTag(ctx, requested, platform, userAgent)
		if err != nil {
			return JavaRelease{}, err
		}
		logJavaRelease(release)
		return release, nil
	}

	var failures []string
	for _, distribution := range javaDistributions {
		release, err := distribution.findRelease(ctx, major, platform, userAgent)
		if err == nil {
			logJavaRelease(release)
			return release, nil
		}
		if ctx.Err() != nil {
			return JavaRelease{}, ctx.Err()
		}
		log.Printf("%s: %v\n", distribution.name(), err)
		failures = append(failures, fmt.Sprintf("%s: %v", distribution.name(), err))
	}
	return JavaRelease{}, fmt.Errorf("no Java %d runtime available for %s/%s (%s)",
		major, platform.OS, platform.Arch, strings.Join(failures, "; "))
}

// GetJavaDownloadURL returns the archive URL for a requested version such as "jdk-25".
func GetJavaDownloadURL(requested string, goosFunc func() string, userAgent string) (string, error) {
	release, err := FindJavaRelease(context.Background(), requested, goosFunc, userAgent)
	if err != nil {
		return "", err
	}
	return release.DownloadURL, nil
}

func logJavaRelease(release JavaRelease) {
	log.Printf("Selected %s Java %s (%s): %s\n", release.Distribution, release.Version, release.Tag, release.DownloadURL)
}

// parseRequestedJava accepts "jdk-25", "25" or a full Temurin tag such as "jdk-25.0.1+9".
func parseRequestedJava(requested string) (major int, pinned bool, err error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return 25, false, nil
	}
	major, err = ExtractMajorVersion(requested)
	if err != nil {
		return 0, false, fmt.Errorf("invalid Java version %q: %w", requested, err)
	}
	return major, strings.TrimPrefix(requested, "jdk-") != strconv.Itoa(major), nil
}

// javaVersionKeyParts is the number of version components compared before the build number.
// Liberica patch releases use five (25.0.5.0.3).
const javaVersionKeyParts = 5

// parseReleaseVersion parses "25.0.5.1" and build "5" into a sortable key and the
// release-file style version (at most four components).
func parseReleaseVersion(version, build string, major int) (key []int, releaseVersion string, ok bool) {
	parts := strings.Split(version, ".")
	if len(parts) == 0 || len(parts) > javaVersionKeyParts {
		return nil, "", false
	}
	key = make([]int, javaVersionKeyParts+1)
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return nil, "", false
		}
		key[index] = value
	}
	if key[0] != major {
		return nil, "", false
	}
	if build != "" {
		value, err := strconv.Atoi(build)
		if err != nil {
			return nil, "", false
		}
		key[javaVersionKeyParts] = value
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return key, strings.Join(parts, "."), true
}

func compareVersionKeys(left, right []int) int {
	for index := range left {
		switch {
		case left[index] < right[index]:
			return -1
		case left[index] > right[index]:
			return 1
		}
	}
	return 0
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// releaseNaming describes how a distribution names its tags and assets.
type releaseNaming struct {
	distribution string
	// parseTag returns the sortable key and release-file version of a stable release tag.
	parseTag func(tag string, major int) (key []int, version string, ok bool)
	// assetNames returns acceptable archive names for the tag, in order of preference.
	assetNames func(tag string, platform javaPlatform) ([]string, error)
}

// selectNewestRelease returns the highest stable version that has an archive for the platform.
func selectNewestRelease(releases []githubRelease, major int, platform javaPlatform, naming releaseNaming) (JavaRelease, error) {
	var best JavaRelease
	var bestKey []int
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		key, version, ok := naming.parseTag(release.TagName, major)
		if !ok || (bestKey != nil && compareVersionKeys(key, bestKey) <= 0) {
			continue
		}
		names, err := naming.assetNames(release.TagName, platform)
		if err != nil {
			return JavaRelease{}, err
		}
		asset, found := findAsset(release.Assets, names)
		if !found {
			log.Printf("%s %s has no %s/%s runtime (%s); skipping\n",
				naming.distribution, release.TagName, platform.OS, platform.Arch, strings.Join(names, ", "))
			continue
		}
		best = JavaRelease{
			Distribution: naming.distribution,
			Tag:          release.TagName,
			Version:      version,
			AssetName:    asset.Name,
			DownloadURL:  asset.BrowserDownloadURL,
		}
		bestKey = key
	}
	if bestKey == nil {
		return JavaRelease{}, fmt.Errorf("no Java %d release with a %s/%s runtime", major, platform.OS, platform.Arch)
	}
	return best, nil
}

func findAsset(assets []githubAsset, names []string) (githubAsset, bool) {
	for _, name := range names {
		for _, asset := range assets {
			if asset.Name == name {
				return asset, true
			}
		}
	}
	return githubAsset{}, false
}

func javaArchitectureError(platform javaPlatform) error {
	return fmt.Errorf("unsupported platform: %s/%s", platform.OS, platform.Arch)
}

func fetchGitHubJSON(ctx context.Context, apiURL, userAgent string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API returned status %d for %s: %s", resp.StatusCode, apiURL, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("parsing %s: %w", apiURL, err)
	}
	return nil
}
