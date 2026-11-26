//go:build !windows

package core

import (
	"os"
	"syscall"
)

// fileOwner returns the file's uid and gid, and false when the platform does
// not say.
func fileOwner(info os.FileInfo) (uid, gid int, ok bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid), true
	}
	return 0, 0, false
}
