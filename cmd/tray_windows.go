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

	s.status = systray.AddMenuItem("Estado: listo", "Estado de la última operación")
	s.status.Disable()
	s.root = systray.AddMenuItem("Cuentas", "Cambiar de cuenta")
	s.add = systray.AddMenuItem("Agregar cuenta...", "Abrir Claude para iniciar sesión con otra cuenta")
	s.delete = systray.AddMenuItem("Eliminar cuenta...", "Eliminar una copia local del switcher")
	s.finish = systray.AddMenuItem("Finalizar registro", "Guardar la cuenta después de iniciar sesión")
	s.finish.Hide()
	s.cancel = systray.AddMenuItem("Cancelar y restaurar cuenta anterior", "Cancelar el alta y recuperar la cuenta anterior")
	s.cancel.Hide()
	systray.AddSeparator()
	s.export = systray.AddMenuItem("Backup", "Guardar o restaurar cuentas y sesiones")
	s.exportPassword = s.export.AddSubMenuItem("Con contraseña...", "Crear un backup portable cifrado con contraseña")
	s.exportLocal = s.export.AddSubMenuItem("Sin contraseña...", "Proteger el backup con este usuario de Windows")
	s.importer = systray.AddMenuItem("Importar backup", "Detectar automáticamente y restaurar un backup")
	s.update = systray.AddMenuItem("Nueva versión disponible", "Abrir la última versión de Windows Claude Swap")
	s.update.Hide()
	systray.AddSeparator()
	s.version = systray.AddMenuItem("Versión actual: "+displayVersion(Version), "Versión instalada de Windows Claude Swap")
	s.version.Disable()
	quit := systray.AddMenuItem("Salir", "Cerrar el icono de bandeja")

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
			s.setStatus("Ya existe una operación activa")
			continue
		}
		name, err := s.promptNewProfileName("Nombre de la cuenta guardada")
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
					err = errors.New("claude no mostró la ventana de inicio de sesión a tiempo")
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
		s.setStatus("Esperando login: " + name)
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
			password, err = traySecretPrompt("Contraseña del backup", "Escribe una contraseña para cifrar todas las cuentas guardadas")
			if err != nil {
				s.setStatus("Backup cancelado")
				continue
			}
		}
		item.Disable()
		s.exportPassword.Disable()
		s.exportLocal.Disable()
		if local {
			s.setStatus("Protegiendo backup en este equipo...")
		} else {
			s.setStatus("Exportando backup portable...")
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
				s.setStatus("Error exportando backup: " + lockErr.Error())
				if incomplete, checkErr := s.store.IncompleteProfiles(); checkErr == nil && len(incomplete) > 0 {
					trayWarning("Cuentas pendientes", "Antes del backup abre y verifica estas cuentas, luego cambia a otra para actualizarlas:\n\n"+strings.Join(incomplete, ", "))
				}
			} else {
				s.setStatus("Backup exportado correctamente")
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
			s.setStatus("Error leyendo backup: " + err.Error())
			continue
		}
		password := ""
		if protection == profile.BackupProtectionPassword {
			password, err = traySecretPrompt("Contraseña del backup", "Escribe la contraseña para descifrar las cuentas")
			if err != nil {
				s.setStatus("Importación cancelada")
				continue
			}
		}
		s.importer.Disable()
		s.setStatus("Importando backup...")
		go func() {
			lock, lockErr := acquireOperationLock("operation")
			if lockErr == nil {
				lockErr = s.store.ImportAuto(path, password)
				lock.Release()
			}
			s.importer.Enable()
			if lockErr != nil {
				s.setStatus("Error importando backup: " + lockErr.Error())
			} else {
				s.loadAccounts()
				s.setStatus("Backup importado; elige una cuenta para activarla")
				if incomplete, checkErr := s.store.IncompleteProfiles(); checkErr == nil && len(incomplete) > 0 {
					trayWarning("Backup antiguo importado", "Estas cuentas necesitan abrirse y verificarse una vez antes de crear un nuevo backup completo:\n\n"+strings.Join(incomplete, ", "))
				}
			}
		}()
	}
}

func (s *trayState) resolveBackupProfile(current string) (string, error) {
	if current != "" && s.store.Exists(current) {
		choice, err := trayChoice("Confirmar cuenta activa", "La sesión abierta no coincide exactamente con la copia de "+current+".\n\nSí: actualizar esa cuenta.\nNo: guardarla como una cuenta nueva.")
		if err != nil || choice == trayCancel {
			return "", errors.New("backup cancelado")
		}
		if choice == trayYes {
			return current, nil
		}
	}
	return s.promptNewProfileName("Guardar cuenta antes del backup")
}

