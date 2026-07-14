//go:build windows

package cmd

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FranCalveyra/claude-desktop-swap/internal/platform"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
	"github.com/getlantern/systray"
	"github.com/spf13/cobra"
)

const loginWindowTimeout = 30 * time.Second

//go:embed assets/windows-claude-swap-icon-v2.ico
var trayIcon []byte

var (
	errDeleteSessionUnknown      = errors.New("claude session cannot be verified")
	errDeleteSessionUnrecognized = errors.New("claude session does not match a saved account")
)

var cmdTray = &cobra.Command{
	Use:   "tray",
	Short: "Run Windows Claude Swap in the system tray",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTray()
	},
}

type trayState struct {
	mu              sync.Mutex
	store           *profile.Store
	trayLock        *operationLock
	root            *systray.MenuItem
	add             *systray.MenuItem
	delete          *systray.MenuItem
	finish          *systray.MenuItem
	cancel          *systray.MenuItem
	status          *systray.MenuItem
	export          *systray.MenuItem
	exportPassword  *systray.MenuItem
	exportLocal     *systray.MenuItem
	importer        *systray.MenuItem
	update          *systray.MenuItem
	version         *systray.MenuItem
	items           map[string]*systray.MenuItem
	deleteItems     map[string]*systray.MenuItem
	workflow        *addWorkflow
	claudeInstalled bool
	switching       bool
}

func runTray() error {
	lock, err := acquireOperationLock("tray")
	if err != nil {
		return err
	}
	defer lock.Release()
	store, err := profile.NewStore()
	if err != nil {
		return err
	}
	state := &trayState{store: store, trayLock: lock, items: make(map[string]*systray.MenuItem), deleteItems: make(map[string]*systray.MenuItem)}
	systray.Run(func() { state.ready() }, func() {})
	return nil
}

func (s *trayState) ready() {
	systray.SetIcon(trayIcon)
	systray.SetTitle(ProductName)
	systray.SetTooltip(ProductName + " - Claude Desktop account switcher")

	s.status = systray.AddMenuItem("Status: Ready", "Last operation status")
	s.status.Disable()
	s.root = systray.AddMenuItem("Accounts", "Switch account")
	s.add = systray.AddMenuItem("Add account...", "Open Claude to sign in with another account")
	s.delete = systray.AddMenuItem("Delete account...", "Delete a local Switcher copy")
	s.finish = systray.AddMenuItem("Finish setup", "Save the account after signing in")
	s.finish.Hide()
	s.cancel = systray.AddMenuItem("Cancel and restore previous account", "Cancel setup and restore the previous account")
	s.cancel.Hide()
	systray.AddSeparator()
	s.export = systray.AddMenuItem("Backup", "Save or restore accounts and sessions")
	s.exportPassword = s.export.AddSubMenuItem("Password-protected...", "Create a portable, password-protected backup")
	s.exportLocal = s.export.AddSubMenuItem("Without password...", "Protect the backup with this Windows account")
	s.importer = systray.AddMenuItem("Import backup", "Automatically detect and restore a backup")
	s.update = systray.AddMenuItem("New version available", "Open the latest Windows Claude Swap release")
	s.update.Hide()
	systray.AddSeparator()
	s.version = systray.AddMenuItem("Current version: "+displayVersion(Version), "Installed Windows Claude Swap version")
	s.version.Disable()
	quit := systray.AddMenuItem("Exit", "Close the tray icon")

	s.loadAccounts()
	s.restorePendingIfPresent()

	go s.handleAdd()
	go s.handleBackupExport(s.exportPassword, false)
	go s.handleBackupExport(s.exportLocal, true)
	go s.handleBackupImport()
	go s.handleUpdate()
	go s.handleFinish()
	go s.handleCancel()
	go func() {
		<-quit.ClickedCh
		systray.Quit()
	}()
	go s.detectInitialLive()
	go s.autoRefresh()
	go s.monitorClaudeClose()
	go s.monitorUpdates()
}

