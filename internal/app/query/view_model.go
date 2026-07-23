// Package query defines application-owned, immutable query snapshots.
package query

import (
	"encoding/json"
	"fmt"
)

const ViewVersion = 1

// Absence is explicit: unknown is not missing, and neither is not applicable.
type Absence string

const (
	Unknown       Absence = "unknown"
	NotApplicable Absence = "not_applicable"
	Missing       Absence = "missing"
)

// ViewModel is a versioned, immutable snapshot. Its payload is copied at
// construction and never exposed for mutation; renderers consume only this DTO.
type ViewModel struct {
	kind           string
	snapshotCursor string
	payload        json.RawMessage
}

func NewViewModel(kind, snapshotCursor string, payload json.RawMessage) (ViewModel, error) {
	if kind == "" {
		return ViewModel{}, fmt.Errorf("view kind is required")
	}
	if !json.Valid(payload) {
		return ViewModel{}, fmt.Errorf("view payload must be valid JSON")
	}
	return ViewModel{
		kind:           kind,
		snapshotCursor: snapshotCursor,
		payload:        append(json.RawMessage(nil), payload...),
	}, nil
}

func (v ViewModel) Version() int           { return ViewVersion }
func (v ViewModel) Kind() string           { return v.kind }
func (v ViewModel) SnapshotCursor() string { return v.snapshotCursor }

// Data returns a defensive copy of the canonical redacted payload.
func (v ViewModel) Data() json.RawMessage {
	return append(json.RawMessage(nil), v.payload...)
}

func (v ViewModel) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ViewVersion    int             `json:"view_version"`
		Kind           string          `json:"kind"`
		SnapshotCursor string          `json:"snapshot_cursor"`
		Data           json.RawMessage `json:"data"`
	}{
		ViewVersion:    ViewVersion,
		Kind:           v.kind,
		SnapshotCursor: v.snapshotCursor,
		Data:           v.payload,
	})
}
