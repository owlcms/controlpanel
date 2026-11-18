package shared

import (
	"fmt"
	"net/url"
)

const ControlPanelReleasesURL = "https://github.com/owlcms/controlpanel/releases"

// ReleaseAsset is a file attached to a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// ControlPanelInstaller describes the control panel installer published for a platform.
type ControlPanelInstaller struct {
	AssetName string
	Label     string
}

// ControlPanelInstallerFor returns the control panel installer asset matching goos/goarch.
func ControlPanelInstallerFor(goos, goarch string) (ControlPanelInstaller, bool) {
	switch goos {
	case "windows":
		return ControlPanelInstaller{"Windows_Control_Panel_Installer.exe", "Windows Installer (.exe)"}, true
	case "darwin":
		if goarch == "amd64" {
			return ControlPanelInstaller{"macOS_Intel_OWLCMS.dmg", "macOS Intel Installer (.dmg)"}, true
		}
		return ControlPanelInstaller{"macOS_OWLCMS.dmg", "macOS Apple Silicon Installer (.dmg)"}, true
	case "linux":
		if goarch == "arm64" {
			return ControlPanelInstaller{"Raspberry_Pi_arm64_Control_Panel_Installer.deb", "Raspberry Pi / Linux ARM64 Installer (.deb)"}, true
		}
		if goarch == "amd64" {
			return ControlPanelInstaller{"Linux_Control_Panel_Installer.deb", "Linux Installer (.deb)"}, true
		}
	}
	return ControlPanelInstaller{}, false
}

// raspberryPiInstallerFor returns the ARM64 Debian installer variant matching the running system:
// Bookworm (or older) Debian-family systems get the Bookworm package, newer ones the Trixie package.
func raspberryPiInstallerFor(legacyDebian bool) ControlPanelInstaller {
	if legacyDebian {
		return ControlPanelInstaller{"Raspberry_Pi_arm64_Bookworm_Control_Panel_Installer.deb", "Raspberry Pi OS Bookworm Installer (.deb)"}
	}
	return ControlPanelInstaller{"Raspberry_Pi_arm64_Trixie_Control_Panel_Installer.deb", "Raspberry Pi OS Trixie Installer (.deb)"}
}

// ControlPanelInstallerLink returns the label and GitHub release download URL of the installer
// for goos/goarch in the release tag. On linux/arm64, legacyDebian selects the Bookworm or Trixie
// package; if the release does not publish it (older releases), the unsuffixed installer is used.
// When the release does not publish the installer, it returns the GitHub release page instead
// and found is false.
func ControlPanelInstallerLink(tag string, assets []ReleaseAsset, goos, goarch string, legacyDebian bool) (label string, link string, found bool) {
	releasePage := fmt.Sprintf("%s/tag/%s", ControlPanelReleasesURL, url.PathEscape(tag))
	installer, ok := ControlPanelInstallerFor(goos, goarch)
	if !ok {
		return "Download the Installer", releasePage, false
	}
	if goos == "linux" && goarch == "arm64" {
		variant := raspberryPiInstallerFor(legacyDebian)
		if link, found := releaseAssetLink(tag, assets, variant.AssetName); found {
			return variant.Label, link, true
		}
	}
	if link, found := releaseAssetLink(tag, assets, installer.AssetName); found {
		return installer.Label, link, true
	}
	return installer.Label, releasePage, false
}

func releaseAssetLink(tag string, assets []ReleaseAsset, assetName string) (string, bool) {
	for _, asset := range assets {
		if asset.Name == assetName {
			if asset.BrowserDownloadURL != "" {
				return asset.BrowserDownloadURL, true
			}
			return fmt.Sprintf("%s/download/%s/%s", ControlPanelReleasesURL, url.PathEscape(tag), url.PathEscape(asset.Name)), true
		}
	}
	return "", false
}
