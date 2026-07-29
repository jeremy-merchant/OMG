// Package ports defines infrastructure capabilities required by the application.
package ports

import (
	"context"
	"io"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/domain/coordination"
	"github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/domain/reservation"
)

// Store is the transactional boundary for canonical state. Every durable
// mutation declares the stable public operation that owns its idempotency key.
type Store interface {
	Read(context.Context, func(Repositories) error) error
	Write(context.Context, domain.IdempotencyKey, string, func(Repositories) (domain.Result, error)) (domain.Receipt, domain.Result, error)
	Backup(context.Context, BackupDestination) (BackupMetadata, error)
	CheckIntegrity(context.Context) (IntegrityReport, error)
	// Scope returns a view whose reads, receipts, and audit facts are isolated
	// to one canonical project.
	Scope(domain.ProjectID) Store
}

// Repositories is deliberately narrow at foundation time; later repository
// capabilities compose here without exposing storage to application handlers.
type Repositories interface {
	Receipts() ReceiptRepository
	Coordination() CoordinationRepository
	Reservations() ReservationRepository
	Git() GitRepository
	Audit() AuditRepository
}

// CoordinationRepository is available only inside Store.Read/Write callbacks.
// Its methods preserve the adapter's transaction boundary.
type CoordinationRepository interface {
	CreateHuman(context.Context, lineage.Human) error
	GetHuman(context.Context, lineage.ID) (lineage.Human, bool, error)
	CreateSession(context.Context, lineage.AgentSession) error
	GetSession(context.Context, lineage.ID) (lineage.AgentSession, bool, error)
	ListSessions(context.Context, lineage.ID) ([]lineage.AgentSession, error)
	IssueToken(context.Context, lineage.DelegationToken) error
	RevokeToken(context.Context, lineage.ID, time.Time) error
	ConsumeToken(context.Context, lineage.ID, lineage.ID, time.Time) error
	FindTokenByVerifier(context.Context, lineage.ID, lineage.ID, lineage.ID) ([]lineage.DelegationToken, error)
	GetToken(context.Context, lineage.ID) (lineage.DelegationToken, bool, error)
	CreateTask(context.Context, lineage.Task) (lineage.Task, error)
	ListTasks(context.Context, domain.ProjectID) ([]lineage.Task, error)
	GetTask(context.Context, lineage.ID) (lineage.Task, bool, error)
	ClaimTask(context.Context, lineage.ID, lineage.ID, time.Time) (lineage.Task, bool, error)
	TransitionTask(context.Context, lineage.ID, lineage.TaskState, []byte, time.Time) (lineage.Task, error)
	CreateRun(context.Context, lineage.TaskRun) error
	GetRun(context.Context, lineage.ID) (lineage.TaskRun, bool, error)
	TransitionRun(context.Context, lineage.ID, lineage.RunState, []byte, time.Time) (lineage.TaskRun, error)
	ListRunsForSession(context.Context, lineage.ID, lineage.ID) ([]lineage.TaskRun, error)
	RecordHeartbeat(context.Context, lineage.Heartbeat) error
	RecordParentLoss(context.Context, lineage.ID, lineage.Heartbeat) (lineage.TaskRun, error)
	CreateProgress(context.Context, coordination.Progress) error
	ListProgress(context.Context, string) ([]coordination.Progress, error)
	GetProgress(context.Context, string) (coordination.Progress, bool, error)
	CreateDependency(context.Context, coordination.Dependency, time.Time) error
	ListDependencies(context.Context, string) ([]coordination.Dependency, error)
	GetDependency(context.Context, string) (coordination.Dependency, bool, error)
	MarkDependencySatisfied(context.Context, string, time.Time, []byte, string) (bool, error)
	HardDependenciesSatisfied(context.Context, string) (bool, error)
	CreateMessage(context.Context, string, coordination.MailMessage) error
	GetMessage(context.Context, string) (coordination.MailMessage, bool, error)
	ListThread(context.Context, string) ([]coordination.MailMessage, error)
	ListInbox(context.Context, string, coordination.RecipientTarget) ([]coordination.MailMessage, error)
	GetDelivery(context.Context, string, coordination.RecipientTarget) (coordination.RecipientDelivery, bool, error)
	GetDeliveryRowID(context.Context, string, coordination.RecipientTarget) (string, bool, error)
	GetDeliveryByID(context.Context, string) (coordination.RecipientDelivery, bool, error)
	SetDelivery(context.Context, coordination.RecipientDelivery) error
	CreateHandoff(context.Context, coordination.Handoff) error
	GetHandoff(context.Context, string) (coordination.Handoff, bool, error)
	ListHandoffs(context.Context, string) ([]coordination.Handoff, error)
	CreateHandoffDecision(context.Context, coordination.HandoffDecision) error
	GetHandoffDecision(context.Context, string) (coordination.HandoffDecision, bool, error)
	GetHandoffDecisionByID(context.Context, string) (coordination.HandoffDecision, bool, error)
	CreateHandoffLifecycleEvent(context.Context, coordination.HandoffLifecycleEvent) error
	GetHandoffLifecycleEventByID(context.Context, string) (coordination.HandoffLifecycleEvent, bool, error)
	ListHandoffLifecycleEvents(context.Context, string) ([]coordination.HandoffLifecycleEvent, error)
	CreateAdoption(context.Context, coordination.Adoption) error
	GetAdoptionByID(context.Context, string) (coordination.Adoption, bool, error)
	LatestGitAdoption(context.Context, string, string) (coordination.Adoption, bool, error)
}

