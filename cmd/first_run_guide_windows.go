//go:build windows

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	firstRunGuideImageName   = "first-run-guide.bmp"
	firstRunGuidePendingName = "first-run-guide.pending"
	nativeImageBitmap        = 0
	nativeLRLoadFromFile     = 0x00000010
	nativeLRCreateDIBSection = 0x00002000
	nativeSRCCopy            = 0x00CC0020
	nativeHalftone           = 4
	nativeGuideScreenMargin  = 64
)

var (
	nativeGuideClassOnce      sync.Once
	nativeGuideClassErr       error
	nativeGuideStatesMu       sync.Mutex
	nativeGuideStates         = make(map[uintptr]*nativeGuideState)
	nativeGuideWndProc        = windows.NewCallback(nativeGuideWindowProc)
	nativeLoadImage           = nativeUser32.NewProc("LoadImageW")
	nativeAdjustWindowRect    = nativeUser32.NewProc("AdjustWindowRectEx")
	nativeSetThreadDPIContext = nativeUser32.NewProc("SetThreadDpiAwarenessContext")
	nativeGetObject           = nativeGDI32.NewProc("GetObjectW")
	nativeCreateCompatibleDC  = nativeGDI32.NewProc("CreateCompatibleDC")
	nativeDeleteDC            = nativeGDI32.NewProc("DeleteDC")
	nativeBitBlt              = nativeGDI32.NewProc("BitBlt")
	nativeStretchBlt          = nativeGDI32.NewProc("StretchBlt")
	nativeSetStretchMode      = nativeGDI32.NewProc("SetStretchBltMode")
)

type nativeBitmap struct {
	bitmapType int32
	width      int32
	height     int32
	widthBytes int32
	planes     uint16
	bitsPixel  uint16
	bits       uintptr
}

type nativeGuideState struct {
	bitmap        uintptr
	sourceWidth   int
	sourceHeight  int
	displayWidth  int
	displayHeight int
}

func firstRunGuidePaths(executable string) (string, string) {
	directory := filepath.Dir(executable)
	return filepath.Join(directory, firstRunGuideImageName), filepath.Join(directory, firstRunGuidePendingName)
}

func (s *trayState) showFirstRunGuideIfPending() {
	executable, err := os.Executable()
	if err != nil {
		s.setStatus("Could not locate the first-run guide: " + err.Error())
		return
	}
	imagePath, pendingPath := firstRunGuidePaths(executable)
	if _, err := os.Stat(pendingPath); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		s.setStatus("Could not read the first-run guide state: " + err.Error())
		return
	}
	if err := nativeShowFirstRunGuide(imagePath); err != nil {
		s.setStatus("Could not show the first-run guide: " + err.Error())
		return
	}
	if err := os.Remove(pendingPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.setStatus("Could not finish the first-run guide: " + err.Error())
	}
}

func nativeRegisterGuideClass() error {
	nativeGuideClassOnce.Do(func() {
		nativeGuideClassErr = nativeRegisterClass("WindowsClaudeSwapFirstRunGuide", nativeGuideWndProc)
	})
	return nativeGuideClassErr
}

