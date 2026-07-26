package shared

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateMetadata checks if metadata contains only allowed characters.
// Allows Unicode letters, digits, hyphens, and dots, but bans filesystem-forbidden characters.
// This is an intentional extension of semver metadata rules to support localized content.
func ValidateMetadata(metadata string) error {
	if metadata == "" {
		return nil
	}

	// Forbidden characters for Windows and Linux filesystems
	// Also ban + to ensure it's only used as metadata separator
	forbidden := `<>:"/\|?*+`

	for _, r := range metadata {
		// Ban control characters (0x00-0x1F)
		if r < 0x20 {
			return fmt.Errorf("metadata contains control character (0x%02X)", r)
		}

		// Ban filesystem-forbidden characters
		if strings.ContainsRune(forbidden, r) {
			return fmt.Errorf("metadata contains forbidden character '%c'", r)
		}
	}

	return nil
}

// ValidateVersionName validates a complete version name including metadata.
// Strips metadata at the FIRST +, validates the base version with semver, then validates the metadata separately.
func ValidateVersionName(versionName string) error {
	// Split on + to separate base version from metadata (only at FIRST +)
	parts := strings.SplitN(versionName, "+", 2)
	baseVersion := parts[0]
	var metadata string
	if len(parts) > 1 {
		metadata = parts[1]
		// Check that metadata doesn't contain additional + signs
		if strings.Contains(metadata, "+") {
			return fmt.Errorf("metadata cannot contain '+' character (only one '+' allowed as separator)")
		}
	}

	// Validate base version (before +) - this should be strict semver
	// We allow the base to have prerelease info (e.g., 1.0.0-beta.1)
	if baseVersion == "" {
		return fmt.Errorf("version name cannot be empty")
	}

	// Validate metadata if present (extended rules: allow Unicode)
	if metadata != "" {
		if err := ValidateMetadata(metadata); err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
	}

	return nil
}

// SanitizeMetadata removes any forbidden characters from metadata.
// Plus signs are replaced with dots to preserve structure.
// Returns the sanitized string and a boolean indicating if changes were made.
func SanitizeMetadata(metadata string) (string, bool) {
	if metadata == "" {
		return "", false
	}

	forbidden := `<>:"/\|?*`
	var result strings.Builder
	changed := false

	for _, r := range metadata {
		// Remove control characters
		if r < 0x20 {
			changed = true
			continue
		}

		// Replace + with . (for manually created filenames with + in metadata)
		if r == '+' {
			result.WriteRune('.')
			changed = true
			continue
		}

		// Remove other forbidden characters
		if strings.ContainsRune(forbidden, r) {
			changed = true
			continue
		}

		result.WriteRune(r)
	}

	return result.String(), changed
}

// NormalizeVersionName silently sanitizes a version name by replacing forbidden characters in metadata.
// This is useful when reading version names from manually created directories/files.
// Returns the normalized version string.
func NormalizeVersionName(versionName string) string {
	// Split on first + to separate base version from metadata
	parts := strings.SplitN(versionName, "+", 2)
	if len(parts) == 1 {
		// No metadata, return as-is
		return versionName
	}

	baseVersion := parts[0]
	metadata := parts[1]

	// Sanitize the metadata
	sanitized, _ := SanitizeMetadata(metadata)

	// Reconstruct version with sanitized metadata
	if sanitized == "" {
		return baseVersion
	}
	return baseVersion + "+" + sanitized
}

// NormalizeVersionMetadata preserves Unicode letters and digits while
// converting unsupported metadata separators to dots.
func NormalizeVersionMetadata(metadata string) string {
	var result strings.Builder
	for _, r := range metadata {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('.')
		}
	}

	normalized := strings.Trim(result.String(), ".-")
	for strings.Contains(normalized, "..") {
		normalized = strings.ReplaceAll(normalized, "..", ".")
	}
	return normalized
}

// StripMetadata removes metadata (everything after +) from a version string.
func StripMetadata(version string) string {
	if before, _, ok := strings.Cut(version, "+"); ok {
		return before
	}
	return version
}

// HasUnicodeLetters checks if a string contains any Unicode letters (non-ASCII).
func HasUnicodeLetters(s string) bool {
	for _, r := range s {
		if r > 127 && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
