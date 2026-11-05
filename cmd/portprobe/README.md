# Port probe

Standalone, unprivileged diagnostic CLI for inspecting a TCP port and testing
process termination. Its process inspection and termination logic lives in
`shared/portowner`, which the Control Panel also uses to free the OWLCMS port.

From the Control Panel directory:

```bash
go build -o portprobe ./cmd/portprobe
./portprobe -port 8080
```

On Windows, use `go build -o portprobe.exe ./cmd/portprobe` and run
`./portprobe.exe -port 8080` in a terminal.

The CLI lists discovered IPv4 and IPv6 listeners. For each process (and its
parent) it shows the name, executable, command line, working directory, and a
classification hint based on those OS-reported facts. The displayed command
line elides the classpath so the java invocation and main class stay visible;
classification looks for `-jar owlcms.jar`, the main classes
`app.owlcms.Main` (development launches) and `app.owlcms.MainWrapper`
(Control Panel daemon launches), the Control Panel executable (`owlcms.exe` /
`owlcms` for releases, `controlpanel` for development builds), and tracker
node processes. It also issues a non-destructive `GET /controlpanel/stop`:
OWLCMS 66.2.2+ answers 405 without stopping anything. Both are hints only:
a 405 can come from another server, and an unavailable field (Windows hides
the command line and working directory of elevated processes) does not prove
anything.

Command lines are displayed as-is and may contain arguments.

When the HTTP check answers 405, the CLI offers `POST /controlpanel/stop`. It
presents the token from `OWLCMS_CONTROLPANELTOKEN`, or a dummy token when that
variable is unset. OWLCMS 66-68 ignore the token; OWLCMS 69+ accepts the stop
only with the token it was launched with. OWLCMS then exits 0, which neither
the Control Panel nor `MainWrapper` restarts.

For each distinct PID, it asks separately before stopping the
listener and its eligible parent. A listener is reported as an orphan when its
parent has exited (Windows keeps the dead parent's PID, which may also have been
reused by a newer process) or is the Unix init process; nothing will restart it.
On Windows, when the parent is a known
supervisor (Control Panel or `MainWrapper`), it offers to stop the supervisor
before asking about the listener. Otherwise it asks about the listener first;
if both are confirmed, it stops the parent first, then rechecks the listener. On Unix it sends SIGTERM, then SIGKILL if
needed. On Windows a non-forced `taskkill` cannot reach a windowless Java
process, so it goes straight to `taskkill /F`, which exits with code 1; the
Control Panel and `MainWrapper` both restart on that code, so stop the
supervisor first. It signals only the confirmed PID, not its process
tree, and never requests elevation.
Stopping a parent can nevertheless affect processes it supervises.

Process or port ownership can change between checks; permissions and OS
enumeration can hide listeners. The program refuses to act on an unverified PID
and does not claim that an empty listener list proves the port is free. It
protects its own ancestors and common shells and service managers from being
stopped. Run it only on a test system or after reviewing the process details.
