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

// fakeServerBinary is a stand-in release asset for the MCP server, with
// different bytes than fakeBinary so a test can tell the two installs apart.
const fakeServerBinary = "#!/bin/sh\necho 'llm-filesystem-mcp fake'\n"

// releaseAsset is the asset name release.yml builds for this platform.
func releaseAsset() string {
	return fmt.Sprintf("llm-filesystem-%s-%s", runtime.GOOS, runtime.GOARCH)
}

// releaseServerAsset is the MCP server asset name the installer fetches
// beside the CLI, for this platform.
func releaseServerAsset() string {
	return fmt.Sprintf("llm-filesystem-mcp-%s-%s", runtime.GOOS, runtime.GOARCH)
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

// installRelease serves bin as this platform's release asset with a matching
// checksum, so a test can vary only the binary. It serves no server asset,
// like the releases that predate the MCP server.
func installRelease(t *testing.T, bin string) *httptest.Server {
	t.Helper()
	asset := releaseAsset()
	return fakeRelease(t, map[string]string{
		asset:            bin,
		"SHA256SUMS.txt": sha256Hex(bin) + "  " + asset + "\n",
	})
}

// installReleaseWithServer serves both of this platform's release assets
// with matching checksums, so a test can vary either binary.
func installReleaseWithServer(t *testing.T, bin, mcp string) *httptest.Server {
	t.Helper()
	asset, serverAsset := releaseAsset(), releaseServerAsset()
	return fakeRelease(t, map[string]string{
		asset:       bin,
		serverAsset: mcp,
		"SHA256SUMS.txt": sha256Hex(bin) + "  " + asset + "\n" +
			sha256Hex(mcp) + "  " + serverAsset + "\n",
	})
}

// The older pair an upgrading machine already has in its install directory.
const (
	oldCLIBinary = "#!/bin/sh\necho 'llm-filesystem version v0.9.0'\n"
	oldMCPBinary = "#!/bin/sh\necho 'llm-filesystem-mcp version v0.9.0'\n"
)

// installExistingPair writes an older CLI and server into the install
// directory, as a machine upgrading would have.
func installExistingPair(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".local", "bin")
	writeExecutable(t, dir, "llm-filesystem", oldCLIBinary)
	writeExecutable(t, dir, "llm-filesystem-mcp", oldMCPBinary)
}

// assertPairUntouched fails if a refused install changed either existing
// binary or left extra files beside them.
func assertPairUntouched(t *testing.T, home, out string) {
	t.Helper()
	dir := filepath.Join(home, ".local", "bin")
	for name, want := range map[string]string{"llm-filesystem": oldCLIBinary, "llm-filesystem-mcp": oldMCPBinary} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Errorf("existing %s was changed by a refused install: %q, %v\noutput:\n%s", name, got, err, out)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("refused install left extra files: %v\noutput:\n%s", entries, out)
	}
}

// writeExecutable writes an executable script at dir/name, creating dir.
func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// A binary can pass its checksum and still not run here: the wrong
// architecture, a truncated release, a noexec install directory (TD #29).
// Installing it reports success for something that fails on first use, so
// the installer runs the staged binary with --version first. One that exits
// non-zero must install nothing and must leave an existing binary alone.
func TestInstallScriptRefusesBinaryThatDoesNotRun(t *testing.T) {
	srv := installRelease(t, "#!/bin/sh\necho 'cannot execute binary file' >&2\nexit 3\n")

	t.Run("fresh install", func(t *testing.T) {
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 over a binary whose --version failed\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})

	t.Run("keeps existing binary", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".local", "bin")
		const old = "#!/bin/sh\necho 'llm-filesystem version v0.9.0'\n"
		dest := writeExecutable(t, dir, "llm-filesystem", old)
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 over a binary whose --version failed\noutput:\n%s", out)
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != old {
			t.Errorf("existing binary was changed by a refused install: %q, %v", got, err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("refused install left extra files: %v", entries)
		}
	})
}

