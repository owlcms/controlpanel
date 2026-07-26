package shared

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/Masterminds/semver/v3"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ============================================================================
// Semantic Version Utilities
// ============================================================================

// IsValidSemVer checks if a string is a valid semantic version.
func IsValidSemVer(version string) bool {
	_, err := semver.NewVersion(version)
	return err == nil
}

// ParseVersionWithBuild parses a version string and extracts the base version and build metadata.
// For example: "1.2.3+4" returns ("1.2.3", "4"), "1.2.3+test.1" returns ("1.2.3", "test.1"), "1.2.3" returns ("1.2.3", "")
// Uses semver library to properly parse build metadata which can contain dots and alphanumerics.
func ParseVersionWithBuild(version string) (string, string) {
	v, err := semver.NewVersion(version)
	if err != nil {
		// Metadata is an opaque extension and may contain characters such as
		// underscores or accented Unicode that strict SemVer rejects.
		if before, after, ok := strings.Cut(version, "+"); ok {
			return before, after
		}
		return version, ""
	}

	// Get the base version without build metadata
	baseVersion := fmt.Sprintf("%d.%d.%d", v.Major(), v.Minor(), v.Patch())
	if v.Prerelease() != "" {
		baseVersion += "-" + v.Prerelease()
	}

	// Get build metadata
	buildMetadata := v.Metadata()

	return baseVersion, buildMetadata
}

// IsPrerelease checks if a version string is a prerelease version.
// Uses semver parsing to properly detect prereleases.
func IsPrerelease(version string) bool {
	v, err := semver.NewVersion(version)
	if err != nil {
		// Fall back to string check if semver parsing fails
		return strings.Contains(version, "-")
	}
	return v.Prerelease() != ""
}

// normalizeForComparison converts SNAPSHOT to lowercase so it sorts after rc/alpha/beta.
// Semver sorts prereleases alphabetically, so "snapshot" > "rc" > "beta" > "alpha".
// This is only used for comparison; actual version strings remain unchanged.
func normalizeForComparison(version string) string {
	// Replace SNAPSHOT with snapshot (lowercase) so it sorts after rc
	return strings.Replace(version, "SNAPSHOT", "snapshot", 1)
}

// NewVersionForComparison creates a semver.Version with SNAPSHOT normalized for proper ordering.
func NewVersionForComparison(version string) (*semver.Version, error) {
	baseVersion, _ := ParseVersionWithBuild(version)
	return semver.NewVersion(normalizeForComparison(baseVersion))
}

// CompareVersions compares two version strings and returns true if v1 > v2.
// SNAPSHOT prereleases are considered more recent than other prereleases (rc, alpha, beta)
// because SNAPSHOT is normalized to lowercase for comparison (snapshot > rc alphabetically).
func CompareVersions(v1Str, v2Str string) bool {
	v1Tag := ExtractSemver(v1Str)
	v2Tag := ExtractSemver(v2Str)

	v1, err1 := NewVersionForComparison(v1Tag)
	v2, err2 := NewVersionForComparison(v2Tag)

	if err1 == nil && err2 == nil {
		return v1.GreaterThan(v2)
	}

	if err1 != nil && err2 == nil {
		return false
	}
	if err1 == nil && err2 != nil {
		return true
	}

	return v1Tag > v2Tag
}

// semverRegex extracts the semver portion from a tag string, handling prefixes
// like "v", "firmata-v", "owlcms-", etc.
var semverRegex = regexp.MustCompile(`(v?\d+\.\d+\.\d+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+)?)$`)

// filenameVersionRegex finds a version suffix in a ZIP filename. Metadata is
// intentionally opaque here because the application allows Unicode and other
// filename-safe extension characters beyond strict SemVer metadata.
var filenameVersionRegex = regexp.MustCompile(`(?:^|_)(v?\d+\.\d+\.\d+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[^/]*)?)$`)

// ExtractSemver extracts the semantic version from a tag string, stripping any
// prefix (e.g. "firmata-v1.2.3" → "1.2.3", "v1.9.0-beta06" → "1.9.0-beta06").
// Returns the original string unchanged if no semver pattern is found.
func ExtractSemver(tag string) string {
	if match := semverRegex.FindString(tag); match != "" {
		return strings.TrimPrefix(match, "v")
	}
	return tag
}

