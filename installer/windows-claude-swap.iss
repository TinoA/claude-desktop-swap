#ifndef AppArch
#define AppArch "amd64"
#endif

#ifndef AppVersion
#define AppVersion "0.0.0"
#endif

#if AppArch == "arm64"
#define AllowedArchitectures "arm64"
#define InstallMode "arm64"
#else
#define AllowedArchitectures "x64compatible"
#define InstallMode "x64compatible"
#endif

[Setup]
AppId={{7B9DFBB8-53B7-4C6C-9C4F-1EAE5B4A7C09}
AppName=Windows Claude Swap
AppVersion={#AppVersion}
AppPublisher=TinoA
AppPublisherURL=https://github.com/TinoA/claude-desktop-swap
AppSupportURL=https://github.com/TinoA/claude-desktop-swap/issues
AppUpdatesURL=https://github.com/TinoA/claude-desktop-swap/releases
DefaultDirName={localappdata}\Windows Claude Swap
DefaultGroupName=Windows Claude Swap
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed={#AllowedArchitectures}
ArchitecturesInstallIn64BitMode={#InstallMode}
CloseApplications=yes
CloseApplicationsFilter=claude-desktop-swap.exe
RestartApplications=yes
OutputDir=dist\installer
OutputBaseFilename=Windows-Claude-Swap-Setup-{#AppArch}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
SetupIconFile=..\cmd\assets\windows-claude-swap-icon-v2.ico
UninstallDisplayName=Windows Claude Swap
Uninstallable=yes
LicenseFile=..\LICENSE

[Tasks]
Name: "startup"; Description: "Iniciar Windows Claude Swap con Windows"; GroupDescription: "Opciones adicionales:"

[Files]
Source: "dist\windows_{#AppArch}\claude-desktop-swap.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\cmd\assets\windows-claude-swap-icon-v2.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Windows Claude Swap"; Filename: "{app}\claude-desktop-swap.exe"; Parameters: "tray"; WorkingDir: "{app}"; IconFilename: "{app}\windows-claude-swap-icon-v2.ico"
Name: "{userstartup}\Windows Claude Swap"; Filename: "{app}\claude-desktop-swap.exe"; Parameters: "tray"; WorkingDir: "{app}"; IconFilename: "{app}\windows-claude-swap-icon-v2.ico"; Tasks: startup

[Run]
Filename: "{app}\claude-desktop-swap.exe"; Parameters: "tray"; Description: "Iniciar Windows Claude Swap"; Flags: nowait postinstall skipifsilent
