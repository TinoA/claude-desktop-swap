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
UninstallDisplayIcon={app}\windows-claude-swap-icon-v2.ico
Uninstallable=yes
LicenseFile=..\LICENSE

[Tasks]
Name: "startup"; Description: "Start Windows Claude Swap with Windows"; GroupDescription: "Additional options:"

[Dirs]
Name: "{app}"; Permissions: users-readexec

[Files]
Source: "dist\windows_{#AppArch}\claude-desktop-swap.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\windows_{#AppArch}\windows-claude-swap-launcher.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\cmd\assets\windows-claude-swap-icon-v2.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "assets\first-run-guide.bmp"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[InstallDelete]
Type: filesandordirs; Name: "{userprograms}\Windows Claude Swap"
Type: files; Name: "{userprograms}\Windows Claude Swap.lnk"

[Icons]
; Windows Search can end the Go GUI process before main; the native launcher relays a normal shell launch without a console.
Name: "{userprograms}\Windows Claude Swap"; Filename: "{app}\windows-claude-swap-launcher.exe"; WorkingDir: "{app}"; IconFilename: "{app}\windows-claude-swap-icon-v2.ico"
Name: "{userstartup}\Windows Claude Swap"; Filename: "{app}\windows-claude-swap-launcher.exe"; WorkingDir: "{app}"; IconFilename: "{app}\windows-claude-swap-icon-v2.ico"; Tasks: startup

[Run]
Filename: "{app}\windows-claude-swap-launcher.exe"; Description: "Start Windows Claude Swap"; Flags: nowait postinstall skipifsilent runasoriginaluser

[Code]
var
  FirstInstall: Boolean;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
    FirstInstall := not DirExists(WizardDirValue)
  else if (CurStep = ssPostInstall) and FirstInstall then
    SaveStringToFile(ExpandConstant('{app}\first-run-guide.pending'), 'pending', False);
end;
