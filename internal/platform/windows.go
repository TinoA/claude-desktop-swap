//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	windowsPackageFamily = "Claude_pzs8sxrjxfjjc"
	windowsAUMID         = windowsPackageFamily + "!Claude"
	processName          = "Claude.exe"
	processPollInterval  = 100 * time.Millisecond
	processPolls         = 100
	processStablePolls   = 3
	installCacheTTL      = 5 * time.Minute
)

type windowsInstallKind uint8

const (
	installUnknown windowsInstallKind = iota
	installSquirrel
	installMSIX
	installPortable
	installWin32
)

type windowsPlatform struct {
	root         string
	executable   string
	processRoot  string
	kind         windowsInstallKind
	msix         bool
	alternatives []windowsInstall
}

type windowsInstall struct {
	root        string
	executable  string
	processRoot string
	kind        windowsInstallKind
	msix        bool
	id          string
}

func current() Platform {
	selected, alternatives := detectWindowsInstallCached()
	return &windowsPlatform{
		root:         selected.root,
		executable:   selected.executable,
		processRoot:  selected.processRoot,
		kind:         selected.kind,
		msix:         selected.msix,
		alternatives: alternatives,
	}
}

func (w *windowsPlatform) AppDataPath() (string, error) {
	if w.root == "" {
		return "", fmt.Errorf("Claude Desktop data directory was not detected")
	}
	return w.root, nil
}

func (w *windowsPlatform) IsInstalled() bool { return w.root != "" }

func (w *windowsPlatform) LaunchPath() string { return w.executable }

func (w *windowsPlatform) IsRunning() (bool, error) {
	for _, alternative := range w.alternatives {
		if alternative.root != w.root && candidateHasProcess(alternative) {
			return false, errors.New("multiple Claude Desktop installations are running; close the other installation first")
		}
	}
	paths, err := w.desktopProcessPaths()
	return len(paths) > 0, err
}

func (w *windowsPlatform) KillApp() error {
	for attempt := range processPolls {
		roots, err := w.desktopProcessRoots()
		if err != nil {
			return err
		}
		if len(roots) == 0 {
			return nil
		}
		args := []string{"/PID", strconv.Itoa(roots[0]), "/T"}
		if attempt >= processPolls/10 {
			args = append(args, "/F")
		}
		for _, pid := range roots {
			args[1] = strconv.Itoa(pid)
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			_ = exec.CommandContext(ctx, "taskkill.exe", args...).Run()
			cancel()
		}
		time.Sleep(processPollInterval)
	}
	return errors.New("Claude Desktop processes did not exit before timeout")
}

func (w *windowsPlatform) LaunchApp() error {
	var err error
	if w.msix {
		verb, _ := windows.UTF16PtrFromString("open")
		target, _ := windows.UTF16PtrFromString("shell:AppsFolder\\" + windowsAUMID)
		err = windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
	} else if w.executable == "" {
		err = errors.New("Claude Desktop executable was not detected")
	} else {
		err = exec.Command(w.executable).Start()
	}
	if err != nil {
		return err
	}
	stable := 0
	for range processPolls {
		paths, pollErr := w.desktopProcessPaths()
		if pollErr != nil {
			return pollErr
		}
		if len(paths) > 0 {
			stable++
			if stable >= processStablePolls {
				return nil
			}
		} else {
			stable = 0
		}
		time.Sleep(processPollInterval)
	}
	return errors.New("Claude Desktop did not start before timeout")
}

func (w *windowsPlatform) WaitForLoginWindow(ctx context.Context) error {
	for {
		if w.hasMainWindow() {
			return nil
		}
		timer := time.NewTimer(processPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *windowsPlatform) hasMainWindow() bool {
	pids, err := w.desktopProcessPaths()
	if err != nil || len(pids) == 0 {
		return false
	}
	owned := make(map[int]bool, len(pids))
	for _, pid := range pids {
		owned[pid] = true
	}
	found := false
	callback := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		windowsGetWindowProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if !owned[int(pid)] {
			return 1
		}
		visible, _, _ := windowsIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		length, _, _ := windowsGetWindowTextLength.Call(hwnd)
		if length > 0 {
			found = true
			return 0
		}
		return 1
	})
	windowsEnumWindows.Call(callback, 0)
	return found
}

