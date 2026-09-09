# Control Panel REPL Commands

This is the proposed interactive command interface for the OWLCMS Control Panel. It replaces long option combinations with a stateful read-eval loop.

```text
controlpanel --repl
controlpanel --batch competition.commands
controlpanel --instance records --repl
controlpanel> owlcms versions
controlpanel> owlcms A port 8080
controlpanel> owlcms start A
```

`owlcms A port 8080` persistently configures the version labelled `A` to use port `8080`, matching the graphical version options. `owlcms start A` then starts that version in detached mode using its saved configuration.

## Batch Mode

`--batch <file>` runs the same REPL commands from a file without creating a Fyne application or displaying a prompt, following the familiar `sftp -b <file>` model.

```text
controlpanel --batch competition.txt
controlpanel --batch -
```

- Use `-` as the batch file to read commands from standard input.
- Blank lines and lines beginning with `#` are ignored.
- Commands run in order for the single instance selected by the startup options. Version selectors are resolved from the current canonical version order and do not require `versions` first.
- The batch run stops at the first command error and returns a non-zero exit status.
- Batch mode never opens a graphical window or prompts for input. Commands, including `remove`, execute immediately.
- Successful install, update, rename, duplicate, and remove commands automatically print the refreshed version list and selectors.

Example `competition.txt`:

```text
# Stop the current OWLCMS module in the current instance, update the most
# recent installed version to the latest available release, then start it.
owlcms stop
owlcms update latest
owlcms A port 8080
owlcms start A
```

## Instance Context

Each REPL process manages exactly one instance, matching the graphical Control Panel. The instance is selected before the REPL starts and cannot be changed from inside the loop. Module commands always begin with `owlcms` or `tracker`, so the REPL does not keep a selected module either.

| Startup command | Managed instance |
| --- | --- |
| `controlpanel --repl` | The normal OWLCMS instance. |
| `controlpanel --instance records --repl` | The named `records` instance. |
| `controlpanel --instance-dir <path> --repl` | The instance associated with the explicit control panel directory. |

Use `--runtime-dir <path>` with either startup form when the instance needs a non-default shared Java, Node.js, and FFmpeg runtime directory. Batch mode uses the same startup options, for example `controlpanel --instance records --batch competition.txt`.

| Command | Description |
| --- | --- |
| `context` | Show the startup-selected instance and resolved directories. |
| `help` | Show general commands and point to the module help commands. |
| `owlcms help` | Show OWLCMS commands and persistent OWLCMS version settings. |
| `tracker help` | Show Tracker commands. |
| `exit` or `quit` | Leave the read-eval loop. |

Create a new `records` instance using the normal startup options, then manage it from its own REPL:

```text
controlpanel --instance records --init
controlpanel --instance records --repl
controlpanel[records]> owlcms install latest
```

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
| `owlcms start [selector]` | Start detached using the version's persistent settings. Without a selector, starts `A`. |
| `owlcms run [selector]` | Start in the foreground using the version's persistent settings and return to the REPL after the process exits. Without a selector, runs `A`. |
| `owlcms stop` | Stop the running OWLCMS process in the current instance; no installed version is selected. |
| `owlcms status` | Show the OWLCMS process currently running in this instance. |
| `owlcms <selector> port <port>` | Persist the OWLCMS HTTP port for one installed version as `OWLCMS_PORT`. |
| `owlcms <selector> tracker on` | Persistently enable the Tracker connection using the GUI's default URL and port. |
| `owlcms <selector> tracker on <port>` | Persistently enable the Tracker connection using the GUI's default URL and the given port. |
| `owlcms <selector> tracker on <url> <port>` | Persistently enable the Tracker connection using a `ws://` or `wss://` URL ending in `/ws` and a separate port. |
| `owlcms <selector> tracker off` | Persistently disable the Tracker connection for one installed version. |
| `owlcms <selector> mqtt on [port]` | Persistently enable embedded MQTT as `OWLCMS_ENABLEEMBEDDEDMQTT`; an optional port sets `OWLCMS_MQTTPORT`. |
| `owlcms <selector> mqtt off` | Persistently disable embedded MQTT for one installed version. |
| `owlcms install [release]` | Download and cleanly install a release. Defaults to the latest available release. |
| `owlcms install-zip <zip-path> [installed-version]` | Install a local ZIP. The semantic version is inferred from the file name when possible; an explicit installed version must be semantic. |
| `owlcms export [selector] <zip-path\|directory>` | Create a ZIP archive from an installed version. Without a selector, exports `A`. A directory receives a timestamped archive. |
| `owlcms update [selector] [release]` | Download a release and migrate data and configuration from a local source version. The selector defaults to `A` and the release defaults to `latest`; therefore `owlcms update A` updates `A` to the latest available release. |
| `owlcms rename [selector] <name>` | Rename an installed version by replacing its metadata with `<name>`. Without a selector, renames `A`. |
| `owlcms duplicate [selector] <name>` | Copy an installed version using `<name>` as its new metadata. Without a selector, duplicates `A`. |
| `owlcms import <source-selector> <target-selector>` | Copy data and configuration between two installed versions. |
| `owlcms remove [selector]` | Permanently remove an installed version. Without a selector, removes `A`; batch mode executes immediately. |

Examples:

```text
owlcms A port 8080
owlcms A tracker on 8096
owlcms A mqtt on 1883
owlcms start A
owlcms update B latest
owlcms rename B practice
owlcms duplicate B backup

# Configure a remote Tracker endpoint for the most recent OWLCMS version.
owlcms A tracker on wss://tracker.example.org/ws 443
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
| `tracker update [selector] [release]` | Download a release and migrate data and configuration from a local source version. The selector defaults to `A` and the release defaults to `latest`; therefore `tracker update A` updates `A` to the latest available release. |
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

- A plain `controlpanel --repl` session operates on the normal OWLCMS instance. Use `controlpanel --instance <name> --repl` to start a REPL for a separate sibling instance.
- `start` is the normal REPL operation and runs detached so subsequent commands remain available.
- `run` preserves the existing foreground behavior and occupies the REPL until the launched module exits.
- `owlcms <selector> port`, `tracker`, and `mqtt` settings persist in that version's `env.properties`, use the same configuration keys as the graphical Control Panel, and apply on its next launch.
- `owlcms versions`, `owlcms start`, `owlcms run`, `owlcms stop`, and their Tracker counterparts remain available while the graphical Control Panel is running when they are safe under the existing runtime rules.
- Installing, updating, importing, duplicating, exporting, and removing versions require exclusive Control Panel ownership.
- In batch mode, `remove` executes immediately. The interactive confirmation behavior is not yet decided.
