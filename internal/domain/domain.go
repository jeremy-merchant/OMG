// Package domain contains stable, transport-neutral vocabulary and safe outcomes.
package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var stableMetadataForbiddenMarkers = [...]string{
	"token", "secret", "password", "credential", "apikey", "bearer", "privatekey",
}

var stableMetadataPrivatePathSegments = [...]string{
	"/users/", "/home/", "/private/", "/tmp/", "/var/folders/", "/volumes/", "/opt/", "/srv/", "/root/",
}

// ContainsSensitiveStableMetadata reports whether value contains credential
// markers or private filesystem locations after normalizing separators. It is
// shared with presentation redaction so equivalent forms are handled alike.
func ContainsSensitiveStableMetadata(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}
	normalizedPath := strings.ToLower(strings.ReplaceAll(value, `\`, "/"))
	for _, marker := range stableMetadataPrivatePathSegments {
		if strings.Contains(normalizedPath, marker) || strings.HasSuffix(normalizedPath, strings.TrimSuffix(marker, "/")) {
			return true
		}
	}
	if strings.HasPrefix(normalizedPath, "/") || strings.HasPrefix(normalizedPath, "~/") ||
		(len(normalizedPath) >= 3 && normalizedPath[1] == ':' && normalizedPath[2] == '/') {
		return true
	}
	compact := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(normalizedPath)
	for _, marker := range stableMetadataForbiddenMarkers {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

// IsSecretFreeStableMetadata reports whether a stable identifier or idempotency
// key is canonical and contains no controls, private paths, or credential
// markers. It must only be used for metadata, never for untrusted content fields.
func IsSecretFreeStableMetadata(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || ContainsSensitiveStableMetadata(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ID types prevent accidental mixing of unrelated identifiers.
type (
	ScopeID          string
	ProjectID        string
	WorkspaceID      string
	ResultID         string
	ReceiptID        string
	IdempotencyKey   string
	OutcomeCode      string
	ErrorCode        string
	InvocationSource string
	Capability       string
)

const (
	OutcomeOK             OutcomeCode = "ok"
	OutcomeAccepted       OutcomeCode = "accepted"
	OutcomeAlreadyApplied OutcomeCode = "already_applied"

	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "conflict"
	CodeUninitialized   ErrorCode = "uninitialized"
	CodeCommandNotWired ErrorCode = "command_not_wired"
	CodeUnavailable     ErrorCode = "unavailable"
	CodeInternal        ErrorCode = "internal"

	InvocationCLI InvocationSource = "cli"
	InvocationMCP InvocationSource = "mcp"

	CapabilityRead  Capability = "read"
	CapabilityWrite Capability = "write"
	CapabilityAdmin Capability = "admin"
)

// DomainError is safe for a transport to serialize. Message must be a fixed,
// user-safe description and must not include raw input, paths, secrets, or tokens.
type DomainError struct {
	Code      ErrorCode
	Message   string
	Retryable bool
}

func NewError(code ErrorCode, message string, retryable bool) DomainError {
	return DomainError{Code: code, Message: message, Retryable: retryable}
}

func (e DomainError) Error() string { return e.Message }

// ActorContext is resolved at the adapter boundary; application code must not
// infer identity, provenance, or authority from command content.
type ActorContext struct {
	Scope      ScopeID
	Project    ProjectID
	Workspace  WorkspaceID
	Invocation InvocationSource
	caps       map[Capability]struct{}
}

func NewActorContext(scope ScopeID, project ProjectID, workspace WorkspaceID, invocation InvocationSource, capabilities []Capability) ActorContext {
	caps := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		caps[capability] = struct{}{}
	}
	return ActorContext{Scope: scope, Project: project, Workspace: workspace, Invocation: invocation, caps: caps}
}

func (a ActorContext) Has(capability Capability) bool {
	_, ok := a.caps[capability]
	return ok
}

// Result is the transport-neutral outcome of a command. Data is an application
// value and warnings are safe, user-facing messages.
type Result struct {
	ID       ResultID
	Outcome  OutcomeCode
	Receipt  ReceiptID
	Data     any
	Warnings []string
}

// Receipt identifies a durable, idempotent command outcome. Operation is the
// stable public command identity that owns the idempotency key.
type Receipt struct {
	ID             ReceiptID
	IdempotencyKey IdempotencyKey
	Operation      string
	Outcome        OutcomeCode
	CreatedAt      time.Time
}
