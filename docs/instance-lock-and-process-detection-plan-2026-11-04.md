# Control Panel Port Recovery And Process Detection Plan

Date: 2026-11-04; revised 2026-11-05

## Goal

Reliably free the configured port when a leftover process prevents OWLCMS from starting, and accurately recover the state of a running OWLCMS after restarting the Control Panel. Distinct OWLCMS instances can coexist on distinct ports.

On Windows, a stop attempt displayed repeated console windows and eventually reported that a process was still running after `taskkill` sent it a termination signal. Task Manager did not show that PID; rebooting resolved the situation. The evidence does not establish whether the process remained alive or detection was incorrect.

There is **no new per-instance Control Panel lock** in this plan. Keep existing duplicate-Control-Panel behavior unchanged. In particular, do not assume there is only one supervisor or that all supervisors cooperate with this Control Panel.

The Control Panel runs **unprivileged** on Windows, macOS and Linux. Discovery and termination must use its existing user permissions: do not invoke UAC, `sudo`, `pkexec`, or another elevation mechanism. A process owned by another user or running elevated may not be inspectable or stoppable; report the limitation and leave the port conflict unresolved.

## Current behavior

- Runtime metadata already records the module PID, version and port. Linux also records process start ticks; Windows and macOS currently do not have a persisted OS process identity.
- On Windows, `IsProcessRunning` runs `tasklist` and checks whether its output contains the PID digits. Repeated polling can flash console windows; substring matching can also misidentify output.
- Startup recovery relies on the saved PID without verifying the saved port's owner.
- An owned OWLCMS process is killed directly on Windows. A recovered process is asked to stop through HTTP, followed by soft termination, but not a forced fallback.
- Before launching, the GUI already asks for confirmation to stop the process occupying the configured port.

## Design

### Process inspection

- Use the existing `gopsutil/process` dependency for native process checks and creation time. Return or log inspection errors explicitly; an access-denied or failed query is **unknown**, not proof that the process is gone.
- Persist an optional OS creation time in module runtime metadata at launch. Compare it with the current PID's creation time to detect PID reuse; retain existing Linux start-ticks compatibility.
- Find the configured port's listeners using `gopsutil/net`; its platform implementations may enumerate processes or sockets. Inspect and act only on relevant PIDs: a listener, a recorded process, and, where useful, its parent. Do not kill arbitrary processes.
- Log the configured port, PID, available executable and creation time, source of the PID, inspection errors, and each stop stage. Do not log tokens or command lines that may contain secrets.
- Hide Windows consoles for `taskkill` invocations. Process existence polling must not spawn `tasklist` consoles.

For each listener PID returned by `gopsutil/net`, use `gopsutil/process.NewProcess(pid)`, then `Ppid()`, `CreateTime()` and `Exe()` as available. Inspect the parent as a separate process and record its creation time before offering to stop it. Before acting, make fresh observations of the child, parent and port owner: `gopsutil` caches some process properties, so do not reuse cached parent or creation-time values as a fresh check. A returned parent PID can be stale or reused, and parentage alone does not establish supervision.

### Platform support

| Platform | Listener discovery | Process identity and liveness | Stop |
| --- | --- | --- | --- |
| Windows | `gopsutil/net` uses the Windows TCP owner-PID tables | `gopsutil/process` uses Windows process APIs; verify creation time and handle access-denied as an uncertain state | Known OWLCMS: HTTP first. Then non-forced `taskkill`, then `/F` if needed; hide command windows and do not request UAC |
| macOS | This version of `gopsutil/net` invokes `lsof`; handle a missing command, permission restrictions and incomplete results | `gopsutil/process` obtains process details and uses OS process checks | Known OWLCMS: HTTP first. Then SIGTERM, then SIGKILL if needed, without elevation |
| Linux | `gopsutil/net` maps `/proc` socket inodes to process file descriptors; restricted `/proc` access can leave a listener without a PID | `gopsutil/process` uses `/proc` and OS process checks | Known OWLCMS: HTTP first. Then SIGTERM, then SIGKILL if needed, without elevation |

Inspect IPv4 and IPv6 listeners and bind addresses relevant to the configured server. Multiple processes can listen on the same numeric port on different addresses; do not assume there is just one. Report each conflicting listener, obtain separate confirmation for each process, and recheck ownership before acting. Verify that OWLCMS can bind the addresses it needs rather than relying solely on a successful or failed connection to `localhost`.

