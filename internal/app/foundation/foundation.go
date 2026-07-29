// Package foundation composes local platform resolution with the SQLite store.
package foundation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/safety"
)

type Dependencies struct {
	Resolver          ports.StoreResolver
	Open              ports.StoreOpener
	InspectBackup     ports.BackupInspector
	PathInspector     ports.PathInspector
	ConfigInitializer ports.ProjectConfigInitializer
	NativeResolver    ports.NativeSessionResolver
}

type Service struct {
	resolver          ports.StoreResolver
	open              ports.StoreOpener
	inspectBackup     ports.BackupInspector
	pathInspector     ports.PathInspector
	configInitializer ports.ProjectConfigInitializer
	nativeResolver    ports.NativeSessionResolver
}

func New(dependencies Dependencies) *Service {
	return &Service{
		resolver: dependencies.Resolver, open: dependencies.Open, inspectBackup: dependencies.InspectBackup,
		pathInspector: dependencies.PathInspector, configInitializer: dependencies.ConfigInitializer, nativeResolver: dependencies.NativeResolver,
	}
}

type Selection struct{ Project, Workspace, Store string }
type Status struct {
	Pending     int   `json:"pending_migrations"`
	Integrity   *bool `json:"integrity,omitempty"`
	Initialized bool  `json:"initialized"`
}
type Plan = ports.MigrationPlan
type Backup = ports.BackupMetadata

type AutomaticMigration struct {
	Eligible       bool   `json:"eligible"`
	Applied        bool   `json:"applied"`
	PlanID         string `json:"plan_id,omitempty"`
	FromVersion    int    `json:"from_version,omitempty"`
	ToVersion      int    `json:"to_version,omitempty"`
	BackupChecksum string `json:"backup_checksum,omitempty"`
	Integrity      bool   `json:"integrity,omitempty"`
}

type ApprovalFile struct {
	ApprovalID        string    `json:"approval_id"`
	ApprovedBy        string    `json:"approved_by"`
	EvidenceReference string    `json:"evidence_reference"`
	PlanID            string    `json:"plan_id"`
	Project           string    `json:"project"`
	FromVersion       int       `json:"from_version"`
	ToVersion         int       `json:"to_version"`
	Checksums         []string  `json:"checksums"`
	BackupLocation    string    `json:"backup_location"`
	BackupChecksum    string    `json:"backup_checksum"`
	Command           string    `json:"command"`
	Timestamp         string    `json:"timestamp"`
	ExpiresAt         time.Time `json:"-"`
	ExpiresAtRaw      string    `json:"expires_at"`
}

func (s *Service) Init(ctx context.Context, selection Selection) (Status, domain.DomainError) {
	if s == nil || s.configInitializer == nil {
		return Status{}, unavailable()
	}
	projectRoot, err := s.ResolveProjectRoot(ctx, selection)
	if err.Code != "" {
		return Status{}, err
	}
	if initErr := s.configInitializer.InitializeProjectConfig(ctx, projectRoot); initErr != nil {
		return Status{}, unavailable()
	}
	resolved, opened, status, err := s.resolveOpen(ctx, selection)
	if err.Code != "" {
		return Status{}, err
	}
	defer opened.Close()
	if len(status.Pending) == 0 && opened.EnsureProject(ctx, resolved.Project) != nil {
		return Status{}, unavailable()
	}
	return Status{Pending: len(status.Pending), Initialized: true}, domain.DomainError{}
}

func (s *Service) Status(ctx context.Context, selection Selection, integrity bool) (Status, domain.DomainError) {
	_, store, openStatus, initialized, err := s.resolveExistingOpen(ctx, selection, true)
	if err.Code != "" {
		return Status{}, err
	}
	if !initialized {
		return Status{Initialized: false}, domain.DomainError{}
	}
	defer store.Close()
	result := Status{Pending: len(openStatus.Pending), Initialized: true}
	if integrity {
		report, checkErr := store.CheckIntegrity(ctx)
		if checkErr != nil {
			return Status{}, unavailable()
		}
		result.Integrity = &report.Healthy
	}
	return result, domain.DomainError{}
}

func (s *Service) Plan(ctx context.Context, selection Selection) (Plan, domain.DomainError) {
	resolved, store, _, initialized, err := s.resolveExistingOpen(ctx, selection, true)
	if err.Code != "" {
		return Plan{}, err
	}
	if !initialized {
		return Plan{}, domain.NewError(domain.CodeUninitialized, "project is not initialized", false)
	}
	defer store.Close()
	plan, planErr := store.PlanMigrations(ctx, string(resolved.Project))
	if planErr != nil {
		return Plan{}, unavailable()
	}
	return plan, domain.DomainError{}
}

