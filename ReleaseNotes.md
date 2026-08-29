This is a control panel for owlcms and associated modules.  It is meant to:

- Start and Stop owlcms, owlcms-tracker and owlcms-firmata
- Install updates
- Allow for multiple installations to be present at once for testing purposes, with the ability to copy data from one to another.

The control panel is installed once. It will automatically download the correct version of Java when used for the first time.


## Release Log

### 3.6.0
.
- Support setting the default ports prior to first install of a module
- Support installing from zip for initial install
- Raspberry installer now opens ports 80+ without root
- Support for video module, including macOS
- (internal) Code lint, macOS assumed to be dev platform.
- On macOS, check for a brew install of ffmpeg when installing the video module because it is not built-in and not supported in the one we download for Windows.

### 3.5.0

- Improve detection of already running stale instance of OWLCMS that can hold the database and prevent upgrade
- (macOS) Notarization of macOS .dmg files to allow direct DMG install
- (macOS) support brew for install and updates
- (Linux) restrict background daemon launching of owlcms to the command-line interface

- Processing of default values
  - separated global defaults from versin defaults in OWLCMS
  - local versions override the global default
  - the global default is kept unless explicitly overridden (previously the global was always hidden)
  - added capability to reach an external tracker without having to change the database

### 3.4.0

- Fixed the update behavior for the camera and replay modules available for Windows and Linux.
- AppleSilicon (M-Series) dmg available; separate dmg for Intel Macs
- Created the command-line option equivalents to the interactive control panel.  Run the program from a terminal with --help to see the options.
- Update button was proposing spurious updates after performing an update.
- More attempts to clean up user interface startup sizing and scaling issues repeatable only on a single computer.

### 3.3.8

- Detect that the tracker version is a custom zip with a non-standard set of plugins. Prevent updating with a standard build.

### 3.3.7

- build for all versions

### 3.3.6

- for owlcms-firmata, we now explicitly extract the shared library or DLL and force it.  Windows-on-Windows emulation broke that in Windows 11.
- On Windows, if a process will not die, we do a priviledge escalation as a last resort

### 3.3.5

- fixed the environment construction prior to launching a Java process so the local env.properties overrides the global one

### 3.3.4

- When updating cameras, copy the prior config.toml 

### 3.3.3

- Disabling the local tracker connection to use the owlcms database value did not work (an override still took place)

### 3.3.2

- Updating a version using the Update button in the version list will preserve the metadata information
  - also, we accept Unicode accented letters in the metadata as an extension to semantic versioning.

### 3.3.1

- Fixed the installer package numbering for Linux .deb files

### 3.3.0

- Improved process kill
  - will now attempt to locate and kill a process using the port even if the PID file is stale
  - SIGINT, SIGTERM, SIGKILL are treated as intentional stops, same as using the stop button, no restarts.
- Command-line options for multiple instances and Linux daemon mode
  - Run controlpanel --help for details
  - These options are Linux-oriented, targeted at virtual privatehosting scenarios.
  - running with --owlcms --tracker both creates a connected tandem where owlcms feeds the tracker on the port indicated by the tracker config.
  - controlpanel --init now initializes the main owlcms instance instead of reporting an empty instance name
  - A daemon mode is provided
    - Under systemd, the Go process stays alive and supervises OWLCMS (restart on non-zero exit).
    - From a terminal, the Go process exits after launch and a Java helper (MainWrapper) babysits OWLCMS in the background, surviving logout.


