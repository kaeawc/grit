package cas

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SchemaVersion identifies the on-disk JSON payload schema for records
// persisted by the CAS (Provenance, ActionResult, CacheSummary).
//
// Bump SchemaVersion when a persisted payload's JSON shape changes in a
// way that older readers cannot tolerate. Old records become unreachable
// (read paths translate the version mismatch into ErrNotFound so callers
// transparently re-execute the producing action) and are reclaimed by
// the next retention pass.
//
// Int rather than string matches the convention used by
// internal/lockfile and internal/configmodel, and lets a legacy payload
// with no schemaVersion field decode to zero — which the mismatch check
// then rejects.
const SchemaVersion = 1

// ErrSchemaMismatch indicates that a persisted record was readable as
// JSON but carries a schemaVersion that does not match the current
// SchemaVersion constant. Store-layer read paths translate this into
// ErrNotFound so that higher layers re-execute the producing action.
var ErrSchemaMismatch = errors.New("cas: schema version mismatch")

// onDiskEnvelope is the wrapper read off disk for every JSON payload
// the CAS persists. Payload is held as json.RawMessage so the envelope
// itself is payload-agnostic and the inner type is decoded only after
// the schemaVersion check passes.
type onDiskEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Payload       json.RawMessage `json:"payload"`
}

// encodeEnvelope serializes payload inside a current-version envelope.
// The on-disk output is pretty-printed to match the project convention
// of human-readable JSON sidecars. A single MarshalIndent pass avoids
// the extra Marshal+RawMessage round-trip that an intermediate buffer
// would require.
func encodeEnvelope(payload any) ([]byte, error) {
	return json.MarshalIndent(struct {
		SchemaVersion int `json:"schemaVersion"`
		Payload       any `json:"payload"`
	}{SchemaVersion: SchemaVersion, Payload: payload}, "", "  ")
}

// decodeEnvelope unwraps data and decodes the inner payload into out.
// It returns ErrSchemaMismatch if the envelope's schemaVersion does
// not match SchemaVersion. The caller is responsible for translating
// ErrSchemaMismatch into a domain-appropriate not-found signal.
func decodeEnvelope(data []byte, out any) error {
	var env onDiskEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d want %d", ErrSchemaMismatch, env.SchemaVersion, SchemaVersion)
	}
	return json.Unmarshal(env.Payload, out)
}

// readOrNotFound reads path, returning ErrNotFound when the file does
// not exist. Other I/O errors are returned unwrapped so callers can
// surface them verbatim.
func readOrNotFound(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}