// GetMostRecentStable finds the most recent stable (non-prerelease) version
// from a list of version strings using proper semver comparison.
// Prefixes are stripped automatically. Returns the clean semver string.
func GetMostRecentStable(releases []string) (string, error) {
	var best *semver.Version
	var bestStr string
	for _, release := range releases {
		tag := ExtractSemver(release)
		v, err := semver.NewVersion(tag)
		if err != nil {
			continue
		}
		if IsPrerelease(tag) {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
			bestStr = tag
		}
	}
	if best == nil {
		return "", fmt.Errorf("no stable release found")
	}
	return bestStr, nil
}

// GetMostRecentPrerelease finds the most recent prerelease version
// from a list of version strings using proper semver comparison.
// SNAPSHOT versions are normalized for comparison so they sort after rc/beta/alpha.
// Prefixes are stripped automatically. Returns the clean semver string.
func GetMostRecentPrerelease(releases []string) (string, error) {
	var best *semver.Version
	var bestStr string
	for _, release := range releases {
		tag := ExtractSemver(release)
		v, err := NewVersionForComparison(tag)
		if err != nil {
			continue
		}
		if !IsPrerelease(tag) {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
			bestStr = tag
		}
	}
	if best == nil {
		return "", fmt.Errorf("no prerelease found")
	}
	return bestStr, nil
}

// ExtractVersionFromFilename extracts a semantic version from a ZIP filename.
// Returns an error if the filename is not a valid semantic version.
func ExtractVersionFromFilename(fileName string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(fileName), ".zip") {
		log.Printf("ExtractVersionFromFilename: rejected '%s' - file must have .zip extension", fileName)
		return "", fmt.Errorf("file must have .zip extension")
	}

	// Remove .zip extension
	nameWithoutExt := strings.TrimSuffix(fileName, ".zip")

	// Extract the version suffix without treating underscores in metadata as a
	// filename separator (e.g. "..._2.20.2+beta09_PanAm").
	if match := filenameVersionRegex.FindStringSubmatch(nameWithoutExt); len(match) == 2 {
		nameWithoutExt = match[1]
		log.Printf("ExtractVersionFromFilename: extracted '%s' from '%s'", nameWithoutExt, fileName)
	}

	// Strip metadata (everything after +) before semver validation.
	// Metadata is intentionally allowed to contain Unicode (including accents).
	baseVersion := StripMetadata(nameWithoutExt)

	// Strip accents only from the base version (before +), not from metadata.
	// Metadata may legitimately contain accented characters (e.g., "équipes").
	normalizedBase := StripAccents(baseVersion)
	if normalizedBase != baseVersion {
		log.Printf("ExtractVersionFromFilename: normalized accents in base '%s' -> '%s'", baseVersion, normalizedBase)
		// Reconstruct nameWithoutExt with the accent-stripped base but original metadata
		if metaIdx := strings.Index(nameWithoutExt, "+"); metaIdx >= 0 {
			nameWithoutExt = normalizedBase + nameWithoutExt[metaIdx:]
		} else {
			nameWithoutExt = normalizedBase
		}
		baseVersion = normalizedBase
	}

	// Validate base version as semver
	if IsValidSemVer(baseVersion) {
		// Normalize the full version name to sanitize any forbidden chars in metadata
		normalized := NormalizeVersionName(nameWithoutExt)
		log.Printf("ExtractVersionFromFilename: successfully extracted version '%s' from '%s'", normalized, fileName)
		return normalized, nil
	}

	log.Printf("ExtractVersionFromFilename: rejected '%s' - '%s' is not a valid semantic version", fileName, nameWithoutExt)
	return "", fmt.Errorf("filename '%s' is not a valid semantic version", nameWithoutExt)
}

