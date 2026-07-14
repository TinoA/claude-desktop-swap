//go:build windows

package cmd

import (
	"errors"
	"testing"

	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
)

func TestResolveDeleteActivityFallsBackToTrackedAccount(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		current      string
		liveName     string
		health       profile.Health
		wantActive   bool
		wantVerified bool
		wantErr      error
	}{
		{name: "unknown live session allows other account", target: "install-test", current: "hgj", health: profile.HealthUnknown},
		{name: "unknown live session protects tracked account", target: "hgj", current: "hgj", health: profile.HealthUnknown, wantActive: true},
		{name: "unknown session without tracking is unsafe", target: "install-test", health: profile.HealthUnknown, wantErr: errDeleteSessionUnknown},
		{name: "verified live account is protected", target: "install-test", current: "hgj", liveName: "install-test", health: profile.HealthUsable, wantActive: true, wantVerified: true},
		{name: "unrecognized live session allows other account", target: "install-test", current: "hgj", health: profile.HealthUsable},
		{name: "unrecognized live session protects tracked account", target: "hgj", current: "hgj", health: profile.HealthUsable, wantErr: errDeleteSessionUnrecognized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active, verified, err := resolveDeleteActivity(tt.target, tt.current, tt.liveName, tt.health)
			if active != tt.wantActive || verified != tt.wantVerified || !errors.Is(err, tt.wantErr) {
				t.Fatalf("got active=%v verified=%v err=%v", active, verified, err)
			}
		})
	}
}
