package query

import (
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
)

// BoardSchemaVersion is the stable application-owned snapshot schema consumed by
// every renderer and transport. Renderers must not derive or enrich facts.
const BoardSchemaVersion = 1

type BoardMode string

const (
	BoardMe   BoardMode = "me"
	BoardTree BoardMode = "tree"
	BoardTask BoardMode = "task"
	BoardAll  BoardMode = "all"
	BoardGit  BoardMode = "git"
)

const (
	BoardRedactionPolicyName    = "board_safe_text"
	BoardRedactionPolicyVersion = 1
)

// BoardScope identifies the authorized projection without exposing any local
// filesystem or runtime locator.
type BoardScope struct {
	ProjectID   string    `json:"project_id"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Mode        BoardMode `json:"mode"`
	Selector    string    `json:"selector,omitempty"`
}

// RedactionView declares the fixed policy applied before a board reaches a
// renderer. It intentionally reports policy effects, not omitted content.
type RedactionView struct {
	PolicyName      string `json:"policy_name"`
	PolicyVersion   int    `json:"policy_version"`
	ContentOmitted  bool   `json:"content_omitted"`
	ContentRedacted bool   `json:"content_redacted"`
}

// BoardRequest is a read-only application query. SessionID is required for me,
// and TaskID is required for task. Other modes are project-wide.
type BoardRequest struct {
	Mode      BoardMode `json:"mode"`
	SessionID string    `json:"session_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
}

func (BoardRequest) QueryName() string { return "board" }

// Validate rejects selector combinations that would otherwise silently widen a
// board query. Project-wide modes never accept a selector.
func (r BoardRequest) Validate() error {
	switch r.Mode {
	case BoardMe:
		if r.SessionID == "" || r.TaskID != "" {
			return domain.NewError(domain.CodeInvalidArgument, "me mode requires exactly session", false)
		}
	case BoardTask:
		if r.TaskID == "" || r.SessionID != "" {
			return domain.NewError(domain.CodeInvalidArgument, "task mode requires exactly task", false)
		}
	case BoardTree, BoardAll, BoardGit:
		if r.SessionID != "" || r.TaskID != "" {
			return domain.NewError(domain.CodeInvalidArgument, "board mode does not accept selectors", false)
		}
	default:
		return domain.NewError(domain.CodeInvalidArgument, "board mode is invalid", false)
	}
	return nil
}

type BoardSnapshot struct {
	SchemaVersion    int                   `json:"schema_version"`
	ViewVersion      int                   `json:"view_version"`
	GeneratedAt      time.Time             `json:"generated_at"`
	Scope            BoardScope            `json:"scope"`
	Mode             BoardMode             `json:"mode"`
	ProjectID        string                `json:"project_id"`
	SnapshotCursor   string                `json:"snapshot_cursor"`
	Redaction        RedactionView         `json:"redaction"`
	Identity         *IdentityView         `json:"identity,omitempty"`
	Sessions         []IdentityView        `json:"sessions"`
	Tasks            []TaskView            `json:"tasks"`
	Runs             []RunView             `json:"runs"`
	Progress         []ProgressView        `json:"progress"`
	Dependencies     []DependencyView      `json:"dependencies"`
	Inbox            []InboxItemView       `json:"inbox"`
	Handoffs         []HandoffView         `json:"handoffs"`
	Reservations     []ReservationView     `json:"reservations"`
	Git              *GitView              `json:"git,omitempty"`
	Warnings         []string              `json:"warnings"`
	SuggestedActions []SuggestedActionView `json:"suggested_actions"`
}

// SessionLiveness is the safe public status of a session checkpoint. It never
// exposes checkpoint detail or a runtime locator.
type SessionLiveness string

const (
	SessionLivenessAlive    SessionLiveness = "alive"
	SessionLivenessStale    SessionLiveness = "stale"
	SessionLivenessNoSignal SessionLiveness = "no_signal"
)

// IdentityView is the safe public identity projection used by board renderers
// and exports.

type IdentityView struct {
	ID                   string          `json:"id"`
	Kind                 string          `json:"kind"`
	Runtime              string          `json:"runtime"`
	Role                 string          `json:"role"`
	InstructionSource    string          `json:"instruction_source"`
	ProvenanceConfidence string          `json:"provenance_confidence"`
	HumanID              string          `json:"human_id,omitempty"`
	ParentSessionID      string          `json:"parent_session_id,omitempty"`
	RootSessionID        string          `json:"root_session_id"`
	RootHumanID          string          `json:"root_human_id,omitempty"`
	ContinuationOfID     string          `json:"continuation_of_session_id,omitempty"`
	TaskID               string          `json:"task_id,omitempty"`
	PreviousTaskID       string          `json:"previous_task_id,omitempty"`
	WorktreeBound        bool            `json:"worktree_bound"`
	WorktreeFingerprint  string          `json:"worktree_fingerprint,omitempty"`
	Branch               string          `json:"branch,omitempty"`
	NativeAccessState    string          `json:"native_access_state"`
	StartedAt            time.Time       `json:"started_at"`
	Liveness             SessionLiveness `json:"liveness"`
	HeartbeatAt          *time.Time      `json:"heartbeat_at,omitempty"`
	EndedAt              *time.Time      `json:"ended_at,omitempty"`
	InterruptedAt        *time.Time      `json:"interrupted_at,omitempty"`
}

