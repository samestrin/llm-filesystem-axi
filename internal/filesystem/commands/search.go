package commands

import (
	"fmt"
	"strings"

	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

func addSearchCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(searchFilesCmd())
	rootCmd.AddCommand(searchCodeCmd())
}

func searchFilesCmd() *cobra.Command {
	var path, pattern string
	var recursive, showHidden bool
	var maxResults int

	cmd := &cobra.Command{
		Use:   "search-files",
		Short: "Search for files by name",
		Long:  "Searches for files matching a pattern within a directory",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.SearchFiles(core.SearchFilesOptions{
				Path:        path,
				Pattern:     pattern,
				Recursive:   recursive,
				ShowHidden:  showHidden,
				MaxResults:  maxResults,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResultAXI(result,
				map[string][]string{"matches": {"path", "name", "size"}},
				func() []string {
					// No match means there is nothing to read, so offer the ways
					// to widen instead of naming a file that is not there.
					if result.Total == 0 {
						return []string{
							"Widen the search: drop terms from --pattern, or add --show-hidden",
							"Search contents instead of names: llm-filesystem search-code --path " + result.Path + " --pattern <text>",
						}
					}
					return []string{
						"Read a match: llm-filesystem read-file --path <path>",
						"Add --full for all fields (is_dir, mod_time).",
					}
				},
				func() string {
					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("Found %d files matching '%s' in %s\n\n",
						result.Total, result.Pattern, result.Path))
					for _, m := range result.Matches {
						typeIndicator := ""
						if m.IsDir {
							typeIndicator = " (dir)"
						}
						sb.WriteString(fmt.Sprintf("%s%s  %d bytes\n", m.Path, typeIndicator, m.Size))
					}
					return sb.String()
				})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Directory to search in (required)")
	cmd.Flags().StringVar(&pattern, "pattern", "", "Search pattern (required)")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "Search recursively")
	cmd.Flags().BoolVar(&showHidden, "show-hidden", false, "Include hidden files")
	cmd.Flags().IntVar(&maxResults, "max-results", 1000, "Maximum results to return")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("pattern")

	return cmd
}

func searchCodeCmd() *cobra.Command {
	var path, pattern string
	var caseInsensitive, regex, showHidden bool
	var contextLines, maxResults int
	var fileTypes []string

	cmd := &cobra.Command{
		Use:   "search-code",
		Short: "Search for patterns in file contents",
		Long:  "Searches for patterns in file contents with optional context lines",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.SearchCode(core.SearchCodeOptions{
				Path:            path,
				Pattern:         pattern,
				CaseInsensitive: caseInsensitive,
				Regex:           regex,
				ContextLines:    contextLines,
				FileTypes:       fileTypes,
				MaxResults:      maxResults,
				ShowHidden:      showHidden,
				AllowedDirs:     GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			// CodeMatch carries Context alongside File, Line and Content, and the
			// minimal set omits it — correctly, since it is empty unless asked
			// for. But when --context N asks for it the lines were read and then
			// discarded by the projection, and the command exited 0 having
			// returned output byte-identical to a search with no --context at
			// all. An explicitly requested field is more specific than a default
			// schema and wins, the same way an explicit --max-size beats --full.
			matchFields := []string{"file", "line", "content"}
			if contextLines > 0 {
				matchFields = append(matchFields, "context")
			}
			OutputResultAXI(result,
				map[string][]string{"matches": matchFields},
				func() []string {
					// No match means there is nothing to open, so offer the ways
					// to widen instead of naming a file that is not there.
					if result.TotalMatches == 0 {
						return []string{
							"Widen the search: add --ignore-case, or --regex to treat the pattern as an expression",
							"Search file names instead of contents: llm-filesystem search-files --path " + result.Path + " --pattern <name>",
						}
					}
					steps := []string{"Open a match: llm-filesystem read-file --path <file>"}
					// This used to read "Add --full for surrounding context
					// lines", which returns none: context exists only when
					// --context asked for it, so following that line ran a
					// second search for the identical output.
					if contextLines == 0 {
						steps = append(steps, "Show the lines around each match: add --context 3")
					}
					return steps
				},
				func() string {
					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("Found %d matches in %d files for '%s'\n\n",
						result.TotalMatches, result.TotalFiles, result.Pattern))
					for _, m := range result.Matches {
						sb.WriteString(fmt.Sprintf("%s:%d: %s\n", m.File, m.Line, m.Content))
						if len(m.Context) > 0 {
							sb.WriteString("  Context:\n")
							for _, c := range m.Context {
								sb.WriteString(fmt.Sprintf("    %s\n", c))
							}
						}
					}
					return sb.String()
				})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Directory to search in (required)")
	cmd.Flags().StringVar(&pattern, "pattern", "", "Search pattern (required)")
	cmd.Flags().BoolVar(&caseInsensitive, "ignore-case", false, "Case insensitive search")
	cmd.Flags().BoolVar(&regex, "regex", false, "Use regex pattern")
	cmd.Flags().IntVar(&contextLines, "context", 0, "Lines of context around matches")
	cmd.Flags().StringSliceVar(&fileTypes, "file-types", nil, "File extensions to include")
	cmd.Flags().IntVar(&maxResults, "max-results", 1000, "Maximum results")
	cmd.Flags().BoolVar(&showHidden, "show-hidden", false, "Include hidden files")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("pattern")

	return cmd
}
