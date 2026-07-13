//go:build windows

package cmd

type switchOverlay struct {
	hwnd  uintptr
	ready chan error
	done  chan struct{}
}

func startSwitchOverlay() *switchOverlay {
	return startNativeOverlay("Cambiando cuenta...", false)
}

func startAddPreparationOverlay() *switchOverlay {
	return startNativeOverlay("Preparando nueva cuenta...", false)
}

func startAddSuccessOverlay() *switchOverlay {
	return startNativeOverlay("Cuenta agregada", true)
}

func startBackupPreparationOverlay() *switchOverlay {
	return startNativeOverlay("Preparando backup...", false)
}

func (o *switchOverlay) Close() {
	if o == nil || o.hwnd == 0 {
		return
	}
	nativePostMessage.Call(o.hwnd, nativeWMClose, 0, 0)
	<-o.done
}
