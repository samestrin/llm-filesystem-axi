package mcpserver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// BinaryEnvVar names the llm-filesystem binary the server runs, overriding
// every other lookup.
const BinaryEnvVar = "LLM_FILESYSTEM_BINARY"

const binaryName = "llm-filesystem"

// ResolveBinary finds the llm-filesystem binary: BinaryEnvVar, then PATH,
// then the directory of the running server (install.sh's ~/.local/bin is
// often not on an MCP host's PATH). When nothing is found, the error names
// the variable and every path tried, so a broken install is fixable from the
// error alone.
func ResolveBinary() (string, error) {
	return resolveBinary(os.Getenv, exec.LookPath, os.Executable)
}

func resolveBinary(getenv func(string) string, lookPath func(string) (string, error), executable func() (string, error)) (string, error) {
	name := binaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	// A variable that names nothing is an error, not a cue to run some
	// other llm-filesystem from PATH than the one the operator chose.
	if override := getenv(BinaryEnvVar); override != "" {
		if usable(override) {
			return override, nil
		}
		return "", fmt.Errorf("%s=%s, but no executable file is there. Point %s at the llm-filesystem binary, or unset it to search PATH",
			BinaryEnvVar, override, BinaryEnvVar)
	}

	if found, err := lookPath(binaryName); err == nil {
		return found, nil
	}

	tried := []string{BinaryEnvVar + " (not set)"}
	for _, dir := range filepath.SplitList(getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		tried = append(tried, filepath.Join(dir, name)+" (PATH)")
	}

	if exe, err := executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), name)
		if usable(sibling) {
			return sibling, nil
		}
		tried = append(tried, sibling+" (beside llm-filesystem-mcp)")
	} else {
		tried = append(tried, "beside llm-filesystem-mcp (its location is unknown: "+err.Error()+")")
	}

	return "", fmt.Errorf("llm-filesystem binary not found. Tried:\n  %s\nInstall llm-filesystem, or set %s to its full path",
		strings.Join(tried, "\n  "), BinaryEnvVar)
}

// usable reports whether path is a file the server could run. A file without
// the exec bit is not: the server would start and fail every call.
func usable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}
