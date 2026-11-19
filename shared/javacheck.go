package shared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ExtractMajorVersion extracts the major version number from a Java version string
// e.g., "jdk-17.0.15+6" -> 17, "jdk-25" -> 25, "25.0.5.1" -> 25
func ExtractMajorVersion(version string) (int, error) {
	// Remove "jdk-" prefix if present
	version = strings.TrimPrefix(version, "jdk-")
	// Split by "." and "+" to get the major version
	parts := strings.Split(strings.Split(version, "+")[0], ".")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid version string: %s", version)
	}
	majorVersion, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("parsing major version: %w", err)
	}
	return majorVersion, nil
}

// CompareJDKVersions compares two jdk directory names and returns true if a is more recent than b
func CompareJDKVersions(a, b string) bool {
	// Extract version numbers from directory names (e.g., "jdk-17.0.9+9" -> "17.0.9")
	aVersion := strings.TrimPrefix(a, "jdk-")
	bVersion := strings.TrimPrefix(b, "jdk-")

	// Split into components
	aParts := strings.Split(strings.Split(aVersion, "+")[0], ".")
	bParts := strings.Split(strings.Split(bVersion, "+")[0], ".")

	// Compare each component
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aNum, _ := strconv.Atoi(aParts[i])
		bNum, _ := strconv.Atoi(bParts[i])
		if aNum != bNum {
			return aNum > bNum
		}
	}
	return len(aParts) > len(bParts)
}

// FindSystemJava finds Java in the system (JAVA_HOME or PATH)
// goosFunc should return the OS string (windows, darwin, linux)
func FindSystemJava(goosFunc func() string) (string, error) {
	javaHome := os.Getenv("JAVA_HOME")
	javaCommand := "java"
	goos := goosFunc()
	if goos == "windows" && !IsWSL() {
		javaCommand = "javaw"
	}
	if javaHome != "" {
		javaExecutable := filepath.Join(javaHome, "bin", javaCommand)
		if _, err := os.Stat(javaExecutable); err == nil {
			return javaExecutable, nil
		}
	}

	javaPath, err := exec.LookPath(javaCommand)
	if err == nil {
		return javaPath, nil
	}
	return "", fmt.Errorf("java executable not found")
}

// GetJavaVersion parses the version from java -version output
func GetJavaVersion(javaPath string) (int, error) {
	cmd := exec.Command(javaPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("getting java version: %w", err)
	}

	versionRegex := regexp.MustCompile(`(?:java|openjdk) version "(?:(\d+)\.)?(?:(\d+)\.)?(\d+)"`)
	matches := versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		majorVersionStr := matches[1]
		if majorVersionStr == "" {
			majorVersionStr = matches[2]
			if majorVersionStr == "" {
				majorVersionStr = matches[3]
			}
		}
		majorVersion, err := strconv.Atoi(majorVersionStr)
		if err != nil {
			return 0, fmt.Errorf("parsing java version: %w", err)
		}
		return majorVersion, nil
	}

	versionRegex = regexp.MustCompile(`(?:java|openjdk) (\d+)`)
	matches = versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		majorVersion, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("parsing java version: %w", err)
		}
		return majorVersion, nil
	}

	return 0, fmt.Errorf("could not parse java version from output: %s", string(output))
}