// StripAccents removes diacritics/accents from Unicode characters and converts to ASCII.
// For example: "équipes" becomes "equipes"
func StripAccents(s string) string {
	// Transform to NFD (Canonical Decomposition) to separate base chars from combining marks
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	if err != nil {
		return s // Return original if transformation fails
	}
	return result
}

// ============================================================================
// Version Management UI Components
// ============================================================================

// CreateDuplicateButton creates a duplicate button for a version.
// installDir is the base directory containing version folders.
// version is the version name to duplicate.
// w is the parent window.
// buttonContainer is the container to add the button to.
// onSuccess is called after successful duplication with the new version name.
func CreateDuplicateButton(installDir, version string, w fyne.Window, buttonContainer *fyne.Container, onSuccess func(newVersion string)) {
	duplicateButton := widget.NewButton("Duplicate", nil)
	duplicateButton.OnTapped = func() {
		ShowDuplicateVersionDialog(installDir, version, w, onSuccess)
	}
	buttonContainer.Add(container.NewPadded(duplicateButton))
}

// CreateRenameButton creates a rename button for a version.
// installDir is the base directory containing version folders.
// version is the version name to rename.
// w is the parent window.
// buttonContainer is the container to add the button to.
// onSuccess is called after successful rename with the new version name.
func CreateRenameButton(installDir, version string, w fyne.Window, buttonContainer *fyne.Container, onSuccess func(newVersion string)) {
	renameButton := widget.NewButton("Rename", nil)
	renameButton.OnTapped = func() {
		ShowRenameVersionDialog(installDir, version, w, onSuccess)
	}
	buttonContainer.Add(container.NewPadded(renameButton))
}

// ShowRenameVersionDialog displays a dialog to rename a version.
// installDir is the base directory containing version folders.
// version is the current version name.
// w is the parent window.
// onSuccess is called after successful rename with the new version name.
func ShowRenameVersionDialog(installDir, version string, w fyne.Window, onSuccess func(newVersion string)) {
	// Create entry widget for the new build identifier
	buildEntry := widget.NewEntry()
	buildEntry.SetPlaceHolder("e.g., test, backup, 1, myversion")
	buildEntry.SetText(GetCurrentBuildString(version))

	// Variable to hold the dialog so we can close it from ENTER handler
	var d dialog.Dialog

	// Function to perform the rename
	doRename := func() {
		newBuild := buildEntry.Text

		// If not empty, validate the build metadata
		if newBuild != "" {
			sanitizedBuild := SanitizeVersionBuild(newBuild)
			if sanitizedBuild == "" {
				dialog.ShowError(fmt.Errorf("build identifier '%s' becomes empty after sanitization", newBuild), w)
				return
			}

			// Validate using our extended metadata rules (allows Unicode; bans only filesystem-forbidden chars)
			if err := ValidateMetadata(sanitizedBuild); err != nil {
				dialog.ShowError(fmt.Errorf("invalid build identifier '%s': %w", newBuild, err), w)
				return
			}
		}

		newVersion, err := RenameVersion(installDir, version, newBuild)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to rename version: %w", err), w)
			return
		}

		if d != nil {
			d.Hide()
		}

		dialog.ShowInformation("Rename Complete",
			fmt.Sprintf("Successfully renamed %s to %s", version, newVersion), w)

		if onSuccess != nil {
			onSuccess(newVersion)
		}
	}

	// Handle ENTER key to trigger rename
	buildEntry.OnSubmitted = func(text string) {
		doRename()
	}

	// Create the form content with label on left, entry on right
	label := widget.NewLabel("New Name")
	entryContainer := container.NewGridWrap(fyne.NewSize(300, 35), buildEntry)
	formRow := container.NewHBox(label, entryContainer)

	noteLabel := widget.NewLabel("Note: Unicode letters, digits, '.', and '-' are allowed; spaces become '.' and other characters are stripped")
	noteLabel.TextStyle = fyne.TextStyle{Italic: true}

	content := container.NewVBox(
		formRow,
		noteLabel,
	)

	// Create custom dialog
	d = dialog.NewCustomConfirm("Rename Version", "Rename", "Cancel", content,
		func(ok bool) {
			if !ok {
				return
			}
			doRename()
		}, w)
	d.Show()

	// Focus the entry widget after showing dialog
	w.Canvas().Focus(buildEntry)
}

