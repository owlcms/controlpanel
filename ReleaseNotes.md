This is a control panel for owlcms and associated modules.  It is meant to:

- Start and Stop owlcms, owlcms-tracker and owlcms-firmata
- Install updates
- Allow for multiple installations to be present at once for testing purposes, with the ability to copy data from one to another.

The control panel is installed once. It will automatically download the correct version of Java when used for the first time.


## Release Log

### 3.6.0

- Support setting the default ports prior to first install of a module
- Support installing from zip for initial install
- Raspberry installer now opens ports 80+ so OWLCMS can access them without root.
- Support for new video module, including macOS
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



