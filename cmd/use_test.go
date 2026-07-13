package cmd

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
)

func TestSwitchProfileOrdersStopCheckpointRestoreAndLaunch(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "inspect:incoming", "current", "stop", "checkpoint:outgoing", "restore:incoming", "launch"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if store.current != "incoming" {
		t.Fatalf("current = %q", store.current)
	}
}

func TestSwitchProfileStopsAfterCheckpointOrRestoreFailure(t *testing.T) {
	tests := []struct {
		name                      string
		checkpointErr, restoreErr error
		forbidden                 string
	}{
		{"checkpoint failure", errors.New("checkpoint failed"), nil, "restore:incoming"},
		{"restore interruption", nil, errors.New("restore failed"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []string{}
			store := &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable, checkpointErr: tt.checkpointErr, restoreErr: tt.restoreErr}
			p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
			if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err == nil {
				t.Fatal("switch should fail")
			}
			if tt.forbidden != "" && strings.Contains(strings.Join(events, ","), tt.forbidden) {
				t.Fatalf("forbidden event %q in %v", tt.forbidden, events)
			}
			if store.current != "outgoing" {
				t.Fatalf("tracking advanced to %q", store.current)
			}
		})
	}
}

func TestSwitchProfileRefusesUnusableTargetBeforeStopping(t *testing.T) {
	for _, health := range []profile.Health{profile.HealthExpired, profile.HealthMissing, profile.HealthUnknown} {
		t.Run(string(health), func(t *testing.T) {
			events := []string{}
			store := &fakeSwitchStore{events: &events, exists: true, health: health}
			p := &fakePlatform{events: &events, appData: t.TempDir()}
			if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "reauthentication") {
				t.Fatalf("error = %v", err)
			}
			if containsEvent(events, "stop") {
				t.Fatalf("stopped app for unusable target: %v", events)
			}
		})
	}
}

func TestSwitchProfileLaunchFailureKeepsCommittedIncomingState(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true, launchErr: errors.New("launch failed")}
	err := switchProfileWith("incoming", store, p, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "launch manually") {
		t.Fatalf("error = %v", err)
	}
	if store.current != "incoming" {
		t.Fatalf("committed current = %q", store.current)
	}
}

func TestSwitchProfileRestoresCurrentProfileWhenLiveCookiesAreMissing(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events, exists: true, current: "incoming", health: profile.HealthUsable}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "inspect:incoming", "current", "stop", "restore:incoming", "launch"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSwitchProfileCheckpointsWhenClaudeIsClosed(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable}
	p := &fakePlatform{events: &events, appData: t.TempDir()}
	if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "inspect:incoming", "current", "checkpoint:outgoing", "restore:incoming", "launch"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSwitchProfileSkipsRestoreForAlreadyActiveProfile(t *testing.T) {
	events := []string{}
	store := &fakeLiveSwitchStore{
		fakeSwitchStore: &fakeSwitchStore{events: &events, exists: true, current: "incoming", health: profile.HealthUsable},
		liveName:        "incoming",
		liveHealth:      profile.HealthUsable,
	}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "inspect:incoming", "current", "match"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSwitchProfileRefusesUnknownLiveSessionAfterStopping(t *testing.T) {
	events := []string{}
	store := &fakeLiveSwitchStore{
		fakeSwitchStore: &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable},
		liveHealth:      profile.HealthUnknown,
	}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	err := switchProfileWith("incoming", store, p, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cannot be verified") {
		t.Fatalf("error = %v", err)
	}
	if containsEvent(events, "checkpoint:outgoing") || containsEvent(events, "restore:incoming") {
		t.Fatalf("unknown live session was modified: %v", events)
	}
	if !containsEvent(events, "launch") {
		t.Fatalf("previous Claude session was not relaunched: %v", events)
	}
}

