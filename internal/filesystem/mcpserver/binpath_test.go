package mcpserver

import (
	"errors"
	"path/filepath"
	"testing"
)

// fakeLookPath returns a canned PATH-resolution result.
func fakeLookPath(result string, err error) func(string) (string, error) {
	return func(string) (string, error) { return result, err }
}

func TestResolveBinaryPath(t *testing.T) {
	notFound := errors.New("not found")

	tests := []struct {
		name    string
		env     string
		selfDir string
		lookOut string
		lookErr error
		execSet map[string]bool // paths isExec reports true for
		want    string
	}{
		{
			name:    "env override wins when executable",
			env:     "/opt/custom/llm-filesystem",
			selfDir: "/somewhere/bin",
			lookOut: "/usr/bin/llm-filesystem",
			execSet: map[string]bool{
				"/opt/custom/llm-filesystem":    true,
				"/somewhere/bin/llm-filesystem": true,
				"/usr/bin/llm-filesystem":       true,
			},
			want: "/opt/custom/llm-filesystem",
		},
		{
			name:    "env set but not executable falls through to self dir",
			env:     "/opt/custom/llm-filesystem",
			selfDir: "/somewhere/bin",
			lookErr: notFound,
			execSet: map[string]bool{
				"/somewhere/bin/llm-filesystem": true,
			},
			want: "/somewhere/bin/llm-filesystem",
		},
		{
			name:    "self-dir sibling wins over PATH",
			selfDir: "/somewhere/bin",
			lookOut: "/usr/bin/llm-filesystem",
			execSet: map[string]bool{
				"/somewhere/bin/llm-filesystem": true,
				"/usr/bin/llm-filesystem":       true,
			},
			want: "/somewhere/bin/llm-filesystem",
		},
		{
			name:    "PATH used when env and self dir miss",
			selfDir: "/somewhere/bin",
			lookOut: "/usr/bin/llm-filesystem",
			execSet: map[string]bool{
				"/usr/bin/llm-filesystem": true,
			},
			want: "/usr/bin/llm-filesystem",
		},
		{
			name:    "empty self dir is skipped, not joined",
			selfDir: "",
			lookOut: "/usr/bin/llm-filesystem",
			execSet: map[string]bool{"/usr/bin/llm-filesystem": true},
			want:    "/usr/bin/llm-filesystem",
		},
		{
			name:    "everything misses falls back to default",
			env:     "",
			selfDir: "/somewhere/bin",
			lookErr: notFound,
			execSet: map[string]bool{},
			want:    defaultBinaryPath,
		},
		{
			name:    "empty env string does not match isExec",
			env:     "",
			selfDir: "",
			lookErr: notFound,
			execSet: map[string]bool{"": true}, // adversarial: isExec("") true must not leak the env branch
			want:    defaultBinaryPath,
		},
		{
			name:    "PATH returns empty string with nil error is ignored",
			selfDir: "/somewhere/bin",
			lookOut: "",
			lookErr: nil,
			execSet: map[string]bool{},
			want:    defaultBinaryPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isExec := func(p string) bool { return tt.execSet[p] }
			got := resolveBinaryPath(tt.env, tt.selfDir, fakeLookPath(tt.lookOut, tt.lookErr), isExec)
			if got != tt.want {
				t.Errorf("resolveBinaryPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveBinaryPathJoinsSiblingName(t *testing.T) {
	// The self-dir candidate must be dir joined with the CLI binary name,
	// never the directory itself.
	seen := ""
	isExec := func(p string) bool { seen = p; return false }
	resolveBinaryPath("", "/a/b", fakeLookPath("", errors.New("x")), isExec)
	if want := filepath.Join("/a/b", cliBinaryName); seen != want {
		t.Errorf("sibling candidate = %q, want %q", seen, want)
	}
}