// ReservationRepository is transaction-scoped canonical storage for advisory
// reservations. It does not expose filesystem, Git, or write authority.
type ReservationRepository interface {
	Create(context.Context, domain.ProjectID, reservation.Reservation, time.Time) error
	Get(context.Context, domain.ProjectID, string) (reservation.Reservation, bool, error)
	List(context.Context, domain.ProjectID) ([]reservation.Reservation, error)
	History(context.Context, domain.ProjectID, string) (reservation.ReservationHistory, bool, error)
	Renew(context.Context, domain.ProjectID, string, reservation.RenewalFact, time.Time) error
	Release(context.Context, domain.ProjectID, string, reservation.ReleaseFact) error
	Override(context.Context, domain.ProjectID, string, reservation.OverrideFact) error
	ReleaseForTask(context.Context, domain.ProjectID, lineage.ID, time.Time, string) ([]reservation.Reservation, error)
}

// GitRepository persists immutable, advisory Git scan facts inside one store transaction.
type GitRepository interface {
	CreateSnapshot(context.Context, git.Snapshot) (git.Snapshot, error)
	GetSnapshot(context.Context, domain.ProjectID, string) (git.Snapshot, bool, error)
	LatestSnapshot(context.Context, domain.ProjectID) (git.Snapshot, bool, error)
	History(context.Context, domain.ProjectID) ([]git.Snapshot, error)
	// LatestSequence returns the canonical project-scoped event cursor from
	// the same read transaction as the caller's other repository reads.
	LatestSequence(context.Context, domain.ProjectID) (int64, error)
}

// Scanner observes Git repositories without granting mutation authority.
type Scanner interface {
	Scan(context.Context, string) (git.Observation, error)
}

// GitVerifier resolves exact revisions and proves integration relationships
// through fixed read-only Git commands. It never mutates refs or worktrees.
type GitVerifier interface {
	ResolveRevision(context.Context, string, string) (git.RevisionEvidence, error)
	Reconcile(context.Context, string, string, string, string, string) (git.ReconcileEvidence, error)
}

type ReceiptRepository interface {
	FindReceipt(context.Context, domain.IdempotencyKey) (domain.Receipt, bool, error)
	GetReceipt(context.Context, domain.ReceiptID) (domain.Receipt, bool, error)
	ListReceipts(context.Context) ([]domain.Receipt, error)
}

// AuditCursor is a canonical project-scoped append cursor. It carries no
// payload and is safe for view metadata.
type AuditCursor struct {
	Sequence   int64
	OccurredAt time.Time
}

type AuditRepository interface {
	LatestCursor(context.Context) (AuditCursor, error)
}

type BackupDestination string

type BackupMetadata struct {
	Location string
	Checksum string
}

type BackupInspection struct {
	Checksum      string
	SchemaVersion int
	Integrity     bool
	Compatible    bool
}

type BackupInspector func(context.Context, string, string) (BackupInspection, error)

type IntegrityReport struct {
	Healthy  bool
	Warnings []string
}

// Migration is an immutable, ordered schema change supplied by a store adapter.
type Migration struct {
	Version       int
	SQL           string
	Checksum      string
	AutomaticSafe bool
}

// OpenOptions controls adapter-level opening behavior without exposing an
// application dependency on a concrete storage implementation.
type OpenOptions struct {
	ReadOnly     bool
	ExistingOnly bool
	WALEligible  func(string) bool
	Migrations   []Migration
	Now          func() time.Time
}