func (s *trayState) handleFinish() {
	for range s.finish.ClickedCh {
		workflow := s.workflowSnapshot()
		if workflow == nil {
			continue
		}
		s.finish.Disable()
		s.cancel.Disable()
		s.setStatus("Guardando cuenta: " + workflow.Name())
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
				s.setStatus("Cuenta activa: " + workflow.Name())
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
		s.setStatus("Restaurando cuenta anterior...")
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
				s.setStatus("Error de recuperación: " + err.Error())
			} else {
				s.setStatus("Cuenta anterior restaurada")
				s.loadAccounts()
			}
		}()
	}
}

func (s *trayState) handleUpdate() {
	for range s.update.ClickedCh {
		if err := openLatestRelease(); err != nil {
			s.setStatus("Error abriendo actualización: " + err.Error())
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
		s.setStatus("No se pudo guardar la sesión cerrada: " + err.Error())
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
			s.update.SetTitle("Nueva versión disponible: " + release.TagName)
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
		s.setStatus("Operación pendiente requiere recuperación manual")
		return
	}
	s.setWorkflow(workflow)
	s.finish.Show()
	s.cancel.Show()
	s.add.Disable()
	s.disableAccounts(true)
	s.setStatus("Operación pendiente: " + workflow.Name())
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
		s.setStatus("Error agregando cuenta: " + err.Error())
		return
	}
	s.setStatus("Cuenta agregada: " + workflow.Name())
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
		s.setStatus("Claude Desktop no está instalado o no fue detectado")
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
		s.setStatus("No se pudo comprobar Claude Desktop: " + err.Error())
		return
	}
	inspection := profile.InspectCookies(live, time.Now())
	hasSession := inspection.Health == profile.HealthUsable
	if inspection.Health == profile.HealthUnknown && running {
		_, digestErr := profile.SessionDigest(live)
		hasSession = digestErr == nil
		if digestErr != nil {
			s.setStatus("Claude Desktop detectado, pero no se pudo verificar su sesión")
			return
		}
	}
	matched, _ := s.store.MatchLiveAt(live)
	profiles, _ := s.store.List()
	current, _ := s.store.Current()
	if matched != "" {
		if current != matched {
			if err := s.store.SetCurrent(matched); err != nil {
				s.setStatus("No se pudo registrar la cuenta activa: " + err.Error())
				return
			}
			s.loadAccounts()
		}
		return
	}
	if !hasSession {
		s.setStatus("Claude Desktop detectado sin una cuenta iniciada")
		return
	}
	if current != "" && s.store.Exists(current) && len(profiles) > 0 {
		choice, choiceErr := trayChoice("Sesión no reconocida", "Claude tiene una sesión activa que no coincide con la copia guardada de "+current+". ¿Quieres actualizar esa copia con la sesión actual? Sí=actualizar, No=dejarla intacta.")
		if choiceErr != nil || choice != trayYes {
			if choiceErr == nil && choice == trayNo {
				s.setStatus("Sesión activa no reconocida; no se modificó la cuenta guardada")
			}
			return
		}
		lock, lockErr := acquireOperationLock("operation")
		if lockErr != nil {
			s.setStatus("No se pudo actualizar cuenta: " + lockErr.Error())
			return
		}
		defer lock.Release()
		var output bytes.Buffer
		if err := saveProfileWith(current, s.store, p, io.Writer(&output)); err != nil {
			s.setStatus("Error actualizando cuenta: " + err.Error())
			return
		}
		s.loadAccounts()
		s.setStatus("Cuenta actualizada: " + current)
		return
	}
	choice, err := trayChoice("Cuenta detectada", "Claude ya tiene una cuenta iniciada que no está guardada en el switcher. ¿Quieres guardarla ahora? Sí=guardar, No=dejarla intacta.")
	if err != nil || choice != trayYes {
		if err == nil && choice == trayNo {
			s.setStatus("Cuenta detectada pero no guardada")
		}
		return
	}
	name, err := s.promptNewProfileName("Guardar cuenta detectada")
	if err != nil {
		return
	}
	lock, lockErr := acquireOperationLock("operation")
	if lockErr != nil {
		s.setStatus("No se pudo guardar cuenta detectada: " + lockErr.Error())
		return
	}
	defer lock.Release()
	if err := s.saveDetectedProfile(name); err != nil {
		s.setStatus("Error guardando cuenta detectada: " + err.Error())
		return
	}
	s.loadAccounts()
	s.setStatus("Cuenta detectada y guardada: " + name)
}

