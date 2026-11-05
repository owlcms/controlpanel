package shared

import "testing"

func TestControlPanelInstallerLink(t *testing.T) {
	assets := []ReleaseAsset{
		{"Linux_Control_Panel_Installer.deb", "https://github.com/owlcms/controlpanel/releases/download/3.8.1/Linux_Control_Panel_Installer.deb"},
		{"macOS_Intel_OWLCMS.dmg", "https://github.com/owlcms/controlpanel/releases/download/3.8.1/macOS_Intel_OWLCMS.dmg"},
		{"macOS_OWLCMS.dmg", "https://github.com/owlcms/controlpanel/releases/download/3.8.1/macOS_OWLCMS.dmg"},
		{"Raspberry_Pi_arm64_Control_Panel_Installer.deb", ""},
		{"Windows_Control_Panel_Installer.exe", "https://github.com/owlcms/controlpanel/releases/download/3.8.1/Windows_Control_Panel_Installer.exe"},
	}
	cases := []struct {
		goos, goarch string
		wantLabel    string
		wantLink     string
		wantFound    bool
	}{
		{"windows", "amd64", "Windows Installer (.exe)", ControlPanelReleasesURL + "/download/3.8.1/Windows_Control_Panel_Installer.exe", true},
		{"darwin", "arm64", "macOS Apple Silicon Installer (.dmg)", ControlPanelReleasesURL + "/download/3.8.1/macOS_OWLCMS.dmg", true},
		{"darwin", "amd64", "macOS Intel Installer (.dmg)", ControlPanelReleasesURL + "/download/3.8.1/macOS_Intel_OWLCMS.dmg", true},
		{"linux", "amd64", "Linux Installer (.deb)", ControlPanelReleasesURL + "/download/3.8.1/Linux_Control_Panel_Installer.deb", true},
		{"linux", "arm64", "Raspberry Pi / Linux ARM64 Installer (.deb)", ControlPanelReleasesURL + "/download/3.8.1/Raspberry_Pi_arm64_Control_Panel_Installer.deb", true},
		{"linux", "386", "Download the Installer", ControlPanelReleasesURL + "/tag/3.8.1", false},
	}
	for _, c := range cases {
		label, link, found := ControlPanelInstallerLink("3.8.1", assets, c.goos, c.goarch)
		if label != c.wantLabel || link != c.wantLink || found != c.wantFound {
			t.Errorf("%s/%s: got (%q, %q, %v), want (%q, %q, %v)", c.goos, c.goarch, label, link, found, c.wantLabel, c.wantLink, c.wantFound)
		}
	}

	label, link, found := ControlPanelInstallerLink("3.8.1", nil, "windows", "amd64")
	if label != "Windows Installer (.exe)" || link != ControlPanelReleasesURL+"/tag/3.8.1" || found {
		t.Errorf("missing asset: got (%q, %q, %v)", label, link, found)
	}
}
