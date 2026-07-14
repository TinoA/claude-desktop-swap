//go:build windows

package cmd

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	deleteWMCommand       = 0x0111
	deleteWMKeyDown       = 0x0100
	deleteWMClose         = 0x0010
	deleteWMSetFont       = 0x0030
	deleteVKEscape        = 0x1B
	deleteWSChild         = 0x40000000
	deleteWSVisible       = 0x10000000
	deleteWSPopup         = 0x80000000
	deleteWSBorder        = 0x00800000
	deleteWSExLayered     = 0x00080000
	deleteWSExTopmost     = 0x00000008
	deleteWSExToolWindow  = 0x00000080
	deleteWSExDialogFrame = 0x00000001
	deleteWSGroup         = 0x00020000
	deleteWSTabStop       = 0x00010000
	deleteBSDefault       = 0x00000001
	deleteSMCXScreen      = 0
	deleteSMCYScreen      = 1
	deleteSWShow          = 5
	deleteLayeredAlpha    = 0x00000002
	deleteIDConfirm       = 1001
	deleteIDCancel        = 1002
)

const (
	deleteOverlayClass = "WindowsClaudeSwapDeleteOverlay"
	deleteDialogClass  = "WindowsClaudeSwapDeleteDialog"
)

var (
	deleteUser32   = windows.NewLazySystemDLL("user32.dll")
	deleteGDI32    = windows.NewLazySystemDLL("gdi32.dll")
	deleteKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	deleteRegisterClass = deleteUser32.NewProc("RegisterClassExW")
	deleteCreateWindow  = deleteUser32.NewProc("CreateWindowExW")
	deleteDefWindowProc = deleteUser32.NewProc("DefWindowProcW")
	deleteDestroyWindow = deleteUser32.NewProc("DestroyWindow")
	deleteDispatch      = deleteUser32.NewProc("DispatchMessageW")
	deleteGetMessage    = deleteUser32.NewProc("GetMessageW")
	deletePostQuit      = deleteUser32.NewProc("PostQuitMessage")
	deleteSetForeground = deleteUser32.NewProc("SetForegroundWindow")
	deleteSetLayered    = deleteUser32.NewProc("SetLayeredWindowAttributes")
	deleteShowWindow    = deleteUser32.NewProc("ShowWindow")
	deleteSetFocus      = deleteUser32.NewProc("SetFocus")
	deleteGetMetrics    = deleteUser32.NewProc("GetSystemMetrics")
	deleteTranslate     = deleteUser32.NewProc("TranslateMessage")
	deleteCreateBrush   = deleteGDI32.NewProc("CreateSolidBrush")
	deleteGetModule     = deleteKernel32.NewProc("GetModuleHandleW")

	deleteClassOnce sync.Once
	deleteClassErr  error
	deleteStatesMu  sync.Mutex
	deleteStates    = make(map[uintptr]*nativeDeleteState)
)

type nativeDeleteState struct {
	dialog  uintptr
	confirm bool
}

type deleteMSG struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

type deleteWndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

var deleteWindowProc = windows.NewCallback(nativeDeleteWndProc)

func trayDeleteConfirm(name string, onlyActive bool) (bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := registerDeleteClasses(); err != nil {
		return false, err
	}
	screenWidthRaw, _, _ := deleteGetMetrics.Call(deleteSMCXScreen)
	screenHeightRaw, _, _ := deleteGetMetrics.Call(deleteSMCYScreen)
	screenWidth := int(screenWidthRaw)
	screenHeight := int(screenHeightRaw)
	if screenWidth <= 0 || screenHeight <= 0 {
		return false, fmt.Errorf("could not determine Windows screen size")
	}
	hInstance, _, _ := deleteGetModule.Call(0)
	overlay, _, err := deleteCreateWindow.Call(
		deleteWSExLayered|deleteWSExTopmost|deleteWSExToolWindow,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(deleteOverlayClass))),
		0,
		deleteWSPopup|deleteWSVisible,
		0, 0, uintptr(screenWidth), uintptr(screenHeight), 0, 0, hInstance, 0,
	)
	if overlay == 0 {
		return false, fmt.Errorf("create delete overlay: %w", err)
	}
	deleteSetLayered.Call(overlay, 0, 180, deleteLayeredAlpha)
	deleteShowWindow.Call(overlay, deleteSWShow)

	const dialogWidth = 560
	const dialogHeight = 270
	dialog, _, err := deleteCreateWindow.Call(
		deleteWSExTopmost|deleteWSExToolWindow|deleteWSExDialogFrame,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(deleteDialogClass))),
		0,
		deleteWSPopup|deleteWSBorder|deleteWSVisible,
		uintptr((screenWidth-dialogWidth)/2), uintptr((screenHeight-dialogHeight)/2), dialogWidth, dialogHeight,
		overlay, 0, hInstance, 0,
	)
	if dialog == 0 {
		deleteDestroyWindow.Call(overlay)
		return false, fmt.Errorf("create delete dialog: %w", err)
	}
	state := &nativeDeleteState{dialog: dialog}
	deleteStatesMu.Lock()
	deleteStates[dialog] = state
	deleteStatesMu.Unlock()
	defer func() {
		deleteStatesMu.Lock()
		delete(deleteStates, dialog)
		deleteStatesMu.Unlock()
		deleteDestroyWindow.Call(dialog)
		deleteDestroyWindow.Call(overlay)
	}()

	createDeleteControls(dialog, name, onlyActive)
	deleteSetForeground.Call(dialog)
	deleteSetFocus.Call(dialog)
	var message deleteMSG
	for {
		result, _, getErr := deleteGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			return false, fmt.Errorf("read delete dialog message: %w", getErr)
		}
		if result == 0 {
			break
		}
		deleteTranslate.Call(uintptr(unsafe.Pointer(&message)))
		deleteDispatch.Call(uintptr(unsafe.Pointer(&message)))
	}
	return state.confirm, nil
}

