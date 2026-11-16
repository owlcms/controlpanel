## Control Panel for OWLCMS and Associated Modules

The control panel is used to launch the OWLCMS competition software at the competition site,
as well as the optional complementary modules owlcms-tracker and owlcms-firmata.  The jury
replay modules will be added in a future release.

For all modules, it allows

- initial installation of the sofware, downloading any prerequisites
- launching the program
- having several versions at once and copying configurations and data from one version to the other

Currently supported: Windows, macOS, Raspberry Pi, Linux on Intel<br>

<img width="952" height="560" alt="image" src="https://github.com/user-attachments/assets/0bab83cc-71dc-4714-851a-6d9d33868e4b" />

See the [OWLCMS Control Panel documentation](https://jflamy.github.io/owlcms4/#/010-RunningLocally/010-LocalControlPanel) for installation and usage details.

### Launching Tracker with OWLCMS

In the OWLCMS tab, choose **Options > Default Tracker Connection** and enable
**Launch the latest locally installed tracker and connect when starting OWLCMS**. Each
OWLCMS version can override this default in its own Tracker Connection options.
The usual connection settings are retained but ignored while automatic launch is
enabled. Graphical launches use the latest installed Tracker's local port and
shared key. If that Tracker is already running, it is reused; a different running
version is reported as a conflict. Tracker stops when OWLCMS stops.

This option applies only to graphical launches, not REPL or batch commands.

