//go:build windows

package cmd

import "testing"

func TestUpdateAvailableComparesStableVersions(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"1.2.3", "v1.2.3", false},
		{"v2.0.0", "v1.9.9", false},
		{"dev", "v1.0.0", true},
		{"dev", "", false},
		{"dev.49", "v1.0.0", false},
	}
	for _, test := range tests {
		if got := updateAvailable(test.current, test.latest); got != test.want {
			t.Fatalf("updateAvailable(%q, %q) = %v, want %v", test.current, test.latest, got, test.want)
		}
	}
}

func TestGitHubPagesPointToThisRepository(t *testing.T) {
	if githubRepoPage != "https://github.com/TinoA/claude-desktop-swap" {
		t.Fatalf("githubRepoPage = %q", githubRepoPage)
	}
	if githubReleasePage != githubRepoPage+"/releases/latest" {
		t.Fatalf("githubReleasePage = %q", githubReleasePage)
	}
}

func TestParseVersionRejectsNonSemverValues(t *testing.T) {
	if _, ok := parseVersion("dev"); ok {
		t.Fatal("dev should not parse as a release version")
	}
	if got, ok := parseVersion("v1.2.3"); !ok || got != [3]int{1, 2, 3} {
		t.Fatalf("parseVersion = %v/%v", got, ok)
	}
}

func TestDisplayVersion(t *testing.T) {
	if got := displayVersion("1.0.3"); got != "v1.0.3" {
		t.Fatalf("displayVersion = %q, want v1.0.3", got)
	}
	if got := displayVersion("v1.0.3"); got != "v1.0.3" {
		t.Fatalf("displayVersion = %q, want v1.0.3", got)
	}
	if got := displayVersion("local-test"); got != "local-test" {
		t.Fatalf("displayVersion = %q, want local-test", got)
	}
}