// ShowDuplicateVersionDialog displays a dialog to duplicate a version with a new name.
// installDir is the base directory containing version folders.
// version is the current version name.
// w is the parent window.
// onSuccess is called after successful duplication with the new version name.
func ShowDuplicateVersionDialog(installDir, version string, w fyne.Window, onSuccess func(newVersion string)) {
	// Get current build metadata and compute the collision-resolved name
	baseVersion, currentBuild := ParseVersionWithBuild(version)
	suggestedBuild := ResolveCollisionForBuild(installDir, baseVersion, currentBuild)

	// Create entry widget for the new build identifier
	buildEntry := widget.NewEntry()
	buildEntry.SetPlaceHolder("e.g., test, backup, copy")
	buildEntry.SetText(suggestedBuild)

	// Variable to hold the dialog so we can close it from ENTER handler
	var d dialog.Dialog

	// Create a status label that we can update
	statusLabel := widget.NewLabel("")
	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	// Create the form content with label on left, entry on right
	label := widget.NewLabel("New Name")
	entryContainer := container.NewGridWrap(fyne.NewSize(300, 35), buildEntry)
	formRow := container.NewHBox(label, entryContainer)

	noteLabel := widget.NewLabel("Note: Unicode letters, digits, '.', and '-' are allowed; spaces become '.' and other characters are stripped")
	noteLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Variables to hold buttons so we can modify them
	var duplicateBtn *widget.Button
	var cancelBtn *widget.Button

	// Function to perform the duplication
	doDuplicate := func() {
		newBuild := buildEntry.Text

		// If not empty, validate the build metadata
		if newBuild != "" {
			sanitizedBuild := SanitizeVersionBuild(newBuild)
			if sanitizedBuild == "" {
				dialog.ShowError(fmt.Errorf("build identifier '%s' becomes empty after sanitization", newBuild), w)
				return
			}

			// Validate using our extended metadata rules (allows Unicode; bans only filesystem-forbidden chars)
			if err := ValidateMetadata(sanitizedBuild); err != nil {
				dialog.ShowError(fmt.Errorf("invalid build identifier '%s': %w", newBuild, err), w)
				return
			}
		}

		// Disable buttons and entry during duplication
		duplicateBtn.Disable()
		cancelBtn.Disable()
		buildEntry.Disable()

		// Show progress message
		statusLabel.SetText("Duplicating...")
		statusLabel.Refresh()
		progressBar.SetValue(0)
		progressBar.Show()
		progressBar.Refresh()
		stopProgress := startTimedProgress(progressBar, 0, 1, 5*time.Second)

		// Perform duplication in background
		go func() {
			newVersion, err := DuplicateVersionWithName(installDir, version, newBuild)

			if err != nil {
				// Re-enable on error
				statusLabel.SetText("")
				statusLabel.Refresh()
				stopProgress()
				progressBar.Hide()
				buildEntry.Enable()
				duplicateBtn.Enable()
				cancelBtn.Enable()
				dialog.ShowError(fmt.Errorf("failed to duplicate version: %w", err), w)
				return
			}

			// Show success message
			stopProgress()
			progressBar.SetValue(1)
			progressBar.Refresh()
			statusLabel.SetText(fmt.Sprintf("Successfully created %s as a duplicate of %s", newVersion, version))
			statusLabel.Refresh()

			// Hide the form fields and note
			formRow.Hide()
			noteLabel.Hide()

			// Change button to "Close"
			duplicateBtn.SetText("Close")
			duplicateBtn.OnTapped = func() {
				if d != nil {
					d.Hide()
				}
				if onSuccess != nil {
					onSuccess(newVersion)
				}
			}
			duplicateBtn.Enable()
			duplicateBtn.Refresh()
		}()
	}

	// Handle ENTER key to trigger duplication
	buildEntry.OnSubmitted = func(text string) {
		doDuplicate()
	}

	content := container.NewVBox(
		formRow,
		noteLabel,
		progressBar,
		statusLabel,
	)

	// Create buttons manually to control dialog closing behavior
	duplicateBtn = widget.NewButton("Duplicate", func() {
		doDuplicate()
	})
	cancelBtn = widget.NewButton("Cancel", func() {
		if d != nil {
			d.Hide()
		}
	})

	buttons := container.NewHBox(
		layout.NewSpacer(),
		cancelBtn,
		duplicateBtn,
	)

	dialogContent := container.NewVBox(
		content,
		buttons,
	)

	// Create custom dialog that won't auto-close
	d = dialog.NewCustomWithoutButtons("Duplicate Version", dialogContent, w)
	d.Show()

	// Focus the entry widget after showing dialog
	w.Canvas().Focus(buildEntry)
}

