param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("amd64", "arm64")]
    [string]$Arch,

    [Parameter(Mandatory = $true)]
    [string]$OutputDir
)

$ErrorActionPreference = "Stop"

$repository = Split-Path -Parent $PSScriptRoot
$source = Join-Path $repository "launcher\windows_launcher.c"
$outputDirectory = [IO.Path]::GetFullPath($OutputDir)
$output = Join-Path $outputDirectory "windows-claude-swap-launcher.exe"
$vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio\Installer\vswhere.exe"
$component = if ($Arch -eq "arm64") {
    "Microsoft.VisualStudio.Component.VC.Tools.ARM64"
} else {
    "Microsoft.VisualStudio.Component.VC.Tools.x86.x64"
}
$targetArch = if ($Arch -eq "arm64") { "arm64" } else { "x64" }

if (-not (Test-Path -LiteralPath $source)) {
    throw "Launcher source not found: $source"
}
if (-not (Test-Path -LiteralPath $vswhere)) {
    throw "Visual Studio discovery tool not found: $vswhere"
}
$installation = & $vswhere -latest -products "*" -requires $component -property installationPath
if (-not $installation) {
    throw "Visual C++ build tools for $Arch are not installed"
}
$developerCommand = Join-Path $installation "Common7\Tools\VsDevCmd.bat"
if (-not (Test-Path -LiteralPath $developerCommand)) {
    throw "Visual Studio developer command not found: $developerCommand"
}

New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("windows-claude-swap-launcher-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
$object = Join-Path $temporaryDirectory "windows_launcher.obj"

try {
    $compiler = @(
        "cl.exe /nologo /TC /utf-8 /O2 /GL /W4 /WX /GS /sdl /MT",
        "/Fo:`"$object`" /Fe:`"$output`" `"$source`"",
        "/link /LTCG /SUBSYSTEM:WINDOWS /DYNAMICBASE /NXCOMPAT /HIGHENTROPYVA /OPT:REF /OPT:ICF /MANIFEST:EMBED shell32.lib user32.lib"
    ) -join " "
    $compile = "call `"$developerCommand`" -no_logo -arch=$targetArch -host_arch=x64 && $compiler"
    & $env:ComSpec /d /s /c $compile
    if ($LASTEXITCODE -ne 0) {
        throw "Native launcher build failed with exit code $LASTEXITCODE"
    }
    if (-not (Test-Path -LiteralPath $output)) {
        throw "Native launcher build did not produce $output"
    }
    Get-Item -LiteralPath $output
} finally {
    $resolvedTemporary = (Resolve-Path -LiteralPath $temporaryDirectory -ErrorAction SilentlyContinue).Path
    $expectedParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd("\")
    if ($resolvedTemporary -and (Split-Path -Parent $resolvedTemporary) -eq $expectedParent) {
        Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
    }
}