func (s *Service) Backup(ctx context.Context, selection Selection, supplied *Plan) (Backup, domain.DomainError) {
	resolved, store, _, initialized, err := s.resolveExistingOpen(ctx, selection, false)
	if err.Code != "" {
		return Backup{}, err
	}
	if !initialized {
		return Backup{}, domain.NewError(domain.CodeUninitialized, "project is not initialized", false)
	}
	defer store.Close()
	plan := ports.MigrationPlan{}
	if supplied != nil {
		if supplied.Project != string(resolved.Project) {
			return Backup{}, conflict()
		}
		plan = *supplied
	} else {
		var planErr error
		plan, planErr = store.PlanMigrations(ctx, string(resolved.Project))
		if planErr != nil {
			return Backup{}, unavailable()
		}
	}
	backup, backupErr := store.CreateMigrationBackup(ctx, plan)
	if backupErr != nil {
		return Backup{}, conflict()
	}
	return backup, domain.DomainError{}
}

func (s *Service) Apply(ctx context.Context, selection Selection, plan Plan, file ApprovalFile) domain.DomainError {
	if s == nil || s.resolver == nil || s.open == nil {
		return unavailable()
	}
	resolved, resolveErr := s.resolver.Resolve(ctx, ports.ResolveRequest{ProjectPath: selection.Project, WorkspacePath: selection.Workspace, StorePath: selection.Store})
	if resolveErr != nil {
		return unavailable()
	}
	approval, valid := validateApproval(plan, resolved.Project, file, time.Now().UTC())
	if !valid {
		return invalidApproval()
	}
	store, status, openErr := s.open(ctx, resolved.Path, ports.OpenOptions{ExistingOnly: true})
	if openErr != nil || !status.Exists || store == nil {
		return unavailable()
	}
	defer store.Close()
	if applyErr := store.ApplyMigrations(ctx, plan, approval); applyErr != nil {
		return invalidApproval()
	}
	return domain.DomainError{}
}

// AutoMigrate applies only an incremental plan whose every pending migration
// is explicitly declared safe by the compiled adapter. It creates and verifies
// an exact pre-migration backup, records the machine policy authorization, and
// verifies integrity before returning. Fresh initialization and mixed/risky
// plans remain human-gated.
func (s *Service) AutoMigrate(ctx context.Context, selection Selection) (AutomaticMigration, domain.DomainError) {
	if s == nil || s.resolver == nil || s.open == nil {
		return AutomaticMigration{}, unavailable()
	}
	resolved, resolveErr := s.resolver.Resolve(ctx, ports.ResolveRequest{ProjectPath: selection.Project, WorkspacePath: selection.Workspace, StorePath: selection.Store})
	if resolveErr != nil {
		return AutomaticMigration{}, unavailable()
	}
	store, status, openErr := s.open(ctx, resolved.Path, ports.OpenOptions{ExistingOnly: true})
	if openErr != nil || !status.Exists || store == nil {
		return AutomaticMigration{}, domain.NewError(domain.CodeUninitialized, "project is not initialized", false)
	}
	defer store.Close()
	plan, planErr := store.PlanMigrations(ctx, string(resolved.Project))
	if planErr != nil {
		return AutomaticMigration{}, unavailable()
	}
	result := AutomaticMigration{Eligible: plan.AutomaticEligible, PlanID: plan.ID, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion}
	if len(plan.Checksums) == 0 || !plan.AutomaticEligible {
		return result, domain.DomainError{}
	}
	backup, backupErr := store.CreateMigrationBackup(ctx, plan)
	if backupErr != nil {
		return result, unavailable()
	}
	result.BackupChecksum = backup.Checksum
	now := time.Now().UTC()
	approval := ports.MigrationApproval{
		ApprovalID:        "auto-safe-" + plan.ID,
		ApprovedBy:        "omg-auto-safe-policy-v1",
		EvidenceReference: "all-pending-migrations-declared-auto-safe",
		PlanID:            plan.ID,
		Project:           plan.Project,
		FromVersion:       plan.FromVersion,
		ToVersion:         plan.ToVersion,
		Checksums:         append([]string(nil), plan.Checksums...),
		BackupLocation:    backup.Location,
		BackupChecksum:    backup.Checksum,
		Command:           "omg preflight auto-migrate",
		Timestamp:         now,
		ExpiresAt:         now.Add(5 * time.Minute),
		AuthorizationKind: ports.MigrationAuthorizationAutomaticSafe,
	}
	if applyErr := store.ApplyMigrations(ctx, plan, approval); applyErr != nil {
		return result, unavailable()
	}
	report, integrityErr := store.CheckIntegrity(ctx)
	if integrityErr != nil || !report.Healthy {
		return result, unavailable()
	}
	result.Applied = true
	result.Integrity = true
	return result, domain.DomainError{}
}

