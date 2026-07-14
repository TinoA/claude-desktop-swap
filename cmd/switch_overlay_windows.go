//go:build windows

package cmd

type switchOverlay struct {
	hwnd  uintptr
	ready chan error
	done  chan struct{}
}

func startSwitchOverlay() *switchOverlay {
	return startNativeOverlay("Switching account...", false)
}

func startAddPreparationOverlay() *switchOverlay {
	return startNativeOverlay("Preparing new account...", false)
}

func startAddSuccessOverlay() *switchOverlay {
	return startNativeOverlay("Account added", true)
}

func startBackupPreparationOverlay() *switchOverlay {
	return startNativeOverlay("Preparing backup...", false)
}

func (o *switchOverlay) Close() {
	if o == nil || o.hwnd == 0 {
		return
	}
	nativePostMessage.Call(o.hwnd, nativeWMClose, 0, 0)
	<-o.done
}
