[Setup]
AppName=owlcms Control Panel
#ifdef DisplayVersion
AppVerName=owlcms Control Panel {#DisplayVersion}
#endif
AppVersion=3.0.5400
AppPublisher=Jean-François Lamy
AppPublisherURL=https://owlcms.jflamy.dev
AppSupportURL=https://groups.google.com/g/owlcms
AppUpdatesURL=https://github.com/owlcms/owlcms-controlpanel
VersionInfoVersion=3.0.5400
VersionInfoCompany=owlcms
VersionInfoDescription=owlcms Control Panel Installer
VersionInfoCopyright=© 2024 Jean-François Lamy
MinVersion=10.0
Compression=lzma2/ultra64
SolidCompression=yes
DefaultDirName={userappdata}\owlcms Control Panel
OutputDir=.
OutputBaseFilename=owlcms-Panel-installer_windows
SetupIconFile=installer.ico
UninstallDisplayIcon={app}\installer.ico
PrivilegesRequired=lowest
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableStartupPrompt=yes

[Files]
Source: "iss\owlcms.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "iss\owlcms.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "iss\installer.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\owlcms Control Panel"; Filename: "{app}\owlcms.exe"; IconFilename: "{app}\owlcms.ico"
Name: "{userdesktop}\owlcms Control Panel"; Filename: "{app}\owlcms.exe"; IconFilename: "{app}\owlcms.ico"
Name: "{group}\Uninstall owlcms Control Panel"; Filename: "{uninstallexe}"; IconFilename: "{app}\installer.ico"

[Run]
Filename: "{app}\owlcms.exe"; Description: "Launch owlcms Control Panel"; Flags: nowait postinstall skipifsilent