func validateApproval(plan Plan, target domain.ProjectID, file ApprovalFile, now time.Time) (ports.MigrationApproval, bool) {
	if safety.Reject(file) != nil ||
		!domain.IsSecretFreeStableMetadata(file.ApprovalID) ||
		!domain.IsSecretFreeStableMetadata(file.ApprovedBy) ||
		!domain.IsSecretFreeStableMetadata(file.EvidenceReference) ||
		!strings.HasSuffix(file.Timestamp, "Z") ||
		!strings.HasSuffix(file.ExpiresAtRaw, "Z") ||
		file.PlanID != plan.ID ||
		file.Project != plan.Project ||
		plan.Project != string(target) ||
		file.Project != string(target) ||
		file.FromVersion != plan.FromVersion ||
		file.ToVersion != plan.ToVersion ||
		!sameChecksums(file.Checksums, plan.Checksums) ||
		file.BackupLocation != plan.BackupLocation ||
		strings.TrimSpace(file.BackupChecksum) == "" ||
		file.Command != "omg migration apply" {
		return ports.MigrationApproval{}, false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, file.Timestamp)
	if err != nil || timestamp.Location() != time.UTC {
		return ports.MigrationApproval{}, false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, file.ExpiresAtRaw)
	if err != nil || expiresAt.Location() != time.UTC ||
		!expiresAt.After(timestamp) ||
		expiresAt.Sub(timestamp) > 15*time.Minute ||
		timestamp.After(now) ||
		!expiresAt.After(now) {
		return ports.MigrationApproval{}, false
	}
	return ports.MigrationApproval{
		ApprovalID:        file.ApprovalID,
		ApprovedBy:        file.ApprovedBy,
		EvidenceReference: file.EvidenceReference,
		PlanID:            file.PlanID,
		Project:           file.Project,
		FromVersion:       file.FromVersion,
		ToVersion:         file.ToVersion,
		Checksums:         file.Checksums,
		BackupLocation:    file.BackupLocation,
		BackupChecksum:    file.BackupChecksum,
		Command:           file.Command,
		Timestamp:         timestamp,
		ExpiresAt:         expiresAt,
		AuthorizationKind: ports.MigrationAuthorizationHuman,
	}, true
}

func sameChecksums(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func ReadPlan(data []byte) (Plan, domain.DomainError) {
	var plan Plan
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || decoder.Decode(&struct{}{}) != io.EOF || plan.ID == "" || plan.Project == "" {
		return Plan{}, domain.NewError(domain.CodeInvalidArgument, "invalid migration plan", false)
	}
	return plan, domain.DomainError{}
}
func ReadApproval(data []byte) (ApprovalFile, domain.DomainError) {
	var approval ApprovalFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&approval) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ApprovalFile{}, invalidApproval()
	}
	return approval, domain.DomainError{}
}

// NativeSessionResolver returns the optional read-only runtime adapter registry.
func (s *Service) NativeSessionResolver() ports.NativeSessionResolver {
	if s == nil {
		return nil
	}
	return s.nativeResolver
}

// ResolveProjectRoot returns the canonical selected project root without
// opening or creating canonical state.
func (s *Service) ResolveProjectRoot(ctx context.Context, selection Selection) (string, domain.DomainError) {
	if s == nil || s.resolver == nil {
		return "", unavailable()
	}
	resolved, err := s.resolver.Resolve(ctx, ports.ResolveRequest{ProjectPath: selection.Project, WorkspacePath: selection.Workspace, StorePath: selection.Store})
	if err != nil || resolved.ProjectRoot == "" {
		return "", unavailable()
	}
	return resolved.ProjectRoot, domain.DomainError{}
}

// WithCurrentStore opens an existing selected local store for one application
// operation and closes it before returning. Coordination services use this
// boundary so transports never own database lifecycle or private store paths.
func (s *Service) WithCurrentStore(ctx context.Context, selection Selection, use func(ports.ResolvedStore, ports.Store) error) domain.DomainError {
	return s.withCurrentStore(ctx, selection, false, use)
}

