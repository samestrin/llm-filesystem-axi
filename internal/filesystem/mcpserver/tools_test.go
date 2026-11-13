package mcpserver

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/commands"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// freshTree builds the CLI's command tree independently of CommandTree, with
// the help and completion commands cobra only adds at Execute time. The drift
// tests compare the tools against this, so a CommandTree that quietly dropped
// a command would not also hide it from the check.
func freshTree() *cobra.Command {
	root := commands.RootCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	return root
}

func mustTools(t *testing.T) []Tool {
	t.Helper()
	tools, err := Tools(CommandTree())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	return tools
}

// The exclusion list is the one place a CLI command can be kept out of the
// server, so it is pinned literally: widening it would hide a command from MCP
// callers without anyone deciding to (epic 09, Q3). Every name must also
// still exist, or the list is stale and pins nothing.
func TestExcludedCommandsArePinned(t *testing.T) {
	want := []string{"completion", "help", "setup"}
	got := append([]string(nil), ExcludedCommands...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExcludedCommands = %v, want exactly %v", got, want)
	}

	root := freshTree()
	for _, name := range want {
		if c, _, err := root.Find([]string{name}); err != nil || c == root {
			t.Errorf("excluded command %q is not in the CLI's tree; the exclusion list is stale", name)
		}
	}
}

// Defends AC2: every operational command has a tool. A command added to the
// CLI and missing from the server is a capability an MCP caller silently does
// not have.
func TestEveryOperationalCommandHasATool(t *testing.T) {
	excluded := map[string]bool{"completion": true, "help": true, "setup": true}

	byCommand := map[string]Tool{}
	for _, tool := range mustTools(t) {
		byCommand[tool.Command] = tool
	}

	var operational int
	for _, c := range freshTree().Commands() {
		if excluded[c.Name()] {
			if _, ok := byCommand[c.Name()]; ok {
				t.Errorf("excluded command %q has a tool", c.Name())
			}
			continue
		}
		operational++
		if _, ok := byCommand[c.Name()]; !ok {
			t.Errorf("operational command %q has no tool", c.Name())
		}
	}
	// A vacuous pass (an empty tree) would prove nothing.
	if operational < 25 {
		t.Fatalf("only %d operational commands found; the tree is not the CLI's", operational)
	}
	if len(byCommand) != operational {
		t.Errorf("%d tools for %d operational commands", len(byCommand), operational)
	}
	if _, ok := byCommand["llm-filesystem"]; ok {
		t.Error("the bare root has a tool")
	}
}

// lookupFlag finds a flag the command accepts, its own or inherited from the
// root.
func lookupFlag(c *cobra.Command, name string) *pflag.Flag {
	if f := c.Flags().Lookup(name); f != nil {
		return f
	}
	return c.InheritedFlags().Lookup(name)
}

// Defends AC2: no tool names a flag its command lacks. A parameter with no
// flag behind it turns every call that uses it into a usage error — or, worse,
// a call the caller believes was constrained when it was not.
func TestEveryToolParameterIsARealFlag(t *testing.T) {
	root := freshTree()
	for _, tool := range mustTools(t) {
		c, _, err := root.Find([]string{tool.Command})
		if err != nil || c == root {
			t.Errorf("tool %s runs %q, which is not a CLI command", tool.Name, tool.Command)
			continue
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		if len(props) == 0 {
			t.Errorf("tool %s has no parameters", tool.Name)
		}
		for prop, raw := range props {
			flagName := strings.ReplaceAll(prop, "_", "-")
			f := lookupFlag(c, flagName)
			if f == nil {
				t.Errorf("tool %s names %q; %s has no --%s", tool.Name, prop, tool.Command, flagName)
				continue
			}
			if f.Hidden {
				t.Errorf("tool %s exposes hidden flag --%s", tool.Name, flagName)
			}
			want := map[string]string{
				"bool": "boolean", "int": "integer", "string": "string",
				"stringSlice": "array", "intSlice": "array",
			}[f.Value.Type()]
			if got := raw.(map[string]any)["type"]; got != want {
				t.Errorf("tool %s param %s has type %v; --%s is %s", tool.Name, prop, got, flagName, f.Value.Type())
			}
		}
	}
}

// The output is the CLI's TOON, so the format switches belong to the server,
// not the caller. Exposing them would let a call ask for JSON or text and get
// a different contract from the one the server documents.
func TestOutputFormatFlagsAreNotParameters(t *testing.T) {
	for _, tool := range mustTools(t) {
		props := tool.InputSchema["properties"].(map[string]any)
		for _, owned := range []string{"format", "json", "min", "help", "axi"} {
			if _, ok := props[owned]; ok {
				t.Errorf("tool %s exposes %q", tool.Name, owned)
			}
		}
	}
}

// A required flag must be required in the schema too, or a client that
// validates against the schema sends a call the CLI refuses with exit 2.
func TestRequiredFlagsAreRequiredInSchema(t *testing.T) {
	root := freshTree()
	var checked int
	for _, tool := range mustTools(t) {
		c, _, _ := root.Find([]string{tool.Command})
		required := map[string]bool{}
		if r, ok := tool.InputSchema["required"].([]string); ok {
			for _, name := range r {
				required[name] = true
			}
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			isReq := len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0
			prop := strings.ReplaceAll(f.Name, "-", "_")
			if isReq != required[prop] {
				t.Errorf("tool %s: --%s required=%v in the CLI, %v in the schema", tool.Name, f.Name, isReq, required[prop])
			}
			if isReq {
				checked++
			}
		})
	}
	if checked < 20 {
		t.Fatalf("only %d required flags checked; the comparison is not looking at the CLI", checked)
	}
}

func TestToolNamesAndDescriptions(t *testing.T) {
	for _, tool := range mustTools(t) {
		want := "llm_filesystem_" + strings.ReplaceAll(tool.Command, "-", "_")
		if tool.Name != want {
			t.Errorf("tool for %s is named %s, want %s", tool.Command, tool.Name, want)
		}
		if len(tool.Description) < 10 {
			t.Errorf("tool %s has description %q", tool.Name, tool.Description)
		}
		if tool.InputSchema["type"] != "object" {
			t.Errorf("tool %s schema type is %v", tool.Name, tool.InputSchema["type"])
		}
	}
}

// A flag type the server cannot encode must stop the server from listing the
// tool at all, rather than registering a parameter it would pass wrongly.
func TestUnsupportedFlagTypeIsRefused(t *testing.T) {
	root := &cobra.Command{Use: "llm-filesystem"}
	c := &cobra.Command{Use: "odd", Short: "odd command", Run: func(*cobra.Command, []string) {}}
	c.Flags().Float64("ratio", 0, "a float")
	root.AddCommand(c)
	if _, err := Tools(root); err == nil || !strings.Contains(err.Error(), "ratio") {
		t.Fatalf("Tools with a float64 flag: err = %v, want one naming --ratio", err)
	}
}
