package tracker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const customBuildMarkerName = ".custom-build"

func customBuildMarkerPath(versionDir string) string {
	return filepath.Join(versionDir, customBuildMarkerName)
}

func hasCustomBuildMarker(versionDir string) bool {
	info, err := os.Stat(customBuildMarkerPath(versionDir))
	return err == nil && !info.IsDir()
}

func removeCustomBuildMarker(versionDir string) error {
	err := os.Remove(customBuildMarkerPath(versionDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// readCustomBuildPlugins reads the sorted plugin list from a .custom-build marker.
// Returns the list (may be empty) and true if the marker exists.
// Returns nil, false if no marker is present.
func readCustomBuildPlugins(versionDir string) ([]string, bool) {
	path := customBuildMarkerPath(versionDir)
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "plugins:"); ok {
			raw := after
			raw = strings.TrimSpace(raw)
			if raw == "" || raw == "(none)" {
				return []string{}, true
			}
			parts := strings.Split(raw, ", ")
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			sort.Strings(result)
			return result, true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false
	}
	// Marker exists but has no plugins line (older format)
	return []string{}, true
}

// customBuildPluginsEqual returns true when both sorted lists are identical.
func customBuildPluginsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pluginListDisplay(plugins []string) string {
	if len(plugins) == 0 {
		return "(none recorded)"
	}
	return strings.Join(plugins, ", ")
}

// --- Warning / error message builders ---

func updateCustomBuildBlockMessage(existingVersion, targetVersion string) string {
	return fmt.Sprintf(
		"Cannot update: version %s is a custom build, but version %s is a standard build.\n\n"+
			"The standard version does not include your custom plugins.",
		existingVersion,
		targetVersion,
	)
}

// importBlockMessage is shown when import cannot proceed safely (mismatch cases).
func importBlockMessage(sourceVersion, targetVersion string, sourcePlugins []string, sourceIsCustom bool, destPlugins []string, destIsCustom bool) string {
	switch {
	case sourceIsCustom && destIsCustom:
		// Both custom but different plugin lists
		return fmt.Sprintf(
			"Cannot import: the custom plugin lists do not match.\n\n"+
				"Source (%s) plugins:      %s\n\n"+
				"Destination (%s) plugins: %s\n\n"+
				"Plugin configuration data is only safe to import between custom builds that have the exact same plugins.\n\n"+
				"Recommendation: obtain a new custom package for version %s from your provider.",
			sourceVersion, pluginListDisplay(sourcePlugins),
			targetVersion, pluginListDisplay(destPlugins),
			targetVersion,
		)
	case sourceIsCustom:
		// Source custom, destination standard
		return fmt.Sprintf(
			"Cannot import: version %s is a custom build, but version %s is a standard build.\n\n"+
				"Source plugins: %s\n\n"+
				"The standard build does not contain the code for your custom plugins.\n\n"+
				"Import is blocked.",
			sourceVersion, targetVersion,
			pluginListDisplay(sourcePlugins),
		)
	default:
		// Destination custom, source standard
		return fmt.Sprintf(
			"Cannot import: version %s is a custom build, but version %s is a standard build.\n\n"+
				"The standard version does not include your custom plugins.",
			targetVersion, sourceVersion,
		)
	}
}

// importMatchedCustomBuildWarning is shown when both sides share the exact same plugin list.
func importMatchedCustomBuildWarning(sourceVersion, targetVersion string, plugins []string) string {
	return fmt.Sprintf(
		"Both versions %s and %s are custom builds with the same plugins:\n%s\n\n"+
			"Import copies only local/ data and configuration. "+
			"Plugin configuration formats may still differ between tracker versions.\n\n"+
			"Proceed only if you are confident the configuration data is compatible with version %s. "+
			"If unsure, obtain a new custom package for version %s from your provider.",
		sourceVersion, targetVersion,
		pluginListDisplay(plugins),
		targetVersion,
		targetVersion,
	)
}

// importMismatchedCustomBuildWarning is shown when both sides are custom builds but with different plugin lists.
func importMismatchedCustomBuildWarning(sourceVersion, targetVersion string, sourcePlugins, destPlugins []string) string {
	return fmt.Sprintf(
		"WARNING: Both versions are custom builds but have different plugins.\n\n"+
			"Source (%s) plugins:      %s\n\n"+
			"Destination (%s) plugins: %s\n\n"+
			"Import copies only local/ data and configuration from local/. "+
			"Proceed only if you are certain the destination custom build includes all the plugins required by the imported configuration.\n\n"+
			"If unsure, obtain a new custom package for version %s from your provider.",
		sourceVersion, pluginListDisplay(sourcePlugins),
		targetVersion, pluginListDisplay(destPlugins),
		targetVersion,
	)
}
