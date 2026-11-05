# Port probe

Standalone, unprivileged diagnostic CLI for inspecting a TCP port and testing
process termination. It does not modify the Control Panel's running-process logic.

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
(Control Panel daemon launches), the `controlpanel` executable, and tracker
node processes. It also issues a non-destructive `GET /controlpanel/stop`:
OWLCMS 66.2.2+ answers 405 without stopping anything. Both are hints only:
a 405 can come from another server, and an unavailable field (Windows hides
the command line and working directory of elevated processes) does not prove
anything.

Command lines are displayed as-is and may contain arguments.

For each distinct PID, it asks separately before stopping the
listener and its eligible parent. If both are confirmed, it stops the parent
first, then rechecks the listener. It sends a soft termination followed by a
forced termination if needed. It signals only the confirmed PID, not its process
tree, does not call OWLCMS's HTTP stop endpoint, and never requests elevation.
Stopping a parent can nevertheless affect processes it supervises.

Process or port ownership can change between checks; permissions and OS
enumeration can hide listeners. The program refuses to act on an unverified PID
and does not claim that an empty listener list proves the port is free. It
protects its own ancestors and common shells and service managers from being
stopped. Run it only on a test system or after reviewing the process details.
