# OWLCMS Control Panel Command-Line Guide

The Control Panel normally opens its graphical interface:

```bash
controlpanel
```

The command line has startup options for selecting and creating instances, plus REPL and batch modes for managing OWLCMS and Tracker without opening the graphical application.

## Instances

An instance has its own Control Panel configuration, OWLCMS versions, Tracker versions, and process state. The normal instance is selected when no option is provided.

```bash
# Open the graphical Control Panel for an existing instance.
controlpanel --instance records

# Create the directory layout for a new instance.
controlpanel --instance records --init

# Use explicit Control Panel and shared runtime directories.
controlpanel --instance-dir /srv/owlcms/records --runtime-dir /srv/owlcms/runtime --init
```

| Option | Purpose |
| --- | --- |
| `-i`, `--instance <name>` | Select a named sibling instance. A single positional name is also accepted. |
| `--instance-dir`, `--instance_dir <path>` | Select an explicit Control Panel directory. |
| `--runtime-dir`, `--runtime_dir <path>` | Select the shared Java, Node.js, and FFmpeg runtime directory. |
| `--init` | Initialize the selected instance directory layout, then exit. |
| `--help`, `-h` | Print command-line help. |

## REPL

Open a textual command loop for one startup-selected instance:

```bash
controlpanel --repl
controlpanel --instance records --repl
```

The instance cannot change during a session. Use `context` to show the selected directories and `help` to show the available OWLCMS and Tracker commands.

```text
controlpanel[records]> owlcms versions
controlpanel[records]> owlcms A port 8080
controlpanel[records]> owlcms start A
controlpanel[records]> tracker start 8096
```

## Batch Mode

Run the same commands from a file, or use `-` for standard input. A batch stops at its first error and returns a non-zero status.

```bash
controlpanel --instance records --batch competition.commands
printf '%s\n' 'owlcms stop' 'owlcms update A latest' 'owlcms start' | controlpanel --instance records --batch -
```

See [REPL Commands](docs/proposals/REPL_COMMANDS.md) for the complete command language, version selector rules, and operational behavior.