func TestSwitchProfileDoesNotCheckpointMissingLiveSession(t *testing.T) {
	events := []string{}
	store := &fakeLiveSwitchStore{
		fakeSwitchStore: &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable},
		liveHealth:      profile.HealthMissing,
	}
	p := &fakePlatform{events: &events, appData: t.TempDir()}
	if err := switchProfileWith("incoming", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if containsEvent(events, "checkpoint:outgoing") {
		t.Fatalf("missing live session was checkpointed: %v", events)
	}
	if !containsEvent(events, "restore:incoming") {
		t.Fatalf("incoming profile was not restored: %v", events)
	}
}

func TestSwitchProfileUpdatesConfirmedUnrecognizedSessionBeforeSwitch(t *testing.T) {
	events := []string{}
	store := &fakeLiveSwitchStore{
		fakeSwitchStore: &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable},
		liveHealth:      profile.HealthUsable,
	}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	confirmed := false
	err := switchProfileWith("incoming", store, p, &bytes.Buffer{}, func(current, target string) bool {
		confirmed = current == "outgoing" && target == "incoming"
		return confirmed
	})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("unrecognized session update was not confirmed")
	}
	if !containsEvent(events, "checkpoint:outgoing") || !containsEvent(events, "restore:incoming") {
		t.Fatalf("confirmed session was not preserved before switch: %v", events)
	}
}

func TestSwitchProfilePreservesUnrecognizedSessionWhenConfirmationIsDeclined(t *testing.T) {
	events := []string{}
	store := &fakeLiveSwitchStore{
		fakeSwitchStore: &fakeSwitchStore{events: &events, exists: true, current: "outgoing", health: profile.HealthUsable},
		liveHealth:      profile.HealthUsable,
	}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	err := switchProfileWith("incoming", store, p, &bytes.Buffer{}, func(_, _ string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error = %v", err)
	}
	if containsEvent(events, "checkpoint:outgoing") || containsEvent(events, "restore:incoming") {
		t.Fatalf("declined session was modified: %v", events)
	}
	if !containsEvent(events, "launch") {
		t.Fatalf("previous Claude session was not relaunched: %v", events)
	}
}

type fakeSwitchStore struct {
	events        *([]string)
	exists        bool
	current       string
	health        profile.Health
	checkpointErr error
	restoreErr    error
}

type fakeLiveSwitchStore struct {
	*fakeSwitchStore
	liveName   string
	liveHealth profile.Health
}

func (s *fakeLiveSwitchStore) MatchLiveAt(string) (string, profile.Health) {
	*s.events = append(*s.events, "match")
	return s.liveName, s.liveHealth
}

func (s *fakeSwitchStore) Exists(string) bool { return s.exists }
func (s *fakeSwitchStore) Inspect(name string) profile.Inspection {
	*s.events = append(*s.events, "inspect:"+name)
	return profile.Inspection{Health: s.health}
}
func (s *fakeSwitchStore) Current() (string, error) {
	*s.events = append(*s.events, "current")
	return s.current, nil
}
func (s *fakeSwitchStore) Checkpoint(name, path string) error {
	*s.events = append(*s.events, "checkpoint:"+name)
	return s.checkpointErr
}
func (s *fakeSwitchStore) Restore(name, path string) error {
	*s.events = append(*s.events, "restore:"+name)
	if s.restoreErr == nil {
		s.current = name
	}
	return s.restoreErr
}

type fakePlatform struct {
	events     *([]string)
	appData    string
	appDataErr error
	running    bool
	launchErr  error
}

func (p *fakePlatform) AppDataPath() (string, error) {
	*p.events = append(*p.events, "app-data")
	return p.appData, p.appDataErr
}
func (p *fakePlatform) IsRunning() (bool, error) { return p.running, nil }
func (p *fakePlatform) IsInstalled() bool        { return p.appDataErr == nil }
func (p *fakePlatform) KillApp() error           { *p.events = append(*p.events, "stop"); return nil }
func (p *fakePlatform) LaunchApp() error         { *p.events = append(*p.events, "launch"); return p.launchErr }

func containsEvent(events []string, event string) bool {
	for _, got := range events {
		if got == event {
			return true
		}
	}
	return false
}
