package shared

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Installed runtimes live in <runtime dir>/java/<requested version>/<archive root>, for example
// java/jdk-25/jdk-25.0.5+8-jre (Temurin) or java/jdk-25/jre-25.0.5.1 (Liberica).
// Runtimes are identified by their contents, not by these names, so every
// distribution is found the same way by launch, inspection and update.

// javaHomeForRoot returns the Java home inside an archive root. Temurin's macOS
// archives use the bundle layout Contents/Home; Liberica's and all other archives
// have bin/ and release directly under the root.
func javaHomeForRoot(root string) string {
	bundleHome := filepath.Join(root, "Contents", "Home")
	if _, err := os.Stat(filepath.Join(bundleHome, "release")); err == nil {
		return bundleHome
	}
	return root
}

// javaExecutable returns the launcher used to start OWLCMS and Firmata.
func javaExecutable(home, goos string) string {
	if goos == "windows" && !IsWSL() {
		return filepath.Join(home, "bin", "javaw.exe")
	}
	return filepath.Join(home, "bin", "java")
}

// listInstalledJava returns every runtime under the shared Java directory that has
// a release file and a launcher.
func listInstalledJava(goos string) ([]JavaInstallation, error) {
	javaBase := filepath.Join(GetRuntimeDir(), "java")
	runtimeDirs, err := os.ReadDir(javaBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrJavaNotInstalled
		}
		return nil, fmt.Errorf("reading Java runtime directory: %w", err)
	}

	var installations []JavaInstallation
	for _, runtimeDir := range runtimeDirs {
		if !runtimeDir.IsDir() {
			continue
		}
		runtimeRoot := filepath.Join(javaBase, runtimeDir.Name())
		archiveRoots, err := os.ReadDir(runtimeRoot)
		if err != nil {
			continue
		}
		for _, archiveRoot := range archiveRoots {
			if !archiveRoot.IsDir() {
				continue
			}
			home := javaHomeForRoot(filepath.Join(runtimeRoot, archiveRoot.Name()))
			version, err := readJavaReleaseVersion(home)
			if err != nil {
				continue
			}
			executable := javaExecutable(home, goos)
			if _, err := os.Stat(executable); err != nil {
				continue
			}
			installations = append(installations, JavaInstallation{
				Version:     version,
				Home:        home,
				RuntimeRoot: runtimeRoot,
				Executable:  executable,
			})
		}
	}
	return installations, nil
}

// bestInstalledJava returns the newest installed runtime whose major version is at
// least the major version of requested ("jdk-25", "jdk-25.0.1+9", ...).
func bestInstalledJava(requested, goos string) (*JavaInstallation, error) {
	requiredMajor, err := ExtractMajorVersion(requested)
	if err != nil {
		return nil, err
	}
	installations, err := listInstalledJava(goos)
	if err != nil {
		return nil, err
	}

	var best *JavaInstallation
	for index := range installations {
		candidate := &installations[index]
		installedMajor, err := ExtractMajorVersion(candidate.Version)
		if err != nil || installedMajor < requiredMajor {
			continue
		}
		if best == nil {
			best = candidate
			continue
		}
		if comparison, err := CompareJavaVersions(candidate.Version, best.Version); err == nil && comparison > 0 {
			best = candidate
		}
	}
	if best == nil {
		return nil, ErrJavaNotInstalled
	}
	return best, nil
}

// FindLocalJavaForVersion returns the launcher of the newest installed runtime whose
// major version is at least that of temurinVersion, whichever distribution provided it.
// goosFunc should return the OS string (windows, darwin, linux)
func FindLocalJavaForVersion(temurinVersion string, goosFunc func() string) (string, error) {
	installation, err := bestInstalledJava(temurinVersion, goosFunc())
	if err != nil {
		log.Printf("*** No suitable Java for %s in %s: %v\n",
			temurinVersion, filepath.Join(GetRuntimeDir(), "java"), err)
		return "", fmt.Errorf("no Java installation found for %s: %w", temurinVersion, err)
	}
	log.Printf("*** Found suitable Java %s for %s at: %s\n",
		installation.Version, temurinVersion, installation.Executable)
	return installation.Executable, nil
}
