//go:build !windows

package core

import (
	"os"
	"syscall"
)

// linkCount returns how many directory entries name the file, or 1 when the
// platform does not say.
func linkCount(_ string, info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
