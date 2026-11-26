//go:build windows

package core

import "os"

// fileOwner reports no owner on Windows: a file there has no uid or gid to
// carry over, and os.Chown is not supported.
func fileOwner(os.FileInfo) (uid, gid int, ok bool) {
	return 0, 0, false
}
