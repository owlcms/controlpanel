package shared

import (
	"os"
	"path/filepath"
	"testing"
)

// The archive roots below are the real top-level directories of the Temurin and
// Liberica archives; launch and update must find both the same way.

func installJavaFixture(t *testing.T, root, home, version, goos string) string {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "release"), []byte("JAVA_VERSION=\""+version+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executable := javaExecutable(home, goos)
	if err := writeJavaLauncher(executable); err != nil {
		t.Fatal(err)
	}
	if got := javaHomeForRoot(root); got != home {
		t.Fatalf("javaHomeForRoot(%s) = %s, want %s", root, got, home)
	}
	return executable
}

func TestFindLocalJavaFindsEveryDistributionLayout(t *testing.T) {
	if IsWSL() {
		t.Skip("launcher names differ under WSL")
	}
	layouts := []struct {
		name, goos, archiveRoot string
		bundle                  bool
	}{
		{"Temurin Windows", "windows", "jdk-25.0.5+8-jre", false},
		{"Liberica Windows", "windows", "jre-25.0.5.1", false},
		{"Temurin macOS", "darwin", "jdk-25.0.5+8-jre", true},
		{"Liberica macOS", "darwin", "jre-25.0.5.1.jre", false},
		{"Temurin Linux", "linux", "jdk-25.0.5+8-jre", false},
		{"Liberica Linux", "linux", "jre-25.0.5.1", false},
	}
	for _, layout := range layouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Setenv("RUNTIME_DIR", t.TempDir())
			root := filepath.Join(GetSharedJavaDir("jdk-25"), layout.archiveRoot)
			home := root
			if layout.bundle {
				home = filepath.Join(root, "Contents", "Home")
			}
			want := installJavaFixture(t, root, home, "25.0.5.1", layout.goos)

			got, err := FindLocalJavaForVersion("jdk-25", func() string { return layout.goos })
			if err != nil {
				t.Fatalf("FindLocalJavaForVersion: %v", err)
			}
			if got != want {
				t.Fatalf("FindLocalJavaForVersion = %s, want %s", got, want)
			}
		})
	}
}

func TestFindLocalJavaPrefersNewestReleaseAcrossDistributions(t *testing.T) {
	t.Setenv("RUNTIME_DIR", t.TempDir())
	goos := GetGoos()
	javaDir := GetSharedJavaDir("jdk-25")
	temurinRoot := filepath.Join(javaDir, "jdk-25.0.5+8-jre")
	installJavaFixture(t, temurinRoot, temurinRoot, "25.0.5", goos)
	libericaRoot := filepath.Join(javaDir, "jre-25.0.5.1")
	want := installJavaFixture(t, libericaRoot, libericaRoot, "25.0.5.1", goos)

	got, err := FindLocalJavaForVersion("jdk-25", GetGoos)
	if err != nil {
		t.Fatalf("FindLocalJavaForVersion: %v", err)
	}
	if got != want {
		t.Fatalf("FindLocalJavaForVersion = %s, want %s", got, want)
	}

	installed, err := InspectJava("jdk-25")
	if err != nil {
		t.Fatalf("InspectJava: %v", err)
	}
	if installed.Executable != want || installed.Version != "25.0.5.1" {
		t.Fatalf("InspectJava and launch disagree: %#v, launch uses %s", installed, want)
	}
}

func TestFindLocalJavaIgnoresOlderMajorAndIncompleteRuntime(t *testing.T) {
	t.Setenv("RUNTIME_DIR", t.TempDir())
	goos := GetGoos()
	oldRoot := filepath.Join(GetSharedJavaDir("jdk-17"), "jdk-17.0.15+6-jre")
	installJavaFixture(t, oldRoot, oldRoot, "17.0.15", goos)
	incomplete := filepath.Join(GetSharedJavaDir("jdk-25"), "jre-25.0.5.1")
	if err := os.MkdirAll(incomplete, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incomplete, "release"), []byte("JAVA_VERSION=\"25.0.5.1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := FindLocalJavaForVersion("jdk-25", GetGoos); err == nil {
		t.Fatalf("expected no Java 25, found %s", got)
	}
}
