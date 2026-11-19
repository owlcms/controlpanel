package shared

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"controlpanel/shared/portowner"

	"github.com/gofrs/flock"
)

const MinimumJavaVersion = "25.0.5.1"

const javaUpdateUserAgent = "controlpanel-java-update"

var ErrJavaNotInstalled = errors.New("java is not installed")

type JavaInstallation struct {
	Version     string
	Home        string
	RuntimeRoot string
}

type JavaUpdateResult struct {
	Previous string
	Current  string
	Updated  bool
}

func InspectJava(temurinVersion string) (*JavaInstallation, error) {
	javaBase := filepath.Join(GetRuntimeDir(), "java")
	entries, err := os.ReadDir(javaBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrJavaNotInstalled
		}
		return nil, fmt.Errorf("reading Java runtime directory: %w", err)
	}

	major, err := ExtractMajorVersion(temurinVersion)
	if err != nil {
		return nil, err
	}

	var best *JavaInstallation
	for _, runtimeDir := range entries {
		if !runtimeDir.IsDir() {
			continue
		}
		root := filepath.Join(javaBase, runtimeDir.Name())
		installs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, install := range installs {
			if !install.IsDir() {
				continue
			}
			home := javaHomeForRoot(filepath.Join(root, install.Name()))
			releaseVersion, err := readJavaReleaseVersion(home)
			if err != nil {
				continue
			}
			installedMajor, err := ExtractMajorVersion(releaseVersion)
			if err != nil || installedMajor < major {
				continue
			}
			candidate := &JavaInstallation{
				Version:     releaseVersion,
				Home:        home,
				RuntimeRoot: root,
			}
			if best == nil {
				best = candidate
				continue
			}
			comparison, err := CompareJavaVersions(candidate.Version, best.Version)
			if err == nil && comparison > 0 {
				best = candidate
			}
		}
	}

	if best == nil {
		return nil, ErrJavaNotInstalled
	}
	return best, nil
}

func CompareJavaVersions(left, right string) (int, error) {
	leftParts, err := parseJavaVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseJavaVersion(right)
	if err != nil {
		return 0, err
	}
	for index := range leftParts {
		switch {
		case leftParts[index] < rightParts[index]:
			return -1, nil
		case leftParts[index] > rightParts[index]:
			return 1, nil
		}
	}
	return 0, nil
}

func JavaVersionAtLeast(current, minimum string) (bool, error) {
	comparison, err := CompareJavaVersions(current, minimum)
	return comparison >= 0, err
}

func parseJavaVersion(version string) ([4]int, error) {
	var parsed [4]int
	version = strings.TrimSpace(strings.TrimPrefix(version, "jdk-"))
	if build := strings.IndexByte(version, '+'); build >= 0 {
		version = version[:build]
	}
	parts := strings.Split(version, ".")
	if len(parts) == 0 || len(parts) > len(parsed) {
		return parsed, fmt.Errorf("invalid Java version %q", version)
	}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return parsed, fmt.Errorf("invalid Java version %q", version)
		}
		parsed[index] = value
	}
	return parsed, nil
}

func EnsureMinimumJava(progress ProgressCallback, cancel <-chan bool) (*JavaUpdateResult, error) {
	installed, err := InspectJava("jdk-25")
	if err != nil && !errors.Is(err, ErrJavaNotInstalled) {
		return nil, err
	}
	if err == nil {
		meets, compareErr := JavaVersionAtLeast(installed.Version, MinimumJavaVersion)
		if compareErr != nil {
			return nil, compareErr
		}
		if meets {
			return &JavaUpdateResult{Previous: installed.Version, Current: installed.Version}, nil
		}
	}
	return updateJava("jdk-25", MinimumJavaVersion, progress, cancel)
}

func UpdateToLatestJava(progress ProgressCallback, cancel <-chan bool) (*JavaUpdateResult, error) {
	return updateJava("jdk-25", "", progress, cancel)
}

