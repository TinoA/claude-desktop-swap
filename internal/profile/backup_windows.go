//go:build windows

package profile

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsDataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32            = windows.NewLazySystemDLL("crypt32.dll")
	cryptProtectData   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	localFree          = kernel32.NewProc("LocalFree")
)

const cryptProtectUIForbidden = 0x1

func protectForCurrentWindowsUser(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("cannot protect empty backup")
	}
	if uint64(len(data)) > uint64(^uint32(0)) {
		return nil, errors.New("backup is too large for Windows user protection")
	}
	in := windowsDataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out windowsDataBlob
	result, _, err := cryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if result == 0 {
		return nil, err
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}

func unprotectForCurrentWindowsUser(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty local backup")
	}
	if uint64(len(data)) > uint64(^uint32(0)) {
		return nil, errors.New("local backup is too large")
	}
	in := windowsDataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out windowsDataBlob
	result, _, err := cryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if result == 0 {
		return nil, err
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}

func replaceBackupFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