func (s *trayState) promptNewProfileName(title string) (string, error) {
	for {
		name, err := trayPrompt(title)
		if err != nil {
			return "", err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			trayWarning("Nombre obligatorio", "No puedes dejar el nombre vacío.\n\nEscribe un nombre para continuar.")
			continue
		}
		if !validAddProfileName(name) {
			trayWarning("Nombre no válido", "Usa un nombre sencillo sin barras ni rutas.")
			continue
		}
		if s.store.Exists(name) {
			trayWarning("Nombre ya utilizado", "Ya existe una cuenta guardada con ese nombre.\n\nElige otro nombre.")
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
		return errors.New("claude no tiene una sesión activa para proteger")
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
	base := "cuenta-actual"
	name := base
	for index := 2; s.store.Exists(name); index++ {
		name = base + "-" + strconv.Itoa(index)
	}
	return name
}

func (s *trayState) saveDetectedProfile(name string) error {
	if s.store.Exists(name) {
		return errors.New("ese nombre de perfil ya existe")
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
		s.setStatus("Error leyendo cuentas: " + err.Error())
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
			item = s.root.AddSubMenuItem(name, "Cambiar a "+name)
			s.items[name] = item
			go s.watchAccount(item, name)
		}
		deleteItem, deleteOK := s.deleteItems[meta.Name]
		if !deleteOK {
			deleteItem = s.delete.AddSubMenuItem(meta.Name, "Eliminar "+meta.Name)
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
		s.setStatus("Claude Desktop no está instalado o no fue detectado")
	}
}

func (s *trayState) watchAccount(item *systray.MenuItem, name string) {
	for range item.ClickedCh {
		if !s.beginSwitch() {
			s.setStatus("Termina o cancela el registro antes de cambiar")
			continue
		}
		s.setStatus("Cambiando a " + name + "...")
		go func() {
			err := switchProfileFromTray(name, s.store)
			s.endSwitch()
			if err != nil {
				if strings.Contains(err.Error(), "account switch cancelled") {
					s.setStatus("Cambio cancelado; la sesión anterior se conservó")
					return
				}
				s.setStatus("Error cambiando cuenta: " + err.Error())
				return
			}
			s.setStatus("Cuenta activa: " + name)
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
		"Cuenta diferente detectada",
		"La cuenta abierta no coincide con \""+current+"\".\n\n¿Quieres guardarla como \""+current+"\" y cambiar a \""+target+"\"?\n\nSí: guardar y cambiar.\nNo o Cancelar: no modificar nada.",
	)
	return err == nil && choice == trayYes
}

func (s *trayState) watchDeleteAccount(item *systray.MenuItem, name string) {
	for range item.ClickedCh {
		if s.switchingSnapshot() {
			s.setStatus("Espera a que termine el cambio de cuenta")
			continue
		}
		if s.workflowSnapshot() != nil {
			s.setStatus("Termina o cancela el registro antes de eliminar una cuenta")
			continue
		}
		active, liveVerified, activeErr := s.deleteAccountIsActive(name)
		if activeErr != nil {
			message := s.deleteVerificationMessage(name, activeErr)
			trayWarning("No se puede comprobar la cuenta activa", message)
			s.setStatus("No se eliminó " + name + ": no se pudo verificar la cuenta activa")
			continue
		}
		profiles, profilesErr := s.store.List()
		onlyActive := active && profilesErr == nil && len(profiles) == 1
		if active && !onlyActive {
			message := "No se eliminó \"" + name + "\" porque está marcada como la cuenta activa.\n\nCambia primero a otra cuenta y vuelve a intentarlo."
			if liveVerified {
				message = "No se eliminó \"" + name + "\" porque es la cuenta que está abierta actualmente en Claude Desktop.\n\nCambia primero a otra cuenta y vuelve a intentarlo."
			}
			trayWarning("No se puede eliminar la cuenta activa", message)
			s.setStatus("No se eliminó " + name + ": es la cuenta activa")
			continue
		}
		confirmed, err := trayDeleteConfirm(name, onlyActive)
		if err != nil || !confirmed {
			continue
		}
		item.Disable()
		s.delete.Disable()
		s.setStatus("Eliminando cuenta: " + name)
		go func() {
			lock, lockErr := acquireOperationLock("operation")
			if lockErr == nil {
				lockErr = s.store.Delete(name)
				lock.Release()
			}
			if lockErr != nil {
				item.Enable()
				s.delete.Enable()
				s.setStatus("Error eliminando cuenta: " + lockErr.Error())
				return
			}
			s.removeDeletedAccount(name)
			s.loadAccounts()
			s.setStatus("Cuenta eliminada: " + name)
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
			return "Windows no pudo comprobar la sesión actual de Claude.\n\n\"" + current + "\" está marcada como la cuenta activa. No se eliminó \"" + name + "\".\n\nCierra Claude Desktop o vuelve a intentarlo."
		}
		return "Windows no pudo comprobar la sesión actual de Claude.\n\nNo se eliminó \"" + name + "\". Cierra Claude Desktop o vuelve a intentarlo."
	}
	if errors.Is(err, errDeleteSessionUnrecognized) {
		return "Claude tiene una sesión abierta que no coincide con una cuenta guardada.\n\nNo se eliminó \"" + name + "\" para proteger tus cuentas. Guarda o actualiza primero la sesión actual."
	}
	return "No se pudo comprobar la cuenta activa.\n\nNo se eliminó \"" + name + "\".\n\n" + err.Error()
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
