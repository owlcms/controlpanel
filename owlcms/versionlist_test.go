package owlcms

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeTestJar(t *testing.T, jarPath string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(jarPath), 0755); err != nil {
		t.Fatalf("mkdir jar dir: %v", err)
	}
	f, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create jar: %v", err)
	}
	zw := zip.NewWriter(f)
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", k, err)
		}
		if _, err := w.Write([]byte(files[k])); err != nil {
			t.Fatalf("write zip entry %s: %v", k, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close jar file: %v", err)
	}
}

func writeTestLocalFiles(t *testing.T, localDir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(localDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return out
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func TestRestoreLocalCreatesMissingNewLocalAndCopiesModifiedCSS(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")

	writeTestJar(t, filepath.Join(oldDir, "owlcms.jar"), map[string]string{
		"css/plates.css":    "old-default-plates",
		"css/results.css":   "old-default-results",
		"flags/README.txt":  "flags-readme",
		"logback.xml":       "default-logback",
		"templates/a/1.txt": "tpl1",
	})
	writeTestLocalFiles(t, filepath.Join(oldDir, "local"), map[string]string{
		"css/plates.css":    "custom-plates",
		"css/results.css":   "old-default-results",
		"flags/README.txt":  "flags-readme",
		"flags/CAN.svg":     "<svg>CAN</svg>",
		"pictures/4123.jpg": "jpg-bytes",
		"logback.xml":       "custom-logback",
		".DS_Store":         "finder-junk",
		"templates/a/1.txt": "tpl1",
	})
	// Intentionally no newDir/local directory at all.
	writeTestJar(t, filepath.Join(newDir, "owlcms.jar"), map[string]string{
		"css/plates.css":    "new-default-plates",
		"css/results.css":   "new-default-results",
		"flags/README.txt":  "flags-readme",
		"logback.xml":       "default-logback",
		"templates/a/1.txt": "tpl1-v2",
		"agegroups/IWF.txt": "iwf-v2",
	})

	if err := restoreLocalFilesFromPreviousVersion(newDir, oldDir); err != nil {
		t.Fatalf("restoreLocalFilesFromPreviousVersion failed: %v", err)
	}

	got := readTree(t, filepath.Join(newDir, "local"))
	want := map[string]string{
		"css/plates.css":    "custom-plates",
		"flags/CAN.svg":     "<svg>CAN</svg>",
		"pictures/4123.jpg": "jpg-bytes",
		"logback.xml":       "custom-logback",
		"templates/a/1.txt": "tpl1-v2", // untouched templates/ present in oldLocal tracks newJar; absent agegroups/ is not created
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("new local tree mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestRestoreLocalPrunesStandardFilesMatchingNewJar(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")

	writeTestJar(t, filepath.Join(oldDir, "owlcms.jar"), map[string]string{
		"css/results.css": "buggy-css",
	})
	writeTestLocalFiles(t, filepath.Join(oldDir, "local"), map[string]string{
		"css/results.css": "fixed-css",
	})
	writeTestJar(t, filepath.Join(newDir, "owlcms.jar"), map[string]string{
		"css/results.css": "fixed-css",
	})

	if err := restoreLocalFilesFromPreviousVersion(newDir, oldDir); err != nil {
		t.Fatalf("restoreLocalFilesFromPreviousVersion failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(newDir, "local")); !os.IsNotExist(err) {
		t.Fatalf("expected empty newDir/local to be removed after pruning, stat err=%v", err)
	}
}

func TestRestoreLocalMissingOldLocalClearsNewLocal(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")

	writeTestJar(t, filepath.Join(oldDir, "owlcms.jar"), map[string]string{
		"css/results.css": "old",
	})
	writeTestJar(t, filepath.Join(newDir, "owlcms.jar"), map[string]string{
		"css/results.css": "new",
	})
	writeTestLocalFiles(t, filepath.Join(newDir, "local"), map[string]string{
		"css/results.css": "stale-default",
	})

	if err := restoreLocalFilesFromPreviousVersion(newDir, oldDir); err != nil {
		t.Fatalf("restoreLocalFilesFromPreviousVersion failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(newDir, "local")); !os.IsNotExist(err) {
		t.Fatalf("expected newDir/local not to exist when oldDir/local is absent, stat err=%v", err)
	}
}

func TestRestoreLocalKitMaintainerWorkflowAcrossUpdates(t *testing.T) {
	root := t.TempDir()
	v1Dir := filepath.Join(root, "v1")
	v2Dir := filepath.Join(root, "v2")
	v3Dir := filepath.Join(root, "v3")

	writeTestJar(t, filepath.Join(v1Dir, "owlcms.jar"), map[string]string{
		"templates/results/K1.xlsx":      "K1-v1",
		"templates/results/K2.xlsx":      "K2-v1",
		"templates/results/Legacy.xlsx":  "Legacy-v1",
		"templates/results/X1.xlsx":      "X1-v1",
		"templates/results/Dropped.xlsx": "Dropped-v1",
		"templates/results/sub/S.xlsx":   "S-v1",
		"templates/scripts/print.bat":    "print-v1",
		"templates/timing/Timing.xlsx":   "Timing-v1",
		"templates/timing/old/Old.xlsx":  "Old-v1",
		"templates/weighin/W1.xlsx":      "W1-v1",
		"agegroups/IWF.xlsx":             "IWF-v1",
		"agegroups/USAW.xlsx":            "USAW-v1",
	})

	// Kit maintainer starts from full v1 kit, modifies only templates/results:
	// adds N1/N2, removes X1 (kept in v2 jar) and Dropped (also removed in v2 jar),
	// keeps K1 and Legacy identical (Legacy is removed in v2 jar), edits K2;
	// leaves sub, timing/Timing.xlsx (without old/), weighin, and agegroups untouched,
	// and omits templates/scripts/.
	writeTestLocalFiles(t, filepath.Join(v1Dir, "local"), map[string]string{
		"templates/results/N1.xlsx":     "N1",
		"templates/results/N2.xlsx":     "N2",
		"templates/results/K1.xlsx":     "K1-v1",
		"templates/results/K2.xlsx":     "K2-custom",
		"templates/results/Legacy.xlsx": "Legacy-v1",
		"templates/results/sub/S.xlsx":  "S-v1",
		"templates/timing/Timing.xlsx":  "Timing-v1",
		"templates/weighin/W1.xlsx":     "W1-v1",
		"agegroups/IWF.xlsx":            "IWF-v1",
		"agegroups/USAW.xlsx":           "USAW-v1",
	})

	writeTestJar(t, filepath.Join(v2Dir, "owlcms.jar"), map[string]string{
		"templates/results/K1.xlsx":     "K1-v2",
		"templates/results/K2.xlsx":     "K2-custom", // integrated fix in v2
		"templates/results/X1.xlsx":     "X1-v2",
		"templates/results/Y1.xlsx":     "Y1-new-in-touched",
		"templates/results/sub/S.xlsx":  "S-v2",
		"templates/results/sub/S2.xlsx": "S2-new-in-untouched-sub",
		"templates/scripts/print.bat":   "print-v2",
		"templates/timing/Timing.xlsx":  "Timing-v2",
		"templates/timing/old/Old.xlsx": "Old-v2",
		"templates/weighin/W1.xlsx":     "W1-v2",
		"templates/weighin/W2.xlsx":     "W2-new-in-untouched",
		"templates/brandnew/BN.xlsx":    "BN-v2",
		"agegroups/IWF.xlsx":            "IWF-v2",
		"agegroups/USAW.xlsx":           "USAW-v2",
		"agegroups/NEW.xlsx":            "NEW-v2",
	})

	if err := restoreLocalFilesFromPreviousVersion(v2Dir, v1Dir); err != nil {
		t.Fatalf("v1->v2 restore failed: %v", err)
	}

	gotV2 := readTree(t, filepath.Join(v2Dir, "local"))
	wantV2 := map[string]string{
		"templates/results/N1.xlsx":     "N1",
		"templates/results/N2.xlsx":     "N2",
		"templates/results/K1.xlsx":     "K1-v2",
		"templates/results/K2.xlsx":     "K2-custom",
		"templates/results/Legacy.xlsx": "Legacy-v1", // missing in v2 jar -> copied from v1 as legacy
		"templates/results/sub/S.xlsx":  "S-v2",
		"templates/results/sub/S2.xlsx": "S2-new-in-untouched-sub",
		"templates/timing/Timing.xlsx":  "Timing-v2", // direct file untouched -> tracks v2 jar without reviving old/
		"templates/weighin/W1.xlsx":     "W1-v2",
		"templates/weighin/W2.xlsx":     "W2-new-in-untouched",
		"templates/brandnew/BN.xlsx":    "BN-v2",
		"agegroups/IWF.xlsx":            "IWF-v2",
		"agegroups/USAW.xlsx":           "USAW-v2",
		"agegroups/NEW.xlsx":            "NEW-v2",
	}
	if !reflect.DeepEqual(gotV2, wantV2) {
		t.Fatalf("v1->v2 kit tree mismatch:\ngot:  %#v\nwant: %#v", gotV2, wantV2)
	}

	// Second update v2 -> v3: untouched folders remain untouched and track v3;
	// K2 now matches v2 jar and updates to K2-v3; Legacy is now treated as an addition;
	// X1, Dropped, Y1, scripts/, and timing/old/ stay absent.
	writeTestJar(t, filepath.Join(v3Dir, "owlcms.jar"), map[string]string{
		"templates/results/K1.xlsx":     "K1-v3",
		"templates/results/K2.xlsx":     "K2-v3",
		"templates/results/X1.xlsx":     "X1-v3",
		"templates/results/Y1.xlsx":     "Y1-v3",
		"templates/results/sub/S.xlsx":  "S-v3",
		"templates/results/sub/S2.xlsx": "S2-v3",
		"templates/scripts/print.bat":   "print-v3",
		"templates/timing/Timing.xlsx":  "Timing-v3",
		"templates/timing/old/Old.xlsx": "Old-v3",
		"templates/weighin/W1.xlsx":     "W1-v3",
		"templates/weighin/W2.xlsx":     "W2-v3",
		"templates/weighin/W3.xlsx":     "W3-v3",
		"templates/brandnew/BN.xlsx":    "BN-v3",
		"agegroups/IWF.xlsx":            "IWF-v3",
		"agegroups/USAW.xlsx":           "USAW-v3",
		"agegroups/NEW.xlsx":            "NEW-v3",
	})

	if err := restoreLocalFilesFromPreviousVersion(v3Dir, v2Dir); err != nil {
		t.Fatalf("v2->v3 restore failed: %v", err)
	}

	gotV3 := readTree(t, filepath.Join(v3Dir, "local"))
	wantV3 := map[string]string{
		"templates/results/N1.xlsx":     "N1",
		"templates/results/N2.xlsx":     "N2",
		"templates/results/K1.xlsx":     "K1-v3",
		"templates/results/K2.xlsx":     "K2-v3",
		"templates/results/Legacy.xlsx": "Legacy-v1", // carried forward as an addition
		"templates/results/sub/S.xlsx":  "S-v3",
		"templates/results/sub/S2.xlsx": "S2-v3",
		"templates/timing/Timing.xlsx":  "Timing-v3",
		"templates/weighin/W1.xlsx":     "W1-v3",
		"templates/weighin/W2.xlsx":     "W2-v3",
		"templates/weighin/W3.xlsx":     "W3-v3",
		"templates/brandnew/BN.xlsx":    "BN-v3",
		"agegroups/IWF.xlsx":            "IWF-v3",
		"agegroups/USAW.xlsx":           "USAW-v3",
		"agegroups/NEW.xlsx":            "NEW-v3",
	}
	if !reflect.DeepEqual(gotV3, wantV3) {
		t.Fatalf("v2->v3 kit tree mismatch:\ngot:  %#v\nwant: %#v", gotV3, wantV3)
	}
}

func TestRestoreLocalSparseCustomTemplateStaysSparse(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")

	writeTestJar(t, filepath.Join(oldDir, "owlcms.jar"), map[string]string{
		"templates/results/A.xlsx": "A-v1",
		"templates/weighin/W.xlsx": "W-v1",
		"agegroups/IWF.xlsx":       "IWF-v1",
	})
	writeTestLocalFiles(t, filepath.Join(oldDir, "local"), map[string]string{
		"templates/results/Custom.xlsx": "custom",
	})
	writeTestJar(t, filepath.Join(newDir, "owlcms.jar"), map[string]string{
		"templates/results/A.xlsx":   "A-v2",
		"templates/weighin/W.xlsx":   "W-v2",
		"templates/brandnew/BN.xlsx": "BN-v2",
		"agegroups/IWF.xlsx":         "IWF-v2",
	})

	if err := restoreLocalFilesFromPreviousVersion(newDir, oldDir); err != nil {
		t.Fatalf("restoreLocalFilesFromPreviousVersion failed: %v", err)
	}

	got := readTree(t, filepath.Join(newDir, "local"))
	want := map[string]string{
		"templates/results/Custom.xlsx": "custom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sparse custom template tree mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestMigrateLocalFilesDryRunDoesNotModifyDestinationAndReportsPruned(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "68.0.8")
	newDir := filepath.Join(root, "69.0.0-rc10")

	writeTestJar(t, filepath.Join(oldDir, "owlcms.jar"), map[string]string{
		"css/results.css":           "buggy",
		"css/plates.css":            "old-plates",
		"templates/timing/T.xlsx":   "T-v1",
		"templates/results/A.xlsx":  "A-v1",
	})
	writeTestLocalFiles(t, filepath.Join(oldDir, "local"), map[string]string{
		"css/results.css":             "fixed",
		"css/plates.css":              "custom-plates",
		"templates/timing/T.xlsx":     "T-v1",
		"templates/results/USAW.xlsx": "usaw",
	})
	writeTestJar(t, filepath.Join(newDir, "owlcms.jar"), map[string]string{
		"css/results.css":          "fixed", // integrated fix -> would be pruned
		"css/plates.css":           "new-plates",
		"templates/timing/T.xlsx":  "T-v2",
		"templates/results/A.xlsx": "A-v2",
	})
	// Pre-existing sentinel file in newDir/local must remain untouched in dry-run mode.
	writeTestLocalFiles(t, filepath.Join(newDir, "local"), map[string]string{
		"sentinel.txt": "keep-me",
	})

	report, err := migrateLocalFiles(newDir, oldDir, true)
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	got := readTree(t, filepath.Join(newDir, "local"))
	want := map[string]string{"sentinel.txt": "keep-me"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dry-run modified destination local/:\ngot:  %#v\nwant: %#v", got, want)
	}

	for _, substr := range []string{
		"Starting Dry-Run Migration Report",
		"Modified (copied from 68.0.8): css/plates.css",
		"Modified (copied from 68.0.8): css/results.css",
		"Would prune (matches new jar): css/results.css",
		"= templates/timing/",
		"+ Copied from 68.0.8: USAW.xlsx",
	} {
		if !strings.Contains(report, substr) {
			t.Fatalf("expected dry-run report to contain %q, got:\n%s", substr, report)
		}
	}
	if strings.Contains(report, "absent from oldLocal") {
		t.Fatalf("expected dry-run report not to include absent lines, got:\n%s", report)
	}
}

func TestRestoreLocalWithReportWritesLogFile(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "68.0.8")
	newDir := filepath.Join(root, "69.0.0-rc10")

	writeTestJar(t, filepath.Join(oldDir, "owlcms.jar"), map[string]string{
		"css/plates.css": "old-plates",
	})
	writeTestLocalFiles(t, filepath.Join(oldDir, "local"), map[string]string{
		"css/plates.css": "custom-plates",
	})
	writeTestJar(t, filepath.Join(newDir, "owlcms.jar"), map[string]string{
		"css/plates.css": "new-plates",
	})

	reportPath, err := restoreLocalFilesFromPreviousVersionWithReport(newDir, oldDir)
	if err != nil {
		t.Fatalf("restoreLocalFilesFromPreviousVersionWithReport failed: %v", err)
	}
	if !strings.HasPrefix(reportPath, filepath.Join(newDir, "logs")) {
		t.Fatalf("expected report under %s/logs, got %s", newDir, reportPath)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report %s: %v", reportPath, err)
	}
	if !strings.Contains(string(data), "Modified (copied from 68.0.8): css/plates.css") {
		t.Fatalf("expected report content to mention css/plates.css, got:\n%s", string(data))
	}
}

func TestExtractDistributionLocalFilesSkipsExistingAndNonCustomizable(t *testing.T) {
	root := t.TempDir()
	versionDir := filepath.Join(root, "69.0.0-rc10")

	writeTestJar(t, filepath.Join(versionDir, "owlcms.jar"), map[string]string{
		"css/plates.css":               "default-plates",
		"css/results.css":              "default-results",
		"templates/timing/T.xlsx":      "timing-tpl",
		"agegroups/IWF.xlsx":           "iwf-ag",
		"flags/README.txt":             "flags-readme",
		"mappings/country-code-map.json": "{}",
		"ageFactors/ageFactors.xlsx":   "af",
		"logback.xml":                  "default-logback",
		"app/owlcms/Main.class":        "bytecode",
		"META-INF/MANIFEST.MF":         "manifest",
	})
	writeTestLocalFiles(t, filepath.Join(versionDir, "local"), map[string]string{
		"css/plates.css": "user-custom-plates",
	})

	// Dry-run must not modify local/
	dryReport, err := extractDistributionLocalFiles(versionDir, true)
	if err != nil {
		t.Fatalf("dry-run extract failed: %v", err)
	}
	gotDry := readTree(t, filepath.Join(versionDir, "local"))
	wantDry := map[string]string{"css/plates.css": "user-custom-plates"}
	if !reflect.DeepEqual(gotDry, wantDry) {
		t.Fatalf("dry-run modified local/:\ngot:  %#v\nwant: %#v", gotDry, wantDry)
	}
	if !strings.Contains(dryReport, "+ Would extract: css/results.css") ||
		!strings.Contains(dryReport, "= Kept existing: css/plates.css") {
		t.Fatalf("unexpected dry-run extract report:\n%s", dryReport)
	}

	// Real extract fills missing customizable files without overwriting css/plates.css
	if _, err := extractDistributionLocalFiles(versionDir, false); err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	got := readTree(t, filepath.Join(versionDir, "local"))
	want := map[string]string{
		"css/plates.css":                 "user-custom-plates", // preserved
		"css/results.css":                "default-results",
		"templates/timing/T.xlsx":        "timing-tpl",
		"agegroups/IWF.xlsx":             "iwf-ag",
		"flags/README.txt":               "flags-readme",
		"mappings/country-code-map.json": "{}",
		"ageFactors/ageFactors.xlsx":     "af",
		"logback.xml":                    "default-logback",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extracted local/ mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestExtractOfficialReleaseZipSkipsLocalWhenDisabled(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "owlcms_69.0.0.zip")
	destDir := filepath.Join(root, "69.0.0")

	writeTestJar(t, zipPath, map[string]string{
		"owlcms.jar":              "jar-bytes",
		"local/css/plates.css":    "default-plates",
		"local/templates/a/1.txt": "tpl",
		"local/logback.xml":       "logback",
	})

	if err := extractOfficialReleaseZip(zipPath, destDir); err != nil {
		t.Fatalf("extractOfficialReleaseZip failed: %v", err)
	}

	got := readTree(t, destDir)
	want := map[string]string{
		"owlcms.jar": "jar-bytes",
	}
	if extractLocalOnInitialInstall {
		want["local/css/plates.css"] = "default-plates"
		want["local/templates/a/1.txt"] = "tpl"
		want["local/logback.xml"] = "logback"
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official release extraction mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