The existing `FindPIDByPort` returns the first matching listener and can return PID 0. Replace or extend it so an inaccessible owner, incomplete enumeration, or ambiguous/multiple listeners is not mistaken for a free port. In particular, the current Linux `gopsutil/net` implementation skips inaccessible process file descriptors, potentially without an enumeration error. Independently check whether the intended addresses can be bound; an occupied port with no discoverable owner is an unresolved conflict, not a free port. If discovery is incomplete or a process cannot be inspected or stopped due to permissions, report that explicitly; do not guess a PID or silently elevate privileges.

Run platform-specific integration tests as an unprivileged user on Windows, macOS and Linux, including same-user listeners and, where a suitably restricted fixture is available, permission-restricted listeners; cover IPv4/IPv6, process identity, parent lookup, and termination. Verify that denied access is reported and never triggers elevation. A cross-compile or a test on one OS does not establish behavior on the other two.

### Startup recovery

Use the saved port and PID, the recorded creation time if available, and the OS listener information. An HTTP readiness probe supplies additional information but a timeout does not mean the process is gone.

| Observation | Result |
| --- | --- |
| Saved PID and creation time match, and it owns the saved port | Recover the process; check readiness |
| Saved PID is alive but does not yet listen | Show starting or unresponsive, according to age; allow Stop |
| Port is occupied by a different PID | Report the port conflict; do not silently adopt it as the saved process |
| Saved process is gone and port is free | Clear stale metadata and show stopped |
| No usable metadata, port occupied | Report the actual listener and offer the confirmed port-freeing flow below |
| Inspection is inconclusive | Show unknown/conflict and preserve metadata; do not claim the port or process is free |

For legacy metadata lacking creation time, take over without an extra confirmation when the recorded PID is also the listener; log that historical identity cannot be verified, and record its current creation time. Revalidate immediately before writing or acting. If the PID and listener disagree, treat it as a conflict, not a match. Do not claim that adding creation time retroactively proves identity.

OWLCMS may take about 30 seconds to reach database readiness. A process less than 60 seconds old can be shown as starting and monitored until the 60-second readiness limit. After that, a process that still does not answer is shown as unresponsive with Stop available; there is no separate wait option. If creation time cannot be obtained, report the uncertainty rather than inventing an age. HTTP probes and waiting must not block the UI thread.

### Freeing an occupied port

The goal is to free the port, not to prove the listener is OWLCMS:

1. Find the actual listener PID and collect its available executable and process identity. Report the port and process details.
2. Ask the user to confirm stopping that process, including when it is not OWLCMS. If identity or ownership cannot be inspected reliably, report the uncertainty and do not claim success or automatically kill a guessed PID.
3. If its parent is a different live process, identify it and separately ask whether to stop it as well. Show its PID and executable and warn that it may manage other processes. If it is this Control Panel, suppress our own restart instead of asking to stop ourselves. See Supervisors and restarts for safe parent handling.
4. If the parent is confirmed and can safely be stopped, stop it first; then re-evaluate the listener, because stopping the parent may also stop its child.
5. Immediately before signaling the listener and before force escalation, verify that the same process identity still owns the target port. If the process or port owner changes, stop and re-evaluate; do not transfer the confirmation to a different process.
6. For a known OWLCMS process, try the polite HTTP stop first. Do not send a stop POST to an unidentified service merely because the port is occupied.
7. If still running, request soft termination. On Windows use `taskkill /T /PID`; on Unix use SIGTERM. Wait with native process checks.
8. If the same process still owns the port, escalate to forced termination: `taskkill /F /T /PID` on Windows, SIGKILL on Unix. Clearly surface failures and permission problems.
9. Verify that the port is free. If another listener has appeared, report it and obtain new confirmation rather than killing it under the earlier confirmation.

If termination or verification fails, the final result is **could not kill / free the port**, not success. Give the user the port, PID, available executable and parent details, failed stage, and the OS error or access limitation so they can investigate; distinguish a surviving process from a port that is still occupied or cannot be inspected.

This flow must work even when an older installation wrote no usable metadata. The listener PID, not a stale PID file, is the target for port freeing. If an owned process is known but never bound the port, the Stop action can target that process separately, after verifying its identity.

### Windows command output encoding

The current `taskkill` path converts captured bytes directly with `string(out)` and inserts them into the dialog. Windows command output can use a legacy code page rather than UTF-8, producing replacement diamonds for French accented characters.

Do not display or log raw `taskkill` output: its language and encoding vary between Windows installations. Instead, present an application-generated error containing the process, port, failed stage, command exit status and any OS error established independently. Do not infer the cause by matching localized text; if it is unknown, say so. This avoids assumptions about French, Russian, Armenian, Japanese or other installations.

