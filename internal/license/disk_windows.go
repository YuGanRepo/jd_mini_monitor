package license

import (
	"fmt"
	"syscall"
	"unsafe"
)

// volumeSerial reads the volume serial number of the C: drive via
// the Win32 GetVolumeInformationW API. It mirrors the technique used by the
// JD extension's deterministic fingerprint.
func volumeSerial() string {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetVolumeInformationW")

	rootPath, _ := syscall.UTF16PtrFromString(`C:\`)
	var (
		volNameBuf [256]uint16
		serial     uint32
		maxLen     uint32
		flags      uint32
		fsBuf      [256]uint16
	)

	ret, _, _ := proc.Call(
		uintptr(unsafe.Pointer(rootPath)),
		uintptr(unsafe.Pointer(&volNameBuf[0])),
		uintptr(len(volNameBuf)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxLen)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsBuf[0])),
		uintptr(len(fsBuf)),
	)

	if ret == 0 {
		return ""
	}
	return fmt.Sprintf("%08X", serial)
}
