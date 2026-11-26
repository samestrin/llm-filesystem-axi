//go:build !windows

package core

import (
	"bufio"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var umaskMu sync.Mutex

// processUmask returns the process's umask, so a file written through a temp
// file can be given the mode os.Create would have given it. Linux reports it
// in /proc/self/status; elsewhere it is read by setting it and putting it
// back, which the mutex keeps two callers in this process from interleaving.
func processUmask() fs.FileMode {
	if f, err := os.Open("/proc/self/status"); err == nil {
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			if v, ok := strings.CutPrefix(s.Text(), "Umask:"); ok {
				if m, err := strconv.ParseUint(strings.TrimSpace(v), 8, 32); err == nil {
					return fs.FileMode(m)
				}
			}
		}
	}
	return umaskBySetting()
}

// umaskBySetting reads the umask by setting it and putting it back, for
// platforms with no /proc/self/status.
func umaskBySetting() fs.FileMode {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	m := syscall.Umask(0)
	syscall.Umask(m)
	return fs.FileMode(m)
}
