// Package mcpserver exposes the llm-filesystem CLI over MCP. It is a thin
// front end: every tool call runs the llm-filesystem binary and returns what it
// printed. Nothing here reimplements a command.
//
// The tools are generated from the CLI's own cobra tree, so a tool's name and
// parameters cannot drift from the command it runs. The tree is read for its
// metadata only; it is never executed in this process.
package mcpserver

import (
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/commands"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ToolPrefix is the prefix on every tool name, as in the rest of the family
// (llm_data_, llm_extract_, ...).
const ToolPrefix = "llm_filesystem_"

// ExcludedCommands are the CLI commands that get no tool. help and completion
// are cobra's own; setup writes .claude/settings.json in the working
// directory, which is the CLI's install step rather than a file operation.
// The bare root has no tool either. TestExcludedCommandsArePinned pins this.
var ExcludedCommands = []string{"completion", "help", "setup"}

// serverOwnedFlags are never parameters. The server always asks for TOON, so
// the output switches are its to set, not the caller's.
var serverOwnedFlags = map[string]bool{"format": true, "json": true, "min": true, "help": true}

// Tool is one MCP tool and the CLI command it runs.
type Tool struct {
	Name        string
	Command     string
	Description string
	InputSchema map[string]any

	params map[string]param
}

type param struct {
	flag string
	kind string // the pflag type: bool, int, string, stringSlice, intSlice
}

var jsonType = map[string]string{
	"bool":        "boolean",
	"int":         "integer",
	"int64":       "integer",
	"string":      "string",
	"stringSlice": "array",
	"intSlice":    "array",
}

// CommandTree returns the CLI's command tree, including the help and
// completion commands cobra otherwise adds only when it executes.
func CommandTree() *cobra.Command {
	root := commands.RootCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	return root
}

// Tools returns a tool for every operational command under root. A flag type
// the server cannot encode is an error rather than a parameter passed wrongly.
func Tools(root *cobra.Command) ([]Tool, error) {
	excluded := map[string]bool{}
	for _, name := range ExcludedCommands {
		excluded[name] = true
	}

	var tools []Tool
	for _, c := range root.Commands() {
		if excluded[c.Name()] || c.Hidden {
			continue
		}
		tool, err := toolFor(c)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func toolFor(c *cobra.Command) (Tool, error) {
	tool := Tool{
		Name:        ToolPrefix + strings.ReplaceAll(c.Name(), "-", "_"),
		Command:     c.Name(),
		Description: c.Long,
		params:      map[string]param{},
	}
	if tool.Description == "" {
		tool.Description = c.Short
	}

	props := map[string]any{}
	var required []string
	var err error
	visit := func(f *pflag.Flag) {
		if err != nil || f.Hidden || serverOwnedFlags[f.Name] {
			return
		}
		name := strings.ReplaceAll(f.Name, "-", "_")
		if _, dup := tool.params[name]; dup {
			return // a local flag shadows the inherited one of the same name
		}
		typ, ok := jsonType[f.Value.Type()]
		if !ok {
			err = fmt.Errorf("%s --%s has type %s, which the MCP server cannot pass", c.Name(), f.Name, f.Value.Type())
			return
		}
		prop := map[string]any{"type": typ, "description": f.Usage}
		switch f.Value.Type() {
		case "stringSlice":
			prop["items"] = map[string]any{"type": "string"}
		case "intSlice":
			prop["items"] = map[string]any{"type": "integer"}
		}
		props[name] = prop
		tool.params[name] = param{flag: f.Name, kind: f.Value.Type()}
		if len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0 {
			required = append(required, name)
		}
	}
	c.LocalFlags().VisitAll(visit)
	c.InheritedFlags().VisitAll(visit)
	if err != nil {
		return Tool{}, err
	}

	sort.Strings(required)
	tool.InputSchema = map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		tool.InputSchema["required"] = required
	}
	return tool, nil
}

// Args turns a call's arguments into the CLI's argv: the command, one
// --flag=value per argument in name order, then --format=toon. An argument
// the tool does not have, or of the wrong type, is an error: dropping it
// would run the command without what the caller asked for and report
// success. A null argument is treated as absent.
func (t Tool) Args(arguments map[string]any) ([]string, error) {
	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)

	argv := []string{t.Command}
	for _, name := range names {
		value := arguments[name]
		if value == nil {
			continue
		}
		p, ok := t.params[name]
		if !ok {
			return nil, fmt.Errorf("%s has no parameter %q; it takes: %s", t.Name, name, strings.Join(t.paramNames(), ", "))
		}
		encoded, err := encode(p.kind, value)
		if err != nil {
			return nil, fmt.Errorf("%s parameter %q: %w", t.Name, name, err)
		}
		argv = append(argv, "--"+p.flag+"="+encoded)
	}
	return append(argv, "--format=toon"), nil
}

func (t Tool) paramNames() []string {
	names := make([]string, 0, len(t.params))
	for name := range t.params {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// encode renders a JSON value as the text pflag parses for a flag of kind.
// JSON numbers arrive as float64, so an integer must have no fraction.
func encode(kind string, value any) (string, error) {
	switch kind {
	case "bool":
		if b, ok := value.(bool); ok {
			return strconv.FormatBool(b), nil
		}
		return "", fmt.Errorf("want a boolean, got %T", value)
	case "string":
		if s, ok := value.(string); ok {
			return s, nil
		}
		return "", fmt.Errorf("want a string, got %T", value)
	case "int", "int64":
		return encodeInt(value)
	case "stringSlice", "intSlice":
		list, ok := value.([]any)
		if !ok {
			return "", fmt.Errorf("want an array, got %T", value)
		}
		items := make([]string, len(list))
		for i, item := range list {
			var err error
			if kind == "intSlice" {
				items[i], err = encodeInt(item)
			} else if s, ok := item.(string); ok {
				items[i] = s
			} else {
				err = fmt.Errorf("want an array of strings, item %d is %T", i, item)
			}
			if err != nil {
				return "", err
			}
		}
		return csvRecord(items)
	}
	return "", fmt.Errorf("unsupported flag type %s", kind)
}

func encodeInt(value any) (string, error) {
	f, ok := value.(float64)
	if !ok || f != math.Trunc(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("want an integer, got %v", value)
	}
	return strconv.FormatInt(int64(f), 10), nil
}

// csvRecord joins items the way pflag's slice flags split them: as one CSV
// record, so an item containing a comma or a quote is quoted and arrives
// whole.
func csvRecord(items []string) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write(items); err != nil {
		return "", err
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n"), w.Error()
}