// WithReadOnlyCurrentStore opens an existing selected local store read-only for
// one query operation. It never creates state or applies pending migrations.
func (s *Service) WithReadOnlyCurrentStore(ctx context.Context, selection Selection, use func(ports.ResolvedStore, ports.Store) error) domain.DomainError {
	return s.withCurrentStore(ctx, selection, true, use)
}

func (s *Service) withCurrentStore(ctx context.Context, selection Selection, readOnly bool, use func(ports.ResolvedStore, ports.Store) error) domain.DomainError {
	resolved, store, status, initialized, err := s.resolveExistingOpen(ctx, selection, readOnly)
	if err.Code != "" {
		return err
	}
	if !initialized {
		return domain.NewError(domain.CodeUninitialized, "project is not initialized", false)
	}
	defer store.Close()
	if len(status.Pending) != 0 {
		return domain.NewError(domain.CodeUnavailable, "schema migration is required", false)
	}
	if use == nil {
		return unavailable()
	}
	if useErr := use(resolved, store.Scope(resolved.Project)); useErr != nil {
		var domainErr domain.DomainError
		if errors.As(useErr, &domainErr) {
			return domainErr
		}
		return unavailable()
	}
	return domain.DomainError{}
}

func (s *Service) resolveExistingOpen(ctx context.Context, selection Selection, readOnly bool) (ports.ResolvedStore, ports.FoundationStore, ports.OpenStatus, bool, domain.DomainError) {
	if s == nil || s.resolver == nil || s.open == nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, false, unavailable()
	}
	resolved, resolveErr := s.resolver.Resolve(ctx, ports.ResolveRequest{ProjectPath: selection.Project, WorkspacePath: selection.Workspace, StorePath: selection.Store})
	if resolveErr != nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, false, unavailable()
	}
	store, status, openErr := s.open(ctx, resolved.Path, ports.OpenOptions{
		ReadOnly: readOnly, ExistingOnly: true,
	})
	if openErr != nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, false, unavailableFrom(openErr)
	}
	if !status.Exists {
		return resolved, nil, status, false, domain.DomainError{}
	}
	if store == nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, false, unavailable()
	}
	return resolved, store, status, true, domain.DomainError{}
}

func (s *Service) resolveOpen(ctx context.Context, selection Selection) (ports.ResolvedStore, ports.FoundationStore, ports.OpenStatus, domain.DomainError) {
	if s == nil || s.resolver == nil || s.open == nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, unavailable()
	}
	resolved, resolveErr := s.resolver.Resolve(ctx, ports.ResolveRequest{ProjectPath: selection.Project, WorkspacePath: selection.Workspace, StorePath: selection.Store})
	if resolveErr != nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, unavailable()
	}
	store, status, openErr := s.open(ctx, resolved.Path, ports.OpenOptions{})
	if openErr != nil {
		return ports.ResolvedStore{}, nil, ports.OpenStatus{}, unavailableFrom(openErr)
	}
	return resolved, store, status, domain.DomainError{}
}
func unavailable() domain.DomainError {
	return domain.NewError(domain.CodeUnavailable, "foundation service is unavailable", false)
}

func unavailableFrom(err error) domain.DomainError {
	if err == nil {
		return unavailable()
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "state ancestor extended ACL is not private"),
		strings.Contains(message, "state path DACL is not private"):
		return domain.NewError(domain.CodeUnavailable, "state path is not owner-only because an ancestor grants another account write access", false)
	case strings.Contains(message, "unsafe writable state ancestor"):
		return domain.NewError(domain.CodeUnavailable, "state path is not owner-only because an ancestor is writable by another account", false)
	case strings.Contains(message, "state directory owner is not the current user"),
		strings.Contains(message, "state path owner is not the current user"):
		return domain.NewError(domain.CodeUnavailable, "state path is not owned by the current user", false)
	case strings.Contains(message, "reparse points are not permitted in state paths"),
		strings.Contains(message, "unsafe state parent"),
		strings.Contains(message, "unsafe state path"),
		strings.Contains(message, "unsafe state artifact"):
		return domain.NewError(domain.CodeUnavailable, "state path contains an unsafe filesystem component", false)
	default:
		return unavailable()
	}
}
func conflict() domain.DomainError {
	return domain.NewError(domain.CodeConflict, "migration plan no longer matches current state", false)
}
func invalidApproval() domain.DomainError {
	return domain.NewError(domain.CodeInvalidArgument, "migration approval is invalid", false)
}