var installCache struct {
	sync.Mutex
	checkedAt    time.Time
	selected     windowsInstall
	alternatives []windowsInstall
}

var windowsMainWindowAPI = windows.NewLazySystemDLL("user32.dll")

var (
	windowsEnumWindows         = windowsMainWindowAPI.NewProc("EnumWindows")
	windowsGetWindowProcessID  = windowsMainWindowAPI.NewProc("GetWindowThreadProcessId")
	windowsIsWindowVisible     = windowsMainWindowAPI.NewProc("IsWindowVisible")
	windowsGetWindowTextLength = windowsMainWindowAPI.NewProc("GetWindowTextLengthW")
)

func detectWindowsInstallCached() (windowsInstall, []windowsInstall) {
	installCache.Lock()
	defer installCache.Unlock()
	if !installCache.checkedAt.IsZero() && time.Since(installCache.checkedAt) < installCacheTTL {
		return installCache.selected, append([]windowsInstall(nil), installCache.alternatives...)
	}
	selected, alternatives := detectWindowsInstall()
	installCache.checkedAt = time.Now()
	installCache.selected = selected
	installCache.alternatives = append([]windowsInstall(nil), alternatives...)
	return selected, alternatives
}

func (w *windowsPlatform) desktopProcessPaths() ([]int, error) {
	processes, err := w.desktopProcesses()
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(processes))
	for _, process := range processes {
		pids = append(pids, process.pid)
	}
	return pids, nil
}

func (w *windowsPlatform) desktopProcessRoots() ([]int, error) {
	processes, err := w.desktopProcesses()
	if err != nil {
		return nil, err
	}
	matched := make(map[int]windowsProcess, len(processes))
	for _, process := range processes {
		matched[process.pid] = process
	}
	roots := make([]int, 0, len(processes))
	for _, process := range processes {
		if _, isChild := matched[process.parent]; !isChild {
			roots = append(roots, process.pid)
		}
	}
	if len(roots) == 0 && len(processes) > 0 {
		roots = append(roots, processes[0].pid)
	}
	return roots, nil
}

type windowsProcess struct {
	pid    int
	parent int
}

func (w *windowsPlatform) desktopProcesses() ([]windowsProcess, error) {
	if w.executable == "" {
		return nil, nil
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	var processes []windowsProcess
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), processName) {
			pid := int(entry.ProcessID)
			if w.ownsProcess(processImagePath(uint32(pid))) {
				processes = append(processes, windowsProcess{pid: pid, parent: int(entry.ParentProcessID)})
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, err
		}
	}
	return processes, nil
}

func (w *windowsPlatform) ownsProcess(path string) bool {
	selected := windowsInstall{
		root:        w.root,
		executable:  w.executable,
		processRoot: w.processRoot,
		kind:        w.kind,
		msix:        w.msix,
	}
	if selected.ownsProcess(path) {
		return true
	}
	for _, alternative := range w.alternatives {
		if alternative.root == w.root && alternative.ownsProcess(path) {
			return true
		}
	}
	return false
}

func (i windowsInstall) ownsProcess(path string) bool {
	if path == "" {
		return false
	}
	path = normalizeWindowsPath(path)
	launch := normalizeWindowsPath(i.executable)
	root := normalizeWindowsPath(i.processRoot)
	switch i.kind {
	case installSquirrel:
		if path == launch {
			return true
		}
		if !pathWithin(path, root) || !strings.EqualFold(filepath.Base(path), processName) {
			return false
		}
		return strings.HasPrefix(strings.ToLower(filepath.Base(filepath.Dir(path))), "app-")
	case installMSIX:
		return pathWithin(path, root) && strings.EqualFold(filepath.Base(path), processName)
	case installPortable, installWin32:
		return path == launch
	default:
		return false
	}
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func processImagePath(pid uint32) string {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(process)
	buffer := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buffer[:size])
}