// A binary that hangs on --version would hang "curl ... | bash" with it. The
// check is bounded at 10 seconds, without timeout(1), which macOS lacks; a
// binary that runs past it counts as one that does not run.
func TestInstallScriptRefusesBinaryThatHangs(t *testing.T) {
	srv := installRelease(t, "#!/bin/sh\nsleep 50\necho 'llm-filesystem version v1.0.0'\n")
	home := t.TempDir()

	start := time.Now()
	out, code := runInstaller(t, srv.URL, home)
	elapsed := time.Since(start)
	if code == 0 {
		t.Errorf("install.sh exited 0 over a binary whose --version hung\noutput:\n%s", out)
	}
	if elapsed > 30*time.Second {
		t.Errorf("install.sh took %v; the --version check is not bounded at 10s\noutput:\n%s", elapsed, out)
	}
	if elapsed < 9*time.Second {
		t.Errorf("install.sh gave up after %v, before the 10s limit\noutput:\n%s", elapsed, out)
	}
	assertNothingInstalled(t, home, out)
}

// Upgrading is re-running the installer, so its output is the only record of
// what changed. Replacing a binary prints the version it replaced and the one
// now installed; a re-run that names neither leaves the user to guess.
func TestInstallScriptPrintsBothVersionsOnUpgrade(t *testing.T) {
	srv := installRelease(t, "#!/bin/sh\necho 'llm-filesystem version v1.2.0'\n")
	home := t.TempDir()
	writeExecutable(t, filepath.Join(home, ".local", "bin"), "llm-filesystem",
		"#!/bin/sh\necho 'llm-filesystem version v0.9.0'\n")

	out, code := runInstaller(t, srv.URL, home)
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	for _, v := range []string{"v0.9.0", "v1.2.0"} {
		if !strings.Contains(out, v) {
			t.Errorf("upgrade output does not name %s\noutput:\n%s", v, out)
		}
	}
}

// An older llm-filesystem earlier on PATH still wins after an install, and a
// pre-AXI one rejects --confirm (TD #25). The install succeeds, but the user
// must be told which binary their shell will run instead, by its path.
func TestInstallScriptWarnsAboutShadowingBinary(t *testing.T) {
	srv := installRelease(t, fakeBinary)
	home := t.TempDir()
	installDir := filepath.Join(home, ".local", "bin")
	shadow := writeExecutable(t, filepath.Join(home, "shadow"), "llm-filesystem", "#!/bin/sh\necho old\n")

	out, code := runInstaller(t, srv.URL, home,
		"PATH="+filepath.Dir(shadow)+string(os.PathListSeparator)+installDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Warning:") || !strings.Contains(out, shadow) {
		t.Errorf("no warning naming the shadowing binary %s\noutput:\n%s", shadow, out)
	}

	// The control: with the install directory first, nothing shadows it, and
	// a warning would be noise users learn to ignore.
	home2 := t.TempDir()
	out, code = runInstaller(t, srv.URL, home2,
		"PATH="+filepath.Join(home2, ".local", "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "shadows") {
		t.Errorf("warned about shadowing with the install directory first on PATH\noutput:\n%s", out)
	}
}

// The MCP server ships beside the CLI now, but an older llm-filesystem-mcp
// earlier on PATH still wins after an install. The install succeeds, and the
// user must be told which binary their shell will run instead, by its path.
// The installer does not delete files it did not install.
func TestInstallScriptWarnsAboutShadowingMCPServer(t *testing.T) {
	srv := installReleaseWithServer(t, fakeBinary, fakeServerBinary)
	home := t.TempDir()
	installDir := filepath.Join(home, ".local", "bin")
	stale := writeExecutable(t, filepath.Join(home, "stale"), "llm-filesystem-mcp", "#!/bin/sh\n")

	out, code := runInstaller(t, srv.URL, home,
		"PATH="+filepath.Dir(stale)+string(os.PathListSeparator)+installDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Warning:") || !strings.Contains(out, "shadows") || !strings.Contains(out, stale) {
		t.Errorf("no warning naming the shadowing server %s\noutput:\n%s", stale, out)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("the installer removed a file it did not install: %v", err)
	}
	// The new server is installed even though an older one shadows it.
	if got, err := os.ReadFile(filepath.Join(installDir, "llm-filesystem-mcp")); err != nil || string(got) != fakeServerBinary {
		t.Errorf("server not installed beside the CLI: %q, %v\noutput:\n%s", got, err, out)
	}

	// The control: with the install directory first the stale copy no longer
	// shadows the new server, and a warning would be noise users learn to
	// ignore.
	home2 := t.TempDir()
	out, code = runInstaller(t, srv.URL, home2,
		"PATH="+filepath.Join(home2, ".local", "bin")+string(os.PathListSeparator)+filepath.Dir(stale)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "shadows") {
		t.Errorf("warned about shadowing with the install directory first on PATH\noutput:\n%s", out)
	}
}

// The installer ships the MCP server beside the CLI: a release with both
// assets installs both, each the exact bytes of its own asset, executable,
// and nothing else in the install directory. Different bytes per asset catch
// an installer that copies one download over both names.
func TestInstallScriptInstallsBothBinaries(t *testing.T) {
	srv := installReleaseWithServer(t, fakeBinary, fakeServerBinary)
	home := t.TempDir()

	out, code := runInstaller(t, srv.URL, home)
	if code != 0 {
		t.Fatalf("install.sh exited %d with both assets present\noutput:\n%s", code, out)
	}
	dir := filepath.Join(home, ".local", "bin")
	for _, f := range []struct{ name, want string }{
		{"llm-filesystem", fakeBinary},
		{"llm-filesystem-mcp", fakeServerBinary},
	} {
		dest := filepath.Join(dir, f.name)
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("%s not installed: %v\noutput:\n%s", f.name, err, out)
		}
		if string(got) != f.want {
			t.Errorf("installed %s differs from its release asset: %q", f.name, got)
		}
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("installed %s is not executable: %v", f.name, info.Mode())
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("install directory holds %d entries, want 2: %v", len(entries), entries)
	}
}

