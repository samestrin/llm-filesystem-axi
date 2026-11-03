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

// installRelease serves bin as this platform's release asset with a matching
// checksum, so a test can vary only the binary.
func installRelease(t *testing.T, bin string) *httptest.Server {
	t.Helper()
	asset := releaseAsset()
	return fakeRelease(t, map[string]string{
		asset:            bin,
		"SHA256SUMS.txt": sha256Hex(bin) + "  " + asset + "\n",
	})
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

// The MCP server was removed, but an old install of it still runs (TD #23).
// The installer does not delete files it did not install; it names the path
// so the user can. The name is split so the no-MCP guard does not trip.
func TestInstallScriptWarnsAboutLeftoverMCPServer(t *testing.T) {
	srv := installRelease(t, fakeBinary)
	home := t.TempDir()
	stale := writeExecutable(t, filepath.Join(home, "stale"), "llm-filesystem"+"-mcp", "#!/bin/sh\n")

	out, code := runInstaller(t, srv.URL, home,
		"PATH="+filepath.Join(home, ".local", "bin")+string(os.PathListSeparator)+filepath.Dir(stale)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 0 {
		t.Fatalf("install.sh exited %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "Warning:") || !strings.Contains(out, stale) {
		t.Errorf("no warning naming the leftover server %s\noutput:\n%s", stale, out)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("the installer removed a file it did not install: %v", err)
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