func (s *trayState) handleAdd() {
	for range s.add.ClickedCh {
		if s.workflowSnapshot() != nil {
			s.setStatus("An operation is already in progress")
			continue
		}
		name, err := s.promptNewProfileName("Saved account name")
		if err != nil {
			continue
		}
		s.add.Disable()
		workflow, err := newAddWorkflow(s.store, platform.Current())
		preparation := startAddPreparationOverlay()
		if err == nil {
			lock, lockErr := acquireOperationLock("operation")
			if lockErr != nil {
				err = lockErr
			} else {
				workflow.lock = lock
				err = s.prepareCurrentForNewAccount()
			}
		}
		if err == nil {
			err = workflow.Begin(name)
		}
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), loginWindowTimeout)
			err = waitForClaudeLoginWindow(ctx, platform.Current())
			cancel()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					err = errors.New("Claude did not show the sign-in window in time")
				}
				if recoverErr := workflow.Cancel(); recoverErr != nil && !errors.Is(recoverErr, errAddHandled) {
					err = fmt.Errorf("%w; recover previous account: %v", err, recoverErr)
				}
			}
		}
		preparation.Close()
		if err != nil {
			if workflow != nil {
				workflow.finishLock()
			}
			s.add.Enable()
			s.setStatus("Error: " + err.Error())
			continue
		}
		s.setWorkflow(workflow)
		s.finish.Show()
		s.cancel.Show()
		s.setStatus("Waiting for sign-in: " + name)
		s.disableAccounts(true)
		go s.autoComplete(workflow)
	}
}

func (s *trayState) handleBackupExport(item *systray.MenuItem, local bool) {
	for range item.ClickedCh {
		path, err := trayFileDialog(false)
		if err != nil || path == "" {
			continue
		}
		password := ""
		if !local {
			password, err = traySecretPrompt("Backup password", "Enter a password to encrypt all saved accounts")
			if err != nil {
				s.setStatus("Backup cancelled")
				continue
			}
		}
		item.Disable()
		s.exportPassword.Disable()
		s.exportLocal.Disable()
		if local {
			s.setStatus("Protecting backup on this device...")
		} else {
			s.setStatus("Exporting portable backup...")
		}
		go func(path, password string, local bool) {
			lock, lockErr := acquireOperationLock("operation")
			preparation := startBackupPreparationOverlay()
			if lockErr == nil {
				lockErr = prepareBackupProfiles(s.store, platform.Current(), s.resolveBackupProfile, io.Discard)
				if lockErr == nil {
					if local {
						lockErr = s.store.ExportLocal(path)
					} else {
						lockErr = s.store.Export(path, password)
					}
				}
				lock.Release()
			}
			preparation.Close()
			item.Enable()
			s.exportPassword.Enable()
			s.exportLocal.Enable()
			if lockErr != nil {
				s.setStatus("Backup export error: " + lockErr.Error())
				if incomplete, checkErr := s.store.IncompleteProfiles(); checkErr == nil && len(incomplete) > 0 {
					trayWarning("Accounts need attention", "Before creating a backup, open and verify these accounts, then switch to another account to update them:\n\n"+strings.Join(incomplete, ", "))
				}
			} else {
				s.setStatus("Backup exported successfully")
			}
		}(path, password, local)
	}
}

func (s *trayState) handleBackupImport() {
	for range s.importer.ClickedCh {
		path, err := trayFileDialog(true)
		if err != nil || path == "" {
			continue
		}
		protection, err := profile.DetectBackupProtection(path)
		if err != nil {
			s.setStatus("Backup read error: " + err.Error())
			continue
		}
		password := ""
		if protection == profile.BackupProtectionPassword {
			password, err = traySecretPrompt("Backup password", "Enter the password to decrypt the accounts")
			if err != nil {
				s.setStatus("Import cancelled")
				continue
			}
		}
		s.importer.Disable()
		s.setStatus("Importing backup...")
		go func() {
			lock, lockErr := acquireOperationLock("operation")
			if lockErr == nil {
				lockErr = s.store.ImportAuto(path, password)
				lock.Release()
			}
			s.importer.Enable()
			if lockErr != nil {
				s.setStatus("Backup import error: " + lockErr.Error())
			} else {
				s.loadAccounts()
				s.setStatus("Backup imported; choose an account to activate it")
				if incomplete, checkErr := s.store.IncompleteProfiles(); checkErr == nil && len(incomplete) > 0 {
					trayWarning("Older backup imported", "These accounts must be opened and verified once before creating a new complete backup:\n\n"+strings.Join(incomplete, ", "))
				}
			}
		}()
	}
}

