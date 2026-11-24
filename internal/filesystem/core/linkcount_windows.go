//go:build windows

package core

import (
	"os"
	"syscall"
)

// fileReadAttributes is FILE_READ_ATTRIBUTES, which syscall does not export.
const fileReadAttributes = 0x80

// linkCount returns how many directory entries name the file, or 1 when it
// cannot be read. os.FileInfo carries no link count on Windows, so the gate
// in WriteFile never fired there (review of PR #88); the count comes from
// GetFileInformationByHandle instead. CreateFile follows a symlink, as the
// os.Stat that produced info did.
func linkCount(path string, _ os.FileInfo) uint64 {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 1
	}
	h, err := syscall.CreateFile(p, fileReadAttributes,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 1
	}
	defer syscall.CloseHandle(h)
	var fi syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &fi); err != nil {
		return 1
	}
	return uint64(fi.NumberOfLinks)
}
