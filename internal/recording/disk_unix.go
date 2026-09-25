// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package recording

import "syscall"

// freeBytes is the space an unprivileged user can still write on dir's disk.
func freeBytes(dir string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
