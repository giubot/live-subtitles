// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin && !windows

package recording

// freeBytes isn't known on this platform; freeBytes is left out of the usage.
func freeBytes(string) (int64, bool) { return 0, false }
