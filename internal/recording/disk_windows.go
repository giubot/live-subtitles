// SPDX-License-Identifier: Apache-2.0

//go:build windows

package recording

import (
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeBytes is the space the current user can still write on dir's disk.
func freeBytes(dir string) (int64, bool) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var avail, total, free uint64
	ok, _, _ := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return 0, false
	}
	return int64(avail), true
}