type TaskView struct {
	ID                 string    `json:"id"`
	DisplayNumber      int64     `json:"display_number"`
	Title              string    `json:"title"`
	State              string    `json:"state"`
	CreatedBySessionID string    `json:"created_by_session_id,omitempty"`
	ClaimedBySessionID string    `json:"claimed_by_session_id,omitempty"`
	ParentTaskID       string    `json:"parent_task_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type RunView struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	SessionID    string     `json:"session_id"`
	State        string     `json:"state"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ParentLostAt *time.Time `json:"parent_lost_at,omitempty"`
}

type ProgressView struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Phase     string    `json:"phase"`
	Done      []string  `json:"done"`
	Doing     []string  `json:"doing"`
	Next      []string  `json:"next"`
	CreatedAt time.Time `json:"created_at"`
}

type DependencyView struct {
	ID              string `json:"id"`
	DependentTaskID string `json:"dependent_task_id"`
	BlockerTaskID   string `json:"blocker_task_id"`
	Type            string `json:"type"`
	UnblockOn       string `json:"unblock_on"`
	Satisfied       bool   `json:"satisfied"`
}

type InboxItemView struct {
	Recipient       string     `json:"recipient"`
	MessageID       string     `json:"message_id"`
	Type            string     `json:"type"`
	Subject         string     `json:"subject"`
	SenderSessionID string     `json:"sender_session_id"`
	RelatedTaskID   string     `json:"related_task_id,omitempty"`
	HandoffID       string     `json:"handoff_id,omitempty"`
	Acknowledgement string     `json:"acknowledgement"`
	ReadAt          *time.Time `json:"read_at,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type HandoffDecisionView struct {
	ID             string    `json:"id"`
	Decision       string    `json:"decision"`
	ActorSessionID string    `json:"actor_session_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type HandoffView struct {
	ID                    string               `json:"id"`
	TaskID                string               `json:"task_id"`
	RunID                 string               `json:"run_id"`
	RunState              string               `json:"run_state"`
	SourceSessionID       string               `json:"source_session_id"`
	TargetSessionID       string               `json:"target_session_id,omitempty"`
	TargetTaskID          string               `json:"target_task_id,omitempty"`
	Summary               string               `json:"summary"`
	FinalOutputPolicy     string               `json:"final_output_policy"`
	FinalOutputHash       string               `json:"final_output_hash,omitempty"`
	ChangedFileCount      int                  `json:"changed_file_count"`
	VerificationItemCount int                  `json:"verification_item_count"`
	Status                string               `json:"status"`
	Decision              *HandoffDecisionView `json:"decision,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
}

type ReservationView struct {
	ID                 string     `json:"id"`
	SessionID          string     `json:"session_id"`
	TaskID             string     `json:"task_id"`
	RunID              string     `json:"run_id"`
	PatternKind        string     `json:"pattern_kind"`
	Pattern            string     `json:"pattern,omitempty"`
	PatternFingerprint string     `json:"pattern_fingerprint"`
	CaseSensitivity    string     `json:"case_sensitivity"`
	Mode               string     `json:"mode"`
	Intent             string     `json:"intent"`
	Lifecycle          string     `json:"lifecycle"`
	ExpiresAt          time.Time  `json:"expires_at"`
	ReleasedAt         *time.Time `json:"released_at,omitempty"`
	ConflictIDs        []string   `json:"conflict_ids"`
}

type GitView struct {
	ObservationID string         `json:"observation_id"`
	ObservedAt    time.Time      `json:"observed_at"`
	Repository    string         `json:"repository"`
	Confidence    string         `json:"confidence"`
	DefaultBranch string         `json:"default_branch,omitempty"`
	Assets        []GitAssetView `json:"assets"`
}

type GitAssetView struct {
	Fingerprint    string   `json:"fingerprint"`
	Type           string   `json:"type"`
	Branch         string   `json:"branch,omitempty"`
	Head           string   `json:"head,omitempty"`
	Upstream       string   `json:"upstream,omitempty"`
	AheadDefault   int      `json:"ahead_default"`
	BehindDefault  int      `json:"behind_default"`
	AheadUpstream  int      `json:"ahead_upstream"`
	BehindUpstream int      `json:"behind_upstream"`
	TrackedDirty   int      `json:"tracked_dirty"`
	UntrackedDirty int      `json:"untracked_dirty"`
	Classification []string `json:"classification"`
	Confidence     string   `json:"confidence"`
	OwnerState     string   `json:"owner_state"`
	OwnerSessionID string   `json:"owner_session_id,omitempty"`
	OwnerTaskID    string   `json:"owner_task_id,omitempty"`
}

type SuggestedActionView struct {
	Code    string     `json:"code"`
	Command string     `json:"command"`
	Argv    []string   `json:"argv"`
	Shell   string     `json:"shell"`
	Scope   BoardScope `json:"scope"`
}

// ReportSnapshot is the canonical export wrapper. The embedded board payload is
// already authorized and redacted; report renderers may only present it.
type ReportSnapshot struct {
	SchemaVersion int           `json:"schema_version"`
	Board         BoardSnapshot `json:"board"`
}
