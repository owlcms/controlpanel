package shared

import (
	"os"
	"strconv"
	"strings"
)

// IsLegacyDebian reports whether this machine runs Debian or Raspberry Pi OS older than 13 (Trixie),
// i.e. Bookworm or earlier, whose glibc is too old for the standard Linux installers.
func IsLegacyDebian() bool {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	return isLegacyDebian(string(content))
}

func isLegacyDebian(osRelease string) bool {
	values := map[string]string{}
	for _, line := range strings.Split(osRelease, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			values[key] = strings.Trim(value, `"'`)
		}
	}
	if values["ID"] != "debian" && values["ID"] != "raspbian" {
		return false
	}
	major, err := strconv.Atoi(strings.SplitN(values["VERSION_ID"], ".", 2)[0])
	return err == nil && major < 13
}
