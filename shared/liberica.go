package shared

import (
	"context"
	"fmt"
	"strings"
)

// libericaDistribution reads BellSoft Liberica releases from github.com/bell-sw/Liberica,
// which holds every Java major version in one repository.
type libericaDistribution struct{}

const libericaReleasesToScan = 100

func (libericaDistribution) name() string { return "Liberica" }

func (libericaDistribution) findRelease(ctx context.Context, major int, platform javaPlatform, userAgent string) (JavaRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/bell-sw/Liberica/releases?per_page=%d", libericaReleasesToScan)
	var releases []githubRelease
	if err := fetchGitHubJSON(ctx, apiURL, userAgent, &releases); err != nil {
		return JavaRelease{}, err
	}
	return selectNewestRelease(releases, major, platform, libericaNaming)
}

var libericaNaming = releaseNaming{
	distribution: "Liberica",
	parseTag:     parseLibericaTag,
	assetNames:   libericaAssetNames,
}

// parseLibericaTag accepts stable tags such as "25.0.5.1+5", "25.0.5.0.3+5" and "25+37";
// early-access tags such as "26+29-ea" are rejected because their build is not numeric.
func parseLibericaTag(tag string, major int) ([]int, string, bool) {
	version, build, _ := strings.Cut(tag, "+")
	return parseReleaseVersion(version, build, major)
}

// libericaAssetNames returns names such as bellsoft-jre25.0.5.1+5-windows-amd64.zip.
// The standard JRE is preferred over the -full variant, which adds JavaFX.
func libericaAssetNames(tag string, platform javaPlatform) ([]string, error) {
	osName := map[string]string{"windows": "windows", "darwin": "macos", "linux": "linux"}[platform.OS]
	arch := map[string]string{"amd64": "amd64", "arm64": "aarch64"}[platform.Arch]
	if osName == "" || arch == "" {
		return nil, javaArchitectureError(platform)
	}
	name := func(imageType string) string {
		return fmt.Sprintf("bellsoft-%s%s-%s-%s.%s", imageType, tag, osName, arch, platform.archiveExtension())
	}
	return []string{name("jre"), name("jdk")}, nil
}
