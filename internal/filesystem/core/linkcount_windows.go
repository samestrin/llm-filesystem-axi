//go:build windows

package core

import "os"

// linkCount returns 1: os.FileInfo on Windows carries no link count, so the
// hard-link gate in WriteFile does not fire there.
func linkCount(info os.FileInfo) uint64 {
	return 1
}