func updateJava(temurinVersion, requiredVersion string, progress ProgressCallback, cancel <-chan bool) (*JavaUpdateResult, error) {
	lock := flock.New(filepath.Join(GetRuntimeDir(), "java-update.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("locking Java update: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("another Java update is already in progress")
	}
	defer lock.Unlock()

	target := GetSharedJavaDir(temurinVersion)
	if err := rejectJavaProcessesUsing(target); err != nil {
		return nil, err
	}
	ctx, stop := contextForJavaUpdate(cancel)
	defer stop()

	latestTag, err := FindLatestTemurinReleaseContext(ctx, temurinVersion, javaUpdateUserAgent)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, context.Canceled
		}
		return nil, err
	}
	latestVersion := strings.TrimSuffix(strings.TrimPrefix(latestTag, "jdk-"), "-LTS")
	_, err = parseJavaVersion(latestVersion)
	if err != nil {
		return nil, fmt.Errorf("parsing latest Temurin version %q: %w", latestTag, err)
	}

	current, inspectErr := InspectJava(temurinVersion)
	if inspectErr != nil && !errors.Is(inspectErr, ErrJavaNotInstalled) {
		return nil, inspectErr
	}
	previous := ""
	if current != nil {
		previous = current.Version
		comparison, compareErr := CompareJavaVersions(current.Version, latestVersion)
		if compareErr != nil {
			return nil, compareErr
		}
		if comparison >= 0 {
			if requiredVersion == "" {
				return &JavaUpdateResult{Previous: previous, Current: previous}, nil
			}
			meets, meetErr := JavaVersionAtLeast(current.Version, requiredVersion)
			if meetErr != nil {
				return nil, meetErr
			}
			if meets {
				return &JavaUpdateResult{Previous: previous, Current: previous}, nil
			}
		}
	}

	if requiredVersion != "" {
		meets, meetErr := JavaVersionAtLeast(latestVersion, requiredVersion)
		if meetErr != nil {
			return nil, meetErr
		}
		if !meets {
			return nil, fmt.Errorf("latest Temurin release %s does not meet required Java %s", latestVersion, requiredVersion)
		}
	}

	if err := checkJavaUpdateCancellation(cancel); err != nil {
		return nil, err
	}
	downloadURL, err := GetTemurinDownloadURLContext(ctx, latestTag, GetGoos, javaUpdateUserAgent)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, context.Canceled
		}
		return nil, err
	}

	runtimeDir := GetRuntimeDir()
	if err := EnsureDir0755(runtimeDir); err != nil {
		return nil, fmt.Errorf("creating runtime directory: %w", err)
	}
	staging, err := os.MkdirTemp(runtimeDir, ".java-update-")
	if err != nil {
		return nil, fmt.Errorf("creating Java staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	archive := filepath.Join(staging, "temurin")
	switch {
	case GetGoos() == "windows" && !IsWSL():
		archive += ".zip"
	default:
		archive += ".tar.gz"
	}
	if err := DownloadArchive(downloadURL, archive, progress, cancel); err != nil {
		return nil, err
	}
	if err := checkJavaUpdateCancellation(cancel); err != nil {
		return nil, err
	}
	extracted := filepath.Join(staging, "extracted")
	if err := EnsureDir0755(extracted); err != nil {
		return nil, fmt.Errorf("creating Java extraction directory: %w", err)
	}
	if strings.HasSuffix(archive, ".zip") {
		err = ExtractZip(archive, extracted, false)
	} else {
		err = ExtractTarGz(archive, extracted)
	}
	if err != nil {
		return nil, fmt.Errorf("extracting Java update: %w", err)
	}
	if err := checkJavaUpdateCancellation(cancel); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(extracted)
	if err != nil {
		return nil, fmt.Errorf("reading extracted Java update: %w", err)
	}
	var installedRoot string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		home := javaHomeForRoot(filepath.Join(extracted, entry.Name()))
		if version, readErr := readJavaReleaseVersion(home); readErr == nil {
			if comparison, compareErr := CompareJavaVersions(version, latestVersion); compareErr == nil && comparison >= 0 {
				installedRoot = filepath.Join(extracted, entry.Name())
				break
			}
		}
	}
	if installedRoot == "" {
		return nil, fmt.Errorf("downloaded Java archive does not contain Temurin %s", latestVersion)
	}

	installedVersion, err := readJavaReleaseVersion(javaHomeForRoot(installedRoot))
	if err != nil {
		return nil, err
	}
	if err := swapJavaInstallation(temurinVersion, extracted, cancel); err != nil {
		return nil, err
	}
	return &JavaUpdateResult{Previous: previous, Current: installedVersion, Updated: true}, nil
}

