package shared

import (
	"testing"
)

func releaseWithAssets(tag string, assets ...string) githubRelease {
	release := githubRelease{TagName: tag}
	for _, name := range assets {
		release.Assets = append(release.Assets, githubAsset{Name: name, BrowserDownloadURL: "https://example.test/" + name})
	}
	return release
}

func TestTemurinSkipsNewestReleaseWithoutPlatformAsset(t *testing.T) {
	windows := javaPlatform{OS: "windows", Arch: "amd64"}
	releases := []githubRelease{
		releaseWithAssets("jdk-25.0.5.1+2",
			"OpenJDK25U-jre_x64_linux_hotspot_25.0.5.1_2.tar.gz",
			"OpenJDK25U-jre_aarch64_mac_hotspot_25.0.5.1_2.tar.gz"),
		releaseWithAssets("jdk-25.0.5+8", "OpenJDK25U-jre_x64_windows_hotspot_25.0.5_8.zip"),
		releaseWithAssets("jdk-25.0.4.1+1", "OpenJDK25U-jre_x64_windows_hotspot_25.0.4.1_1.zip"),
		{TagName: "jdk25u-2026-02-02-15-22-beta", Prerelease: true},
	}

	release, err := selectNewestRelease(releases, 25, windows, temurinNaming)
	if err != nil {
		t.Fatalf("selectNewestRelease: %v", err)
	}
	if release.Tag != "jdk-25.0.5+8" || release.Version != "25.0.5" ||
		release.AssetName != "OpenJDK25U-jre_x64_windows_hotspot_25.0.5_8.zip" {
		t.Fatalf("unexpected release: %#v", release)
	}
}

func TestTemurinFallsBackToJdkAsset(t *testing.T) {
	mac := javaPlatform{OS: "darwin", Arch: "arm64"}
	releases := []githubRelease{
		releaseWithAssets("jdk-25.0.5+8", "OpenJDK25U-jdk_aarch64_mac_hotspot_25.0.5_8.tar.gz"),
	}

	release, err := selectNewestRelease(releases, 25, mac, temurinNaming)
	if err != nil {
		t.Fatalf("selectNewestRelease: %v", err)
	}
	if release.AssetName != "OpenJDK25U-jdk_aarch64_mac_hotspot_25.0.5_8.tar.gz" {
		t.Fatalf("unexpected asset %q", release.AssetName)
	}
}

func TestLibericaSelectsNewestStableReleaseOfMajor(t *testing.T) {
	platforms := map[javaPlatform]string{
		{OS: "windows", Arch: "amd64"}: "bellsoft-jre25.0.5.1+5-windows-amd64.zip",
		{OS: "windows", Arch: "arm64"}: "bellsoft-jre25.0.5.1+5-windows-aarch64.zip",
		{OS: "darwin", Arch: "arm64"}:  "bellsoft-jre25.0.5.1+5-macos-aarch64.tar.gz",
		{OS: "darwin", Arch: "amd64"}:  "bellsoft-jre25.0.5.1+5-macos-amd64.tar.gz",
		{OS: "linux", Arch: "amd64"}:   "bellsoft-jre25.0.5.1+5-linux-amd64.tar.gz",
		{OS: "linux", Arch: "arm64"}:   "bellsoft-jre25.0.5.1+5-linux-aarch64.tar.gz",
	}
	var latestAssets []string
	for _, name := range platforms {
		latestAssets = append(latestAssets, name, "bellsoft-jre25.0.5.1+5-windows-amd64-full.zip")
	}
	releases := []githubRelease{
		releaseWithAssets("26.0.2.1+1", "bellsoft-jre26.0.2.1+1-windows-amd64.zip"),
		releaseWithAssets("25.0.5.0.3+5", "bellsoft-jre25.0.5.0.3+5-windows-amd64.zip"),
		releaseWithAssets("25.0.5.1+5", latestAssets...),
		releaseWithAssets("25.0.5+11", "bellsoft-jre25.0.5+11-windows-amd64.zip"),
		releaseWithAssets("26+29-ea", "bellsoft-jre26+29-ea-windows-amd64.zip"),
		releaseWithAssets("25+21-ea-leyden", "bellsoft-jre25+21-ea-leyden-windows-amd64.zip"),
	}

	for platform, want := range platforms {
		release, err := selectNewestRelease(releases, 25, platform, libericaNaming)
		if err != nil {
			t.Fatalf("%v: selectNewestRelease: %v", platform, err)
		}
		if release.Tag != "25.0.5.1+5" || release.Version != "25.0.5.1" || release.AssetName != want {
			t.Fatalf("%v: unexpected release: %#v", platform, release)
		}
	}
}

func TestLibericaPatchReleaseOrdering(t *testing.T) {
	windows := javaPlatform{OS: "windows", Arch: "amd64"}
	releases := []githubRelease{
		releaseWithAssets("25.0.5+11", "bellsoft-jre25.0.5+11-windows-amd64.zip"),
		releaseWithAssets("25.0.5.0.3+5", "bellsoft-jre25.0.5.0.3+5-windows-amd64.zip"),
		releaseWithAssets("25.0.5.0.2+5", "bellsoft-jre25.0.5.0.2+5-windows-amd64.zip"),
	}

	release, err := selectNewestRelease(releases, 25, windows, libericaNaming)
	if err != nil {
		t.Fatalf("selectNewestRelease: %v", err)
	}
	if release.Tag != "25.0.5.0.3+5" || release.Version != "25.0.5.0" {
		t.Fatalf("unexpected release: %#v", release)
	}
	if _, err := parseJavaVersion(release.Version); err != nil {
		t.Fatalf("release version must be comparable with installed versions: %v", err)
	}
}

func TestNoReleaseForPlatform(t *testing.T) {
	windows := javaPlatform{OS: "windows", Arch: "amd64"}
	releases := []githubRelease{
		releaseWithAssets("jdk-25.0.5.1+2", "OpenJDK25U-jre_x64_linux_hotspot_25.0.5.1_2.tar.gz"),
	}
	if _, err := selectNewestRelease(releases, 25, windows, temurinNaming); err == nil {
		t.Fatal("expected an error when no release has a Windows runtime")
	}
}

func TestParseRequestedJava(t *testing.T) {
	tests := []struct {
		requested string
		major     int
		pinned    bool
	}{
		{"", 25, false},
		{"jdk-25", 25, false},
		{"25", 25, false},
		{"jdk-25.0.1+9", 25, true},
	}
	for _, test := range tests {
		major, pinned, err := parseRequestedJava(test.requested)
		if err != nil {
			t.Fatalf("parseRequestedJava(%q): %v", test.requested, err)
		}
		if major != test.major || pinned != test.pinned {
			t.Fatalf("parseRequestedJava(%q) = %d, %v; want %d, %v",
				test.requested, major, pinned, test.major, test.pinned)
		}
	}
}
