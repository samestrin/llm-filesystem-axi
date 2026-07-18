package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
)

// cliBinaryName is the name of the CLI binary the MCP server shells out to.
const cliBinaryName = "llm-filesystem"

// defaultBinaryPath is the install location used when no other candidate resolves.
const defaultBinaryPath = "/usr/local/bin/" + cliBinaryName

// BinaryEnvVar lets an operator pin the CLI binary explicitly.
const BinaryEnvVar = "LLM_FILESYSTEM_BIN"

// resolveBinaryPath selects the llm-filesystem CLI path using this order:
//  1. $LLM_FILESYSTEM_BIN, if it points at an executable file
//  2. an llm-filesystem binary sitting next to the running MCP executable
//  3. an llm-filesystem binary found on $PATH
//  4. the default install location
//
// It is written as a pure function (dependencies injected) so the resolution
// order can be tested without touching the real filesystem.
func resolveBinaryPath(envVal, selfDir string, lookPath func(string) (string, error), isExec func(string) bool) string {
	if envVal != "" && isExec(envVal) {
		return envVal
	}
	if selfDir != "" {
		if cand := filepath.Join(selfDir, cliBinaryName); isExec(cand) {
			return cand
		}
	}
	if p, err := lookPath(cliBinaryName); err == nil && p != "" {
		return p
	}
	return defaultBinaryPath
}

// isExecutableFile reports whether path is a regular, executable file.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// ResolveBinaryPath wires resolveBinaryPath to the real environment: the
// LLM_FILESYSTEM_BIN override, the directory of the running executable, and
// $PATH lookup, with the default install location as the final fallback.
func ResolveBinaryPath() string {
	selfDir := ""
	if exe, err := os.Executable(); err == nil {
		selfDir = filepath.Dir(exe)
	}
	return resolveBinaryPath(os.Getenv(BinaryEnvVar), selfDir, exec.LookPath, isExecutableFile)
}