// Releases before the MCP server existed (v1.0.3 and earlier, which
// releases/latest still serves) have no server asset. The installer must say
// the server was not installed and why, still install the verified CLI, and
// exit 0 — not fail the whole install, and not pretend it installed a server.
func TestInstallScriptSkipsServerWhenReleaseHasNoAsset(t *testing.T) {
	srv := installRelease(t, fakeBinary) // no server asset: it 404s
	home := t.TempDir()

	out, code := runInstaller(t, srv.URL, home)
	if code != 0 {
		t.Fatalf("install.sh exited %d on a release without the server asset\noutput:\n%s", code, out)
	}
	dest := filepath.Join(home, ".local", "bin", "llm-filesystem")
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != fakeBinary {
		t.Errorf("CLI not installed from a serverless release: %q, %v\noutput:\n%s", got, err, out)
	}
	if !strings.Contains(out, "llm-filesystem-mcp was not installed") || !strings.Contains(out, releaseServerAsset()) {
		t.Errorf("no skip line saying the server was not installed and why\noutput:\n%s", out)
	}
	entries, _ := os.ReadDir(filepath.Dir(dest))
	if len(entries) != 1 {
		t.Errorf("install directory holds %d entries, want only the CLI: %v", len(entries), entries)
	}
}

// When the server is skipped there is no new server to shadow, so the
// installer must not warn about one — and it still must not touch an old
// llm-filesystem-mcp it did not install.
func TestInstallScriptSkipLeavesOldServerAlone(t *testing.T) {
	srv := installRelease(t, fakeBinary)
	home := t.TempDir()
	stale := writeExecutable(t, filepath.Join(home, "stale"), "llm-filesystem-mcp", "#!/bin/sh\n")

	out, code := runInstaller(t, srv.URL, home,
		"PATH="+filepath.Join(home, ".local", "bin")+string(os.PathListSeparator)+filepath.Dir(stale)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "shadows") {
		t.Errorf("warned about a shadowing server when no server was installed\noutput:\n%s", out)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("the installer removed a file it did not install: %v", err)
	}
}

