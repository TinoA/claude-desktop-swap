//go:build windows

package cmd

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/TinoA/claude-desktop-switcher/internal/profile"
)

type closedDeletionStoreFake struct {
	events     []string
	current    string
	profiles   []profile.Meta
	deleteErr  error
	restoreErr map[string]error
	wipeErr    error
}

func (s *closedDeletionStoreFake) Current() (string, error) {
	s.events = append(s.events, "current")
	return s.current, nil
}

func (s *closedDeletionStoreFake) List() ([]profile.Meta, error) {
	s.events = append(s.events, "list")
	return s.profiles, nil
}

func (s *closedDeletionStoreFake) Delete(name string) error {
	s.events = append(s.events, "delete:"+name)
	return s.deleteErr
}

func (s *closedDeletionStoreFake) RestoreAt(name, _, _ string) error {
	s.events = append(s.events, "restore:"+name)
	if err := s.restoreErr[name]; err != nil {
		return err
	}
	s.current = name
	return nil
}

func (s *closedDeletionStoreFake) WipeAt(_, _ string) error {
	s.events = append(s.events, "wipe")
	return s.wipeErr
}

func TestDeleteClosedCurrentRestoresRemainingAccountBeforeDeletion(t *testing.T) {
	now := time.Now()
	store := &closedDeletionStoreFake{
		current: "deleting",
		profiles: []profile.Meta{
			{Name: "deleting", ObservedHealth: profile.HealthUsable, LastUsed: now},
			{Name: "older", ObservedHealth: profile.HealthUsable, LastUsed: now.Add(-time.Hour)},
			{Name: "newer", ObservedHealth: profile.HealthUsable, LastUsed: now.Add(-time.Minute)},
		},
		restoreErr: make(map[string]error),
	}
	if err := deleteClosedAccount("deleting", store, `C:\Claude`, `C:\Claude\Network\Cookies`); err != nil {
		t.Fatal(err)
	}
	want := []string{"current", "list", "restore:newer", "delete:deleting"}
	if !reflect.DeepEqual(store.events, want) {
		t.Fatalf("events = %v, want %v", store.events, want)
	}
}

func TestDeleteClosedLastAccountWipesBeforeDeleting(t *testing.T) {
	store := &closedDeletionStoreFake{
		current:    "deleting",
		profiles:   []profile.Meta{{Name: "deleting", ObservedHealth: profile.HealthUsable}},
		restoreErr: make(map[string]error),
	}
	if err := deleteClosedAccount("deleting", store, `C:\Claude`, `C:\Claude\Network\Cookies`); err != nil {
		t.Fatal(err)
	}
	want := []string{"current", "list", "wipe", "delete:deleting"}
	if !reflect.DeepEqual(store.events, want) {
		t.Fatalf("events = %v, want %v", store.events, want)
	}
}

func TestDeleteClosedCurrentRollsBackWhenProfileDeletionFails(t *testing.T) {
	store := &closedDeletionStoreFake{
		current: "deleting",
		profiles: []profile.Meta{
			{Name: "deleting", ObservedHealth: profile.HealthUsable},
			{Name: "remaining", ObservedHealth: profile.HealthUsable},
		},
		deleteErr:  errors.New("delete failed"),
		restoreErr: make(map[string]error),
	}
	err := deleteClosedAccount("deleting", store, `C:\Claude`, `C:\Claude\Network\Cookies`)
	if !errors.Is(err, store.deleteErr) {
		t.Fatalf("error = %v", err)
	}
	want := []string{"current", "list", "restore:remaining", "delete:deleting", "restore:deleting"}
	if !reflect.DeepEqual(store.events, want) {
		t.Fatalf("events = %v, want %v", store.events, want)
	}
}

func TestResolveDeleteActivityRequiresVerifiedLiveState(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		liveName     string
		health       profile.Health
		wantActive   bool
		wantVerified bool
		wantErr      error
	}{
		{name: "unknown live session blocks deletion", target: "install-test", health: profile.HealthUnknown, wantErr: errDeleteSessionUnknown},
		{name: "expired live session blocks deletion", target: "install-test", health: profile.HealthExpired, wantErr: errDeleteSessionUnknown},
		{name: "missing live session allows deletion", target: "install-test", health: profile.HealthMissing},
		{name: "verified live account is protected", target: "install-test", liveName: "install-test", health: profile.HealthUsable, wantActive: true, wantVerified: true},
		{name: "verified different account allows deletion", target: "install-test", liveName: "hgj", health: profile.HealthUsable, wantVerified: true},
		{name: "unrecognized usable session blocks deletion", target: "install-test", health: profile.HealthUsable, wantErr: errDeleteSessionUnrecognized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active, verified, err := resolveDeleteActivity(tt.target, tt.liveName, tt.health)
			if active != tt.wantActive || verified != tt.wantVerified || !errors.Is(err, tt.wantErr) {
				t.Fatalf("got active=%v verified=%v err=%v", active, verified, err)
			}
		})
	}
}

