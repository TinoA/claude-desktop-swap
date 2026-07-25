package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestWindowsInstallerUsesNativeLauncher(t *testing.T) {
	data, err := os.ReadFile("installer/windows-claude-swap.iss")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	launcher := `Filename: "{app}\windows-claude-swap-launcher.exe"`
	if count := strings.Count(script, launcher); count != 3 {
		t.Fatalf("native launcher invocation count = %d, want 3", count)
	}
	if !strings.Contains(script, `Source: "dist\windows_{#AppArch}\windows-claude-swap-launcher.exe"`) {
		t.Fatal("installer does not package the native launcher")
	}
	if !strings.Contains(script, `UninstallDisplayIcon={app}\windows-claude-swap-icon-v2.ico`) {
		t.Fatal("installer does not set its uninstall display icon")
	}
	if strings.Contains(script, `Filename: "{app}\claude-desktop-swap.exe"`) {
		t.Fatal("installer launches the Go GUI executable directly")
	}
	if strings.Contains(script, "rundll32.exe") || strings.Contains(script, "FileProtocolHandler") {
		t.Fatal("installer still depends on the rundll32 compatibility relay")
	}
	for _, shortcut := range []string{
		`Name: "{userprograms}\Windows Claude Swap"; Filename: "{app}\windows-claude-swap-launcher.exe"; WorkingDir: "{app}"; IconFilename: "{app}\windows-claude-swap-icon-v2.ico"`,
		`Name: "{userstartup}\Windows Claude Swap"; Filename: "{app}\windows-claude-swap-launcher.exe"; WorkingDir: "{app}"; IconFilename: "{app}\windows-claude-swap-icon-v2.ico"; Tasks: startup`,
	} {
		if !strings.Contains(script, shortcut) {
			t.Fatalf("installer shortcut changed: missing %q", shortcut)
		}
	}
}

func TestWindowsTrayIconIncludesCommonSizes(t *testing.T) {
	data, err := os.ReadFile("cmd/assets/windows-claude-swap-icon-v2.ico")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 6 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		t.Fatal("icon is not a valid ICO file")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if len(data) < 6+count*16 {
		t.Fatal("icon directory is incomplete")
	}
	sizes := make(map[int]bool)
	for index := 0; index < count; index++ {
		width := int(data[6+index*16])
		if width == 0 {
			width = 256
		}
		sizes[width] = true
	}
	for _, size := range []int{16, 32, 48, 256} {
		if !sizes[size] {
			t.Fatalf("icon is missing %dx%d frame", size, size)
		}
	}
}

func TestTrayIconIsTransparentAndUsesOrangeAsset(t *testing.T) {
	source, err := os.ReadFile("cmd/tray_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "//go:embed assets/windows-claude-swap-tray.ico") {
		t.Fatal("tray does not embed the dedicated monochrome icon")
	}
	data, err := os.ReadFile("cmd/assets/windows-claude-swap-tray.ico")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 22 || binary.LittleEndian.Uint16(data[4:6]) < 9 {
		t.Fatal("tray icon directory is incomplete")
	}
	var frame []byte
	for index := 0; index < int(binary.LittleEndian.Uint16(data[4:6])); index++ {
		entry := 6 + index*16
		if data[entry] != 0 {
			continue
		}
		size := int(binary.LittleEndian.Uint32(data[entry+8 : entry+12]))
		offset := int(binary.LittleEndian.Uint32(data[entry+12 : entry+16]))
		if offset+size <= len(data) {
			frame = data[offset : offset+size]
		}
		break
	}
	if len(frame) == 0 {
		t.Fatal("tray icon is missing its 256px frame")
	}
	image, err := png.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, cornerAlpha := image.At(0, 0).RGBA()
	if cornerAlpha != 0 {
		t.Fatal("tray icon corner must be transparent")
	}
	visible := false
	orange := false
	for y := image.Bounds().Min.Y; y < image.Bounds().Max.Y; y++ {
		for x := image.Bounds().Min.X; x < image.Bounds().Max.X; x++ {
			red, green, blue, alpha := image.At(x, y).RGBA()
			if alpha == 0 {
				continue
			}
			visible = true
			if red > green && green > blue {
				orange = true
			}
		}
	}
	if !visible {
		t.Fatal("tray icon contains no visible pixels")
	}
	if !orange {
		t.Fatal("tray icon must contain orange pixels")
	}
}

func TestWindowsInstallerShowsTrayGuideOnlyOnFirstInstall(t *testing.T) {
	data, err := os.ReadFile("installer/windows-claude-swap.iss")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`Source: "assets\first-run-guide.bmp"; DestDir: "{app}"; Flags: ignoreversion`,
		`FirstInstall := not DirExists(WizardDirValue)`,
		`if CurStep = ssInstall then`,
		`if (CurStep = ssPostInstall) and FirstInstall then`,
		`SaveStringToFile(ExpandConstant('{app}\first-run-guide.pending'), 'pending', False);`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing %q", required)
		}
	}
	for _, removed := range []string{"ShowTrayGuide", "Got it", "CreateCustomForm", "ExtractTemporaryFile('first-run-guide.bmp')"} {
		if strings.Contains(script, removed) {
			t.Fatalf("installer still shows the old tray guide: found %q", removed)
		}
	}
}
