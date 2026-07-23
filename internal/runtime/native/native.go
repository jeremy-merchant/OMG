// Package native resolves stored native-session locators through registered read-only adapters.
package native

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"
	"unicode"

	"example.invalid/coordledger/internal/domain/lineage"
	"example.invalid/coordledger/internal/ports"
)

const maxRuntimeNameLength = 64

var (
	ErrDuplicateRuntime    = errors.New("native: duplicate runtime")
	ErrInvalidRuntime      = errors.New("native: invalid runtime")
	ErrInvalidContext      = errors.New("native: invalid context")
	ErrInvalidMetadata     = errors.New("native: invalid metadata")
	ErrFingerprintMismatch = errors.New("native: session fingerprint mismatch")
)

// RuntimeName identifies a runtime adapter. Names are case-sensitive and bounded.
type RuntimeName string

// Locator is the stored private locator supplied only to the matching adapter.
// It intentionally has no serializable fields.
type Locator struct {
	runtimeHome      string
	nativeSessionID  string
	nativeSessionRef string
}

func (l Locator) RuntimeHome() string      { return l.runtimeHome }
func (l Locator) NativeSessionID() string  { return l.nativeSessionID }
func (l Locator) NativeSessionRef() string { return l.nativeSessionRef }

// Metadata is the only native-session information an adapter may return.
// It deliberately excludes native references, runtime homes, and content.
type Metadata struct {
	NativeSessionID       string                    `json:"native_session_id,omitempty"`
	StartedAt             *time.Time                `json:"native_session_started_at,omitempty"`
	NativeParentSessionID string                    `json:"native_parent_session_id,omitempty"`
	AccessState           lineage.NativeAccessState `json:"native_access_state"`
}

// Adapter resolves one runtime's explicitly stored native locators. Implementations
// must not probe ambient runtime homes or read or retain transcript content.
type Adapter interface {
	Runtime() RuntimeName
	Resolve(context.Context, Locator) (Metadata, error)
}

// Registry owns the read-only mapping from runtime name to adapter.
type Registry struct {
	adapters map[RuntimeName]Adapter
}

// NewRegistry builds a registry whose runtime names are non-empty and at most 64 bytes.
func NewRegistry(adapters ...Adapter) (*Registry, error) {
	registry := &Registry{adapters: make(map[RuntimeName]Adapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil || !validRuntime(adapter.Runtime()) {
			return nil, ErrInvalidRuntime
		}
		name := adapter.Runtime()
		if _, exists := registry.adapters[name]; exists {
			return nil, ErrDuplicateRuntime
		}
		registry.adapters[name] = adapter
	}
	return registry, nil
}

// Resolution is safe to render or serialize. It never contains private locator data.
type Resolution = ports.NativeSessionResolution

// Resolve maps one stored AgentSession to its matching adapter and verifies available
// metadata against the stored fingerprint before returning it.
func (r *Registry) Resolve(ctx context.Context, session lineage.AgentSession) (Resolution, error) {
	if ctx == nil {
		return Resolution{AccessState: lineage.NativeAccessUnreadable}, ErrInvalidContext
	}
	if session.NativeAccessState == lineage.NativeAccessUnsupported || !validRuntime(RuntimeName(session.Runtime)) {
		return Resolution{AccessState: lineage.NativeAccessUnsupported}, nil
	}
	if session.NativeSessionID == "" || session.NativeSessionRef == "" || session.NativeSessionFingerprint == "" {
		return Resolution{AccessState: lineage.NativeAccessMissing}, nil
	}
	if r == nil {
		return Resolution{AccessState: lineage.NativeAccessUnsupported}, nil
	}
	adapter, exists := r.adapters[RuntimeName(session.Runtime)]
	if !exists {
		return Resolution{AccessState: lineage.NativeAccessUnsupported}, nil
	}

	metadata, err := adapter.Resolve(ctx, Locator{
		runtimeHome:      session.RuntimeHome,
		nativeSessionID:  session.NativeSessionID,
		nativeSessionRef: session.NativeSessionRef,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Resolution{}, err
		}
		return Resolution{AccessState: lineage.NativeAccessUnreadable}, nil
	}
	if metadata.AccessState == lineage.NativeAccessMissing || metadata.AccessState == lineage.NativeAccessUnreadable || metadata.AccessState == lineage.NativeAccessUnsupported {
		return Resolution{AccessState: metadata.AccessState}, nil
	}
	if metadata.AccessState != lineage.NativeAccessAvailable {
		return Resolution{AccessState: lineage.NativeAccessUnreadable}, nil
	}
	if !validMetadata(metadata) {
		return Resolution{AccessState: lineage.NativeAccessUnreadable}, ErrInvalidMetadata
	}

	fingerprint := lineage.NativeSessionFingerprint(session.Runtime, metadata.NativeSessionID, session.NativeSessionRef, metadata.StartedAt)
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(session.NativeSessionFingerprint)) != 1 {
		return Resolution{}, ErrFingerprintMismatch
	}
	return Resolution{
		NativeSessionID:       metadata.NativeSessionID,
		StartedAt:             copyTime(metadata.StartedAt),
		NativeParentSessionID: metadata.NativeParentSessionID,
		AccessState:           lineage.NativeAccessAvailable,
	}, nil
}

func validRuntime(name RuntimeName) bool {
	if len(name) == 0 || len(name) > maxRuntimeNameLength || strings.TrimSpace(string(name)) != string(name) {
		return false
	}
	for _, value := range name {
		if !((value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '.' || value == '_' || value == '-') {
			return false
		}
	}
	return true
}

func validMetadata(metadata Metadata) bool {
	if !validNativeValue(metadata.NativeSessionID, true) || !validNativeValue(metadata.NativeParentSessionID, false) {
		return false
	}
	return metadata.StartedAt == nil || (!metadata.StartedAt.IsZero() && metadata.StartedAt.Location() == time.UTC)
}

func validNativeValue(value string, required bool) bool {
	if len(value) > 1024 || (required && strings.TrimSpace(value) == "") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
