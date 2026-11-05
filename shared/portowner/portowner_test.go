package portowner

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInspectCurrentProcess(t *testing.T) {
	id, err := Inspect(int32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if id.Created <= 0 || id.Parent <= 0 {
		t.Fatalf("invalid identity: %+v", id)
	}
	alive, err := SameProcess(id)
	if err != nil || !alive {
		t.Fatalf("current process: alive=%v error=%v", alive, err)
	}
	id.Created++
	if _, err := SameProcess(id); err == nil {
		t.Fatal("expected changed creation time to be rejected")
	}
}

func TestOwnAncestorsIncludesSelf(t *testing.T) {
	ancestors, err := OwnAncestors()
	if err != nil {
		t.Fatal(err)
	}
	if !ancestors[int32(os.Getpid())] || !ancestors[int32(os.Getppid())] {
		t.Fatalf("ancestors %v should include PID %d and parent %d", ancestors, os.Getpid(), os.Getppid())
	}
}

func TestAssessRefusesSelf(t *testing.T) {
	if _, err := Assess(int32(os.Getpid())); err == nil {
		t.Fatal("expected Assess to refuse this process")
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		id   Identity
		want string
	}{
		{Identity{Executable: "/usr/bin/java", Cmdline: "java -jar owlcms.jar"}, KindOwlcmsJar},
		{Identity{Executable: `C:\java\bin\javaw.exe`, Cmdline: "javaw -jar owlcms.jar"}, KindOwlcmsJar},
		{Identity{Executable: "/usr/bin/java", Cmdline: "java -cp owlcms.jar app.owlcms.MainWrapper"}, KindMainWrapper},
		{Identity{Executable: "/usr/bin/java", Cmdline: "java -ea -XX:HotswapAgent=fatjar -cp /long/dev/classpath app.owlcms.Main"}, KindOwlcmsMain},
		{Identity{Executable: "/usr/bin/java", Cwd: "/opt/owlcms/68.0.5"}, KindOwlcmsProbably},
		{Identity{Executable: "/usr/local/bin/controlpanel"}, KindControlPanel},
		{Identity{Executable: `C:\Program Files\owlcms\owlcms.exe`}, KindControlPanel},
		{Identity{Executable: "/usr/bin/node", Cmdline: "node tracker/dist/index.js"}, KindTracker},
		{Identity{Executable: "/usr/bin/python3", Cmdline: "python3 -m http.server"}, KindUnrecognized},
	} {
		if got := Classify(tc.id); got != tc.want {
			t.Errorf("Classify(%+v) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestSupervisorAndOwlcms(t *testing.T) {
	cp := Identity{Executable: `C:\x\owlcms.exe`}
	wrapper := Identity{Executable: "java", Cmdline: "java -cp x app.owlcms.MainWrapper"}
	server := Identity{Executable: "javaw.exe", Cmdline: "javaw -jar owlcms.jar"}
	if !IsSupervisor(cp) || !IsSupervisor(wrapper) || IsSupervisor(server) {
		t.Error("supervisor classification is wrong")
	}
	if !IsOwlcms(server) || IsOwlcms(wrapper) || IsOwlcms(cp) {
		t.Error("OWLCMS server classification is wrong")
	}
}

func TestTrimCommand(t *testing.T) {
	dev := "java -ea -cp /a/vaadin.jar:/b/reactor.jar:/c/more.jar app.owlcms.Main"
	want := "java -ea -cp (elided) app.owlcms.Main"
	if got := TrimCommand(dev); got != want {
		t.Errorf("TrimCommand = %q, want %q", got, want)
	}
	long := "java " + strings.Repeat("-Dx ", 400)
	if got := TrimCommand(long); len(got) > 601 || !strings.HasSuffix(got, "…") {
		t.Errorf("TrimCommand should cap length and mark truncation, got %d chars", len(got))
	}
}

// TestStopListenerChild starts a child listener, finds it by port, and stops it.
func TestStopListenerChild(t *testing.T) {
	if os.Getenv("PORTOWNER_HELPER_LISTEN") != "" {
		return
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperListen")
	cmd.Env = append(os.Environ(), "PORTOWNER_HELPER_LISTEN="+strconv.Itoa(port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() {
		cmd.Process.Kill()
		<-done
	}()

	var pid int32
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && pid == 0; time.Sleep(100 * time.Millisecond) {
		listeners, err := FindListeners(port)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range listeners {
			if l.PID == int32(cmd.Process.Pid) {
				pid = l.PID
			}
		}
	}
	if pid == 0 {
		t.Fatalf("helper PID %d never listened on port %d", cmd.Process.Pid, port)
	}

	a, err := Assess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if a.ParentKind != ParentProtected || a.Parent == nil || a.Parent.PID != int32(os.Getpid()) {
		t.Fatalf("the test process is the helper's parent and must be protected: %+v", a)
	}
	if err := Stop(a.Listener, func() error { return ValidateListener(a.Listener, port) }, func(string) {}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit")
	}
}

// TestEnvValueFromChild starts a child with a variable in its environment and
// reads it back through the OS, as done for the OWLCMS launch token.
func TestEnvValueFromChild(t *testing.T) {
	if os.Getenv("PORTOWNER_HELPER_LISTEN") != "" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperSleep")
	cmd.Env = append(os.Environ(), "PORTOWNER_HELPER_SLEEP=1", "PORTOWNER_TEST_TOKEN=Ab3-xY9_kLmN")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	got := EnvValue(int32(cmd.Process.Pid), "PORTOWNER_TEST_TOKEN")
	if got != "Ab3-xY9_kLmN" {
		t.Fatalf("EnvValue = %q, want the token given to the child", got)
	}
	if EnvValue(int32(cmd.Process.Pid), "PORTOWNER_ABSENT") != "" {
		t.Fatal("absent variable must read as empty")
	}
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("PORTOWNER_HELPER_SLEEP") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func TestHelperListen(t *testing.T) {
	port := os.Getenv("PORTOWNER_HELPER_LISTEN")
	if port == "" {
		t.Skip("helper process only")
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(2)
	}
	defer l.Close()
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
