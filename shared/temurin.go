package shared

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// temurinDistribution reads Eclipse Temurin releases from github.com/adoptium/temurin<major>-binaries.
type temurinDistribution struct{}

const temurinReleasesToScan = 20

func (temurinDistribution) name() string { return "Temurin" }

func (temurinDistribution) findRelease(ctx context.Context, major int, platform javaPlatform, userAgent string) (JavaRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/adoptium/temurin%d-binaries/releases?per_page=%d",
		major, temurinReleasesToScan)
	var releases []githubRelease
	if err := fetchGitHubJSON(ctx, apiURL, userAgent, &releases); err != nil {
		return JavaRelease{}, err
	}
	return selectNewestRelease(releases, major, platform, temurinNaming)
}

// findTemurinTag returns the runtime of one specific Temurin tag such as "jdk-25.0.1+9".
func findTemurinTag(ctx context.Context, tag string, platform javaPlatform, userAgent string) (JavaRelease, error) {
	major, err := ExtractMajorVersion(tag)
	if err != nil {
		return JavaRelease{}, err
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/adoptium/temurin%d-binaries/releases/tags/%s",
		major, url.PathEscape(tag))
	var release githubRelease
	if err := fetchGitHubJSON(ctx, apiURL, userAgent, &release); err != nil {
		return JavaRelease{}, err
	}
	return selectNewestRelease([]githubRelease{release}, major, platform, temurinNaming)
}

var temurinNaming = releaseNaming{
	distribution: "Temurin",
	parseTag:     parseTemurinTag,
	assetNames:   temurinAssetNames,
}

// parseTemurinTag accepts "jdk-25.0.5+8" and "jdk-25+36".
func parseTemurinTag(tag string, major int) ([]int, string, bool) {
	version, found := strings.CutPrefix(tag, "jdk-")
	if !found {
		return nil, "", false
	}
	version, build, _ := strings.Cut(version, "+")
	return parseReleaseVersion(version, build, major)
}

// temurinAssetNames returns names such as OpenJDK25U-jre_x64_windows_hotspot_25.0.5_8.zip.
func temurinAssetNames(tag string, platform javaPlatform) ([]string, error) {
	major, err := ExtractMajorVersion(tag)
	if err != nil {
		return nil, err
	}
	osName := map[string]string{"windows": "windows", "darwin": "mac", "linux": "linux"}[platform.OS]
	arch := map[string]string{"amd64": "x64", "arm64": "aarch64"}[platform.Arch]
	if osName == "" || arch == "" {
		return nil, javaArchitectureError(platform)
	}
	version := strings.ReplaceAll(strings.TrimPrefix(tag, "jdk-"), "+", "_")
	name := func(imageType string) string {
		return fmt.Sprintf("OpenJDK%dU-%s_%s_%s_hotspot_%s.%s",
			major, imageType, arch, osName, version, platform.archiveExtension())
	}
	return []string{name("jre"), name("jdk")}, nil
}