// PromptForInstallVersionName checks for a version collision and prompts for a new build name when needed.
// If no collision, it calls onConfirm with the original version.
func PromptForInstallVersionName(installDir, version string, w fyne.Window, onConfirm func(newVersion string)) {
	if version == "" {
		onConfirm(version)
		return
	}

	basePath := filepath.Join(installDir, version)
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		onConfirm(version)
		return
	}

	baseVersion, currentBuild := ParseVersionWithBuild(version)
	suggestedBuild := ResolveCollisionForBuild(installDir, baseVersion, currentBuild)

	buildEntry := widget.NewEntry()
	buildEntry.SetPlaceHolder("e.g., test, backup, 1, myversion")
	buildEntry.SetText(suggestedBuild)

	message := widget.NewLabel("This version name already exists. Please enter a new build identifier to install this copy.")
	message.Wrapping = fyne.TextWrapWord

	label := widget.NewLabel("New Name")
	entryContainer := container.NewGridWrap(fyne.NewSize(300, 35), buildEntry)
	formRow := container.NewHBox(label, entryContainer)

	noteLabel := widget.NewLabel("Note: Unicode allowed in metadata, but no filesystem-forbidden characters (< > : \" / \\ | ? *)")
	noteLabel.Wrapping = fyne.TextWrapWord
	noteLabel.TextStyle = fyne.TextStyle{Italic: true}

	content := container.NewVBox(
		message,
		formRow,
		noteLabel,
	)

	var d dialog.Dialog
	confirm := func() {
		newBuild := strings.TrimSpace(buildEntry.Text)
		if newBuild == "" {
			dialog.ShowError(fmt.Errorf("new name cannot be empty"), w)
			return
		}

		// Validate metadata (allows Unicode, bans forbidden chars)
		if err := ValidateMetadata(newBuild); err != nil {
			dialog.ShowError(fmt.Errorf("invalid metadata: %w", err), w)
			return
		}

		testVersion := fmt.Sprintf("%s+%s", baseVersion, newBuild)

		if _, err := os.Stat(filepath.Join(installDir, testVersion)); err == nil {
			dialog.ShowError(fmt.Errorf("version %s already exists", testVersion), w)
			return
		}

		if d != nil {
			d.Hide()
		}
		onConfirm(testVersion)
	}

	buildEntry.OnSubmitted = func(text string) {
		confirm()
	}

	d = dialog.NewCustomConfirm("Rename Version", "Continue", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		confirm()
	}, w)
	d.Show()
	w.Canvas().Focus(buildEntry)
}

// startTimedProgress updates the progress bar from start to end over the given duration.
// It returns a stop function to halt updates when the task completes early.
func startTimedProgress(bar *widget.ProgressBar, start, end float64, duration time.Duration) func() {
	if bar == nil {
		return func() {}
	}
	if end < start {
		end = start
	}
	startTime := time.Now()
	ticker := time.NewTicker(200 * time.Millisecond)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)
				frac := float64(elapsed) / float64(duration)
				if frac > 1 {
					frac = 1
				}
				value := start + (end-start)*frac
				bar.SetValue(value)
				if frac >= 1 {
					return
				}
			}
		}
	}()

	return func() {
		select {
		case <-done:
			return
		default:
			close(done)
		}
	}
}