// A server asset that is present but fails its checksum is fatal: neither
// binary is installed, and an existing pair is left untouched. The CLI
// passing its own checksum must not buy the server a pass.
func TestInstallScriptRefusesBadServerChecksum(t *testing.T) {
	asset, serverAsset := releaseAsset(), releaseServerAsset()
	srv := fakeRelease(t, map[string]string{
		asset:       fakeBinary,
		serverAsset: fakeServerBinary,
		"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  " + asset + "\n" +
			sha256Hex("a different server") + "  " + serverAsset + "\n",
	})

	t.Run("fresh install", func(t *testing.T) {
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 on a server checksum mismatch\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})

	t.Run("keeps existing binaries", func(t *testing.T) {
		home := t.TempDir()
		installExistingPair(t, home)
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 on a server checksum mismatch\noutput:\n%s", out)
		}
		assertPairUntouched(t, home, out)
	})
}

// A present server asset with no usable checksum line is as fatal as a
// mismatch: the server cannot be verified, so neither binary installs.
func TestInstallScriptRefusesUnverifiableServer(t *testing.T) {
	asset, serverAsset := releaseAsset(), releaseServerAsset()
	goodCLI := sha256Hex(fakeBinary) + "  " + asset + "\n"
	cases := map[string]string{
		"no checksum for the server":      goodCLI,
		"two checksums for the server":    goodCLI + sha256Hex(fakeServerBinary) + "  " + serverAsset + "\n" + sha256Hex("other") + "  " + serverAsset + "\n",
		"checksum only for a longer name": goodCLI + sha256Hex(fakeServerBinary) + "  " + serverAsset + ".exe\n",
	}
	for name, sums := range cases {
		t.Run(name, func(t *testing.T) {
			srv := fakeRelease(t, map[string]string{
				asset:            fakeBinary,
				serverAsset:      fakeServerBinary,
				"SHA256SUMS.txt": sums,
			})
			home := t.TempDir()
			out, code := runInstaller(t, srv.URL, home)
			if code == 0 {
				t.Errorf("install.sh exited 0\noutput:\n%s", out)
			}
			assertNothingInstalled(t, home, out)
		})
	}
}

// A server that passes its checksum but does not run here is fatal too, and
// the CLI that verified must not install without it: all or nothing.
func TestInstallScriptRefusesServerThatDoesNotRun(t *testing.T) {
	srv := installReleaseWithServer(t, fakeBinary, "#!/bin/sh\necho 'cannot execute binary file' >&2\nexit 3\n")

	t.Run("fresh install", func(t *testing.T) {
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 over a server whose --version failed\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})

	t.Run("keeps existing binaries", func(t *testing.T) {
		home := t.TempDir()
		installExistingPair(t, home)
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 over a server whose --version failed\noutput:\n%s", out)
		}
		assertPairUntouched(t, home, out)
	})
}

