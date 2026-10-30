This is a control panel for owlcms and associated modules.  It is meant to:

- Start and Stop owlcms, owlcms-tracker and owlcms-firmata
- Install updates
- Allow for multiple installations to be present at once for testing purposes, with the ability to copy data from one to another.

The control panel is installed once. It will automatically download the correct version of Java when used for the first time.


## Release Log

### 3.10.0

- OWLCMS is given a new control panel token at each launch, restarts included. The OWLCMS tab has a copy icon for it; the token is not displayed.
  - Log in to OWLCMS as `admin` with this token as the password to set the admin password, the first time or when it is forgotten.

- REPL: `secret encrypt <keyname> <value>` encrypts with the key file `~/.owlcms/<keyname>`, created on first use, so that keys given to other programs are kept apart from the control panel's own key. For example, `secret encrypt apiprotection.aes <value>` for the API keys that OWLCMS decrypts with `~/.owlcms/apiprotection.aes`.

### 3.9.0

- The separate Cameras and Replays tabs have been removed. The Video module replaces both programs.
- Update notifications are more conservative about prereleases:
  - Alpha versions are only proposed as updates to users who already run an alpha version. Users running a beta or release candidate are no longer told about the next alpha.
  - Prereleases are only proposed to users who already run a prerelease. This now also applies to the OWLCMS "Check for Updates" dialog.
  - All versions can still be installed from the release list by using "Show Prereleases".
- The "Check for Updates" menu entry now runs in the background so the window stays responsive.
- When a new control panel version is available, the download link now points to the GitHub installer for the platform (Windows, macOS Apple Silicon or Intel, Linux, Raspberry Pi). The "No Updates" dialog also links to the latest installer for the platform.
- (internal) Screen updates from background tasks (downloads, release lists, process monitoring, cleanup) are now applied safely on the display thread. This removes the "Error in Fyne call thread" warnings and possible display glitches.

### 3.8.1

- 3.8.1 Tracker installation on Windows was not putting the node executables in the correct locations.
- The control panel can now manage the keys for tracker, so there is no need to go to the OWLCMS database to store these values.
- Fixed the "default" dialogs for OWLCMS and Tracker to allow the definition of a default key.
  - Default options are applied when the versions are installed
  - Tracker and OWLCMS manage their own default keys. For normal usage, these would be set to the same value.
  - There is a "reset to default key" for the OWLCMS and for Tracker
- Shared keys per version: "Reset to Default Key" makes the version use the default key again; "Clear Key" sets no key for that version even if a default key exists. The default dialogs also have "Clear Key".

### 3.7.0

- controlpanel --repl
  - this starts a text-based interactive command loop used when running OWLCMS or tracker on a Linux server with no GUI.
  - inside you can type `owlcms help` or `tracker help` to get the commands.  The commands mirror the GUI menus.
  - controlpanel --init creates the environment same as starting the GUI.
- packaging: fix macOS camera permission issues when running from an icon as opposed to Terminal.

### 3.6.0

- Support setting the default ports prior to first install of a module
- Support installing from zip for initial install
- Raspberry installer now opens ports 80+ so OWLCMS can access them without root.
- Support for new video module, including macOS
- Fix camera detection timing out when the video module is launched from the macOS control panel.
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