func (s *trayState) resolveBackupProfile(current string) (string, error) {
	if current != "" && s.store.Exists(current) {
		choice, err := trayChoice("Confirm active account", "The open session does not exactly match the saved copy of "+current+".\n\nYes: update that account.\nNo: save it as a new account.")
		if err != nil || choice == trayCancel {
			return "", errors.New("backup cancelled")
		}
		if choice == trayYes {
			return current, nil
		}
	}
	return s.promptNewProfileName("Save account before backup")
}

func (s *trayState) handleFinish() {
	for range s.finish.ClickedCh {
		workflow := s.workflowSnapshot()
		if workflow == nil {
			continue
		}
		s.finish.Disable()
		s.cancel.Disable()
		s.setStatus("Saving account: " + workflow.Name())
		go func() {
			err := completeWithSuccessOverlay(workflow)
			if errors.Is(err, errAddHandled) {
				return
			}
			s.clearWorkflow()
			s.finish.Hide()
			s.cancel.Hide()
			s.add.Enable()
			s.disableAccounts(false)
			if err != nil {
				s.setStatus("Error: " + err.Error())
			} else {
				s.setStatus("Active account: " + workflow.Name())
				s.loadAccounts()
			}
		}()
	}
}

func (s *trayState) handleCancel() {
	for range s.cancel.ClickedCh {
		workflow := s.workflowSnapshot()
		if workflow == nil {
			continue
		}
		s.finish.Disable()
		s.cancel.Disable()
		s.setStatus("Restoring previous account...")
		go func() {
			err := workflow.Cancel()
			if errors.Is(err, errAddHandled) {
				return
			}
			s.clearWorkflow()
			s.finish.Hide()
			s.cancel.Hide()
			s.add.Enable()
			s.disableAccounts(false)
			if err != nil {
				s.setStatus("Recovery error: " + err.Error())
			} else {
				s.setStatus("Previous account restored")
				s.loadAccounts()
			}
		}()
	}
}

func (s *trayState) handleUpdate() {
	for range s.update.ClickedCh {
		if err := openLatestRelease(); err != nil {
			s.setStatus("Update opening error: " + err.Error())
		}
	}
}

func (s *trayState) autoRefresh() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if s.workflowSnapshot() == nil && !s.switchingSnapshot() {
			s.loadAccounts()
		}
	}
}

func (s *trayState) monitorClaudeClose() {
	p := platform.Current()
	wasRunning, err := p.IsRunning()
	if err != nil {
		return
	}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		running, err := p.IsRunning()
		if err != nil {
			wasRunning = false
			continue
		}
		if wasRunning && !running {
			s.saveClosedSession(p)
		}
		wasRunning = running
	}
}

func (s *trayState) saveClosedSession(p platform.Platform) {
	if s.workflowSnapshot() != nil || s.switchingSnapshot() {
		return
	}
	appData, err := p.AppDataPath()
	if err != nil {
		return
	}
	current, err := s.store.Current()
	if err != nil || current == "" || !s.store.Exists(current) {
		return
	}
	matched, health := s.store.MatchLiveAt(platform.CookiesPath(appData))
	if health != profile.HealthUsable || matched != current {
		return
	}
	if s.store.IdentityEmailChangedAt(current, platform.CookiesPath(appData)) {
		choice, err := trayChoice(
			"Email updated",
			"This appears to be the same account, but the email changed.\n\nUpdate the profile information?",
		)
		if err != nil || choice != trayYes {
			s.setStatus("Session closed; profile was not updated")
			return
		}
	}
	lock, err := acquireOperationLock("operation")
	if err != nil {
		return
	}
	defer lock.Release()
	if s.workflowSnapshot() != nil || s.switchingSnapshot() {
		return
	}
	running, err := p.IsRunning()
	if err != nil || running {
		return
	}
	if err := saveProfileWith(current, s.store, p, io.Discard); err != nil {
		s.setStatus("Could not save closed session: " + err.Error())
	}
}

