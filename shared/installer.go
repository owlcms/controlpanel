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

// ControlPanelInstallerLink returns the label and GitHub release download URL of the installer
// for goos/goarch in the release tag. When the release does not publish that installer,
// it returns the GitHub release page instead and found is false.
func ControlPanelInstallerLink(tag string, assets []ReleaseAsset, goos, goarch string) (label string, link string, found bool) {
	releasePage := fmt.Sprintf("%s/tag/%s", ControlPanelReleasesURL, url.PathEscape(tag))
	installer, ok := ControlPanelInstallerFor(goos, goarch)
	if !ok {
		return "Download the Installer", releasePage, false
	}
	for _, asset := range assets {
		if asset.Name == installer.AssetName {
			link = asset.BrowserDownloadURL
			if link == "" {
				link = fmt.Sprintf("%s/download/%s/%s", ControlPanelReleasesURL, url.PathEscape(tag), url.PathEscape(asset.Name))
			}
			return installer.Label, link, true
		}
	}
	return installer.Label, releasePage, false
}