func TestShouldOfferInitialSaveWithLockedOpenSession(t *testing.T) {
	tests := []struct {
		name         string
		profileCount int
		health       profile.Health
		running      bool
		want         bool
	}{
		{name: "usable session without profiles", health: profile.HealthUsable, want: true},
		{name: "locked open session without profiles", health: profile.HealthUnknown, running: true, want: true},
		{name: "unknown closed session", health: profile.HealthUnknown},
		{name: "missing session", health: profile.HealthMissing, running: true},
		{name: "expired session", health: profile.HealthExpired, running: true},
		{name: "existing profile keeps conservative verification", profileCount: 1, health: profile.HealthUnknown, running: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldOfferInitialSave(tt.profileCount, tt.health, tt.running); got != tt.want {
				t.Fatalf("shouldOfferInitialSave = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimInitialSaveOfferOnlyOnce(t *testing.T) {
	state := &trayState{}
	if !state.claimInitialSaveOffer(0, profile.HealthUnknown, true) {
		t.Fatal("first initial save offer was not claimed")
	}
	if state.claimInitialSaveOffer(0, profile.HealthUnknown, true) {
		t.Fatal("initial save offer was claimed more than once")
	}
}

func TestResolveActiveProfileUsesOnlyVerifiedOrTrustedState(t *testing.T) {
	tests := []struct {
		name           string
		detected       string
		health         profile.Health
		current        string
		trusted        string
		running        bool
		busy           bool
		identity       bool
		detectedExists bool
		trustedExists  bool
		want           string
	}{
		{name: "verified account", detected: "work", health: profile.HealthUsable, running: true, want: "work"},
		{name: "locked UUID account after tray restart", detected: "work", health: profile.HealthUnknown, identity: true, current: "work", running: true, detectedExists: true, want: "work"},
		{name: "locked UUID updates a different current marker", detected: "work", health: profile.HealthUnknown, identity: true, current: "personal", running: true, detectedExists: true, want: "work"},
		{name: "locked UUID profile must exist", detected: "work", health: profile.HealthUnknown, identity: true, current: "work", running: true},
		{name: "locked controlled account", health: profile.HealthUnknown, current: "work", trusted: "work", running: true, trustedExists: true, want: "work"},
		{name: "locked without trust", health: profile.HealthUnknown, current: "work", running: true, trustedExists: true},
		{name: "trust must match current", health: profile.HealthUnknown, current: "personal", trusted: "work", running: true, trustedExists: true},
		{name: "trusted profile must exist", health: profile.HealthUnknown, current: "work", trusted: "work", running: true},
		{name: "usable unknown account clears trust", health: profile.HealthUsable, current: "work", trusted: "work", running: true, trustedExists: true},
		{name: "closed Claude has no active account", health: profile.HealthUnknown, current: "work", trusted: "work", trustedExists: true},
		{name: "busy tray exposes no active account", health: profile.HealthUnknown, current: "work", trusted: "work", running: true, busy: true, trustedExists: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveActiveProfile(tt.detected, tt.health, tt.identity, tt.current, tt.trusted, tt.running, tt.busy, tt.detectedExists, tt.trustedExists)
			if got != tt.want {
				t.Fatalf("resolveActiveProfile = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveActiveIdentityUsesPersistedUUIDEvidence(t *testing.T) {
	tests := []struct {
		name        string
		detected    string
		health      profile.Health
		persisted   bool
		identity    string
		identityErr error
		wantName    string
		wantHealth  profile.Health
	}{
		{name: "UUID identifies saved account conservatively", health: profile.HealthUnknown, persisted: true, identity: "work", wantName: "work", wantHealth: profile.HealthUnknown},
		{name: "UUID identifies unsaved account conservatively", health: profile.HealthUnknown, persisted: true, wantHealth: profile.HealthUnknown},
		{name: "cookie match remains authoritative", detected: "personal", health: profile.HealthUsable, persisted: true, identity: "work", wantName: "personal", wantHealth: profile.HealthUsable},
		{name: "missing persisted authentication", health: profile.HealthUnknown, identity: "work", wantHealth: profile.HealthUnknown},
		{name: "identity read failure stays unknown", health: profile.HealthUnknown, persisted: true, identityErr: errors.New("locked"), wantHealth: profile.HealthUnknown},
		{name: "missing cookie is never promoted by stale identity", health: profile.HealthMissing, persisted: true, identity: "work", wantHealth: profile.HealthMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, health := resolveActiveIdentity(tt.detected, tt.health, tt.persisted, tt.identity, tt.identityErr)
			if name != tt.wantName || health != tt.wantHealth {
				t.Fatalf("resolveActiveIdentity = %q/%s, want %q/%s", name, health, tt.wantName, tt.wantHealth)
			}
		})
	}
}