func (s *trayState) monitorUpdates() {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	<-timer.C
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		release, err := latestGitHubRelease(ctx)
		cancel()
		if err == nil && updateAvailable(Version, release.TagName) {
			s.update.SetTitle("New version available: " + release.TagName)
			s.update.Show()
		}
		ticker := time.NewTimer(6 * time.Hour)
		<-ticker.C
	}
}

func openLatestRelease() error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", githubReleasePage).Start()
}

func (s *trayState) restorePendingIfPresent() {
	if _, err := loadPendingAdd(); err != nil {
		return
	}
	workflow, err := newPendingAddWorkflow(s.store, platform.Current())
	if err != nil {
		s.setStatus("Pending operation requires manual recovery")
		return
	}
	s.setWorkflow(workflow)
	s.finish.Show()
	s.cancel.Show()
	s.add.Disable()
	s.disableAccounts(true)
	s.setStatus("Pending operation: " + workflow.Name())
	go s.autoComplete(workflow)
}

func (s *trayState) autoComplete(workflow *addWorkflow) {
	err := workflow.WaitForLogin(context.Background())
	if errors.Is(err, errAddHandled) {
		return
	}
	if err == nil {
		err = completeWithSuccessOverlay(workflow)
	}
	if errors.Is(err, errAddHandled) {
		return
	}
	if s.workflowSnapshot() != workflow {
		return
	}
	s.clearWorkflow()
	s.finish.Hide()
	s.cancel.Hide()
	s.add.Enable()
	s.disableAccounts(false)
	if err != nil {
		s.setStatus("Account add error: " + err.Error())
		return
	}
	s.setStatus("Account added: " + workflow.Name())
	s.loadAccounts()
}

func completeWithSuccessOverlay(workflow *addWorkflow) error {
	err := workflow.Complete()
	if err != nil {
		return err
	}
	success := startAddSuccessOverlay()
	defer success.Close()
	time.Sleep(1500 * time.Millisecond)
	return nil
}

func (s *trayState) detectInitialLive() {
	if s.workflowSnapshot() != nil {
		return
	}
	if !platform.Installed() {
		s.setStatus("Claude Desktop is not installed or was not detected")
		return
	}
	p := platform.Current()
	appData, err := p.AppDataPath()
	if err != nil {
		return
	}
	live := platform.CookiesPath(appData)
	running, err := p.IsRunning()
	if err != nil {
		s.setStatus("Could not verify Claude Desktop: " + err.Error())
		return
	}
	inspection := profile.InspectCookies(live, time.Now())
	hasSession := inspection.Health == profile.HealthUsable
	if inspection.Health == profile.HealthUnknown && running {
		_, digestErr := profile.SessionDigest(live)
		hasSession = digestErr == nil
		if digestErr != nil {
			s.setStatus("Claude Desktop detected, but its session could not be verified")
			return
		}
	}
	matched, _ := s.store.MatchLiveAt(live)
	profiles, _ := s.store.List()
	current, _ := s.store.Current()
	if matched != "" {
		if current != matched {
			if err := s.store.SetCurrent(matched); err != nil {
				s.setStatus("Could not register the active account: " + err.Error())
				return
			}
			s.loadAccounts()
		}
		return
	}
	if !hasSession {
		s.setStatus("Claude Desktop detected without a signed-in account")
		return
	}
	if current != "" && s.store.Exists(current) && len(profiles) > 0 {
		choice, choiceErr := trayChoice("Unrecognized session", "Claude has an active session that does not match the saved copy of "+current+". Update that copy with the current session? Yes=update, No=keep it unchanged.")
		if choiceErr != nil || choice != trayYes {
			if choiceErr == nil && choice == trayNo {
				s.setStatus("Unrecognized active session; saved account was not changed")
			}
			return
		}
		lock, lockErr := acquireOperationLock("operation")
		if lockErr != nil {
			s.setStatus("Could not update account: " + lockErr.Error())
			return
		}
		defer lock.Release()
		var output bytes.Buffer
		if err := saveProfileWith(current, s.store, p, io.Writer(&output)); err != nil {
			s.setStatus("Account update error: " + err.Error())
			return
		}
		s.loadAccounts()
		s.setStatus("Account updated: " + current)
		return
	}
	choice, err := trayChoice("Account detected", "Claude already has a signed-in account that is not saved in the Switcher. Save it now? Yes=save, No=keep it unchanged.")
	if err != nil || choice != trayYes {
		if err == nil && choice == trayNo {
			s.setStatus("Account detected but not saved")
		}
		return
	}
	name, err := s.promptNewProfileName("Save detected account")
	if err != nil {
		return
	}
	lock, lockErr := acquireOperationLock("operation")
	if lockErr != nil {
		s.setStatus("Could not save detected account: " + lockErr.Error())
		return
	}
	defer lock.Release()
	if err := s.saveDetectedProfile(name); err != nil {
		s.setStatus("Detected account save error: " + err.Error())
		return
	}
	s.loadAccounts()
	s.setStatus("Account detected and saved: " + name)
}

