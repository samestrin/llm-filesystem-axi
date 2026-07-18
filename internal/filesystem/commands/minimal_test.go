package commands

import (
	"reflect"
	"testing"
)

func TestResolveFull(t *testing.T) {
	tests := []struct {
		name    string
		flagSet bool
		flagVal bool
		env     string
		want    bool
	}{
		{"default minimal", false, false, "", false},
		{"env 1 enables full", false, false, "1", true},
		{"env true enables full", false, false, "true", true},
		{"env YES case-insensitive", false, false, "YES", true},
		{"env 0 stays minimal", false, false, "0", false},
		{"env garbage stays minimal", false, false, "nope", false},
		{"explicit --full wins over empty env", true, true, "", true},
		{"explicit --full=false overrides env", true, false, "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveFull(tt.flagSet, tt.flagVal, tt.env); got != tt.want {
				t.Errorf("resolveFull(%v,%v,%q) = %v, want %v", tt.flagSet, tt.flagVal, tt.env, got, tt.want)
			}
		})
	}
}

func TestProjectGenericReducesListItems(t *testing.T) {
	// list-directory shape: reduce items[] to name/type/size_readable.
	in := map[string]interface{}{
		"path": "/x",
		"items": []interface{}{
			map[string]interface{}{"name": "a.go", "path": "/x/a.go", "type": "file", "size": 10, "size_readable": "10 B", "mode": "-rw-r--r--"},
			map[string]interface{}{"name": "b.go", "path": "/x/b.go", "type": "file", "size": 20, "size_readable": "20 B", "mode": "-rw-r--r--"},
		},
		"total": 2,
	}
	spec := map[string][]string{"items": {"name", "type", "size_readable"}}
	got := projectGeneric(in, spec)

	gm := got.(map[string]interface{})
	if gm["path"] != "/x" || gm["total"] != 2 {
		t.Errorf("top-level scalars must be preserved: %v", gm)
	}
	items := gm["items"].([]interface{})
	first := items[0].(map[string]interface{})
	want := map[string]interface{}{"name": "a.go", "type": "file", "size_readable": "10 B"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("item[0] = %v, want %v", first, want)
	}
}

func TestProjectGenericPerSpecAvoidsCrossCommandLeak(t *testing.T) {
	// search-files "matches" have a different shape than search-code "matches";
	// the spec is per-command, so only the given keys survive.
	in := map[string]interface{}{
		"matches": []interface{}{
			map[string]interface{}{"path": "/x/a.go", "name": "a.go", "size": 23, "is_dir": false, "mod_time": "T"},
		},
		"total": 1,
	}
	spec := map[string][]string{"matches": {"path", "size"}}
	got := projectGeneric(in, spec).(map[string]interface{})
	m := got["matches"].([]interface{})[0].(map[string]interface{})
	if _, leaked := m["name"]; leaked {
		t.Errorf("unspecified key leaked: %v", m)
	}
	if _, leaked := m["mod_time"]; leaked {
		t.Errorf("unspecified key leaked: %v", m)
	}
	if m["path"] != "/x/a.go" || m["size"] != 23 {
		t.Errorf("kept keys wrong: %v", m)
	}
}

func TestProjectGenericRecursesIntoNestedTree(t *testing.T) {
	// get-directory-tree: children[] nest recursively; each level reduced.
	in := map[string]interface{}{
		"tree": map[string]interface{}{
			"name": "root", "path": ".", "is_dir": true, "size": 100,
			"children": []interface{}{
				map[string]interface{}{"name": "sub", "path": "sub", "is_dir": true, "size": 64,
					"children": []interface{}{
						map[string]interface{}{"name": "deep.go", "path": "sub/deep.go", "is_dir": false, "size": 5},
					},
				},
			},
		},
	}
	spec := map[string][]string{"children": {"name", "is_dir", "size"}}
	got := projectGeneric(in, spec).(map[string]interface{})
	tree := got["tree"].(map[string]interface{})
	child := tree["children"].([]interface{})[0].(map[string]interface{})
	if _, leaked := child["path"]; leaked {
		t.Errorf("nested child kept unspecified key path: %v", child)
	}
	grand := child["children"].([]interface{})[0].(map[string]interface{})
	if _, leaked := grand["path"]; leaked {
		t.Errorf("deep child not projected recursively: %v", grand)
	}
	if grand["name"] != "deep.go" {
		t.Errorf("deep child lost kept key: %v", grand)
	}
}

func TestInjectNextSteps(t *testing.T) {
	in := map[string]interface{}{"total": 0}
	got := injectNextSteps(in, []string{"do X", "try Y"}).(map[string]interface{})
	steps, ok := got["next_steps"].([]string)
	if !ok {
		t.Fatalf("next_steps missing or wrong type: %v", got["next_steps"])
	}
	if len(steps) != 2 || steps[0] != "do X" {
		t.Errorf("next_steps = %v", steps)
	}
	// Empty steps must not add the key.
	got2 := injectNextSteps(map[string]interface{}{"a": 1}, nil).(map[string]interface{})
	if _, present := got2["next_steps"]; present {
		t.Errorf("empty next_steps should not inject a key")
	}
}
