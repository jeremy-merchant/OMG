// Package lineage defines canonical local coordination facts and transition rules.
package lineage

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
)

type ID string
type LineageKind string
type InstructionSource string
type ProvenanceConfidence string
type TaskState string
type RunState string
type Liveness string
type NativeAccessState string

const (
	HumanDirect             LineageKind          = "human_direct"
	AgentDelegated          LineageKind          = "agent_delegated"
	Resumed                 LineageKind          = "resumed"
	Adopted                 LineageKind          = "adopted"
	Imported                LineageKind          = "imported"
	SourceHuman             InstructionSource    = "human"
	SourceDelegationToken   InstructionSource    = "delegation_token"
	SourceResume            InstructionSource    = "resume"
	SourceAdoption          InstructionSource    = "adoption"
	SourceImport            InstructionSource    = "import"
	ConfidenceExplicit      ProvenanceConfidence = "explicit"
	ConfidenceVerified      ProvenanceConfidence = "verified"
	ConfidenceAsserted      ProvenanceConfidence = "asserted"
	ConfidenceUnknown       ProvenanceConfidence = "unknown"
	TaskReady               TaskState            = "READY"
	TaskClaimed             TaskState            = "CLAIMED"
	TaskInProgress          TaskState            = "IN_PROGRESS"
	TaskWaiting             TaskState            = "WAITING"
	TaskBlocked             TaskState            = "BLOCKED"
	TaskRework              TaskState            = "REWORK"
	TaskWorkComplete        TaskState            = "WORK_COMPLETE"
	TaskVerifiedDone        TaskState            = "VERIFIED_DONE"
	TaskFailed              TaskState            = "FAILED"
	TaskAbandoned           TaskState            = "ABANDONED"
	TaskInterrupted         TaskState            = "INTERRUPTED"
	TaskStale               TaskState            = "STALE"
	TaskCancelled           TaskState            = "CANCELLED"
	RunRunning              RunState             = "RUNNING"
	RunWaiting              RunState             = "WAITING"
	RunBlocked              RunState             = "BLOCKED"
	RunRework               RunState             = "REWORK"
	RunWorkComplete         RunState             = "WORK_COMPLETE"
	RunVerifiedDone         RunState             = "VERIFIED_DONE"
	RunFailed               RunState             = "FAILED"
	RunAbandoned            RunState             = "ABANDONED"
	RunInterrupted          RunState             = "INTERRUPTED"
	RunStale                RunState             = "STALE"
	RunCancelled            RunState             = "CANCELLED"
	Alive                   Liveness             = "alive"
	Stale                   Liveness             = "stale"
	Interrupted             Liveness             = "interrupted"
	NativeAccessAvailable   NativeAccessState    = "available"
	NativeAccessMissing     NativeAccessState    = "missing"
	NativeAccessUnreadable  NativeAccessState    = "unreadable"
	NativeAccessUnsupported NativeAccessState    = "unsupported"
)

var ErrInvalid = errors.New("lineage: invalid record")

type Human struct {
	ID          ID
	ProjectID   ID
	DisplayName string
	Confidence  ProvenanceConfidence
	CreatedAt   time.Time
	Supersedes  ID
}
type AgentSession struct {
	ID                       ID                `json:"id"`
	ProjectID                ID                `json:"project_id"`
	HumanID                  ID                `json:"human_id,omitempty"`
	Kind                     LineageKind       `json:"lineage_kind"`
	Runtime                  string            `json:"runtime"`
	Role                     string            `json:"role"`
	Source                   InstructionSource `json:"instruction_source"`
	SourceRef                string            `json:"-"`
	ParentID                 ID                `json:"parent_session_id,omitempty"`
	RootID                   ID                `json:"root_session_id"`
	ContinuationOfID         ID                `json:"continuation_of_id,omitempty"`
	TaskID                   ID                `json:"task_id,omitempty"`
	WorktreeRef              string            `json:"-"`
	StartedAt                time.Time         `json:"started_at"`
	EndedAt                  *time.Time        `json:"ended_at,omitempty"`
	InterruptedAt            *time.Time        `json:"interrupted_at,omitempty"`
	HeartbeatAt              *time.Time        `json:"heartbeat_at,omitempty"`
	Liveness                 Liveness          `json:"liveness"`
	Supersedes               ID                `json:"supersedes_id,omitempty"`
	NativeAccessState        NativeAccessState `json:"native_access_state"`
	RuntimeHome              string            `json:"-"`
	NativeSessionID          string            `json:"-"`
	NativeSessionRef         string            `json:"-"`
	NativeSessionStartedAt   *time.Time        `json:"-"`
	NativeSessionFingerprint string            `json:"-"`
	NativeParentSessionID    string            `json:"-"`
}

// MaxDelegationTTL limits bearer-token exposure even when an issuer requests more.
const MaxDelegationTTL = time.Hour