func (s *trayState) promptNewProfileName(title string) (string, error) {
	for {
		name, err := trayPrompt(title)
		if err != nil {
			return "", err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			trayWarning("Name required", "The name cannot be empty.\n\nEnter a name to continue.")
			continue
		}
		if !validAddProfileName(name) {
			trayWarning("Invalid name", "Use a simple name without slashes or paths.")
			continue
		}
		if s.store.Exists(name) {
			trayWarning("Name already in use", "A saved account already uses that name.\n\nChoose another name.")
			continue
		}
		return name, nil
	}
}

func (s *trayState) prepareCurrentForNewAccount() error {
	appData, err := platform.Current().AppDataPath()
	if err != nil {
		return err
	}
	live := platform.CookiesPath(appData)
	running, _ := platform.Current().IsRunning()
	if !profile.HasActiveSessionAt(live) && !running {
		return errors.New("Claude has no active session to protect")
	}
	matched, _ := s.store.MatchLiveAt(live)
	if matched != "" {
		if current, _ := s.store.Current(); current != matched {
			return s.store.SetCurrent(matched)
		}
		return nil
	}
	if current, _ := s.store.Current(); current != "" && s.store.Exists(current) {
		return nil
	}
	if err := s.saveDetectedProfile(s.nextAutomaticProfileName()); err != nil {
		return err
	}
	s.loadAccounts()
	return nil
}

func (s *trayState) nextAutomaticProfileName() string {
	base := "current-account"
	name := base
	for index := 2; s.store.Exists(name); index++ {
		name = base + "-" + strconv.Itoa(index)
	}
	return name
}

func (s *trayState) saveDetectedProfile(name string) error {
	if s.store.Exists(name) {
		return errors.New("that profile name already exists")
	}
	var output bytes.Buffer
	return saveProfileWith(name, s.store, platform.Current(), io.Writer(&output))
}

func (s *trayState) loadAccounts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	switching := s.switching
	profiles, err := s.store.List()
	if err != nil {
		s.setStatus("Account read error: " + err.Error())
		return
	}
	s.claudeInstalled = platform.Installed()
	if s.claudeInstalled && !switching {
		s.add.Enable()
	} else {
		s.add.Disable()
	}
	if len(profiles) == 0 {
		s.root.Hide()
		s.delete.Hide()
		s.delete.Disable()
	} else {
		s.root.Show()
		s.delete.Show()
		if switching {
			s.delete.Disable()
		} else {
			s.delete.Enable()
		}
	}
	current, _ := s.store.Current()
	seen := make(map[string]bool, len(profiles))
	for _, meta := range profiles {
		seen[meta.Name] = true
		item, ok := s.items[meta.Name]
		if !ok {
			name := meta.Name
			item = s.root.AddSubMenuItem(name, "Switch to "+name)
			s.items[name] = item
			go s.watchAccount(item, name)
		}
		deleteItem, deleteOK := s.deleteItems[meta.Name]
		if !deleteOK {
			deleteItem = s.delete.AddSubMenuItem(meta.Name, "Delete "+meta.Name)
			s.deleteItems[meta.Name] = deleteItem
			go s.watchDeleteAccount(deleteItem, meta.Name)
		}
		if strings.TrimSpace(current) == meta.Name {
			item.SetTitle("✓ " + meta.Name)
		} else {
			item.SetTitle(meta.Name)
		}
		if s.claudeInstalled && !switching {
			item.Enable()
		} else {
			item.Disable()
		}
		item.Show()
		deleteItem.SetTitle(meta.Name)
		deleteItem.Show()
		deleteItem.Enable()
	}
	for name, item := range s.items {
		if !seen[name] {
			item.Hide()
			item.Disable()
		}
	}
	for name, item := range s.deleteItems {
		if !seen[name] {
			item.Hide()
			item.Disable()
		}
	}
	if !s.claudeInstalled {
		s.setStatus("Claude Desktop is not installed or was not detected")
	}
}

