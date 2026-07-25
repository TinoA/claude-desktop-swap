//go:build windows

package cmd

type switchOverlay struct {
	hwnd  uintptr
	ready chan error
	done  chan struct{}
}

func startSwitchOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Switching account...", false, iconPath...)
}

func startAddPreparationOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Preparing new account...", false, iconPath...)
}

func startAddCompletionOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Saving account and restarting Claude...", false, iconPath...)
}

func startLoginReopenOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Reopening Claude...", false, iconPath...)
}

func startAddSuccessOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Account added", true, iconPath...)
}

func startBackupPreparationOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Preparing backup...", false, iconPath...)
}

func startInitialAccountSaveOverlay(iconPath ...string) *switchOverlay {
	return startNativeOverlay("Saving current account...", false, iconPath...)
}

func (o *switchOverlay) Close() {
	if o == nil || o.hwnd == 0 {
		return
	}
	nativePostMessage.Call(o.hwnd, nativeWMClose, 0, 0)
	<-o.done
}
