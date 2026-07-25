//go:build windows

package cmd

import (
	"path/filepath"
	"testing"
)

func TestFirstRunGuidePathsStayBesideInstalledExecutable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "claude-desktop-swap.exe")
	image, pending := firstRunGuidePaths(executable)
	if image != filepath.Join(filepath.Dir(executable), firstRunGuideImageName) {
		t.Fatalf("image path = %q", image)
	}
	if pending != filepath.Join(filepath.Dir(executable), firstRunGuidePendingName) {
		t.Fatalf("pending path = %q", pending)
	}
}

func TestFitGuideSizeUsesOriginalDimensionsWhenTheyFit(t *testing.T) {
	width, height := fitGuideSize(768, 768, 1200, 900)
	if width != 768 || height != 768 {
		t.Fatalf("fitGuideSize = %dx%d", width, height)
	}
}

func TestFitGuideSizeShrinksProportionallyToScreen(t *testing.T) {
	width, height := fitGuideSize(768, 768, 1200, 620)
	if width != 620 || height != 620 {
		t.Fatalf("fitGuideSize = %dx%d", width, height)
	}
}