func (s *trayState) watchAccount(item *systray.MenuItem, name string) {
	for range item.ClickedCh {
		if !s.beginSwitch() {
			s.setStatus("Finish or cancel setup before switching")
			continue
		}
		s.setStatus("Switching to " + name + "...")
		go func() {
			err := switchProfileFromTray(name, s.store)
			s.endSwitch()
			if err != nil {
				if strings.Contains(err.Error(), "account switch cancelled") {
					s.setStatus("Switch cancelled; previous session was preserved")
					return
				}
				s.setStatus("Account switch error: " + err.Error())
				return
			}
			s.setStatus("Active account: " + name)
			s.loadAccounts()
		}()
	}
}

func switchProfileFromTray(name string, store *profile.Store) error {
	lock, err := acquireOperationLock("operation")
	if err != nil {
		return err
	}
	defer lock.Release()
	overlay := startSwitchOverlay()
	defer overlay.Close()
	err = switchProfileWith(name, store, platform.Current(), io.Discard, confirmSessionUpdate)
	if err == nil {
		if pending, pendingErr := loadPendingAdd(); pendingErr == nil && pending.Previous == name {
			_ = clearPendingAdd()
		}
	}
	return err
}

func confirmSessionUpdate(current, target string) bool {
	choice, err := trayChoice(
		"Different account detected",
		"The open account does not match \""+current+"\".\n\nSave it as \""+current+"\" and switch to \""+target+"\"?\n\nYes: save and switch.\nNo or Cancel: make no changes.",
	)
	return err == nil && choice == trayYes
}

func (s *trayState) watchDeleteAccount(item *systray.MenuItem, name string) {
	for range item.ClickedCh {
		if s.switchingSnapshot() {
			s.setStatus("Wait for the account switch to finish")
			continue
		}
		if s.workflowSnapshot() != nil {
			s.setStatus("Finish or cancel setup before deleting an account")
			continue
		}
		active, liveVerified, activeErr := s.deleteAccountIsActive(name)
		if activeErr != nil {
			message := s.deleteVerificationMessage(name, activeErr)
			trayWarning("Unable to verify active account", message)
			s.setStatus(name + " was not deleted: active account could not be verified")
			continue
		}
		profiles, profilesErr := s.store.List()
		onlyActive := active && profilesErr == nil && len(profiles) == 1
		if active && !onlyActive {
			message := "\"" + name + "\" was not deleted because it is marked as the active account.\n\nSwitch to another account and try again."
			if liveVerified {
				message = "\"" + name + "\" was not deleted because it is currently open in Claude Desktop.\n\nSwitch to another account and try again."
			}
			trayWarning("The active account cannot be deleted", message)
			s.setStatus(name + " was not deleted: it is the active account")
			continue
		}
		confirmed, err := trayDeleteConfirm(name, onlyActive)
		if err != nil || !confirmed {
			continue
		}
		item.Disable()
		s.delete.Disable()
		s.setStatus("Deleting account: " + name)
		go func() {
			lock, lockErr := acquireOperationLock("operation")
			if lockErr == nil {
				lockErr = s.store.Delete(name)
				lock.Release()
			}
			if lockErr != nil {
				item.Enable()
				s.delete.Enable()
				s.setStatus("Account deletion error: " + lockErr.Error())
				return
			}
			s.removeDeletedAccount(name)
			s.loadAccounts()
			s.setStatus("Account deleted: " + name)
		}()
	}
}