// Only a 404 means "this release has no server asset". A server download
// that fails any other way — a stalled connection, an HTTP server error —
// must fail the install, not quietly skip the server, so a network fault
// cannot be reported as a successful install of the pair.
func TestInstallScriptRefusesFailedServerDownload(t *testing.T) {
	asset, serverAsset := releaseAsset(), releaseServerAsset()
	sums := sha256Hex(fakeBinary) + "  " + asset + "\n" + sha256Hex(fakeServerBinary) + "  " + serverAsset + "\n"

	t.Run("stalled download", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch strings.TrimPrefix(r.URL.Path, "/") {
			case asset:
				_, _ = w.Write([]byte(fakeBinary))
			case "SHA256SUMS.txt":
				_, _ = w.Write([]byte(sums))
			case serverAsset:
				w.Header().Set("Content-Length", "1000000")
				_, _ = w.Write([]byte(fakeServerBinary))
				w.(http.Flusher).Flush()
				select {
				case <-release:
				case <-r.Context().Done():
				}
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) }) // runs first, so Close does not wait on the handler
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home, "LLM_FILESYSTEM_DOWNLOAD_TIMEOUT=2")
		if code == 0 {
			t.Errorf("install.sh exited 0 on a stalled server download\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})

	t.Run("http server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch strings.TrimPrefix(r.URL.Path, "/") {
			case asset:
				_, _ = w.Write([]byte(fakeBinary))
			case "SHA256SUMS.txt":
				_, _ = w.Write([]byte(sums))
			case serverAsset:
				http.Error(w, "boom", http.StatusInternalServerError)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home)
		if code == 0 {
			t.Errorf("install.sh exited 0 on an HTTP 500 for the server asset\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})
}

// Re-running the installer upgrades both binaries; the output is the record
// of what changed, so it names the version installed and the one replaced
// for the server as it already does for the CLI.
func TestInstallScriptUpgradesBothBinaries(t *testing.T) {
	const newCLI = "#!/bin/sh\necho 'llm-filesystem version v1.2.0'\n"
	const newMCP = "#!/bin/sh\necho 'llm-filesystem-mcp version v1.2.0'\n"
	srv := installReleaseWithServer(t, newCLI, newMCP)
	home := t.TempDir()
	installExistingPair(t, home)

	out, code := runInstaller(t, srv.URL, home)
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	for _, v := range []string{"v0.9.0", "v1.2.0"} {
		if !strings.Contains(out, v) {
			t.Errorf("upgrade output does not name %s\noutput:\n%s", v, out)
		}
	}
	if n := strings.Count(out, "Replaced:"); n != 2 {
		t.Errorf("upgrade output has %d Replaced lines, want one per binary\noutput:\n%s", n, out)
	}
	dir := filepath.Join(home, ".local", "bin")
	for name, want := range map[string]string{"llm-filesystem": newCLI, "llm-filesystem-mcp": newMCP} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s was not upgraded to the release asset: %q, %v\noutput:\n%s", name, got, err, out)
		}
	}
}

// pathWithout builds a directory of symlinks to every command on PATH except
// the named ones, so a test can run the installer on a machine that lacks
// them. Only the first command of each name is linked, as PATH would find.
func pathWithout(t *testing.T, hide ...string) string {
	t.Helper()
	dir := t.TempDir()
	hidden := map[string]bool{}
	for _, h := range hide {
		hidden[h] = true
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if hidden[name] {
				continue
			}
			link := filepath.Join(dir, name)
			if _, err := os.Lstat(link); err == nil {
				continue
			}
			_ = os.Symlink(filepath.Join(p, name), link)
		}
	}
	return dir
}

