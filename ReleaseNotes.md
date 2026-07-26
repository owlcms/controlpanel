This is a control panel for owlcms and associated modules.  It is meant to:

- Start and Stop owlcms, owlcms-tracker and owlcms-firmata
- Install updates
- Allow for multiple installations to be present at once for testing purposes, with the ability to copy data from one to another.

The control panel is installed once. It will automatically download the correct version of Java when used for the first time.


## Release Log

- 3.6.0: Support for cameras and tracker on macOS
- 3.6.0: (internal) Code lint, macOS assumed to be dev platform.

- 3.5.0: Improve detection of already running stale instance of OWLCMS that can hold the database and prevent upgrade
- 3.5.0: (macOS) Notarization of macOS .dmg files to allow direct DMG install
- 3.5.0: (macOS) support brew for install and updates
- 3.5.0: (Linux) restrict background daemon launching of owlcms to the command-line interface

- 3.5.0: Processing of default values
  - separated global defaults from versin defaults in OWLCMS
  - local versions override the global default
  - the global default is kept unless explicitly overridden (previously the global was always hidden)
  - added capability to reach an external tracker without having to change the database

- 3.4.0: Fixed the update behavior for the camera and replay modules available for Windows and Linux.
- 3.4.0: AppleSilicon (M-Series) dmg available; separate dmg for Intel Macs
- 3.4.0: Created the command-line option equivalents to the interactive control panel.  Run the program from a terminal with --help to see the options.
- 3.4.0: Update button was proposing spurious updates after performing an update.
- 3.4.0: More attempts to clean up user interface startup sizing and scaling issues repeatable only on a single computer.

- 3.3.8: Detect that the tracker version is a custom zip with a non-standard set of plugins. Prevent updating with a standard build.

- 3.3.7: build for all versions
- 3.3.6: for owlcms-firmata, we now explicitly extract the shared library or DLL and force it.  Windows-on-Windows emulation broke that in Windows 11.
- 3.3.6: On Windows, if a process will not die, we do a priviledge escalation as a last resort

- 3.3.5: fixed the environment construction prior to launching a Java process so the local env.properties overrides the global one

- 3.3.4: When updating cameras, copy the prior config.toml 

- 3.3.3: Disabling the local tracker connection to use the owlcms database value did not work (an override still took place)

- 3.3.2: Updating a version using the Update button in the version list will preserve the metadata information
  - also, we accept Unicode accented letters in the metadata as an extension to semantic versioning.

- 3.3.1: Fixed the installer package numbering for Linux .deb files

- 3.3.0: Improved process kill
  - will now attempt to locate and kill a process using the port even if the PID file is stale
  - SIGINT, SIGTERM, SIGKILL are treated as intentional stops, same as using the stop button, no restarts.
- 3.3.0: Command-line options for multiple instances and Linux daemon mode
  - Run controlpanel --help for details
  - These options are Linux-oriented, targeted at virtual privatehosting scenarios.
  - running with --owlcms --tracker both creates a connected tandem where owlcms feeds the tracker on the port indicated by the tracker config.
  - controlpanel --init now initializes the main owlcms instance instead of reporting an empty instance name
  - A daemon mode is provided
    - Under systemd, the Go process stays alive and supervises OWLCMS (restart on non-zero exit).
    - From a terminal, the Go process exits after launch and a Java helper (MainWrapper) babysits OWLCMS in the background, surviving logout.

### Installing the Control Panel

> When downloading the following files, some browsers may give you warnings about "rarely downloaded files".   You may have to select "Keep" one or more times to download the file.

- For Windows, 
  1. Download the Windows installer from the Assets section below.
  2. Execute the .exe file
  3. The first time you run the program, you may get warnings in a blue dialog box.  Select "More Info" and "Run Anyway"
- For Mac
  1. Download the macOS `.dmg`  file
  4. Execute the `.dmg` file.  Drag the application to the Application folder, or drag the application to your desktop
  5. The first time you use the program, you **must** *Right-click on the application and use Open.*  This is only needed once, to allow execution.
- For Raspberry Pi
  1. Download the `.deb` file that starts with `Raspberry`.
  2. Go to your Downloads directory, *right-click on the downloaded file and select `Package Install`.*
     You will be prompted for your password to get installation permissions. This will create a desktop icon, and an entry in the "Other" section of the system menu.
- For Linux on Intel or AMD computers
  1. Download the `.deb` file that starts with `Linux_Intel`
  2. Start a terminal window and go to `~/Downloads`
  6. Use `sudo apt install ./Linux*.deb` (replacing the name with the actual name of the .deb file)

