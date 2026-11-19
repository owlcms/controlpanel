package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareJavaVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"25.0.3", "25.0.5.1", -1},
		{"25.0.5", "25.0.5.1", -1},
		{"25.0.5.1+2", "25.0.5.1+99", 0},
		{"25.0.6", "25.0.5.1", 1},
		{"26", "25.0.5.1", 1},
	}
	for _, test := range tests {
		t.Run(test.left+"_vs_"+test.right, func(t *testing.T) {
			got, err := CompareJavaVersions(test.left, test.right)
			if err != nil {
				t.Fatalf("CompareJavaVersions: %v", err)
			}
			if got != test.want {
				t.Fatalf("CompareJavaVersions(%q, %q) = %d, want %d",
					test.left, test.right, got, test.want)
			}
		})
	}
}

func TestJavaVersionAtLeastRejectsMissingFourthPart(t *testing.T) {
	meets, err := JavaVersionAtLeast("25.0.5", "25.0.5.1")
	if err != nil {
		t.Fatalf("JavaVersionAtLeast: %v", err)
	}
	if meets {
		t.Fatal("25.0.5 must not meet minimum 25.0.5.1")
	}
}

func TestInspectJavaReadsReleaseFile(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtime)
	home := javaHomeForRoot(filepath.Join(runtime, "java", "jdk-25", "jdk-25.0.3+9-jre"))
	if err := writeJavaRelease(home, "25.0.3"); err != nil {
		t.Fatal(err)
	}

	installed, err := InspectJava("jdk-25")
	if err != nil {
		t.Fatalf("InspectJava: %v", err)
	}
	if installed.Version != "25.0.3" || installed.Home != home {
		t.Fatalf("unexpected Java installation: %#v", installed)
	}
}

func TestSwapJavaInstallationPreservesOldLayoutOnFailure(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtime)
	target := filepath.Join(runtime, "java", "jdk-25")
	oldRoot := filepath.Join(target, "jdk-25.0.3+9-jre")
	if err := writeJavaRelease(javaHomeForRoot(oldRoot), "25.0.3"); err != nil {
		t.Fatal(err)
	}

	stage := filepath.Join(runtime, ".java-update-test")
	if err := writeJavaRelease(javaHomeForRoot(filepath.Join(stage, "jdk-25.0.5.1+2-jre")), "25.0.5.1"); err != nil {
		t.Fatal(err)
	}
	cancel := make(chan bool)
	close(cancel)

	if err := swapJavaInstallation("jdk-25", stage, cancel); err == nil {
		t.Fatal("cancelled swap unexpectedly succeeded")
	}
	if version, err := readJavaReleaseVersion(javaHomeForRoot(oldRoot)); err != nil || version != "25.0.3" {
		t.Fatalf("old Java changed after cancelled swap: version=%q err=%v", version, err)
	}
}

func TestSwapJavaInstallationReplacesRuntimeAndInspectsNewRelease(t *testing.T) {
	runtime := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtime)
	target := filepath.Join(runtime, "java", "jdk-25")
	oldHome := javaHomeForRoot(filepath.Join(target, "jdk-25.0.3+9-jre"))
	if err := writeJavaRelease(oldHome, "25.0.3"); err != nil {
		t.Fatal(err)
	}

	stage := filepath.Join(runtime, ".java-update-test")
	newHome := javaHomeForRoot(filepath.Join(stage, "jdk-25.0.5.1+2-jre"))
	if err := writeJavaRelease(newHome, "25.0.5.1"); err != nil {
		t.Fatal(err)
	}
	if err := swapJavaInstallation("jdk-25", stage, nil); err != nil {
		t.Fatalf("swapJavaInstallation: %v", err)
	}

	installed, err := InspectJava("jdk-25")
	if err != nil {
		t.Fatalf("InspectJava: %v", err)
	}
	if installed.Version != "25.0.5.1" {
		t.Fatalf("installed version = %q, want 25.0.5.1", installed.Version)
	}
	if _, err := os.Stat(oldHome); !os.IsNotExist(err) {
		t.Fatalf("old Java still exists at %s", oldHome)
	}
}

func writeJavaRelease(home, version string) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	if err := writeJavaLauncher(javaExecutable(home, GetGoos())); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "release"),
		[]byte("IMPLEMENTOR=\"Eclipse Adoptium\"\nJAVA_VERSION=\""+version+"\"\n"), 0o644)
}

func writeJavaLauncher(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("launcher"), 0o755)
}
