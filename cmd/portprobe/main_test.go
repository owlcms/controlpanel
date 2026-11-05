package main

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
		error bool
	}{
		{"y\n", true, false},
		{"NO\n", false, false},
		{"maybe\ny\n", true, false},
		{"", false, true},
	} {
		got, err := confirm(bufio.NewReader(strings.NewReader(tc.input)), &bytes.Buffer{}, "Stop?")
		if got != tc.want || (err != nil) != tc.error {
			t.Errorf("confirm(%q) = %v, %v", tc.input, got, err)
		}
	}
}

func TestInspectCurrentProcess(t *testing.T) {
	id, err := inspect(int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if id.created <= 0 || id.parent <= 0 {
		t.Fatalf("invalid identity: %+v", id)
	}
	alive, err := sameProcess(id)
	if err != nil || !alive {
		t.Fatalf("current process: alive=%v error=%v", alive, err)
	}
	id.created++
	if _, err := sameProcess(id); err == nil {
		t.Fatal("expected changed creation time to be rejected")
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		id   identity
		want string
	}{
		{identity{executable: "/usr/bin/java", cmdline: "java -jar owlcms.jar"}, "OWLCMS (java -jar owlcms.jar)"},
		{identity{executable: `C:\java\bin\javaw.exe`, cmdline: "javaw -jar owlcms.jar"}, "OWLCMS (java -jar owlcms.jar)"},
		{identity{executable: "/usr/bin/java", cmdline: "java -cp owlcms.jar app.owlcms.MainWrapper"}, "OWLCMS (MainWrapper supervisor)"},
		{identity{executable: "/usr/bin/java", cmdline: "java -ea -XX:HotswapAgent=fatjar -cp /long/dev/classpath app.owlcms.Main"}, "OWLCMS (app.owlcms.Main)"},
		{identity{executable: "/usr/bin/java", cwd: "/opt/owlcms/68.0.5"}, "probably OWLCMS (Java in an owlcms directory)"},
		{identity{executable: "/usr/local/bin/controlpanel"}, "Control Panel"},
		{identity{executable: "/usr/bin/node", cmdline: "node tracker/dist/index.js"}, "probably Tracker (node)"},
		{identity{executable: "/usr/bin/python3", cmdline: "python3 -m http.server"}, "unrecognized"},
	} {
		if got := classify(tc.id); got != tc.want {
			t.Errorf("classify(%+v) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestTrimCommand(t *testing.T) {
	dev := "java -ea -cp /a/vaadin.jar:/b/reactor.jar:/c/more.jar app.owlcms.Main"
	want := "java -ea -cp (elided) app.owlcms.Main"
	if got := trimCommand(dev); got != want {
		t.Errorf("trimCommand = %q, want %q", got, want)
	}
	long := "java " + strings.Repeat("-Dx ", 400)
	if got := trimCommand(long); len(got) > 601 || !strings.HasSuffix(got, "…") {
		t.Errorf("trimCommand should cap length and mark truncation, got %d chars", len(got))
	}
}
