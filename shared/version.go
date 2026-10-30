package shared

import "strings"

var (
	launcherVersion = "3.10.0-SNAPSHOT"
	buildVersion    = "_TAG_"
)

func init() {
	if buildVersion != ("_" + "TAG" + "_") {
		// not running in a development environment
		launcherVersion = buildVersion
	}
}

// GetLauncherVersion returns the current launcher version
func GetLauncherVersion() string {
	return launcherVersion
}

// GetLauncherVersionSemver returns a semver-friendly version string.
// Our release tags are typically like "v3.0.0-rc10"; some consumers expect "3.0.0-rc10".
func GetLauncherVersionSemver() string {
	return strings.TrimPrefix(GetLauncherVersion(), "v")
}
