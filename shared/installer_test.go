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
		label, link, found := ControlPanelInstallerLink("3.8.1", assets, c.goos, c.goarch, false)
		if label != c.wantLabel || link != c.wantLink || found != c.wantFound {
			t.Errorf("%s/%s: got (%q, %q, %v), want (%q, %q, %v)", c.goos, c.goarch, label, link, found, c.wantLabel, c.wantLink, c.wantFound)
		}
	}

	label, link, found := ControlPanelInstallerLink("3.8.1", nil, "windows", "amd64", false)
	if label != "Windows Installer (.exe)" || link != ControlPanelReleasesURL+"/tag/3.8.1" || found {
		t.Errorf("missing asset: got (%q, %q, %v)", label, link, found)
	}
}

func TestControlPanelInstallerLinkRaspberryPiVariants(t *testing.T) {
	standard := ReleaseAsset{"Raspberry_Pi_arm64_Control_Panel_Installer.deb", ""}
	bookworm := ReleaseAsset{"Raspberry_Pi_arm64_Bookworm_Control_Panel_Installer.deb", ""}
	trixie := ReleaseAsset{"Raspberry_Pi_arm64_Trixie_Control_Panel_Installer.deb", ""}
	all := []ReleaseAsset{standard, bookworm, trixie}

	cases := []struct {
		name      string
		assets    []ReleaseAsset
		goarch    string
		legacy    bool
		wantLabel string
		wantAsset string
	}{
		{"Bookworm system gets Bookworm", all, "arm64", true, "Raspberry Pi OS Bookworm Installer (.deb)", bookworm.Name},
		{"Trixie system gets Trixie", all, "arm64", false, "Raspberry Pi OS Trixie Installer (.deb)", trixie.Name},
		{"Trixie package missing, fall back to unsuffixed", []ReleaseAsset{standard, bookworm}, "arm64", false, "Raspberry Pi / Linux ARM64 Installer (.deb)", standard.Name},
		{"Bookworm package missing, fall back to unsuffixed", []ReleaseAsset{standard}, "arm64", true, "Raspberry Pi / Linux ARM64 Installer (.deb)", standard.Name},
		{"amd64 has no variants", []ReleaseAsset{{"Linux_Control_Panel_Installer.deb", ""}}, "amd64", true, "Linux Installer (.deb)", "Linux_Control_Panel_Installer.deb"},
	}
	for _, c := range cases {
		label, link, found := ControlPanelInstallerLink("3.9.0", c.assets, "linux", c.goarch, c.legacy)
		if label != c.wantLabel || link != ControlPanelReleasesURL+"/download/3.9.0/"+c.wantAsset || !found {
			t.Errorf("%s: got (%q, %q, %v)", c.name, label, link, found)
		}
	}
}

func TestIsLegacyDebian(t *testing.T) {
	cases := []struct {
		name, osRelease string
		want            bool
	}{
		{"Raspberry Pi OS Bookworm 64-bit", "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\n", true},
		{"Raspberry Pi OS Bookworm 32-bit", "ID=raspbian\nID_LIKE=debian\nVERSION_ID=\"12\"\n", true},
		{"Debian Trixie", "ID=debian\nVERSION_ID=\"13\"\n", false},
		{"Debian testing without version", "ID=debian\n", false},
		{"Ubuntu", "ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"24.04\"\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := isLegacyDebian(c.osRelease); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