// FindNextUniqueVersionName finds the next unique version name by adding +1, +2, +3, etc.
// If the version already has a +N suffix, it increments it. Otherwise, it starts with +1.
func FindNextUniqueVersionName(baseDir, version string) (string, error) {
	// Parse the version to extract the base and any existing +N suffix
	baseVersion, buildMeta := ParseVersionWithBuild(version)

	// Try to parse build metadata as a number, or start from 1
	buildNum := 1
	if buildMeta != "" {
		if num, err := strconv.Atoi(buildMeta); err == nil {
			buildNum = num + 1
		}
	}

	// Try successive numbers until we find one that doesn't exist
	for {
		newVersion := fmt.Sprintf("%s+%d", baseVersion, buildNum)
		newPath := filepath.Join(baseDir, newVersion)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newVersion, nil
		}
		buildNum++

		// Safety check to avoid infinite loop
		if buildNum > 1000 {
			return "", fmt.Errorf("could not find unique version name after 1000 attempts")
		}
	}
}

// ResolveCollisionForBuild computes the collision-resolved build metadata for a version.
// If there's no collision, returns the original build metadata.
// If there's a collision, applies auto-increment logic.
func ResolveCollisionForBuild(installDir, baseVersion, currentBuild string) string {
	// Construct the test version
	var testVersion string
	if currentBuild == "" {
		testVersion = baseVersion
	} else {
		testVersion = fmt.Sprintf("%s+%s", baseVersion, currentBuild)
	}

	// Check if this would collide
	testPath := filepath.Join(installDir, testVersion)
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		// No collision - return original
		return currentBuild
	}

	// Collision detected - compute the auto-incremented name
	resolvedBuild := currentBuild

	if currentBuild == "" {
		// No build suffix - would become +1, +2, +3, etc.
		for i := 1; i < 1000; i++ {
			testVersion = fmt.Sprintf("%s+%d", baseVersion, i)
			testPath = filepath.Join(installDir, testVersion)
			if _, err := os.Stat(testPath); os.IsNotExist(err) {
				resolvedBuild = strconv.Itoa(i)
				break
			}
		}
	} else {
		// Has build suffix - check last dot-separated identifier
		parts := strings.Split(currentBuild, ".")
		lastPart := parts[len(parts)-1]

		if collisionNum, err := strconv.Atoi(lastPart); err == nil && len(lastPart) == 1 && collisionNum < 9 {
			// Single-digit 0-8 - try to increment
			baseParts := parts[:len(parts)-1]
			foundFree := false
			for i := collisionNum + 1; i < 10; i++ {
				if len(baseParts) > 0 {
					resolvedBuild = strings.Join(baseParts, ".") + "." + strconv.Itoa(i)
				} else {
					resolvedBuild = strconv.Itoa(i)
				}
				testVersion = fmt.Sprintf("%s+%s", baseVersion, resolvedBuild)
				testPath = filepath.Join(installDir, testVersion)
				if _, err := os.Stat(testPath); os.IsNotExist(err) {
					foundFree = true
					break
				}
			}
			if !foundFree {
				// All single digits taken, append .1
				for i := 1; i < 1000; i++ {
					resolvedBuild = fmt.Sprintf("%s.%d", currentBuild, i)
					testVersion = fmt.Sprintf("%s+%s", baseVersion, resolvedBuild)
					testPath = filepath.Join(installDir, testVersion)
					if _, err := os.Stat(testPath); os.IsNotExist(err) {
						break
					}
				}
			}
		} else {
			// Multi-digit, 9, or non-numeric - append .1
			for i := 1; i < 1000; i++ {
				resolvedBuild = fmt.Sprintf("%s.%d", currentBuild, i)
				testVersion = fmt.Sprintf("%s+%s", baseVersion, resolvedBuild)
				testPath = filepath.Join(installDir, testVersion)
				if _, err := os.Stat(testPath); os.IsNotExist(err) {
					break
				}
			}
		}
	}

	return resolvedBuild
}