// OpenStatus reports adapter discovery without granting authority to mutate
// storage. Exists is false only after a successful existing-only inspection.
type OpenStatus struct {
	Exists      bool
	Pending     []Migration
	JournalMode string
}

// MigrationPlan is immutable discovery data and carries no authority.
type MigrationPlan struct {
	ID                string   `json:"id"`
	Project           string   `json:"project"`
	FromVersion       int      `json:"from_version"`
	ToVersion         int      `json:"to_version"`
	Checksums         []string `json:"checksums"`
	BackupLocation    string   `json:"backup_location"`
	AutomaticEligible bool     `json:"automatic_eligible"`
}

type MigrationAuthorizationKind string

const (
	MigrationAuthorizationHuman         MigrationAuthorizationKind = "human"
	MigrationAuthorizationAutomaticSafe MigrationAuthorizationKind = "automatic_safe_policy"
)

// MigrationApproval binds one exact migration authorization to an exact backup
// and plan. Human and machine-policy evidence are retained as references.
type MigrationApproval struct {
	ApprovalID        string
	ApprovedBy        string
	EvidenceReference string
	PlanID            string
	Project           string
	FromVersion       int
	ToVersion         int
	Checksums         []string
	BackupLocation    string
	BackupChecksum    string
	Command           string
	Timestamp         time.Time
	ExpiresAt         time.Time
	AuthorizationKind MigrationAuthorizationKind
}

// ProjectConfigInitializer creates and validates project-local configuration
// through the platform's filesystem and access-control boundary.
type ProjectConfigInitializer interface {
	InitializeProjectConfig(context.Context, string) error
}

// FoundationStore is the adapter-neutral lifecycle boundary used only by the
// foundation application service.
type FoundationStore interface {
	Store
	io.Closer
	PlanMigrations(context.Context, string) (MigrationPlan, error)
	CreateMigrationBackup(context.Context, MigrationPlan) (BackupMetadata, error)
	ApplyMigrations(context.Context, MigrationPlan, MigrationApproval) error
	EnsureProject(context.Context, domain.ProjectID) error
}

// StoreOpener opens a FoundationStore without making the application depend on
// a particular persistence adapter.
type StoreOpener func(context.Context, string, OpenOptions) (FoundationStore, OpenStatus, error)

// StoreMode identifies how a store location was selected.
type StoreMode string

const (
	StoreModeOverride  StoreMode = "override"
	StoreModeGit       StoreMode = "git"
	StoreModeProject   StoreMode = "project"
	StoreModeWorkspace StoreMode = "workspace"
)

type ResolveRequest struct {
	// ProjectPath and WorkspacePath are explicit CLI selections. StorePath is
	// the explicit CLI store override.
	ProjectPath   string
	WorkspacePath string
	StorePath     string
}

// ResolvedStore contains resolver metadata for diagnostics and store opening.
// Renderers must not expose path fields in default human-facing views.
type ResolvedStore struct {
	Store         Store
	Path          string
	Mode          StoreMode
	Project       domain.ProjectID
	Workspace     domain.WorkspaceID
	ProjectRoot   string
	WorkspaceRoot string
	GitCommonDir  string
}

// NativeSessionResolution is the bounded, render-safe result of an on-demand
// runtime lookup. Private runtime homes and opaque locators never cross this port.
type NativeSessionResolution struct {
	NativeSessionID       string
	StartedAt             *time.Time
	NativeParentSessionID string
	AccessState           lineage.NativeAccessState
}

// NativeSessionResolver delegates a stored private locator only to its matching
// read-only runtime adapter.
type NativeSessionResolver interface {
	Resolve(context.Context, lineage.AgentSession) (NativeSessionResolution, error)
}

type StoreResolver interface {
	Resolve(context.Context, ResolveRequest) (ResolvedStore, error)
}

// PathInspector owns host-filesystem identity and safety checks that cannot be
// expressed with lexical path operations alone.
type PathInspector interface {
	FreshDestination(string) bool
	SameDirectory(string, string) bool
}

type Clock interface {
	Now() time.Time
}

// FS keeps filesystem access explicit and testable.
type FS interface {
	Open(context.Context, string) (io.ReadCloser, error)
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte, uint32) error
	MkdirAll(context.Context, string, uint32) error
}

// Process observes a process without implying it may control it.
type Process interface {
	Observe(context.Context, int) (Liveness, error)
}

type Liveness struct {
	Running bool
}

type Notifier interface {
	Notify(context.Context, Notification) error
}

type Notification struct {
	Recipient domain.ProjectID
	Code      string
	Message   string
}
