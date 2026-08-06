# Control Panel REPL Commands

This is the proposed interactive command interface for the OWLCMS Control Panel. It replaces long option combinations with a stateful read-eval loop.

```text
controlpanel repl
controlpanel --batch competition.commands
controlpanel> owlcms versions
controlpanel> owlcms start 8080
```

`owlcms start A 8080` starts the version labelled `A` on port `8080` in detached mode. Because `A` is always the most recent installed version, `owlcms start 8080` has the same result.

## Batch Mode

`--batch <file>` runs the same REPL commands from a file without creating a Fyne application or displaying a prompt, following the familiar `sftp -b <file>` model.

```text
controlpanel --batch competition.txt
controlpanel --batch -
```

- Use `-` as the batch file to read commands from standard input.
- Blank lines and lines beginning with `#` are ignored.
- Commands run in order and retain their session context, including the selected instance. Version selectors are resolved from the current canonical version order and do not require `versions` first.
- The batch run stops at the first command error and returns a non-zero exit status.
- Batch mode never opens a graphical window or prompts for input. Commands, including `remove`, execute immediately.

Example `competition.txt`:

```text
# Stop the current OWLCMS module in the current instance, update the most
# recent installed version to the latest available release, then start it.
owlcms stop
owlcms update latest
owlcms start 8080
```

## Session Context

The standard session uses the normal OWLCMS instance automatically. Module commands always begin with `owlcms` or `tracker`, so the session does not keep a selected module.

| Command | Description |
| --- | --- |
| `instance <name>` | Advanced: select a named sibling instance, for example `instance records`. |
| `instance-dir <path>` | Advanced: select an explicit control panel directory instead of the normal instance. |
| `runtime-dir <path>` | Advanced: select the shared Java, Node.js, and FFmpeg runtime directory. |
| `context` | Show the selected instance and resolved directories. |
| `init` | Create and display the directories for the selected instance. |
| `help [command]` | Show all commands or detailed syntax for one command. |
| `exit` or `quit` | Leave the read-eval loop. |

## Version Selectors

`owlcms versions` and `tracker versions` list installed versions and show each letter selector. They are for human inspection only: commands resolve selectors from the current canonical version order without requiring `versions` first.

```text
controlpanel[records]> owlcms versions

  A   66.0.0        latest
  B   65.1.0
  C   65.0.0
  D   64.0.0
```

Selectors are generated in spreadsheet order: `A` through `Z`, then `AA` through `ZZ`, followed by `AAA` and onward. Each command resolves them from the installed directories sorted in canonical semantic-version order. Consequently, an installation, update, or removal may change the selector that represents a version for the next command.

Installed versions have a semantic base version and may include a `+metadata` suffix. A `selector` is either a letter label such as `A` or `AA`, or the full installed semantic version value. For commands with one version target, omitting the selector means `A`, the most recent installed version. Commands with separate source and destination versions, such as `import`, require both selectors.

The `<name>` arguments of `rename` and `duplicate` are metadata labels, not complete versions. They preserve the selected version's semantic base and set its `+metadata` suffix; for example, renaming or duplicating `66.0.0` with `practice` produces `66.0.0+practice`. A collision receives an automatically generated metadata suffix.

## OWLCMS Commands

