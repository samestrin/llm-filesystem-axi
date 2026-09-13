package commands

import (
	"fmt"
	"sort"
	"strings"
)

// normalizeFields trims each requested field name and rejects a blank one, so
// "--fields name,,size" fails loud rather than quietly selecting two fields.
func normalizeFields(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		t := strings.TrimSpace(f)
		if t == "" {
			return nil, fmt.Errorf("--fields contains an empty field name")
		}
		out = append(out, t)
	}
	return out, nil
}

// overrideSpec replaces every array's keep-list with fields.
//
// This is what lets --fields work without the caller knowing which array key a
// command uses: the caller names the fields, the call site names the array. It
// replaces rather than intersects, so a field the call site's minimal view omits
// is still reachable by name.
func overrideSpec(spec map[string][]string, fields []string) map[string][]string {
	out := make(map[string][]string, len(spec))
	for k := range spec {
		out[k] = fields
	}
	return out
}

// availableFields returns the union of item keys present under every spec array
// in the payload — exactly the set --full would expose for this result,
// ,omitempty behaviour included, since it reads the marshalled payload rather
// than the Go struct.
func availableFields(v interface{}, spec map[string][]string) map[string]bool {
	found := map[string]bool{}
	collectFields(v, spec, found)
	return found
}

func collectFields(v interface{}, spec map[string][]string, found map[string]bool) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if _, wanted := spec[k]; wanted {
				if arr, isArr := child.([]interface{}); isArr {
					for _, e := range arr {
						if em, isMap := e.(map[string]interface{}); isMap {
							for key := range em {
								found[key] = true
							}
						}
						collectFields(e, spec, found)
					}
					continue
				}
			}
			collectFields(child, spec, found)
		}
	case []interface{}:
		for _, e := range val {
			collectFields(e, spec, found)
		}
	}
}

// unknownFields returns the requested names the payload does not have, sorted so
// the diagnostic is stable enough to assert against.
func unknownFields(requested []string, available map[string]bool) []string {
	var missing []string
	for _, f := range requested {
		if !available[f] {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	return missing
}

// sortedFieldNames returns the available field names in a stable order, for the
// "available: ..." guidance that teaches the schema in one turn.
func sortedFieldNames(available map[string]bool) []string {
	out := make([]string, 0, len(available))
	for k := range available {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FullEnvVar, when truthy, makes full (non-minimal) output the default so
// legacy consumers can opt out of minimal schemas without passing --full.
const FullEnvVar = "LLM_FILESYSTEM_FULL"

// resolveFull decides whether to emit full field sets. An explicit --full flag
// (set on the command line, including --full=false) always wins; otherwise the
// LLM_FILESYSTEM_FULL environment variable decides; otherwise output is minimal.
func resolveFull(flagSet, flagVal bool, env string) bool {
	if flagSet {
		return flagVal
	}
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// projectGeneric reduces a JSON-generic value (map/slice/scalar) to its minimal
// view. spec maps an array field name to the item keys to keep; wherever a map
// contains one of those keys holding an array, each element is reduced to the
// listed keys. It recurses so nested arrays under the same key names (e.g. a
// directory tree's children) are reduced at every level. The spec is supplied
// per command, so identically-named arrays in different commands never collide.
func projectGeneric(v interface{}, spec map[string][]string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			if keep, ok := spec[k]; ok {
				if arr, isArr := child.([]interface{}); isArr {
					out[k] = projectArray(arr, keep, spec)
					continue
				}
			}
			out[k] = projectGeneric(child, spec)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, e := range val {
			out[i] = projectGeneric(e, spec)
		}
		return out
	default:
		return v
	}
}

// projectArray reduces each element of an array to the keep keys, recursing so
// nested arrays covered by spec are reduced too.
func projectArray(arr []interface{}, keep []string, spec map[string][]string) []interface{} {
	keepSet := make(map[string]bool, len(keep))
	for _, k := range keep {
		keepSet[k] = true
	}
	out := make([]interface{}, len(arr))
	for i, e := range arr {
		em, ok := e.(map[string]interface{})
		if !ok {
			out[i] = projectGeneric(e, spec)
			continue
		}
		reduced := make(map[string]interface{})
		for k, child := range em {
			if keepSet[k] {
				reduced[k] = child
				continue
			}
			// Preserve nested arrays that the spec still wants (e.g. children),
			// even when the key itself is not in this level's keep list.
			if nestedKeep, wanted := spec[k]; wanted {
				if nestedArr, isArr := child.([]interface{}); isArr {
					reduced[k] = projectArray(nestedArr, nestedKeep, spec)
				}
			}
		}
		out[i] = reduced
	}
	return out
}

// injectNextSteps adds a next_steps field to a generic map (AXI principle #9).
// It is a no-op when there are no steps or when v is not a map.
func injectNextSteps(v interface{}, steps []string) interface{} {
	if len(steps) == 0 {
		return v
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	out := make(map[string]interface{}, len(m)+1)
	for k, val := range m {
		out[k] = val
	}
	out["next_steps"] = steps
	return out
}
