package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	goaxi "github.com/samestrin/go-axi"
)

// decodePayload decodes a JSON flag value strictly.
//
// json.Unmarshal ignores keys it does not know, so a payload keyed
// {"path","text"} decoded to an entry with empty content and
// write-multiple-files truncated the file it named, then reported success.
// Every JSON flag goes through here instead, so a wrong key is refused
// before anything runs.
func decodePayload(raw string, v any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after the JSON array")
	}
	return nil
}

// decodeEntries decodes a JSON array flag one entry at a time, so an error
// names the entry it is in: noun and a number counted from 1 ("edit 2"). In a
// long payload the decoder's own `json: unknown field "x"` left the caller
// guessing which entry to fix.
//
// Keys must match T's json tags exactly. encoding/json matches them
// case-insensitively, so {"Path","Content"} was accepted although the
// documented keys are lower case; that is checked here before decoding.
func decodeEntries[T any](raw, noun string) ([]T, error) {
	var items []json.RawMessage
	if err := decodePayload(raw, &items); err != nil {
		return nil, err
	}
	known := jsonKeys(reflect.TypeOf((*T)(nil)).Elem())
	out := make([]T, len(items))
	for i, item := range items {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return nil, fmt.Errorf("%s %d: %s", noun, i+1, trimJSONPrefix(err))
		}
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, ok := known[k]; ok {
				continue
			}
			for want := range known {
				if strings.EqualFold(k, want) {
					return nil, fmt.Errorf("%s %d: unknown key %q (keys are case-sensitive: %q)", noun, i+1, k, want)
				}
			}
			return nil, fmt.Errorf("%s %d: unknown key %q", noun, i+1, k)
		}
		if err := decodePayload(string(item), &out[i]); err != nil {
			return nil, fmt.Errorf("%s %d: %s", noun, i+1, trimJSONPrefix(err))
		}
	}
	return out, nil
}

// jsonKeys returns the json key of every field of struct type t.
func jsonKeys(t reflect.Type) map[string]struct{} {
	keys := map[string]struct{}{}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = t.Field(i).Name
		}
		keys[name] = struct{}{}
	}
	return keys
}

func trimJSONPrefix(err error) string {
	return strings.TrimPrefix(err.Error(), "json: ")
}

// maxFailedListed caps how many failed entries a help line spells out, so a
// run that fails thousands of entries does not print thousands of reasons.
const maxFailedListed = 10

// failedEntrySteps is the help for a run in which some entries failed: which
// ones and why, then how to retry only those. The entries that worked are
// done, and re-running the whole payload would repeat them. nums are the
// failed entries counted from 1; details describes each.
func failedEntrySteps(flag, singular, plural string, nums []int, details []string) []string {
	if len(nums) == 0 {
		return nil
	}
	shown := details
	if len(shown) > maxFailedListed {
		shown = shown[:maxFailedListed]
	}
	failed := fmt.Sprintf("Failed (%d): %s", len(nums), strings.Join(shown, "; "))
	if more := len(details) - len(shown); more > 0 {
		failed += fmt.Sprintf("; and %d more", more)
	}
	strs := make([]string, len(nums))
	for i, n := range nums {
		strs[i] = fmt.Sprint(n)
	}
	noun := singular
	if len(nums) > 1 {
		noun = plural
	}
	return []string{
		failed,
		fmt.Sprintf("Fix the cause, then re-run with only %s %s in --%s", noun, strings.Join(strs, ", "), flag),
	}
}

// refusePayload reports a malformed JSON flag as a usage error.
//
// Nothing was attempted, so exit 2 ("fix the invocation") rather than exit 1,
// which would tell the agent the tool tried and broke. The next step shows the
// expected shape: the generic --help pointer would send the agent to read the
// flag list for a problem that is inside the value.
func refusePayload(prefix string, err error, example string) {
	emitDiagnostic(activeFmt, activeCompact,
		fmt.Errorf("%s: %w", prefix, err),
		goaxi.ExitUsage,
		[]string{"Fix the JSON to this shape and re-run: " + example})
}
