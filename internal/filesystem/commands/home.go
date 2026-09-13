package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

// homePageSize bounds the landing listing. A bare invocation in a directory of
// 10,000 entries must not answer with 10,000 rows — that would spend on the
// landing page the tokens the minimal field set exists to save.
const homePageSize = 50

// homeView is the content-first landing payload (AXI principle 6): identity,
// one line of orientation, and live data from the working directory.
//
// bin is the running executable rather than a name looked up on $PATH, so an
// agent knows exactly what it is talking to. home is the tilde convention
// itself, and cwd shows it in use.
type homeView struct {
	About string                `json:"about"`
	Bin   string                `json:"bin"`
	Cwd   string                `json:"cwd"`
	Home  string                `json:"home"`
	Items []core.DirectoryEntry `json:"items"`
	Total int                   `json:"total"`
}

// tildePath abbreviates a path under the home directory to ~/..., and returns it
// unchanged when it is somewhere else or when the home directory is unknown.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

// buildHomeView assembles the landing payload and the steps that go with it.
//
// It degrades rather than fails. If the working directory cannot be listed —
// most often because it sits outside --allowed-dirs — identity and orientation
// are still returned, with guidance pointing at the restriction. A landing page
// that exits non-zero because the cwd happens to be unreadable would tell an
// agent less than the usage screen it replaces.
func buildHomeView(cwd string) (homeView, []string) {
	view := homeView{
		About: "Fast filesystem operations for an AI agent: read, write, edit, search and manage files.",
		Cwd:   tildePath(cwd),
		Items: []core.DirectoryEntry{},
	}

	if exe, err := os.Executable(); err == nil {
		view.Bin = exe
	} else {
		view.Bin = os.Args[0]
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		view.Home = tildePath(home)
	}

	res, err := core.ListDirectory(core.ListDirectoryOptions{
		Path:        cwd,
		Page:        1,
		PageSize:    homePageSize,
		AllowedDirs: GetAllowedDirs(),
	})
	if err != nil {
		return view, []string{
			"This directory is not listable here: " + err.Error(),
			"See what is permitted: llm-filesystem list-allowed-directories",
			"Full command reference: llm-filesystem --help",
		}
	}

	view.Items = res.Items
	view.Total = res.Total

	return view, []string{
		"List another directory: llm-filesystem list-directory --path <path>",
		"Search file contents: llm-filesystem search-code --path . --pattern <text>",
		"Full command reference: llm-filesystem --help",
	}
}

// runHome renders the landing view through the normal output pipeline, so it
// inherits TOON, sanitization, the help[] block, the minimal projection and
// --fields without re-implementing any of them.
func runHome(_ *cobra.Command, _ []string) {
	cwd, err := os.Getwd()
	if err != nil {
		// Nothing live can be shown and the location is unknowable, so this is
		// the one case that is a genuine failure rather than a degraded view.
		OutputError(fmt.Errorf("cannot determine the working directory: %w", err))
		return
	}

	view, steps := buildHomeView(cwd)

	OutputResultAXI(view,
		// The same spec list-directory uses, so the landing listing is minimal
		// by default and --fields applies to it too.
		map[string][]string{"items": {"name", "type", "size_readable"}},
		func() []string { return steps },
		func() string {
			var sb strings.Builder
			sb.WriteString(view.About + "\n\n")
			sb.WriteString("bin: " + view.Bin + "\n")
			sb.WriteString("cwd: " + view.Cwd + "\n\n")
			for _, e := range view.Items {
				suffix := ""
				if e.IsDir {
					suffix = "/"
				}
				sb.WriteString(fmt.Sprintf("%s%s  %s\n", e.Name, suffix, e.SizeReadable))
			}
			return sb.String()
		})
}
