//go:build !windows

package main

import "syscall"

// diskFree 返回 path 所在文件系统的**可用**字节数（非 root 用户可用的部分）。
// 出错时返回 0，调用方应把它当作「未知」并跳过磁盘余量判断。
func diskFree(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
