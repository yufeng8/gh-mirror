//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpace  = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// diskFree 返回 path 所在卷的**可用**字节数（当前用户可用配额）。
// 出错时返回 0，调用方应把它当作「未知」并跳过磁盘余量判断。
func diskFree(path string) int64 {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var freeToCaller, total, totalFree uint64
	r, _, _ := procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0
	}
	return int64(freeToCaller)
}