func swapJavaInstallation(temurinVersion, installedRoot string, cancel <-chan bool) error {
	target := GetSharedJavaDir(temurinVersion)
	javaBase := filepath.Dir(target)
	if err := EnsureDir0755(javaBase); err != nil {
		return fmt.Errorf("creating Java runtime directory: %w", err)
	}
	if err := checkJavaUpdateCancellation(cancel); err != nil {
		return err
	}
	if err := rejectJavaProcessesUsing(target); err != nil {
		return err
	}

	backup := ""
	if _, err := os.Stat(target); err == nil {
		backup = target + ".old-" + time.Now().Format("20060102T150405.000000000")
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("moving current Java aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking current Java: %w", err)
	}

	restore := func() error {
		var restoreErrors []string
		if err := os.RemoveAll(target); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("remove new Java: %v", err))
		}
		if backup != "" {
			if err := os.Rename(backup, target); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("restore old Java: %v", err))
			}
		}
		if len(restoreErrors) > 0 {
			return errors.New(strings.Join(restoreErrors, "; "))
		}
		return nil
	}
	if err := os.Rename(installedRoot, target); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return fmt.Errorf("installing updated Java: %w (rollback failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("installing updated Java: %w", err)
	}
	if err := rejectJavaProcessesUsing(backup); backup != "" && err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, restoreErr)
		}
		return err
	}
	if backup != "" {
		if err := os.RemoveAll(backup); err != nil {
			log.Printf("Could not remove old Java from %s: %v", backup, err)
		}
	}
	return nil
}

func rejectJavaProcessesUsing(runtimeRoot string) error {
	if strings.TrimSpace(runtimeRoot) == "" {
		return nil
	}
	processes, err := portowner.ListProcessesNamed(portowner.JavaExecutableNames...)
	if err != nil {
		return fmt.Errorf("checking processes using Java: %w", err)
	}
	absoluteRoot, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return fmt.Errorf("resolving Java runtime path: %w", err)
	}
	for _, process := range processes {
		executable, err := filepath.Abs(process.Executable)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(absoluteRoot, executable)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Java is still in use by PID %d; stop OWLCMS and Firmata before updating", process.PID)
		}
	}
	return nil
}

func javaHomeForRoot(root string) string {
	if GetGoos() == "darwin" {
		return filepath.Join(root, "Contents", "Home")
	}
	return root
}

func readJavaReleaseVersion(home string) (string, error) {
	file, err := os.Open(filepath.Join(home, "release"))
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found && key == "JAVA_VERSION" {
			return strings.Trim(strings.TrimSpace(value), `"`), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("JAVA_VERSION not found in %s", filepath.Join(home, "release"))
}

func checkJavaUpdateCancellation(cancel <-chan bool) error {
	select {
	case <-cancel:
		return context.Canceled
	default:
		return nil
	}
}

func contextForJavaUpdate(cancel <-chan bool) (context.Context, context.CancelFunc) {
	ctx, stop := context.WithCancel(context.Background())
	if cancel != nil {
		go func() {
			select {
			case <-cancel:
				stop()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, stop
}

var javaUpdateMu sync.Mutex

func SerializeJavaUpdate(update func() (*JavaUpdateResult, error)) (*JavaUpdateResult, error) {
	javaUpdateMu.Lock()
	defer javaUpdateMu.Unlock()
	return update()
}
