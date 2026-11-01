package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// install.sh downloads a release binary and installs it without sudo. These
// tests run the real script against a local server standing in for a GitHub
// release, because the property that matters is a side effect: what ends up
// in $HOME/.local/bin. An installer that prints "installed" over a binary that
// failed its checksum is the exact failure this repository exists to prevent,
// so every refusal case asserts the install directory, not just the exit code.

const installScript = "../../install.sh"

// fakeBinary is a stand-in release binary. It is a shell script so the
// installer's "does it run" check can execute it on any platform.
const fakeBinary = "#!/bin/sh\necho 'llm-filesystem fake'\n"

// releaseAsset is the asset name release.yml builds for this platform.
func releaseAsset() string {
	return fmt.Sprintf("llm-filesystem-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func sha256Hex(b string) string {
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// fakeRelease serves files by name, and 404s anything not in the map.
func fakeRelease(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runInstaller runs install.sh with HOME pointed at a temp directory and
// returns the combined output and the exit status. extraEnv is appended.
//
// The script is read here and fed to "bash -s", the same way the documented
// "curl ... | bash" runs it. Reading it through os.ReadFile is what makes Go's
// test cache depend on install.sh: handing bash the path instead let an
// install.sh-only edit report a cached PASS without running anything.
func runInstaller(t *testing.T, baseURL string, home string, extraEnv ...string) (string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh does not target Windows")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	script, err := os.ReadFile(installScript)
	if err != nil {
		t.Fatalf("reading %s: %v", installScript, err)
	}
	// Bounded, so a download that hangs fails the test instead of the run.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-s")
	cmd.Stdin = bytes.NewReader(script)
	// Killing bash leaves curl holding the output pipe; stop waiting on it.
	cmd.WaitDelay = 5 * time.Second
	var env []string
	for _, kv := range os.Environ() {
		// Drop anything that would redirect the install away from $HOME.
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "INSTALL_DIR=") ||
			strings.HasPrefix(kv, "LLM_FILESYSTEM_") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(append(env, "HOME="+home, "LLM_FILESYSTEM_BASE_URL="+baseURL), extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("install.sh did not finish within 60s\noutput:\n%s", out.String())
	}
	if err == nil {
		return out.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), ee.ExitCode()
	}
	t.Fatalf("running install.sh: %v", err)
	return "", -1
}

// assertNothingInstalled fails if the install directory holds any file.
func assertNothingInstalled(t *testing.T, home, out string) {
	t.Helper()
	dir := filepath.Join(home, ".local", "bin")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, e := range entries {
		t.Errorf("refused install left %s in %s\noutput:\n%s", e.Name(), dir, out)
	}
}

// The happy path: a matching checksum installs the exact bytes served, as an
// executable, at $HOME/.local/bin/llm-filesystem, with no root needed.
func TestInstallScriptInstallsOnGoodChecksum(t *testing.T) {
	asset := releaseAsset()
	srv := fakeRelease(t, map[string]string{
		asset:            fakeBinary,
		"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  " + asset + "\n" + sha256Hex("other") + "  llm-filesystem-plan9-mips\n",
	})
	home := t.TempDir()

	out, code := runInstaller(t, srv.URL, home)
	if code != 0 {
		t.Fatalf("install.sh exited %d on a good checksum\noutput:\n%s", code, out)
	}

	dest := filepath.Join(home, ".local", "bin", "llm-filesystem")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("binary not installed at %s: %v\noutput:\n%s", dest, err, out)
	}
	if string(got) != fakeBinary {
		t.Errorf("installed binary differs from the release asset: %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}
	// Only the binary: no temp files left beside it.
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Errorf("install directory holds %d entries, want 1: %v", len(entries), entries)
	}
}

// The case the checksum exists for: a binary that does not match
// SHA256SUMS.txt must not be installed, and the script must not exit 0.
func TestInstallScriptRefusesBadChecksum(t *testing.T) {
	asset := releaseAsset()
	srv := fakeRelease(t, map[string]string{
		asset:            fakeBinary,
		"SHA256SUMS.txt": sha256Hex("a different binary") + "  " + asset + "\n",
	})
	home := t.TempDir()

	out, code := runInstaller(t, srv.URL, home)
	if code == 0 {
		t.Errorf("install.sh exited 0 on a checksum mismatch\noutput:\n%s", out)
	}
	assertNothingInstalled(t, home, out)
}

// A bad download must not replace a binary that is already installed. The
// install is a rename at the end, so a refusal leaves the old one untouched.
func TestInstallScriptBadChecksumKeepsExistingBinary(t *testing.T) {
	asset := releaseAsset()
	srv := fakeRelease(t, map[string]string{
		asset:            fakeBinary,
		"SHA256SUMS.txt": sha256Hex("a different binary") + "  " + asset + "\n",
	})
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const old = "#!/bin/sh\necho old\n"
	dest := filepath.Join(dir, "llm-filesystem")
	if err := os.WriteFile(dest, []byte(old), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := runInstaller(t, srv.URL, home)
	if code == 0 {
		t.Errorf("install.sh exited 0 on a checksum mismatch\noutput:\n%s", out)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != old {
		t.Errorf("existing binary was changed by a refused install: %q, %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("refused install left extra files: %v", entries)
	}
}

// A download that connects and then stalls is the failed download a user is
// most likely to hit. Without a timeout "curl ... | bash" hangs on it instead
// of refusing. LLM_FILESYSTEM_DOWNLOAD_TIMEOUT shortens the limit so the test
// does not wait out the default.
func TestInstallScriptRefusesStalledDownload(t *testing.T) {
	asset := releaseAsset()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case asset:
			w.Header().Set("Content-Length", "1000000")
			_, _ = w.Write([]byte(fakeBinary))
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
			}
		case "SHA256SUMS.txt":
			_, _ = w.Write([]byte(sha256Hex(fakeBinary) + "  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first, so Close does not wait on the handler
	home := t.TempDir()

	start := time.Now()
	out, code := runInstaller(t, srv.URL, home, "LLM_FILESYSTEM_DOWNLOAD_TIMEOUT=2")
	if code == 0 {
		t.Errorf("install.sh exited 0 on a stalled download\noutput:\n%s", out)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("install.sh took %v to give up on a stalled download", elapsed)
	}
	assertNothingInstalled(t, home, out)
}

// A malformed timeout must be refused, not handed to curl, where an empty
// or zero value would mean no limit at all.
func TestInstallScriptRefusesBadDownloadTimeout(t *testing.T) {
	asset := releaseAsset()
	srv := fakeRelease(t, map[string]string{
		asset:            fakeBinary,
		"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  " + asset + "\n",
	})
	for _, v := range []string{"0", "abc", "-5", "1.5"} {
		t.Run(v, func(t *testing.T) {
			home := t.TempDir()
			out, code := runInstaller(t, srv.URL, home, "LLM_FILESYSTEM_DOWNLOAD_TIMEOUT="+v)
			if code == 0 {
				t.Errorf("install.sh exited 0 with timeout %q\noutput:\n%s", v, out)
			}
			assertNothingInstalled(t, home, out)
		})
	}
}

// Every other way the release can be unusable: the binary or the checksum
// file missing, or a checksum file with no line for this platform. Each one
// must refuse rather than install something unverified.
func TestInstallScriptRefusesUnverifiableRelease(t *testing.T) {
	asset := releaseAsset()
	cases := map[string]map[string]string{
		"binary download fails": {
			"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  " + asset + "\n",
		},
		"checksum download fails": {
			asset: fakeBinary,
		},
		"no checksum for this platform": {
			asset:            fakeBinary,
			"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  llm-filesystem-plan9-mips\n",
		},
		// A name that merely contains the asset must not count as its line.
		"checksum only for a longer name": {
			asset:            fakeBinary,
			"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  " + asset + ".exe\n",
		},
		"empty checksum file": {
			asset:            fakeBinary,
			"SHA256SUMS.txt": "",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			srv := fakeRelease(t, files)
			home := t.TempDir()

			out, code := runInstaller(t, srv.URL, home)
			if code == 0 {
				t.Errorf("install.sh exited 0\noutput:\n%s", out)
			}
			assertNothingInstalled(t, home, out)
		})
	}
}

// The old installer built from source and had to be run with sudo. The
// docs told readers to run that for as long as it existed; once the script
// stopped needing root, any page still saying so is wrong.
func TestNoDocNamesSudoInstall(t *testing.T) {
	banned := "sudo ./" + "install.sh"
	var checked int
	err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "build", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".sh", ".go", ".yml", ".yaml":
		default:
			return nil
		}
		// The changelog has to be able to say what the old usage was.
		if info.Name() == "CHANGELOG.md" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		if strings.Contains(string(body), banned) {
			t.Errorf("%s still says %q; install.sh no longer needs root", path, banned)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Fatalf("checked only %d files; the walk root is wrong and this would pass vacuously", checked)
	}
}
