package shared

import (
	"os"
	"strings"
)

var localeEnvPriority = []string{"LC_ALL", "LC_MESSAGES", "LANG"}
var localeEnvGOOS = GetGoos

const defaultUTF8Locale = "en_US.UTF-8"

// NormalizeLocaleEnvironment replaces POSIX locale values such as C with a
// locale setting that GUI startup and child processes can safely use.
func NormalizeLocaleEnvironment() {
	if !usesPOSIXLocaleEnvironment(localeEnvGOOS()) {
		return
	}

	key, value := selectedLocaleEnv()
	if !isPOSIXLocale(value) {
		return
	}

	if key == "LC_ALL" || key == "LC_MESSAGES" {
		_ = os.Unsetenv(key)
	}

	_ = os.Setenv("LANG", fallbackLocaleFromEnvironment())
}

func selectedLocaleEnv() (string, string) {
	for _, key := range localeEnvPriority {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return key, value
		}
	}

	return "", ""
}

func fallbackLocaleFromEnvironment() string {
	for candidate := range strings.SplitSeq(os.Getenv("LANGUAGE"), ":") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || isPOSIXLocale(candidate) {
			continue
		}
		return candidate
	}

	// LANG is a POSIX locale variable, so using a UTF-8 locale here is
	// intentional: Fyne strips the charset when parsing, while child processes
	// still benefit from a UTF-8 process locale.
	return defaultUTF8Locale
}

func usesPOSIXLocaleEnvironment(goos string) bool {
	switch goos {
	case "linux", "darwin", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris":
		return true
	default:
		return false
	}
}

func isPOSIXLocale(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}

	trimmed, _, _ = strings.Cut(trimmed, ".")
	trimmed = strings.ReplaceAll(trimmed, "-", "_")

	return strings.EqualFold(trimmed, "C") || strings.EqualFold(trimmed, "POSIX")
}
