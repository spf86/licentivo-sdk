package licentivo

import (
	"os"
	"syscall"
	"unsafe"
)

func platformDisplayResources() (int64, int64) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	var installedKB uint64
	kernel.NewProc("GetPhysicallyInstalledSystemMemory").Call(uintptr(unsafe.Pointer(&installedKB)))
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	path, _ := syscall.UTF16PtrFromString(drive + `\`)
	var available, total, free uint64
	kernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	return int64(installedKB / 1024), int64(total / (1024 * 1024 * 1024))
}