// macOS has no sha256sum, so every Mac install verifies with shasum, a
// branch CI on Linux never ran (TD #31). This hides sha256sum and wraps
// shasum with a recorder, so the test proves shasum did the check: a good
// checksum installs and a bad one installs nothing.
func TestInstallScriptVerifiesWithShasum(t *testing.T) {
	real, err := exec.LookPath("shasum")
	if err != nil {
		t.Skip("shasum not available")
	}
	bin := pathWithout(t, "sha256sum", "shasum")
	marker := filepath.Join(t.TempDir(), "shasum-ran")
	writeExecutable(t, bin, "shasum", "#!/bin/sh\ntouch '"+marker+"'\nexec '"+real+"' \"$@\"\n")
	if _, err := exec.LookPath(filepath.Join(bin, "sha256sum")); err == nil {
		t.Fatal("sha256sum is still reachable; the test would run the wrong branch")
	}

	t.Run("good checksum installs", func(t *testing.T) {
		_ = os.Remove(marker)
		srv := installRelease(t, "#!/bin/sh\necho 'llm-filesystem version v1.2.0'\n")
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home, "PATH="+bin)
		if code != 0 {
			t.Fatalf("install.sh exited %d with only shasum\noutput:\n%s", code, out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("shasum never ran; the checksum was not verified by it\noutput:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", "llm-filesystem")); err != nil {
			t.Errorf("not installed: %v\noutput:\n%s", err, out)
		}
		// The --version check ran too, and its result is reported.
		if !strings.Contains(out, "v1.2.0") {
			t.Errorf("install output does not report the installed version\noutput:\n%s", out)
		}
	})

	t.Run("bad checksum installs nothing", func(t *testing.T) {
		_ = os.Remove(marker)
		asset := releaseAsset()
		srv := fakeRelease(t, map[string]string{
			asset:            fakeBinary,
			"SHA256SUMS.txt": sha256Hex("a different binary") + "  " + asset + "\n",
		})
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home, "PATH="+bin)
		if code == 0 {
			t.Errorf("install.sh exited 0 on a checksum mismatch with only shasum\noutput:\n%s", out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("shasum never ran\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})
}

// A machine with wget but no curl takes a different fetch branch, and the
// server download parses its HTTP status from wget's --server-response
// output. CI on Linux has curl and never runs that branch (the same gap as
// TD #31 for shasum), so hide curl and prove wget installs both binaries,
// skips a release without the server asset, and refuses a bad server
// checksum.
func TestInstallScriptWorksWithWget(t *testing.T) {
	real, err := exec.LookPath("wget")
	if err != nil {
		t.Skip("wget not available")
	}
	bin := pathWithout(t, "curl", "wget")
	marker := filepath.Join(t.TempDir(), "wget-ran")
	writeExecutable(t, bin, "wget", "#!/bin/sh\ntouch '"+marker+"'\nexec '"+real+"' \"$@\"\n")
	if _, err := exec.LookPath(filepath.Join(bin, "curl")); err == nil {
		t.Fatal("curl is still reachable; the test would run the wrong branch")
	}

	t.Run("both assets install", func(t *testing.T) {
		_ = os.Remove(marker)
		srv := installReleaseWithServer(t, fakeBinary, fakeServerBinary)
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home, "PATH="+bin)
		if code != 0 {
			t.Fatalf("install.sh exited %d with only wget\noutput:\n%s", code, out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("wget never ran; the downloads did not use it\noutput:\n%s", out)
		}
		dir := filepath.Join(home, ".local", "bin")
		for name, want := range map[string]string{"llm-filesystem": fakeBinary, "llm-filesystem-mcp": fakeServerBinary} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(got) != want {
				t.Errorf("%s not installed from its asset with only wget: %q, %v\noutput:\n%s", name, got, err, out)
			}
		}
	})

	t.Run("missing server asset skips", func(t *testing.T) {
		_ = os.Remove(marker)
		srv := installRelease(t, fakeBinary)
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home, "PATH="+bin)
		if code != 0 {
			t.Fatalf("install.sh exited %d on a serverless release with only wget\noutput:\n%s", code, out)
		}
		if !strings.Contains(out, "llm-filesystem-mcp was not installed") {
			t.Errorf("no skip line with only wget\noutput:\n%s", out)
		}
		if got, err := os.ReadFile(filepath.Join(home, ".local", "bin", "llm-filesystem")); err != nil || string(got) != fakeBinary {
			t.Errorf("CLI not installed with only wget: %q, %v\noutput:\n%s", got, err, out)
		}
		if entries, _ := os.ReadDir(filepath.Join(home, ".local", "bin")); len(entries) != 1 {
			t.Errorf("install directory holds %d entries, want only the CLI: %v", len(entries), entries)
		}
	})

	t.Run("bad server checksum installs nothing", func(t *testing.T) {
		_ = os.Remove(marker)
		asset, serverAsset := releaseAsset(), releaseServerAsset()
		srv := fakeRelease(t, map[string]string{
			asset:       fakeBinary,
			serverAsset: fakeServerBinary,
			"SHA256SUMS.txt": sha256Hex(fakeBinary) + "  " + asset + "\n" +
				sha256Hex("a different server") + "  " + serverAsset + "\n",
		})
		home := t.TempDir()
		out, code := runInstaller(t, srv.URL, home, "PATH="+bin)
		if code == 0 {
			t.Errorf("install.sh exited 0 on a server checksum mismatch with only wget\noutput:\n%s", out)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("wget never ran\noutput:\n%s", out)
		}
		assertNothingInstalled(t, home, out)
	})
}