func registerDeleteClasses() error {
	deleteClassOnce.Do(func() {
		hInstance, _, _ := deleteGetModule.Call(0)
		for _, class := range []struct {
			name  string
			brush uintptr
		}{
			{deleteOverlayClass, newDeleteBrush(0x51565A)},
			{deleteDialogClass, 6},
		} {
			className := windows.StringToUTF16Ptr(class.name)
			windowClass := deleteWndClassEx{
				cbSize:        uint32(unsafe.Sizeof(deleteWndClassEx{})),
				lpfnWndProc:   deleteWindowProc,
				hInstance:     hInstance,
				hbrBackground: class.brush,
				lpszClassName: className,
			}
			if result, _, err := deleteRegisterClass.Call(uintptr(unsafe.Pointer(&windowClass))); result == 0 {
				deleteClassErr = fmt.Errorf("register delete window class %s: %w", class.name, err)
				return
			}
		}
	})
	return deleteClassErr
}

func newDeleteBrush(color uint32) uintptr {
	brush, _, _ := deleteCreateBrush.Call(uintptr(color))
	return brush
}

func createDeleteControls(dialog uintptr, name string, onlyActive bool) {
	createDeleteControl("STATIC", "Confirmar eliminación", 35, 28, 490, 32, dialog, 0)
	if onlyActive {
		createDeleteControl("STATIC", "Es la única cuenta guardada y está activa.", 35, 82, 490, 24, dialog, 0)
		createDeleteControl("STATIC", "Claude Desktop seguirá abierto con esta sesión.", 35, 108, 490, 24, dialog, 0)
		createDeleteControl("STATIC", "El switcher dejará de guardar esta cuenta.", 35, 134, 490, 24, dialog, 0)
		createDeleteControl("STATIC", "La cuenta de Anthropic no se elimina.", 35, 160, 490, 24, dialog, 0)
	} else {
		createDeleteControl("STATIC", "Se eliminarán perfil, token cifrado y datos locales de:", 35, 82, 490, 24, dialog, 0)
		createDeleteControl("STATIC", name, 35, 108, 490, 24, dialog, 0)
		createDeleteControl("STATIC", "La cuenta de Anthropic no se elimina.", 35, 142, 490, 24, dialog, 0)
		createDeleteControl("STATIC", "Las demás cuentas se conservarán.", 35, 166, 490, 24, dialog, 0)
	}
	if onlyActive {
		createDeleteControl("STATIC", name, 35, 186, 490, 24, dialog, 0)
	}
	createDeleteControl("BUTTON", "Eliminar", 300, 210, 110, 32, dialog, deleteIDConfirm)
	createDeleteControl("BUTTON", "Cancelar", 420, 210, 110, 32, dialog, deleteIDCancel)
}

func createDeleteControl(className, text string, x, y, width, height int, parent uintptr, id uintptr) uintptr {
	classPtr := windows.StringToUTF16Ptr(className)
	textPtr := windows.StringToUTF16Ptr(text)
	style := uintptr(deleteWSChild | deleteWSVisible)
	if className == "BUTTON" {
		style |= deleteWSTabStop | deleteWSGroup | deleteBSDefault
	}
	control, _, _ := deleteCreateWindow.Call(0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(textPtr)), style, uintptr(x), uintptr(y), uintptr(width), uintptr(height), parent, id, 0, 0)
	if control != 0 {
		deleteUser32.NewProc("SendMessageW").Call(control, deleteWMSetFont, 0, 1)
	}
	return control
}

func nativeDeleteWndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	deleteStatesMu.Lock()
	state := deleteStates[hwnd]
	deleteStatesMu.Unlock()
	if state != nil {
		switch message {
		case deleteWMCommand:
			switch wParam & 0xffff {
			case deleteIDConfirm:
				state.confirm = true
				deletePostQuit.Call(0)
			case deleteIDCancel:
				deletePostQuit.Call(0)
			}
		case deleteWMKeyDown:
			if wParam == deleteVKEscape {
				deletePostQuit.Call(0)
			}
		case deleteWMClose:
			deletePostQuit.Call(0)
		}
	}
	result, _, _ := deleteDefWindowProc.Call(hwnd, message, wParam, lParam)
	return result
}