func desktopProcessPIDs(output, executable string, allowUnknownPath bool) []int {
	want := normalizeWindowsPath(executable)
	var pids []int
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 {
			continue
		}
		path := normalizeWindowsPath(parts[1])
		if path != want && !(allowUnknownPath && path == "") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

func desktopProcessRootPIDs(output, executable string, allowUnknownPath bool) []int {
	type process struct {
		pid    int
		parent int
	}
	want := normalizeWindowsPath(executable)
	matched := make(map[int]process)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 {
			continue
		}
		path := normalizeWindowsPath(parts[2])
		if path != want && !(allowUnknownPath && path == "") {
			continue
		}
		pid, pidErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		parent, parentErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if pidErr == nil && parentErr == nil {
			matched[pid] = process{pid: pid, parent: parent}
		}
	}
	roots := make([]int, 0, len(matched))
	for pid, process := range matched {
		if _, isChild := matched[process.parent]; !isChild {
			roots = append(roots, pid)
		}
	}
	if len(roots) == 0 {
		for pid := range matched {
			roots = append(roots, pid)
			break
		}
	}
	return roots
}

func detectWindowsInstall() (windowsInstall, []windowsInstall) {
	local := os.Getenv("LOCALAPPDATA")
	appData := os.Getenv("APPDATA")
	msixRoot := filepath.Join(local, "Packages", windowsPackageFamily, "LocalCache", "Roaming", "Claude")
	candidates := make([]windowsInstall, 0, 6)
	if registered, ok := detectRegisteredWin32(appData); ok {
		candidates = append(candidates, registered)
	}
	squirrelRoot := filepath.Join(local, "AnthropicClaude")
	squirrelExe := filepath.Join(squirrelRoot, processName)
	if packageInstalled(squirrelExe) {
		candidates = appendInstallCandidate(candidates, windowsInstall{
			id:          "squirrel:" + normalizeWindowsPath(squirrelRoot),
			root:        filepath.Join(appData, "Claude"),
			executable:  squirrelExe,
			processRoot: squirrelRoot,
			kind:        installSquirrel,
		})
	}
	installLocation := appxInstallLocation()
	msixExe := filepath.Join(installLocation, "app", processName)
	if installLocation != "" && packageInstalled(msixExe) {
		candidates = appendInstallCandidate(candidates, windowsInstall{
			id:          "msix:" + windowsPackageFamily,
			root:        msixRoot,
			executable:  msixExe,
			processRoot: installLocation,
			kind:        installMSIX,
			msix:        true,
		})
	}
	home, _ := os.UserHomeDir()
	for _, exe := range []string{
		filepath.Join(local, "ClaudeChatOnly", "app", "claude.exe"),
		filepath.Join(home, "ClaudeChatOnly", "app", "claude.exe"),
	} {
		if _, err := os.Stat(exe); err == nil {
			candidates = appendInstallCandidate(candidates, windowsInstall{
				id:          "portable:" + normalizeWindowsPath(exe),
				root:        filepath.Join(appData, "Claude"),
				executable:  exe,
				processRoot: exe,
				kind:        installPortable,
			})
		}
	}
	for _, candidate := range []string{
		filepath.Join(appData, "Claude"),
		filepath.Join(local, "Claude"),
		filepath.Join(local, "Programs", "Claude"),
		filepath.Join(os.Getenv("ProgramFiles"), "Claude"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Claude"),
	} {
		exe := filepath.Join(candidate, processName)
		if _, err := os.Stat(exe); err == nil {
			candidates = appendInstallCandidate(candidates, windowsInstall{
				id:          "win32:" + normalizeWindowsPath(exe),
				root:        candidate,
				executable:  exe,
				processRoot: candidate,
				kind:        installWin32,
			})
		}
	}
	if len(candidates) == 0 {
		return windowsInstall{}, nil
	}
	selectedIndex := chooseInstallCandidate(candidates)
	selected := candidates[selectedIndex]
	alternatives := make([]windowsInstall, 0, len(candidates)-1)
	for index, candidate := range candidates {
		if index != selectedIndex {
			alternatives = append(alternatives, candidate)
		}
	}
	return selected, alternatives
}

func appendInstallCandidate(candidates []windowsInstall, candidate windowsInstall) []windowsInstall {
	for _, existing := range candidates {
		if existing.id == candidate.id || normalizeWindowsPath(existing.executable) == normalizeWindowsPath(candidate.executable) {
			return candidates
		}
	}
	return append(candidates, candidate)
}

func chooseInstallCandidate(candidates []windowsInstall) int {
	selected := -1
	for index, candidate := range candidates {
		if !candidateHasProcess(candidate) {
			continue
		}
		if selected == -1 || installPriority(candidate.kind) < installPriority(candidates[selected].kind) {
			selected = index
		}
	}
	if selected >= 0 {
		return selected
	}
	for index := range candidates {
		if selected == -1 || installPriority(candidates[index].kind) < installPriority(candidates[selected].kind) {
			selected = index
		}
	}
	return selected
}

func installPriority(kind windowsInstallKind) int {
	switch kind {
	case installSquirrel:
		return 0
	case installMSIX:
		return 1
	case installWin32:
		return 2
	case installPortable:
		return 3
	default:
		return 4
	}
}

func candidateHasProcess(candidate windowsInstall) bool {
	if candidate.executable == "" {
		return false
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return false
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), processName) && candidate.ownsProcess(processImagePath(entry.ProcessID)) {
			return true
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			return false
		}
	}
}

