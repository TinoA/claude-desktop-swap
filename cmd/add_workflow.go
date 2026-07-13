package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
)

type addStage string

const (
	addIdle         addStage = "idle"
	addWaitingLogin addStage = "waiting_for_login"
	addCompleting   addStage = "completing"
	addCompleted    addStage = "completed"
	addCancelled    addStage = "cancelled"
)

var errAddCancelled = errors.New("account add cancelled")
var errAddHandled = errors.New("account-add workflow was already handled")

type addStore interface {
	Exists(string) bool
	Current() (string, error)
	CheckpointAt(string, string, string) error
	WipeAt(string, string) error
	RestoreAt(string, string, string) error
}

type addWorkflow struct {
	store          addStore
	platform       platform.Platform
	appData        string
	live           string
	name           string
	previous       string
	stage          addStage
	lock           *operationLock
	stopped        bool
	sessionUsable  func(string) bool
	persistPending func(pendingAdd) error
	removePending  func() error
	loginPoll      time.Duration
	mu             sync.Mutex
}

func newAddWorkflow(store addStore, p platform.Platform) (*addWorkflow, error) {
	appData, err := p.AppDataPath()
	if err != nil {
		return nil, err
	}
	return &addWorkflow{
		store:          store,
		platform:       p,
		appData:        appData,
		live:           platform.CookiesPath(appData),
		stage:          addIdle,
		sessionUsable:  profile.HasActiveSessionAt,
		persistPending: writePendingAdd,
		removePending:  clearPendingAdd,
		loginPoll:      time.Second,
	}, nil
}

func (w *addWorkflow) Begin(name string) error {
	if w.stage != addIdle {
		return fmt.Errorf("account-add workflow is already %s", w.stage)
	}
	if !validAddProfileName(name) {
		w.finishLock()
		return errors.New("profile name is invalid")
	}
	if w.store.Exists(name) {
		w.finishLock()
		return fmt.Errorf("profile %q already exists", name)
	}
	if w.lock == nil {
		lock, err := acquireOperationLock("operation")
		if err != nil {
			return err
		}
		w.lock = lock
	}
	w.name = name
	if w.previous, _ = w.store.Current(); w.previous == "" {
		w.finishLock()
		return errors.New("the active session has no tracked profile; save it before adding another account")
	}
	if err := w.persistPending(pendingAdd{Name: name, Previous: w.previous, AppData: w.appData, Live: w.live, CreatedAt: time.Now()}); err != nil {
		w.finishLock()
		return err
	}
	if running, err := w.platform.IsRunning(); err != nil {
		return w.failBeforeMutation(err)
	} else if running {
		if err := w.platform.KillApp(); err != nil {
			return w.failBeforeMutation(err)
		}
		w.stopped = true
	}
	if !w.sessionUsable(w.live) {
		return w.recover(errors.New("the current Claude session is not usable"))
	}
	if err := w.store.CheckpointAt(w.previous, w.appData, w.live); err != nil {
		return w.failBeforeMutation(fmt.Errorf("checkpoint current profile: %w", err))
	}
	if err := w.store.WipeAt(w.appData, w.live); err != nil {
		return w.recover(fmt.Errorf("clear session state: %w", err))
	}
	if err := w.platform.LaunchApp(); err != nil {
		return w.recover(fmt.Errorf("launch Claude for login: %w", err))
	}
	w.mu.Lock()
	w.stage = addWaitingLogin
	w.mu.Unlock()
	return nil
}

func validAddProfileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.HasPrefix(name, ".") && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func (w *addWorkflow) Complete() error {
	if err := w.claim(addWaitingLogin, addCompleting); err != nil {
		return err
	}
	if !w.sessionUsable(w.live) {
		return w.recover(errors.New("new Claude session is not ready; log in before finishing"))
	}
	if err := w.platform.KillApp(); err != nil {
		return w.recover(fmt.Errorf("stop Claude after login: %w", err))
	}
	if err := w.store.CheckpointAt(w.name, w.appData, w.live); err != nil {
		return w.recover(fmt.Errorf("save new profile: %w", err))
	}
	if err := w.store.RestoreAt(w.name, w.appData, w.live); err != nil {
		return w.recover(fmt.Errorf("activate new profile: %w", err))
	}
	if err := w.platform.LaunchApp(); err != nil {
		w.stage = addCompleted
		w.finishLock()
		_ = w.removePending()
		return fmt.Errorf("profile saved but Claude could not restart: %w", err)
	}
	w.stage = addCompleted
	w.finishLock()
	return w.removePending()
}