// SanitizeVersionBuild sanitizes a user-provided build identifier.
// Extended metadata rules: Unicode letters, digits, dots, and hyphens are accepted.
// Spaces become dots, and other characters are stripped.
func SanitizeVersionBuild(input string) string {
	// Replace spaces with dots
	input = strings.ReplaceAll(input, " ", ".")

	// Build the sanitized string keeping only allowed characters
	var result strings.Builder
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			// Keep all Unicode letters and digits (including accented characters)
			result.WriteRune(r)
		} else if r == '.' || r == '-' {
			// Keep dots and hyphens
			result.WriteRune(r)
		}
	}

	sanitized := result.String()

	// Remove leading/trailing dots and hyphens
	sanitized = strings.Trim(sanitized, ".-")

	// Collapse multiple consecutive dots or hyphens
	for strings.Contains(sanitized, "..") {
		sanitized = strings.ReplaceAll(sanitized, "..", ".")
	}
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}

	return sanitized
}

// RenameVersion renames a version by updating the build metadata after the +.
// If newBuild is empty, removes the build metadata. If there's a collision, auto-increments.
// Returns the new version name.
func RenameVersion(baseDir, oldVersion, newBuild string) (string, error) {
	baseVersion, _ := ParseVersionWithBuild(oldVersion)

	// Sanitize the new build identifier (empty is allowed)
	newBuild = SanitizeVersionBuild(newBuild)

	// Use collision resolution to get the final build metadata
	resolvedBuild := ResolveCollisionForBuild(baseDir, baseVersion, newBuild)

	// Create the new version name
	var newVersion string
	if resolvedBuild == "" {
		newVersion = baseVersion
	} else {
		newVersion = fmt.Sprintf("%s+%s", baseVersion, resolvedBuild)
	}

	if resolvedBuild != newBuild {
		log.Printf("RenameVersion: resolved collision from %s to %s", newBuild, resolvedBuild)
	}

	// Construct full paths
	oldPath := filepath.Join(baseDir, oldVersion)
	newPath := filepath.Join(baseDir, newVersion)

	// Log the current working directory and full paths before rename
	cwd, _ := os.Getwd()
	log.Printf("RenameVersion: current working directory: %s", cwd)
	log.Printf("RenameVersion: baseDir: %s", baseDir)
	log.Printf("RenameVersion: attempting to rename:")
	log.Printf("  FROM: %s", oldPath)
	log.Printf("  TO:   %s", newPath)

	// Change to the base directory to avoid locking the directory being renamed
	// This is needed on Windows where a directory cannot be renamed if it's the current working directory
	oldCwd, err := os.Getwd()
	if err == nil {
		if chErr := os.Chdir(baseDir); chErr == nil {
			newCwd, _ := os.Getwd()
			log.Printf("RenameVersion: changed working directory to: %s", newCwd)
			defer os.Chdir(oldCwd)
		} else {
			log.Printf("RenameVersion: failed to change directory to %s: %v", baseDir, chErr)
		}
	}

	// Rename the directory
	if err := os.Rename(oldPath, newPath); err != nil {
		return "", fmt.Errorf("failed to rename directory: %w", err)
	}

	log.Printf("RenameVersion: successfully renamed to %s", newVersion)
	return newVersion, nil
}

// DuplicateVersion creates a copy of a version directory with a new unique name.
// Returns the new version name.
func DuplicateVersion(baseDir, version string) (string, error) {
	// Find a unique name for the duplicate
	newVersion, err := FindNextUniqueVersionName(baseDir, version)
	if err != nil {
		return "", err
	}

	// Copy the directory
	srcPath := filepath.Join(baseDir, version)
	dstPath := filepath.Join(baseDir, newVersion)

	if err := CopyDir(srcPath, dstPath); err != nil {
		return "", fmt.Errorf("failed to copy directory: %w", err)
	}

	return newVersion, nil
}