func (s *trayState) removeDeletedAccount(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.items[name]; ok {
		item.Hide()
		item.Disable()
		delete(s.items, name)
	}
	if item, ok := s.deleteItems[name]; ok {
		item.Hide()
		item.Disable()
		delete(s.deleteItems, name)
	}
}

func (s *trayState) deleteAccountIsActive(name string) (bool, bool, error) {
	current, _ := s.store.Current()
	p := platform.Current()
	appData, err := p.AppDataPath()
	if err != nil {
		return current == name, false, nil
	}
	if _, err := p.IsRunning(); err != nil {
		if current != "" {
			return current == name, false, nil
		}
		return false, false, fmt.Errorf("claude no puede verificarse: %w", err)
	}
	liveName, liveHealth := s.store.MatchLiveAt(platform.CookiesPath(appData))
	return resolveDeleteActivity(name, current, liveName, liveHealth)
}

func resolveDeleteActivity(name, current, liveName string, liveHealth profile.Health) (bool, bool, error) {
	if liveHealth == profile.HealthUnknown {
		if current != "" {
			return current == name, false, nil
		}
		return false, false, errDeleteSessionUnknown
	}
	if liveHealth == profile.HealthUsable {
		if liveName != "" {
			return liveName == name, true, nil
		}
		if current == name {
			return false, false, errDeleteSessionUnrecognized
		}
	}
	return current == name, false, nil
}

func (s *trayState) deleteVerificationMessage(name string, err error) string {
	current, _ := s.store.Current()
	if errors.Is(err, errDeleteSessionUnknown) {
		if current != "" {
			return "Windows could not verify the current Claude session.\n\n\"" + current + "\" is marked as the active account. \"" + name + "\" was not deleted.\n\nClose Claude Desktop or try again."
		}
		return "Windows could not verify the current Claude session.\n\n\"" + name + "\" was not deleted. Close Claude Desktop or try again."
	}
	if errors.Is(err, errDeleteSessionUnrecognized) {
		return "Claude has an open session that does not match a saved account.\n\n\"" + name + "\" was not deleted to protect your accounts. Save or update the current session first."
	}
	return "The active account could not be verified.\n\n\"" + name + "\" was not deleted.\n\n" + err.Error()
}

func (s *trayState) setWorkflow(workflow *addWorkflow) {
	s.mu.Lock()
	s.workflow = workflow
	s.mu.Unlock()
}

func (s *trayState) beginSwitch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.switching || s.workflow != nil {
		return false
	}
	s.switching = true
	for _, item := range s.items {
		item.Disable()
	}
	s.add.Disable()
	s.delete.Disable()
	return true
}

func (s *trayState) endSwitch() {
	s.mu.Lock()
	s.switching = false
	if s.claudeInstalled {
		for _, item := range s.items {
			item.Enable()
		}
		s.add.Enable()
	}
	if len(s.items) > 0 {
		s.delete.Enable()
	}
	s.mu.Unlock()
}

func (s *trayState) switchingSnapshot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switching
}

func (s *trayState) clearWorkflow() {
	s.mu.Lock()
	s.workflow = nil
	s.mu.Unlock()
}

func (s *trayState) workflowSnapshot() *addWorkflow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workflow
}

func (s *trayState) disableAccounts(disabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if disabled || s.switching || !s.claudeInstalled {
			item.Disable()
		} else {
			item.Enable()
		}
	}
}

func (s *trayState) setStatus(value string) {
	if len(value) > 180 {
		value = value[:180]
	}
	s.status.SetTitle(value)
}

type trayChoiceValue string

const (
	trayYes    trayChoiceValue = "Yes"
	trayNo     trayChoiceValue = "No"
	trayCancel trayChoiceValue = "Cancel"
)

func trayChoice(title, message string) (trayChoiceValue, error) {
	return nativeTrayChoice(title, message)
}

func trayFileDialog(open bool) (string, error) {
	return nativeTrayFileDialog(open)
}

func traySecretPrompt(title, message string) (string, error) {
	return nativeTraySecretPrompt(title, message)
}
