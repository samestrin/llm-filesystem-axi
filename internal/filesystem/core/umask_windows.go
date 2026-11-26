package core

import "io/fs"

// processUmask is zero on Windows, which has no umask.
func processUmask() fs.FileMode { return 0 }
