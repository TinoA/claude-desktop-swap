package cmd

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
)

func TestPrepareBackupRefreshesMatchedAccountAndRelaunchesClaude(t *testing.T) {
	events := []string{}
	store := &backupPreparationStoreFake{events: &events, matched: "work", health: profile.HealthUsable}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}

	if err := prepareBackupProfiles(store, p, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "stop", "match", "checkpoint:work", "current:work", "incomplete", "launch"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestPrepareBackupNamesUntrackedActiveAccount(t *testing.T) {
	events := []string{}
	store := &backupPreparationStoreFake{events: &events, current: "old", health: profile.HealthUsable}
	p := &fakePlatform{events: &events, appData: t.TempDir()}
	resolver := func(current string) (string, error) {
		if current != "old" {
			t.Fatalf("current = %q, want old", current)
		}
		return "new", nil
	}

	if err := prepareBackupProfiles(store, p, resolver, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "match", "get-current", "checkpoint:new", "current:new", "incomplete"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestPrepareBackupRejectsLegacyProfileAndStillRelaunchesClaude(t *testing.T) {
	events := []string{}
	store := &backupPreparationStoreFake{events: &events, health: profile.HealthMissing, incomplete: []string{"legacy"}}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}

	err := prepareBackupProfiles(store, p, nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("error = %v, want legacy profile warning", err)
	}
	if !containsEvent(events, "launch") {
		t.Fatal("Claude was not relaunched after backup preparation failed")
	}
}

func TestPrepareBackupStillAllowsCompleteProfilesWithoutClaudeInstalled(t *testing.T) {
	events := []string{}
	store := &backupPreparationStoreFake{events: &events}
	p := &fakePlatform{events: &events, appDataErr: errors.New("not installed")}

	if err := prepareBackupProfiles(store, p, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "incomplete"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

type backupPreparationStoreFake struct {
	events        *[]string
	current       string
	matched       string
	health        profile.Health
	incomplete    []string
	checkpointErr error
}

func (s *backupPreparationStoreFake) Current() (string, error) {
	*s.events = append(*s.events, "get-current")
	return s.current, nil
}

func (s *backupPreparationStoreFake) Exists(name string) bool { return name == s.current }

func (s *backupPreparationStoreFake) MatchLiveAt(string) (string, profile.Health) {
	*s.events = append(*s.events, "match")
	return s.matched, s.health
}

func (s *backupPreparationStoreFake) CheckpointAt(name, _, _ string) error {
	*s.events = append(*s.events, "checkpoint:"+name)
	return s.checkpointErr
}

func (s *backupPreparationStoreFake) SetCurrent(name string) error {
	*s.events = append(*s.events, "current:"+name)
	s.current = name
	return nil
}

func (s *backupPreparationStoreFake) IncompleteProfiles() ([]string, error) {
	*s.events = append(*s.events, "incomplete")
	return s.incomplete, nil
}

var _ backupPreparationStore = (*backupPreparationStoreFake)(nil)
