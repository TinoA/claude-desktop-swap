package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type workflowStoreFake struct {
	events        []string
	current       string
	checkpointErr error
	restoreErr    error
}

func (s *workflowStoreFake) Exists(string) bool { return false }
func (s *workflowStoreFake) Current() (string, error) {
	s.events = append(s.events, "current")
	return s.current, nil
}
func (s *workflowStoreFake) CheckpointAt(name, _, _ string) error {
	s.events = append(s.events, "checkpoint:"+name)
	return s.checkpointErr
}
func (s *workflowStoreFake) WipeAt(_, _ string) error {
	s.events = append(s.events, "wipe")
	return nil
}
func (s *workflowStoreFake) RestoreAt(name, _, _ string) error {
	s.events = append(s.events, "restore:"+name)
	return s.restoreErr
}

type workflowPlatformFake struct {
	events  []string
	running bool
}

func (p *workflowPlatformFake) AppDataPath() (string, error) { return `C:\synthetic`, nil }
func (p *workflowPlatformFake) IsRunning() (bool, error) {
	p.events = append(p.events, "is-running")
	return p.running, nil
}
func (p *workflowPlatformFake) KillApp() error {
	p.events = append(p.events, "kill")
	p.running = false
	return nil
}
func (p *workflowPlatformFake) LaunchApp() error {
	p.events = append(p.events, "launch")
	p.running = true
	return nil
}

func newWorkflowTest(store *workflowStoreFake, platform *workflowPlatformFake) *addWorkflow {
	return &addWorkflow{
		store:          store,
		platform:       platform,
		appData:        `C:\synthetic`,
		live:           `C:\synthetic\Network\Cookies`,
		stage:          addIdle,
		sessionUsable:  func(string) bool { return true },
		persistPending: func(pendingAdd) error { return nil },
		removePending:  func() error { return nil },
	}
}

func TestAddWorkflowBeginCompleteAndRelaunchesClaude(t *testing.T) {
	store := &workflowStoreFake{current: "personal"}
	platform := &workflowPlatformFake{running: true}
	w := newWorkflowTest(store, platform)

	if err := w.Begin("work"); err != nil {
		t.Fatal(err)
	}
	if w.Stage() != addWaitingLogin {
		t.Fatalf("stage after Begin = %s", w.Stage())
	}
	if err := w.Complete(); err != nil {
		t.Fatal(err)
	}
	wantStore := []string{"current", "checkpoint:personal", "wipe", "checkpoint:work", "restore:work"}
	wantPlatform := []string{"is-running", "kill", "launch", "kill", "launch"}
	if !reflect.DeepEqual(store.events, wantStore) {
		t.Fatalf("store events = %v, want %v", store.events, wantStore)
	}
	if !reflect.DeepEqual(platform.events, wantPlatform) {
		t.Fatalf("platform events = %v, want %v", platform.events, wantPlatform)
	}
	if w.Stage() != addCompleted || platform.running != true {
		t.Fatalf("stage/running = %s/%v", w.Stage(), platform.running)
	}
}

func TestAddWorkflowCancelRestoresPreviousProfile(t *testing.T) {
	store := &workflowStoreFake{current: "personal"}
	platform := &workflowPlatformFake{running: true}
	w := newWorkflowTest(store, platform)

	if err := w.Begin("work"); err != nil {
		t.Fatal(err)
	}
	if err := w.Cancel(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.events, []string{"current", "checkpoint:personal", "wipe", "restore:personal"}) {
		t.Fatalf("store events = %v", store.events)
	}
	if !reflect.DeepEqual(platform.events, []string{"is-running", "kill", "launch", "is-running", "kill", "launch"}) {
		t.Fatalf("platform events = %v", platform.events)
	}
	if w.Stage() != addCancelled || !platform.running {
		t.Fatalf("stage/running = %s/%v", w.Stage(), platform.running)
	}
}

func TestAddWorkflowFailedNewCheckpointRecoversPreviousProfile(t *testing.T) {
	store := &workflowStoreFake{current: "personal"}
	platform := &workflowPlatformFake{running: true}
	w := newWorkflowTest(store, platform)

	if err := w.Begin("work"); err != nil {
		t.Fatal(err)
	}
	store.checkpointErr = errors.New("login cookie missing")
	if err := w.Complete(); err == nil {
		t.Fatal("Complete should fail when the new session is unusable")
	}
	if !reflect.DeepEqual(store.events, []string{"current", "checkpoint:personal", "wipe", "checkpoint:work", "restore:personal"}) {
		t.Fatalf("store events = %v", store.events)
	}
	if !reflect.DeepEqual(platform.events, []string{"is-running", "kill", "launch", "kill", "is-running", "launch"}) {
		t.Fatalf("platform events = %v", platform.events)
	}
	if w.Stage() != addCancelled || !platform.running {
		t.Fatalf("stage/running = %s/%v", w.Stage(), platform.running)
	}
}

func TestOperationLockRejectsConcurrentOperation(t *testing.T) {
	first, err := acquireOperationLock("test-operation")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if _, err := acquireOperationLock("test-operation"); err == nil {
		t.Fatal("second operation should be rejected")
	}
}

func TestEOFConfirmationCancelsAndRecovers(t *testing.T) {
	store := &workflowStoreFake{current: "personal"}
	platform := &workflowPlatformFake{running: true}
	w := newWorkflowTest(store, platform)
	if err := w.Begin("work"); err != nil {
		t.Fatal(err)
	}
	if err := confirmAddFromInput(bytes.NewReader(nil), w); err == nil {
		t.Fatal("EOF should be reported as cancellation")
	}
	if w.Stage() != addCancelled || !platform.running {
		t.Fatalf("stage/running = %s/%v", w.Stage(), platform.running)
	}
}

func TestAddWorkflowAutoCompletesWhenLoginAppears(t *testing.T) {
	store := &workflowStoreFake{current: "personal"}
	platform := &workflowPlatformFake{running: true}
	w := newWorkflowTest(store, platform)
	if err := w.Begin("work"); err != nil {
		t.Fatal(err)
	}
	var active atomic.Bool
	w.sessionUsable = func(string) bool { return active.Load() }
	w.loginPoll = time.Millisecond
	go func() {
		time.Sleep(5 * time.Millisecond)
		active.Store(true)
	}()
	if err := w.WaitAndComplete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.Stage() != addCompleted || !platform.running {
		t.Fatalf("stage/running = %s/%v", w.Stage(), platform.running)
	}
}