func (w *addWorkflow) Cancel() error {
	if err := w.claim(addWaitingLogin, addCancelled); err != nil {
		return err
	}
	return w.recover(errAddCancelled)
}

// WaitAndComplete watches the live Cookies database and completes as soon as
// Claude has established a usable session. The context lets Ctrl+C or tray
// shutdown recover the previous account safely.
func (w *addWorkflow) WaitAndComplete(ctx context.Context) error {
	if err := w.WaitForLogin(ctx); err != nil {
		return err
	}
	return w.Complete()
}

// WaitForLogin waits until the new live Cookies database contains a usable
// session, without committing it yet. Tray callers use this phase to show the
// final green confirmation overlay during the commit/relaunch phase.
func (w *addWorkflow) WaitForLogin(ctx context.Context) error {
	interval := w.loginPoll
	if interval <= 0 {
		interval = time.Second
	}
	for {
		if !w.waiting() {
			return errAddHandled
		}
		if w.sessionUsable(w.live) {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			_ = w.Cancel()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *addWorkflow) Stage() addStage {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stage
}

func (w *addWorkflow) Name() string { return w.name }

func (w *addWorkflow) waiting() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stage == addWaitingLogin
}

func (w *addWorkflow) claim(expected, next addStage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stage != expected {
		if w.stage == addCompleting || w.stage == addCancelled || w.stage == addCompleted {
			return errAddHandled
		}
		return fmt.Errorf("account-add workflow is %s, not waiting for login", w.stage)
	}
	w.stage = next
	return nil
}

func (w *addWorkflow) failBeforeMutation(err error) error {
	if w.stopped {
		return w.recover(err)
	}
	w.mu.Lock()
	w.stage = addCancelled
	w.mu.Unlock()
	w.finishLock()
	_ = w.removePending()
	return err
}

func newPendingAddWorkflow(store addStore, p platform.Platform) (*addWorkflow, error) {
	pending, err := loadPendingAdd()
	if err != nil {
		return nil, err
	}
	if pending.Name == "" || pending.Previous == "" {
		return nil, errors.New("pending account-add state is incomplete")
	}
	appData, err := p.AppDataPath()
	if err != nil {
		return nil, err
	}
	lock, err := acquireOperationLock("operation")
	if err != nil {
		return nil, err
	}
	return &addWorkflow{
		store:          store,
		platform:       p,
		appData:        appData,
		live:           platform.CookiesPath(appData),
		name:           pending.Name,
		previous:       pending.Previous,
		stage:          addWaitingLogin,
		lock:           lock,
		sessionUsable:  profile.HasActiveSessionAt,
		persistPending: writePendingAdd,
		removePending:  clearPendingAdd,
		loginPoll:      time.Second,
	}, nil
}

func (w *addWorkflow) recover(cause error) error {
	if running, err := w.platform.IsRunning(); err == nil && running {
		_ = w.platform.KillApp()
	}
	restoreErr := w.store.RestoreAt(w.previous, w.appData, w.live)
	launchErr := w.platform.LaunchApp()
	w.mu.Lock()
	w.stage = addCancelled
	w.mu.Unlock()
	w.finishLock()
	_ = w.removePending()
	if restoreErr != nil {
		return fmt.Errorf("%w; restore previous profile: %v", cause, restoreErr)
	}
	if launchErr != nil {
		return fmt.Errorf("%w; restored profile but Claude could not restart: %v", cause, launchErr)
	}
	if errors.Is(cause, errAddCancelled) {
		return nil
	}
	return cause
}

func (w *addWorkflow) finishLock() {
	if w.lock != nil {
		w.lock.Release()
		w.lock = nil
	}
}