### OWLCMS HTTP stop and legacy versions

For both owned and recovered OWLCMS processes, attempt `POST /controlpanel/stop` with the saved token where available, then use the OS termination sequence if the polite request cannot stop it. A known OWLCMS is established from reliable process/runtime evidence; do not treat a generic HTTP status as conclusive identity.

The endpoint exists in the OWLCMS 66 and 67 worktrees and was introduced in 66.2.2-rc01. `GET /controlpanel/stop` is non-destructive and returns 405 in the inspected implementation, but a different service can also return 405. A 404, timeout or unavailable token likewise does not prove a process is not OWLCMS. Early response before database readiness remains to be verified; do not depend on it for legacy compatibility.

If polite stop fails because an older version lacks the endpoint, or the token is unavailable or refused, the confirmed OS stop sequence remains available. Do not log the token.

### Supervisors and restarts

A port owner can be restarted by another Control Panel, systemd, or another supervisor. An HTTP clean stop is preferable for known OWLCMS, but it does not guarantee every external supervisor will leave it stopped.

Known restart rules:

- **OWLCMS MainWrapper** (Java): restarts only when the child exits 1-127. Exit 0 or ≥128 (all Unix signal deaths, including SIGTERM/SIGINT/SIGKILL) are terminal. Stopping the wrapper itself sets its shutdown flag, and its shutdown hook destroys the child. So on Unix, SIGTERM to either the wrapper or the child never restarts.
- **Control Panel supervision** (Go): exit 0, 130, 137, 143 are deliberate stops; anything else restarts (limited retries).
- **Windows risk:** `taskkill /F` typically produces exit 1, which both supervisors treat as restartable. Force-kill must therefore be preceded by stopping or suppressing the supervisor, or followed by re-detection of a replacement listener. The exit codes produced by gentle vs. forced `taskkill` against a Java process must be verified empirically — the original incident shows this behavior is not yet understood.

When this Control Panel owns the child, suppress its own restart before the stop and coordinate with the monitoring goroutine. A parent can itself be an OWLCMS Java wrapper such as `MainWrapper`, or an older Control Panel. For a recovered or foreign process, inspect the parent and available supervisor information; absence of such evidence does not prove there is no supervisor, and a parent PID alone does not establish that it supervises the child.

The listener and an external parent require **two separate confirmations**. Describe the parent and its possible effects, and revalidate its identity and relationship before acting. A parent may manage other modules; confirmation to stop the parent does not authorize killing its whole process tree. If the user declines to stop it, explain that the listener may restart; still follow the separate listener confirmation and report any replacement. Do not kill an unidentified system process, shell, launchd, or systemd itself; for a service manager, stopping the appropriate service is a separate authorized operation. If safe parent handling is unavailable, report the limitation rather than guessing.

After each stop, verify the port. If it becomes occupied again, report the new PID and possible supervision. Do not automatically chase or repeatedly kill replacement processes; any further stop requires renewed confirmation.

## Implementation phases

1. **Native checks and diagnostics:** replace Windows `tasklist` polling, preserve unknown/error states, hide stop-command consoles, record optional process creation time, and add port/PID diagnostics. Ensure the affected stop paths never invoke the existing elevated `taskkill` retry.
2. **Recovery:** reconcile saved metadata against process identity and the actual listener; support old metadata and no metadata; keep the UI responsive during readiness checks.
3. **Stop and port freeing:** unify owned and recovered OWLCMS polite stop, then soft and forced fallback; revalidate identity and ownership at each stage; preserve the explicit confirmation for a port occupant.
4. **Supervision:** coordinate intentional stops with the local monitor; offer separate, informed confirmation for an external parent; detect a replacement listener without assuming all supervisors can be identified.
5. **Tests and release notes:** cover stale and missing metadata, PID reuse, unknown process/port inspection, an unrelated listener, both confirmations and cancellation, parent changes and unavailable parent information, ownership changes, unavailable HTTP stop, restart by a supervisor, readiness timing, soft-to-forced escalation, failed termination and its diagnostic message, non-UTF-8 command output that must not appear in messages or logs, port verification, and independent ports. Add a Control Panel release note.

## Scope and compatibility

- Preserve multiple separately configured instances on different ports; no per-instance lock is added.
- Keep the existing duplicate-Control-Panel check and its \"Stop Existing and Continue\" behavior unchanged.
- Apply shared process-check changes carefully to Tracker and other callers; preserve their existing lifecycle semantics.
- No OWLCMS changes are required.
- Older versions need not have written new metadata or an HTTP endpoint. Never assume a PID file is current.
