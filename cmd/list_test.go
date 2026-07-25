package cmd

import (
	"testing"

	"github.com/TinoA/claude-desktop-switcher/internal/profile"
)

func TestHealthLabelDistinguishesAllStates(t *testing.T) {
	for _, health := range []profile.Health{profile.HealthUsable, profile.HealthExpired, profile.HealthMissing, profile.HealthUnknown} {
		if got := healthLabel(health); got != string(health) {
			t.Fatalf("healthLabel(%q) = %q", health, got)
		}
	}
}