type DelegationToken struct {
	ID, ProjectID, TaskID, ParentSessionID ID
	Algorithm                              string
	Iterations                             int
	Salt, Verifier                         []byte
	IssuedAt, ExpiresAt                    time.Time
	RevokedAt, ConsumedAt                  *time.Time
	ConsumedBySessionID                    ID
}
type Task struct {
	ID, ProjectID                                        ID
	DisplayNumber                                        int64
	Title                                                string
	State                                                TaskState
	CreatedBySessionID, ClaimedBySessionID, ParentTaskID ID
	CreatedAt, UpdatedAt                                 time.Time
	Supersedes                                           ID
}
type TaskRun struct {
	ID, TaskID, SessionID ID
	State                 RunState
	Evidence              []byte
	StartedAt             time.Time
	EndedAt, ParentLostAt *time.Time
	Supersedes            ID
}
type Heartbeat struct {
	ID, SessionID ID
	ObservedAt    time.Time
	Liveness      Liveness
	Detail        []byte
}

func nonempty(v string) bool           { return strings.TrimSpace(v) != "" }
func bounded(v string, limit int) bool { return len(v) <= limit }
func utc(t time.Time) bool             { return !t.IsZero() && t.Location() == time.UTC }
func stableID(v ID) bool {
	return v == "" || domain.IsSecretFreeStableMetadata(string(v))
}
func stableIDs(values ...ID) bool {
	for _, value := range values {
		if !stableID(value) {
			return false
		}
	}
	return true
}
func utcOptional(t *time.Time) bool { return t == nil || utc(*t) }
func (h Human) Validate() error {
	if !nonempty(string(h.ID)) || !stableIDs(h.ID, h.ProjectID, h.Supersedes) || !nonempty(h.DisplayName) || !validConfidence(h.Confidence) || !utc(h.CreatedAt) {
		return ErrInvalid
	}
	return nil
}
func (s AgentSession) Validate() error {
	if !nonempty(string(s.ID)) || !stableIDs(s.ID, s.ProjectID, s.HumanID, s.ParentID, s.RootID, s.ContinuationOfID, s.TaskID, s.Supersedes) || !nonempty(string(s.ProjectID)) || !validKind(s.Kind) || !nonempty(s.Runtime) || !bounded(s.Runtime, 64) || !nonempty(s.Role) || !bounded(s.Role, 256) || !validSource(s.Source) || !nonempty(s.SourceRef) || !bounded(s.SourceRef, 1024) || !bounded(s.WorktreeRef, 4096) || !utc(s.StartedAt) || s.RootID == "" || !validNativeAccessState(s.NativeAccessState) || !bounded(s.RuntimeHome, 4096) || !bounded(s.NativeSessionID, 1024) || !bounded(s.NativeSessionRef, 4096) || !bounded(s.NativeParentSessionID, 1024) || !utcOptional(s.NativeSessionStartedAt) {
		return ErrInvalid
	}
	if s.NativeAccessState == NativeAccessUnsupported {
		if s.RuntimeHome != "" || s.NativeSessionID != "" || s.NativeSessionRef != "" || s.NativeSessionStartedAt != nil || s.NativeSessionFingerprint != "" || s.NativeParentSessionID != "" {
			return ErrInvalid
		}
	} else if !nonempty(s.NativeSessionID) || !nonempty(s.NativeSessionRef) || !validNativeSessionFingerprint(s.NativeSessionFingerprint) || s.NativeSessionFingerprint != NativeSessionFingerprint(s.Runtime, s.NativeSessionID, s.NativeSessionRef, s.NativeSessionStartedAt) {
		return ErrInvalid
	}
	switch s.Kind {
	case HumanDirect:
		if s.HumanID == "" || s.ParentID != "" || s.ContinuationOfID != "" || s.Source != SourceHuman || s.RootID != s.ID {
			return ErrInvalid
		}
	case AgentDelegated:
		if s.ParentID == "" || s.Source != SourceDelegationToken {
			return ErrInvalid
		}
	case Resumed:
		if s.ContinuationOfID == "" || s.Source != SourceResume {
			return ErrInvalid
		}
	case Adopted:
		if s.ParentID == "" || s.Source != SourceAdoption {
			return ErrInvalid
		}
	case Imported:
		if s.Source != SourceImport {
			return ErrInvalid
		}
	}
	return nil
}
func NativeSessionFingerprint(runtime, nativeSessionID, nativeSessionRef string, nativeSessionStartedAt *time.Time) string {
	h := sha256.New()
	for _, field := range []string{runtime, nativeSessionID, nativeSessionRef, nativeStartedAtField(nativeSessionStartedAt)} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(field))
	}
	return fmtHex(h.Sum(nil))
}
func nativeStartedAtField(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func fmtHex(v []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(v)*2)
	for i, b := range v {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&15]
	}
	return string(out)
}
func (t DelegationToken) Validate() error { return validToken(t) }
func validToken(t DelegationToken) error {
	if !nonempty(string(t.ID)) || !stableIDs(t.ID, t.ProjectID, t.TaskID, t.ParentSessionID, t.ConsumedBySessionID) || !nonempty(string(t.ProjectID)) || !nonempty(string(t.ParentSessionID)) || t.Algorithm != "PBKDF2-HMAC-SHA256" || t.Iterations < 100000 || len(t.Salt) < 16 || len(t.Verifier) != 32 || !utc(t.IssuedAt) || !utc(t.ExpiresAt) || !t.ExpiresAt.After(t.IssuedAt) || t.ExpiresAt.Sub(t.IssuedAt) > MaxDelegationTTL {
		return ErrInvalid
	}
	return nil
}
func (t Task) Validate() error {
	if !nonempty(string(t.ID)) || !stableIDs(t.ID, t.ProjectID, t.CreatedBySessionID, t.ClaimedBySessionID, t.ParentTaskID, t.Supersedes) || !nonempty(string(t.ProjectID)) || t.DisplayNumber < 1 || !nonempty(t.Title) || !validTaskState(t.State) || !utc(t.CreatedAt) || !utc(t.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}
func (r TaskRun) Validate() error {
	if !nonempty(string(r.ID)) || !stableIDs(r.ID, r.TaskID, r.SessionID, r.Supersedes) || !nonempty(string(r.TaskID)) || !nonempty(string(r.SessionID)) || !validRunState(r.State) || !utc(r.StartedAt) || (r.State == RunVerifiedDone && len(r.Evidence) == 0) {
		return ErrInvalid
	}
	return nil
}
func (h Heartbeat) Validate() error {
	if !nonempty(string(h.ID)) || !stableIDs(h.ID, h.SessionID) || !nonempty(string(h.SessionID)) || !utc(h.ObservedAt) || (h.Liveness != Alive && h.Liveness != Stale && h.Liveness != Interrupted) {
		return ErrInvalid
	}
	return nil
}
func CanTransitionRun(from, to RunState, evidence []byte) bool {
	if to == RunVerifiedDone && len(evidence) == 0 {
		return false
	}
	if from == RunVerifiedDone || from == RunCancelled || from == RunFailed || from == RunAbandoned {
		return false
	}
	if from == RunWorkComplete {
		return to == RunVerifiedDone
	}
	return to == RunRunning || to == RunWaiting || to == RunBlocked || to == RunRework || to == RunWorkComplete || to == RunVerifiedDone || to == RunInterrupted || to == RunStale || to == RunFailed || to == RunAbandoned || to == RunCancelled
}
func CanTransitionTask(from, to TaskState, evidence []byte) bool {
	if to == TaskVerifiedDone && len(evidence) == 0 {
		return false
	}
	if from == TaskVerifiedDone || from == TaskCancelled || from == TaskFailed || from == TaskAbandoned {
		return false
	}
	if from == TaskReady {
		return to == TaskClaimed || to == TaskBlocked || to == TaskCancelled
	}
	if from == TaskClaimed || from == TaskInProgress || from == TaskWaiting || from == TaskBlocked || from == TaskRework || from == TaskInterrupted || from == TaskStale {
		return to == TaskReady || to == TaskInProgress || to == TaskWaiting || to == TaskBlocked || to == TaskRework || to == TaskWorkComplete || to == TaskVerifiedDone || to == TaskFailed || to == TaskAbandoned || to == TaskCancelled
	}
	return from == TaskWorkComplete && to == TaskVerifiedDone
}
func ParentLossState(h Heartbeat) RunState {
	if h.Liveness == Stale {
		return RunStale
	}
	return RunInterrupted
}
func RunHasEnded(state RunState) bool {
	return state == RunWorkComplete || state == RunVerifiedDone || state == RunFailed || state == RunAbandoned || state == RunInterrupted || state == RunStale || state == RunCancelled
}
func validKind(v LineageKind) bool {
	return v == HumanDirect || v == AgentDelegated || v == Resumed || v == Adopted || v == Imported
}
func validNativeAccessState(v NativeAccessState) bool {
	return v == NativeAccessAvailable || v == NativeAccessMissing || v == NativeAccessUnreadable || v == NativeAccessUnsupported
}
func validNativeSessionFingerprint(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func validSource(v InstructionSource) bool {
	return v == SourceHuman || v == SourceDelegationToken || v == SourceResume || v == SourceAdoption || v == SourceImport
}
func validConfidence(v ProvenanceConfidence) bool {
	return v == ConfidenceExplicit || v == ConfidenceVerified || v == ConfidenceAsserted || v == ConfidenceUnknown
}
func validTaskState(v TaskState) bool {
	return v == TaskReady || v == TaskClaimed || v == TaskInProgress || v == TaskWaiting || v == TaskBlocked || v == TaskRework || v == TaskWorkComplete || v == TaskVerifiedDone || v == TaskFailed || v == TaskAbandoned || v == TaskInterrupted || v == TaskStale || v == TaskCancelled
}
func validRunState(v RunState) bool {
	return v == RunRunning || v == RunWaiting || v == RunBlocked || v == RunRework || v == RunWorkComplete || v == RunVerifiedDone || v == RunFailed || v == RunAbandoned || v == RunInterrupted || v == RunStale || v == RunCancelled
}