// DuplicateVersionWithName creates a copy of a version directory with a specified build name.
// Handles collision resolution automatically. Returns the new version name.
func DuplicateVersionWithName(baseDir, version, newBuild string) (string, error) {
	baseVersion, _ := ParseVersionWithBuild(version)

	// Sanitize the new build identifier (empty is allowed)
	newBuild = SanitizeVersionBuild(newBuild)

	// Use collision resolution to get the final build metadata
	resolvedBuild := ResolveCollisionForBuild(baseDir, baseVersion, newBuild)

	// Create the new version name
	var newVersion string
	if resolvedBuild == "" {
		newVersion = baseVersion
	} else {
		newVersion = fmt.Sprintf("%s+%s", baseVersion, resolvedBuild)
	}

	if resolvedBuild != newBuild {
		log.Printf("DuplicateVersionWithName: resolved collision from %s to %s", newBuild, resolvedBuild)
	}

	if _, err := DuplicateVersionDirectory(baseDir, version, newVersion); err != nil {
		return "", err
	}

	log.Printf("DuplicateVersionWithName: successfully duplicated to %s", newVersion)
	return newVersion, nil
}

// DuplicateVersionDirectory creates an exact copy of version under newVersion.
func DuplicateVersionDirectory(baseDir, version, newVersion string) (string, error) {
	version = strings.TrimSpace(version)
	newVersion = strings.TrimSpace(newVersion)
	if version == "" {
		return "", fmt.Errorf("source version is required")
	}
	if newVersion == "" {
		return "", fmt.Errorf("duplicate version name is required")
	}
	if newVersion == "." || newVersion == ".." || filepath.Base(newVersion) != newVersion || strings.ContainsAny(newVersion, `<>:"/\|?*`) {
		return "", fmt.Errorf("duplicate version name %q is not a safe directory name", newVersion)
	}

	srcPath := filepath.Join(baseDir, version)
	dstPath := filepath.Join(baseDir, newVersion)

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("source version %q not found: %w", version, err)
	}
	if !srcInfo.IsDir() {
		return "", fmt.Errorf("source version %q is not a directory", version)
	}
	if _, err := os.Stat(dstPath); err == nil {
		return "", fmt.Errorf("destination version %q already exists", newVersion)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking destination version %q: %w", newVersion, err)
	}

	log.Printf("DuplicateVersionDirectory: copying %s to %s", srcPath, dstPath)
	if err := CopyDir(srcPath, dstPath); err != nil {
		return "", fmt.Errorf("failed to copy directory: %w", err)
	}

	return newVersion, nil
}

// CopyDir recursively copies a directory
func CopyDir(src, dst string) error {
	// Get properties of source dir
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create the destination directory
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// CopyFile copies a single file and preserves timestamps
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := srcFile.WriteTo(dstFile); err != nil {
		return err
	}

	// Preserve the modification time
	if err := os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		// Log but don't fail if we can't preserve timestamps
		log.Printf("Warning: failed to preserve timestamp for %s: %v", dst, err)
	}

	return nil
}

// GetVersionsWithBuildMetadata returns a list of all versions including those with +N suffixes
func GetVersionsWithBuildMetadata(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	// Pattern matches: X.Y.Z, X.Y.Z-prerelease, X.Y.Z+build, X.Y.Z-prerelease+build
	versionPattern := regexp.MustCompile(`^\d+\.\d+\.\d+(?:-(?:rc|alpha|beta)(?:\d+)?)?(?:\+.*)?$`)

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && versionPattern.MatchString(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}

	// Sort versions using semver logic
	var semvers []*semver.Version
	versionMap := make(map[string]string) // Maps parsed version to original string

	for _, v := range versions {
		// For sorting purposes, we need to handle the +build metadata
		// semver library ignores build metadata in comparisons, which is what we want
		baseVersion, _ := ParseVersionWithBuild(v)
		sv, err := semver.NewVersion(baseVersion)
		if err == nil {
			semvers = append(semvers, sv)
			versionMap[sv.String()] = v
		}
	}

	// Sort in reverse (newest first)
	var sorted []string
	for _, semver := range slices.Backward(semvers) {
		originalVersion := versionMap[semver.String()]
		sorted = append(sorted, originalVersion)
	}

	return sorted, nil
}

// GetCurrentBuildString extracts the current build metadata from a version string.
// Returns empty string if no build metadata exists.
func GetCurrentBuildString(version string) string {
	_, buildMetadata := ParseVersionWithBuild(version)
	return buildMetadata
}