| Command | Description |
| --- | --- |
| `owlcms versions` | List installed OWLCMS versions and show their selectors. |
| `owlcms start [selector] [port]` | Start detached. Without a selector, starts `A`; a numeric first argument is the port. The optional port is stored for that version. |
| `owlcms run [selector] [port]` | Start in the foreground and return to the REPL after the process exits. Without a selector, runs `A`; a numeric first argument is the port. |
| `owlcms stop` | Stop the running OWLCMS process in the current instance; no installed version is selected. |
| `owlcms status` | Show the OWLCMS process currently running in this instance. |
| `owlcms tracker [selector] [port\|url]` | Configure a Tracker endpoint. Without a selector, targets `A`. A port uses `ws://localhost:<port>/ws`; a URL must be a `ws://` or `wss://` endpoint ending in `/ws`. With no endpoint, the local default is port `8096`. |
| `owlcms mqtt <on\|off>` | Enable or disable MQTT for the next OWLCMS `start` or `run`. |
| `owlcms install [release]` | Download and cleanly install a release. Defaults to the latest available release. |
| `owlcms install-zip <zip-path> [installed-version]` | Install a local ZIP. The semantic version is inferred from the file name when possible; an explicit installed version must be semantic. |
| `owlcms export [selector] <zip-path\|directory>` | Create a ZIP archive from an installed version. Without a selector, exports `A`. A directory receives a timestamped archive. |
| `owlcms update [selector] <release>` | Download a release and migrate data and configuration from a local source version. Without a selector, uses `A`. |
| `owlcms rename [selector] <name>` | Rename an installed version by replacing its metadata with `<name>`. Without a selector, renames `A`. |
| `owlcms duplicate [selector] <name>` | Copy an installed version using `<name>` as its new metadata. Without a selector, duplicates `A`. |
| `owlcms import <source-selector> <target-selector>` | Copy data and configuration between two installed versions. |
| `owlcms remove [selector]` | Permanently remove an installed version. Without a selector, removes `A`; batch mode executes immediately. |

Examples:

```text
owlcms tracker 8096
owlcms start 8080
owlcms update B latest
owlcms rename B practice
owlcms duplicate B backup

# Configure a remote Tracker endpoint for the most recent OWLCMS version.
owlcms tracker wss://tracker.example.org:443/ws
```

## Tracker Commands

| Command | Description |
| --- | --- |
| `tracker versions` | List installed Tracker versions and show their selectors. |
| `tracker start [selector] [port]` | Start detached. Without a selector, starts `A`; a numeric first argument is the port. The optional port is stored for that version. |
| `tracker run [selector] [port]` | Start in the foreground and return to the REPL after the process exits. Without a selector, runs `A`; a numeric first argument is the port. |
| `tracker stop` | Stop the running Tracker process in the current instance; no installed version is selected. |
| `tracker status` | Show the Tracker process currently running in this instance. |
| `tracker install [release]` | Download and cleanly install a release. Defaults to the latest available release. |
| `tracker install-zip <zip-path> [installed-version]` | Install a local ZIP. The semantic version is inferred from the file name when possible; an explicit installed version must be semantic. |
| `tracker export [selector] <zip-path\|directory>` | Create a ZIP archive from an installed version. Without a selector, exports `A`. A directory receives a timestamped archive. |
| `tracker update [selector] <release>` | Download a release and migrate data and configuration from a local source version. Without a selector, uses `A`. |
| `tracker rename [selector] <name>` | Rename an installed version by replacing its metadata with `<name>`. Without a selector, renames `A`. |
| `tracker duplicate [selector] <name>` | Copy an installed version using `<name>` as its new metadata. Without a selector, duplicates `A`. |
| `tracker import <source-selector> <target-selector>` | Copy data and configuration between two installed versions. |
| `tracker remove [selector]` | Permanently remove an installed version. Without a selector, removes `A`; batch mode executes immediately. |

Examples:

```text
tracker start B 8097
tracker install-zip ~/Downloads/owlcms-tracker_3.4.0.zip
tracker import B A
```

## Operational Rules

- A plain `controlpanel repl` session operates on the normal OWLCMS instance. Use `instance <name>` only when managing a separate sibling instance.
- `start` is the normal REPL operation and runs detached so subsequent commands remain available.
- `run` preserves the existing foreground behavior and occupies the REPL until the launched module exits.
- `owlcms versions`, `owlcms start`, `owlcms run`, `owlcms stop`, and their Tracker counterparts remain available while the graphical Control Panel is running when they are safe under the existing runtime rules.
- Installing, updating, importing, duplicating, exporting, and removing versions require exclusive Control Panel ownership.
- In batch mode, `remove` executes immediately. The interactive confirmation behavior is not yet decided.