func nativeShowFirstRunGuide(imagePath string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := nativeRegisterGuideClass(); err != nil {
		return err
	}
	previousDPIContext := setGuideThreadDPIAwareness()
	if previousDPIContext != 0 {
		defer nativeSetThreadDPIContext.Call(previousDPIContext)
	}

	bitmapPath, err := windows.UTF16PtrFromString(imagePath)
	if err != nil {
		return err
	}
	bitmap, _, loadErr := nativeLoadImage.Call(
		0,
		uintptr(unsafe.Pointer(bitmapPath)),
		nativeImageBitmap,
		0,
		0,
		nativeLRLoadFromFile|nativeLRCreateDIBSection,
	)
	if bitmap == 0 {
		return fmt.Errorf("load first-run guide: %w", loadErr)
	}
	defer nativeDeleteObject.Call(bitmap)

	var bitmapInfo nativeBitmap
	if result, _, objectErr := nativeGetObject.Call(bitmap, unsafe.Sizeof(bitmapInfo), uintptr(unsafe.Pointer(&bitmapInfo))); result == 0 {
		return fmt.Errorf("inspect first-run guide: %w", objectErr)
	}
	if bitmapInfo.width <= 0 || bitmapInfo.height <= 0 {
		return errors.New("first-run guide has invalid dimensions")
	}

	screenWidth, _, _ := nativeGetMetrics.Call(nativeSMCXScreen)
	screenHeight, _, _ := nativeGetMetrics.Call(nativeSMCYScreen)
	displayWidth, displayHeight := fitGuideSize(
		int(bitmapInfo.width),
		int(bitmapInfo.height),
		int(screenWidth)-nativeGuideScreenMargin,
		int(screenHeight)-nativeGuideScreenMargin,
	)
	style := uintptr(nativeWSCaption | nativeWSSysMenu)
	exStyle := uintptr(nativeWSExTopmost | nativeWSExDialogFrame)
	windowRect := nativeRect{right: int32(displayWidth), bottom: int32(displayHeight)}
	if result, _, adjustErr := nativeAdjustWindowRect.Call(uintptr(unsafe.Pointer(&windowRect)), style, 0, exStyle); result == 0 {
		return fmt.Errorf("size first-run guide: %w", adjustErr)
	}
	windowWidth := int(windowRect.right - windowRect.left)
	windowHeight := int(windowRect.bottom - windowRect.top)
	left := (int(screenWidth) - windowWidth) / 2
	top := (int(screenHeight) - windowHeight) / 2
	hInstance, _, _ := nativeGetModule.Call(0)
	hwnd, _, createErr := nativeCreateWindow.Call(
		exStyle,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("WindowsClaudeSwapFirstRunGuide"))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(ProductName))),
		style,
		uintptr(left),
		uintptr(top),
		uintptr(windowWidth),
		uintptr(windowHeight),
		0,
		0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("create first-run guide: %w", createErr)
	}

	nativeGuideStatesMu.Lock()
	nativeGuideStates[hwnd] = &nativeGuideState{
		bitmap:        bitmap,
		sourceWidth:   int(bitmapInfo.width),
		sourceHeight:  int(bitmapInfo.height),
		displayWidth:  displayWidth,
		displayHeight: displayHeight,
	}
	nativeGuideStatesMu.Unlock()
	defer func() {
		nativeGuideStatesMu.Lock()
		delete(nativeGuideStates, hwnd)
		nativeGuideStatesMu.Unlock()
	}()

	nativeShowWindow.Call(hwnd, nativeSWShow)
	nativeUpdateWindow.Call(hwnd)
	nativeActivateWindow(hwnd)

	var message nativeMSG
	for {
		result, _, messageErr := nativeGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("read first-run guide message: %w", messageErr)
		}
		if result == 0 {
			break
		}
		nativeTranslate.Call(uintptr(unsafe.Pointer(&message)))
		nativeDispatch.Call(uintptr(unsafe.Pointer(&message)))
	}
	return nil
}

func setGuideThreadDPIAwareness() uintptr {
	if err := nativeSetThreadDPIContext.Find(); err != nil {
		return 0
	}
	previous, _, _ := nativeSetThreadDPIContext.Call(^uintptr(3))
	return previous
}

func fitGuideSize(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	if width*maxHeight > height*maxWidth {
		return maxWidth, height * maxWidth / width
	}
	return width * maxHeight / height, maxHeight
}

func nativeGuideWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case nativeWMPaint:
		var paint nativePaintStruct
		hdc, _, _ := nativeBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		nativeGuideStatesMu.Lock()
		state := nativeGuideStates[hwnd]
		nativeGuideStatesMu.Unlock()
		if hdc != 0 && state != nil {
			memoryDC, _, _ := nativeCreateCompatibleDC.Call(hdc)
			if memoryDC != 0 {
				previous, _, _ := nativeSelectObject.Call(memoryDC, state.bitmap)
				if state.sourceWidth == state.displayWidth && state.sourceHeight == state.displayHeight {
					nativeBitBlt.Call(hdc, 0, 0, uintptr(state.displayWidth), uintptr(state.displayHeight), memoryDC, 0, 0, nativeSRCCopy)
				} else {
					nativeSetStretchMode.Call(hdc, nativeHalftone)
					nativeStretchBlt.Call(
						hdc,
						0,
						0,
						uintptr(state.displayWidth),
						uintptr(state.displayHeight),
						memoryDC,
						0,
						0,
						uintptr(state.sourceWidth),
						uintptr(state.sourceHeight),
						nativeSRCCopy,
					)
				}
				nativeSelectObject.Call(memoryDC, previous)
				nativeDeleteDC.Call(memoryDC)
			}
		}
		nativeEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		return 0
	case nativeWMClose:
		nativeDestroyWindow.Call(hwnd)
		return 0
	case nativeWMDestroy:
		nativePostQuit.Call(0)
		return 0
	}
	result, _, _ := nativeDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}
