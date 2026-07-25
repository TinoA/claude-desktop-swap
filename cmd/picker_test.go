package cmd

import (
	"strings"
	"testing"

	"github.com/TinoA/claude-desktop-switcher/internal/profile"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPickerShowsHealthWithoutSecretData(t *testing.T) {
	m := newPickerModel([]profile.Meta{{Name: "work", ObservedHealth: profile.HealthExpired}}, "work")
	view := m.View()
	if !strings.Contains(view, "expired") {
		t.Fatalf("view = %q", view)
	}
	if strings.Contains(view, "sessionKey") || strings.Contains(view, "secret") {
		t.Fatalf("view exposed session data: %q", view)
	}
}

func TestPickerBlocksUnusableSelection(t *testing.T) {
	for _, health := range []profile.Health{profile.HealthExpired, profile.HealthMissing, profile.HealthUnknown} {
		m := newPickerModel([]profile.Meta{{Name: "work", ObservedHealth: health}}, "")
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		got := updated.(pickerModel)
		if got.chosen != "" {
			t.Fatalf("health %s selected %q", health, got.chosen)
		}
	}
}

func TestPickerAllowsUsableSelection(t *testing.T) {
	m := newPickerModel([]profile.Meta{{Name: "work", ObservedHealth: profile.HealthUsable}}, "")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(pickerModel).chosen; got != "work" {
		t.Fatalf("chosen = %q", got)
	}
}

func TestAccountLabelPrefersEmailAndHidesAutomaticID(t *testing.T) {
	meta := profile.Meta{Name: "account-12345678-1234-4234-8234-123456789abc", Email: "person@example.test"}
	if got := accountLabel(meta); got != "person@example.test" {
		t.Fatalf("accountLabel with email = %q", got)
	}
	meta.Email = ""
	if got := accountLabel(meta); got != "Account 12345678" {
		t.Fatalf("accountLabel fallback = %q", got)
	}
	if got := accountLabel(profile.Meta{Name: "personal"}); got != "personal" {
		t.Fatalf("legacy accountLabel = %q", got)
	}
}
