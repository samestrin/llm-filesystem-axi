package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

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