func detectRegisteredWin32(appData string) (windowsInstall, bool) {
	locations := []struct {
		hive   registry.Key
		access uint32
	}{
		{registry.CURRENT_USER, 0},
		{registry.LOCAL_MACHINE, 0},
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
	}
	keys := []string{
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\AnthropicClaude`,
		`Software\Microsoft\Windows\CurrentVersion\Uninstall\Claude`,
	}
	for _, location := range locations {
		for _, keyPath := range keys {
			key, err := registry.OpenKey(location.hive, keyPath, registry.QUERY_VALUE|location.access)
			if err != nil {
				continue
			}
			installLocation, _, valueErr := key.GetStringValue("InstallLocation")
			key.Close()
			if valueErr != nil || strings.TrimSpace(installLocation) == "" {
				continue
			}
			launcher := filepath.Join(installLocation, processName)
			if packageInstalled(launcher) {
				kind := installWin32
				if strings.HasSuffix(strings.ToLower(keyPath), "anthropicclaude") {
					kind = installSquirrel
				}
				return windowsInstall{
					id:          fmt.Sprintf("install-%d:%s", kind, normalizeWindowsPath(installLocation)),
					root:        filepath.Join(appData, "Claude"),
					executable:  launcher,
					processRoot: installLocation,
					kind:        kind,
				}, true
			}
		}
	}
	return windowsInstall{}, false
}

func appxInstallLocation() string {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-AppxPackage -Name Claude | Select-Object -First 1 -ExpandProperty InstallLocation)").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func packageInstalled(executable string) bool {
	if executable == "" {
		return false
	}
	_, err := os.Stat(executable)
	return err == nil
}

func cookiesPath(appDataPath string) string {
	network := filepath.Join(appDataPath, "Network", "Cookies")
	if _, err := os.Stat(network); err == nil {
		return network
	}
	return filepath.Join(appDataPath, "Cookies")
}

func normalizeWindowsPath(path string) string {
	path = strings.TrimSpace(strings.Trim(path, "\r"))
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	return strings.ToLower(filepath.Clean(path))
}
