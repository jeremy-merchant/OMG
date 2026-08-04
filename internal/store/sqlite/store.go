// Package sqlite provides the CGO-free canonical SQLite implementation.
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	_ "modernc.org/sqlite"
)

const (
	busyTimeoutMS                    = 5000
	sqlitePrimaryBusy                = 5
	sqlitePrimaryLocked              = 6
	migrationApplyCommand            = "omg migration apply"
	automaticMigrationApplyCommand   = "omg preflight auto-migrate"
	automaticMigrationPolicyActor    = "omg-automatic-backup-policy-v1"
	automaticMigrationPolicyEvidence = "compiled-plan-backed-up-and-integrity-checked"
)

var (
	ErrPendingMigrations = errors.New("sqlite: pending migrations")
	ErrMigrationApproval = errors.New("sqlite: migration approval does not match plan")
	ErrMigrationState    = errors.New("sqlite: invalid migration state")
)

type Migration = ports.Migration
type OpenOptions = ports.OpenOptions
type OpenStatus = ports.OpenStatus

// SQLiteStore implements ports.Store and explicit migration operations.
type SQLiteStore struct {
	db              *sql.DB
	path            string
	migrations      []Migration
	now             func() time.Time
	journalMode     string
	journalFallback string
}

var _ ports.Store = (*SQLiteStore)(nil)

// Scope returns a project-isolated view of this shared SQLite store.
func (s *SQLiteStore) Scope(project domain.ProjectID) ports.Store {
	return scopedStore{store: s, project: project}
}

type scopedStore struct {
	store   *SQLiteStore
	project domain.ProjectID
}

func (s scopedStore) Scope(project domain.ProjectID) ports.Store {
	return scopedStore{store: s.store, project: project}
}
func (s scopedStore) Read(ctx context.Context, fn func(ports.Repositories) error) error {
	return s.store.read(ctx, s.project, fn)
}
func (s scopedStore) Write(ctx context.Context, key domain.IdempotencyKey, operation string, fn func(ports.Repositories) (domain.Result, error)) (domain.Receipt, domain.Result, error) {
	return s.store.write(ctx, s.project, key, operation, fn)
}
func (s scopedStore) Backup(ctx context.Context, destination ports.BackupDestination) (ports.BackupMetadata, error) {
	return s.store.Backup(ctx, destination)
}
func (s scopedStore) CheckIntegrity(ctx context.Context) (ports.IntegrityReport, error) {
	return s.store.CheckIntegrity(ctx)
}

// Open inspects or creates the state location and configures SQLite without
// applying schema migrations. ExistingOnly performs safe inspection only and
// returns an absent status without creating state.

// EnsureProject enrolls a resolved project exactly once after schema readiness.
func (s *SQLiteStore) EnsureProject(ctx context.Context, project domain.ProjectID) error {
	if project == "" {
		return errors.New("sqlite: project is required")
	}
	if err := s.requireReady(ctx, s.db); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,created_at) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, project, s.now().UTC().Format(time.RFC3339Nano))
	return err
}
func Open(ctx context.Context, path string, options OpenOptions) (*SQLiteStore, OpenStatus, error) {
	if err := secureStatePath(path, !options.ReadOnly && !options.ExistingOnly); err != nil {
		return nil, OpenStatus{}, err
	}
	if options.ExistingOnly {
		exists, err := stateFileExists(path)
		if err != nil {
			return nil, OpenStatus{}, err
		}
		if !exists {
			return nil, OpenStatus{Exists: false}, nil
		}
	}
	migrations, err := normalizeMigrations(options.Migrations)
	if err != nil {
		return nil, OpenStatus{}, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	dsn := sqliteURI(path, options.ReadOnly, options.ExistingOnly)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, OpenStatus{}, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	store := &SQLiteStore{db: db, path: path, migrations: migrations, now: now}
	if options.ReadOnly {
		store.journalMode = "readonly"
	} else {
		if err := store.configure(ctx, options.WALEligible); err != nil {
			db.Close()
			return nil, OpenStatus{}, err
		}
		var artifactErr error
		if options.ExistingOnly {
			artifactErr = secureStatePath(path, false)
		} else {
			artifactErr = secureStateArtifacts(path)
		}
		if artifactErr != nil {
			db.Close()
			return nil, OpenStatus{}, artifactErr
		}
	}
	pending, err := store.pending(ctx)
	if err != nil {
		db.Close()
		return nil, OpenStatus{}, err
	}
	if store.journalFallback != "" && len(pending) == 0 {
		if err := store.appendJournalFallback(ctx); err != nil {
			db.Close()
			return nil, OpenStatus{}, err
		}
	}
	return store, OpenStatus{Exists: true, Pending: pending, JournalMode: store.journalMode}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) configure(ctx context.Context, walEligible func(string) bool) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var fk int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		if err != nil {
			return fmt.Errorf("sqlite: verify foreign keys: %w", err)
		}
		return errors.New("sqlite: foreign keys unavailable")
	}
	var timeout int
	if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != busyTimeoutMS {
		if err != nil {
			return fmt.Errorf("sqlite: verify busy timeout: %w", err)
		}
		return errors.New("sqlite: busy timeout unavailable")
	}
	desired := "DELETE"
	if walEligible != nil {
		if walEligible(s.path) {
			desired = "WAL"
		} else {
			s.journalFallback = "wal ineligible for state path"
		}
	}
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode="+desired).Scan(&mode); err != nil {
		return err
	}
	mode = strings.ToLower(mode)
	if desired == "WAL" && mode == "wal" && sidecarsPossible(filepath.Dir(s.path)) {
		s.journalMode = "wal"
		return nil
	}
	if desired == "WAL" {
		if mode != "wal" {
			s.journalFallback = "sqlite returned " + mode
		} else {
			s.journalFallback = "wal sidecars unavailable"
		}
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
			return err
		}
	}
	if strings.ToLower(mode) != "delete" {
		return errors.New("sqlite: rollback journal unavailable")
	}
	s.journalMode = "delete"
	return nil
}

func (s *SQLiteStore) appendJournalFallback(ctx context.Context) error {
	return appendJournalFallback(ctx, s.db, s.journalFallback, s.now)
}

func appendJournalFallback(ctx context.Context, q queryer, reason string, now func() time.Time) error {
	payload, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return err
	}
	at := now().UTC()
	_, err = q.ExecContext(ctx, `INSERT INTO audit_events(id,receipt_id,event_type,payload_json,occurred_at) VALUES(?,?,?,?,?)`, newID("event", "journal_mode_fallback", at), nil, "journal_mode_fallback", payload, at.Format(time.RFC3339Nano))
	return err
}

func sidecarsPossible(dir string) bool {
	probe, err := os.CreateTemp(dir, ".omg-sidecar-probe-")
	if err != nil {
		return false
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return false
	}
	return os.Remove(name) == nil
}

// Read runs a read-only repository callback. It refuses a database whose
// foundation migration has not been explicitly applied.
func (s *SQLiteStore) Read(ctx context.Context, fn func(ports.Repositories) error) error {
	return s.read(ctx, domain.ProjectID("legacy"), fn)
}

func (s *SQLiteStore) read(ctx context.Context, project domain.ProjectID, fn func(ports.Repositories) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.requireReady(ctx, tx); err != nil {
		return err
	}
	if err := fn(repositories{tx: tx, project: project}); err != nil {
		return err
	}
	return tx.Commit()
}

// Write atomically stores the callback outcome, one audit event, and a receipt.
// A duplicate idempotency key returns the previously persisted result without
// invoking the callback or appending another event.
func (s *SQLiteStore) Write(ctx context.Context, key domain.IdempotencyKey, operation string, fn func(ports.Repositories) (domain.Result, error)) (domain.Receipt, domain.Result, error) {
	return s.write(ctx, domain.ProjectID("legacy"), key, operation, fn)
}

func (s *SQLiteStore) write(ctx context.Context, project domain.ProjectID, key domain.IdempotencyKey, operation string, fn func(ports.Repositories) (domain.Result, error)) (domain.Receipt, domain.Result, error) {
	if key == "" || project == "" || operation == "" {
		return domain.Receipt{}, domain.Result{}, domain.NewError(domain.CodeInvalidArgument, "idempotency key and operation are required", false)
	}
	if !domain.IsSecretFreeStableMetadata(string(key)) || !validOperation(operation) {
		return domain.Receipt{}, domain.Result{}, domain.NewError(domain.CodeInvalidArgument, "invalid idempotency receipt metadata", false)
	}
	for attempt := range 8 {
		receipt, result, retry, err := s.writeOnce(ctx, project, key, operation, fn)
		if !retry || err == nil {
			return receipt, result, err
		}
		if err := sleepRetry(ctx, key, attempt); err != nil {
			return domain.Receipt{}, domain.Result{}, err
		}
	}
	return domain.Receipt{}, domain.Result{}, domain.NewError(domain.CodeUnavailable, "store is busy; retry the command", true)
}

func validOperation(operation string) bool {
	for i := range len(operation) {
		c := operation[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		if i > 0 && ((c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			continue
		}
		return false
	}
	return operation != ""
}

func (s *SQLiteStore) writeOnce(ctx context.Context, project domain.ProjectID, key domain.IdempotencyKey, operation string, fn func(ports.Repositories) (domain.Result, error)) (domain.Receipt, domain.Result, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Receipt{}, domain.Result{}, transient(err), err
	}
	defer tx.Rollback()
	if err := s.requireReady(ctx, tx); err != nil {
		return domain.Receipt{}, domain.Result{}, false, err
	}
	if receipt, result, found, err := findReceipt(ctx, tx, project, key); err != nil {
		return domain.Receipt{}, domain.Result{}, transient(err), err
	} else if found {
		if receipt.Operation != operation {
			return domain.Receipt{}, domain.Result{}, false, domain.NewError(domain.CodeConflict, "idempotency key belongs to a different operation", false)
		}
		return receipt, result, false, tx.Commit()
	}
	result, err := fn(repositories{tx: tx, project: project})
	if err != nil {
		return domain.Receipt{}, domain.Result{}, transient(err), err
	}
	createdAt := s.now().UTC()
	receipt := domain.Receipt{ID: domain.ReceiptID(newID("receipt", string(project)+"\x00"+string(key), createdAt)), IdempotencyKey: key, Operation: operation, Outcome: result.Outcome, CreatedAt: createdAt}
	result.Receipt = receipt.ID
	encoded, err := json.Marshal(result)
	if err != nil {
		return domain.Receipt{}, domain.Result{}, false, err
	}
	now := createdAt.Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO command_receipts(id,project_id,idempotency_key,operation,outcome,result_json,created_at) VALUES(?,?,?,?,?,?,?)`, receipt.ID, project, key, receipt.Operation, receipt.Outcome, encoded, now); err != nil {
		return domain.Receipt{}, domain.Result{}, transient(err), err
	}
	payload, _ := json.Marshal(map[string]string{"receipt_id": string(receipt.ID), "outcome": string(receipt.Outcome)})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,project_id,receipt_id,event_type,payload_json,occurred_at) VALUES(?,?,?,?,?,?)`, newID("event", string(receipt.ID), s.now()), project, receipt.ID, "command_completed", payload, now); err != nil {
		return domain.Receipt{}, domain.Result{}, transient(err), err
	}
	if err = tx.Commit(); err != nil {
		return domain.Receipt{}, domain.Result{}, transient(err), err
	}
	return receipt, result, false, nil
}

func sleepRetry(ctx context.Context, key domain.IdempotencyKey, attempt int) error {
	timer := time.NewTimer(retryDelay(key, attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryDelay(key domain.IdempotencyKey, attempt int) time.Duration {
	sum := sha256.Sum256([]byte(string(key)))
	return time.Duration(15*(1<<attempt)+int(sum[attempt])%11) * time.Millisecond
}

func transient(err error) bool {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		// SQLite extended result codes retain the primary result code in the
		// least-significant byte. Classify BUSY and LOCKED without depending on
		// driver-specific error strings; keep the string fallback for adapters
		// that do not expose a result code.
		switch coded.Code() & 0xff {
		case sqlitePrimaryBusy, sqlitePrimaryLocked:
			return true
		default:
			return false
		}
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "database is busy") || strings.Contains(text, "sqlite_busy") || strings.Contains(text, "sqlite_locked")
}

func (s *SQLiteStore) Backup(ctx context.Context, destination ports.BackupDestination) (ports.BackupMetadata, error) {
	destinationPath := string(destination)
	if err := secureStatePath(destinationPath, true); err != nil {
		return ports.BackupMetadata{}, err
	}
	// VACUUM INTO is SQLite's consistent online backup primitive; the source
	// remains open and no restore is attempted here.
	escaped := strings.ReplaceAll(sqliteFileURI(destinationPath), "'", "''")
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO '"+escaped+"'"); err != nil {
		return ports.BackupMetadata{}, err
	}
	if err := secureStateArtifacts(destinationPath); err != nil {
		return ports.BackupMetadata{}, err
	}
	healthy, err := integrityPath(ctx, destinationPath)
	if err != nil {
		return ports.BackupMetadata{}, err
	}
	if !healthy {
		return ports.BackupMetadata{}, errors.New("sqlite: backup integrity check failed")
	}
	checksum, err := fileChecksum(destinationPath)
	if err != nil {
		return ports.BackupMetadata{}, err
	}
	return ports.BackupMetadata{Location: destinationPath, Checksum: checksum}, nil
}

func (s *SQLiteStore) CheckIntegrity(ctx context.Context) (ports.IntegrityReport, error) {
	healthy, err := integrityDB(ctx, s.db)
	if err != nil {
		return ports.IntegrityReport{}, err
	}
	return ports.IntegrityReport{Healthy: healthy}, nil
}

type MigrationPlan = ports.MigrationPlan
type Approval = ports.MigrationApproval

func (s *SQLiteStore) PlanMigrations(ctx context.Context, project string) (MigrationPlan, error) {
	pending, from, err := s.pendingWithVersion(ctx)
	if err != nil {
		return MigrationPlan{}, err
	}
	checksums := make([]string, len(pending))
	to := from
	automaticEligible := len(pending) > 0
	for i, migration := range pending {
		checksums[i] = migration.Checksum
		to = migration.Version
	}
	backup := filepath.Join(filepath.Dir(s.path), "backups", "migration-"+planHash(project, from, to, checksums)+".db")
	return MigrationPlan{ID: planHash(project, from, to, checksums), Project: project, FromVersion: from, ToVersion: to, Checksums: checksums, BackupLocation: backup, AutomaticEligible: automaticEligible}, nil
}

// CreateMigrationBackup creates and verifies the backup bound to a plan. It
// does not apply any migration and refuses a stale plan.
func (s *SQLiteStore) CreateMigrationBackup(ctx context.Context, plan MigrationPlan) (ports.BackupMetadata, error) {
	current, err := s.PlanMigrations(ctx, plan.Project)
	if err != nil {
		return ports.BackupMetadata{}, err
	}
	if !samePlan(current, plan) {
		return ports.BackupMetadata{}, ErrMigrationState
	}
	return s.Backup(ctx, ports.BackupDestination(plan.BackupLocation))
}

// ApplyMigrations validates the exact plan and approval binding, then applies
// each pending migration and its metadata record transactionally.
func (s *SQLiteStore) ApplyMigrations(ctx context.Context, plan MigrationPlan, approval Approval) error {
	current, err := s.PlanMigrations(ctx, plan.Project)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if !samePlan(current, plan) || !approvalMatches(plan, approval) || approval.Timestamp.After(now) || !approval.ExpiresAt.After(now) {
		return ErrMigrationApproval
	}
	checksum, err := fileChecksum(approval.BackupLocation)
	if err != nil || checksum != approval.BackupChecksum {
		return ErrMigrationApproval
	}
	healthy, err := integrityPath(ctx, approval.BackupLocation)
	if err != nil || !healthy {
		return ErrMigrationApproval
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, migration := range s.migrations {
		if migration.Version <= plan.FromVersion {
			continue
		}
		if migration.Version > plan.ToVersion {
			break
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("sqlite: apply migration %d: %w", migration.Version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)`, migration.Version, migration.Checksum, s.now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("sqlite: record migration %d: %w", migration.Version, err)
		}
	}
	now = s.now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,created_at) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, plan.Project, now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("sqlite: bootstrap migration project: %w", err)
	}
	checksums, err := json.Marshal(approval.Checksums)
	if err != nil {
		return ErrMigrationApproval
	}
	authorizationKind := approval.AuthorizationKind
	if authorizationKind == "" {
		authorizationKind = ports.MigrationAuthorizationHuman
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO migration_approvals(approval_id,plan_id,project_id,approved_by,evidence_reference,from_version,to_version,checksums_json,backup_location,backup_checksum,command,approved_at,expires_at,consumed_at,authorization_kind) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, approval.ApprovalID, approval.PlanID, approval.Project, approval.ApprovedBy, approval.EvidenceReference, approval.FromVersion, approval.ToVersion, checksums, approval.BackupLocation, approval.BackupChecksum, approval.Command, approval.Timestamp.Format(time.RFC3339Nano), approval.ExpiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), authorizationKind); err != nil {
		return ErrMigrationApproval
	}
	operation := "migration.apply"
	eventType := "migration_applied"
	idempotencyPrefix := "migration-apply"
	if authorizationKind == ports.MigrationAuthorizationAutomaticSafe {
		operation = "migration.auto_apply"
		eventType = "migration_auto_applied"
		idempotencyPrefix = "migration-auto-apply"
	}
	receiptID := newID("receipt", idempotencyPrefix+"\x00"+approval.ApprovalID, now)
	receiptJSON, _ := json.Marshal(map[string]string{"approval_id": approval.ApprovalID, "authorization_kind": string(authorizationKind), "plan_id": approval.PlanID, "outcome": "applied"})
	if _, err := tx.ExecContext(ctx, `INSERT INTO command_receipts(id,project_id,idempotency_key,operation,outcome,result_json,created_at) VALUES(?,?,?,?,?,?,?)`, receiptID, approval.Project, idempotencyPrefix+":"+approval.ApprovalID, operation, domain.OutcomeOK, receiptJSON, now.Format(time.RFC3339Nano)); err != nil {
		return ErrMigrationApproval
	}
	auditJSON, _ := json.Marshal(map[string]string{"approval_id": approval.ApprovalID, "authorization_kind": string(authorizationKind), "plan_id": approval.PlanID, "from_version": fmt.Sprint(approval.FromVersion), "to_version": fmt.Sprint(approval.ToVersion)})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id,project_id,receipt_id,event_type,payload_json,occurred_at) VALUES(?,?,?,?,?,?)`, newID("event", approval.ApprovalID, now), approval.Project, receiptID, eventType, auditJSON, now.Format(time.RFC3339Nano)); err != nil {
		return ErrMigrationApproval
	}
	if s.journalFallback != "" {
		if err := appendJournalFallback(ctx, tx, s.journalFallback, s.now); err != nil {
			return fmt.Errorf("sqlite: record journal fallback: %w", err)
		}
	}
	healthy, err = integrityDB(ctx, tx)
	if err != nil {
		return fmt.Errorf("sqlite: post-migration integrity check failed: %w", err)
	}
	if !healthy {
		return errors.New("sqlite: post-migration integrity check failed")
	}
	return tx.Commit()
}

func approvalMatches(plan MigrationPlan, a Approval) bool {
	if a.ApprovalID == "" || a.ApprovedBy == "" || a.EvidenceReference == "" || a.ExpiresAt.IsZero() || a.ExpiresAt.Location() != time.UTC || !a.ExpiresAt.After(a.Timestamp) || a.ExpiresAt.Sub(a.Timestamp) > 15*time.Minute || !saneApprovalTime(a.Timestamp, a.ExpiresAt) {
		return false
	}
	kind := a.AuthorizationKind
	if kind == "" {
		kind = ports.MigrationAuthorizationHuman
	}
	authorized := kind == ports.MigrationAuthorizationHuman && a.Command == migrationApplyCommand
	if kind == ports.MigrationAuthorizationAutomaticSafe {
		authorized = plan.AutomaticEligible && len(plan.Checksums) > 0 && a.Command == automaticMigrationApplyCommand && a.ApprovedBy == automaticMigrationPolicyActor && a.EvidenceReference == automaticMigrationPolicyEvidence
	}
	return authorized && a.PlanID == plan.ID && a.Project == plan.Project && a.FromVersion == plan.FromVersion && a.ToVersion == plan.ToVersion && strings.Join(a.Checksums, ",") == strings.Join(plan.Checksums, ",") && a.BackupLocation == plan.BackupLocation && a.BackupChecksum != ""
}

func saneApprovalTime(issued, expires time.Time) bool {
	return issued.Location() == time.UTC && expires.Location() == time.UTC
}

func (s *SQLiteStore) pending(ctx context.Context) ([]Migration, error) {
	pending, _, err := s.pendingWithVersion(ctx)
	return pending, err
}

func (s *SQLiteStore) pendingWithVersion(ctx context.Context) ([]Migration, int, error) {
	return s.pendingWithQuery(ctx, s.db)
}

func (s *SQLiteStore) pendingWithQuery(ctx context.Context, q queryer) ([]Migration, int, error) {
	exists, err := tableExists(ctx, q, "schema_migrations")
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return append([]Migration(nil), s.migrations...), 0, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT version,checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	applied := map[int]string{}
	current := 0
	expected := 1
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, 0, err
		}
		if version != expected {
			return nil, 0, ErrMigrationState
		}
		applied[version] = checksum
		current = version
		expected++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	known := make(map[int]Migration, len(s.migrations))
	for _, migration := range s.migrations {
		known[migration.Version] = migration
	}
	for version, checksum := range applied {
		migration, ok := known[version]
		if !ok || migration.Checksum != checksum {
			return nil, 0, ErrMigrationState
		}
	}
	pending := make([]Migration, 0, len(s.migrations))
	for _, migration := range s.migrations {
		if _, ok := applied[migration.Version]; !ok {
			pending = append(pending, migration)
		}
	}
	return pending, current, nil
}

func (s *SQLiteStore) requireReady(ctx context.Context, q queryer) error {
	pending, _, err := s.pendingWithQuery(ctx, q)
	if err != nil {
		return err
	}
	if len(pending) != 0 {
		return ErrPendingMigrations
	}
	return nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func tableExists(ctx context.Context, q queryer, table string) (bool, error) {
	var name string
	err := q.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func findReceipt(ctx context.Context, tx *sql.Tx, project domain.ProjectID, key domain.IdempotencyKey) (domain.Receipt, domain.Result, bool, error) {
	var receipt domain.Receipt
	var encoded []byte
	var createdAt string
	err := tx.QueryRowContext(ctx, `SELECT id,operation,outcome,result_json,created_at FROM command_receipts WHERE project_id=? AND idempotency_key=?`, project, key).Scan(&receipt.ID, &receipt.Operation, &receipt.Outcome, &encoded, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Receipt{}, domain.Result{}, false, nil
	}
	if err != nil {
		return domain.Receipt{}, domain.Result{}, false, err
	}
	receipt.IdempotencyKey = key
	if receipt.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return domain.Receipt{}, domain.Result{}, false, err
	}
	var result domain.Result
	if err := json.Unmarshal(encoded, &result); err != nil {
		return domain.Receipt{}, domain.Result{}, false, err
	}
	return receipt, result, true, nil
}

type repositories struct {
	tx      *sql.Tx
	project domain.ProjectID
}

func (r repositories) Receipts() ports.ReceiptRepository {
	return receipts{tx: r.tx, project: r.project}
}
func (r repositories) Audit() ports.AuditRepository { return audits{tx: r.tx, project: r.project} }

type receipts struct {
	tx      *sql.Tx
	project domain.ProjectID
}

func (r receipts) FindReceipt(ctx context.Context, key domain.IdempotencyKey) (domain.Receipt, bool, error) {
	receipt, _, found, err := findReceipt(ctx, r.tx, r.project, key)
	return receipt, found, err
}

func (r receipts) GetReceipt(ctx context.Context, id domain.ReceiptID) (domain.Receipt, bool, error) {
	var receipt domain.Receipt
	var createdAt string
	err := r.tx.QueryRowContext(ctx, `SELECT id,idempotency_key,operation,outcome,created_at FROM command_receipts WHERE project_id=? AND id=?`, r.project, id).Scan(&receipt.ID, &receipt.IdempotencyKey, &receipt.Operation, &receipt.Outcome, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Receipt{}, false, nil
	}
	if err != nil {
		return domain.Receipt{}, false, err
	}
	receipt.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	return receipt, err == nil, err
}

func (r receipts) ListReceipts(ctx context.Context) ([]domain.Receipt, error) {
	rows, err := r.tx.QueryContext(ctx, `SELECT id,idempotency_key,operation,outcome,created_at FROM command_receipts WHERE project_id=? ORDER BY created_at DESC,id DESC`, r.project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []domain.Receipt
	for rows.Next() {
		var receipt domain.Receipt
		var createdAt string
		if err := rows.Scan(&receipt.ID, &receipt.IdempotencyKey, &receipt.Operation, &receipt.Outcome, &createdAt); err != nil {
			return nil, err
		}
		if receipt.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, err
		}
		values = append(values, receipt)
	}
	return values, rows.Err()
}

type audits struct {
	tx      *sql.Tx
	project domain.ProjectID
}

func (r audits) LatestCursor(ctx context.Context) (ports.AuditCursor, error) {
	var cursor ports.AuditCursor
	var occurred string
	err := r.tx.QueryRowContext(ctx, `SELECT sequence_no, occurred_at FROM audit_events WHERE project_id=? ORDER BY sequence_no DESC LIMIT 1`, r.project).Scan(&cursor.Sequence, &occurred)
	if errors.Is(err, sql.ErrNoRows) {
		return cursor, nil
	}
	if err != nil {
		return ports.AuditCursor{}, err
	}
	cursor.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		return ports.AuditCursor{}, err
	}
	return cursor, nil
}

type integrityQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func integrityDB(ctx context.Context, db integrityQuerier) (bool, error) {
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return false, err
	}
	return result == "ok", nil
}
func integrityPath(ctx context.Context, path string) (bool, error) {
	db, err := sql.Open("sqlite", sqliteURI(path, true, true))
	if err != nil {
		return false, err
	}
	defer db.Close()
	return integrityDB(ctx, db)
}
func fileChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func newID(prefix, value string, at time.Time) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + value + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}
func planHash(project string, from, to int, checksums []string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%s", project, from, to, strings.Join(checksums, ","))))
	return hex.EncodeToString(sum[:16])
}
func samePlan(left, right MigrationPlan) bool {
	return left.ID == right.ID &&
		left.Project == right.Project &&
		left.FromVersion == right.FromVersion &&
		left.ToVersion == right.ToVersion &&
		left.BackupLocation == right.BackupLocation &&
		left.AutomaticEligible == right.AutomaticEligible &&
		slices.Equal(left.Checksums, right.Checksums)
}
func normalizeMigrations(input []Migration) ([]Migration, error) {
	if len(input) == 0 {
		input = []Migration{{Version: 1, SQL: foundationSQL}, {Version: 2, SQL: coordinationSQL}, {Version: 3, SQL: reservationSQL}, {Version: 4, SQL: gitInventorySQL}, {Version: 5, SQL: scopedAuditSQL}, {Version: 6, SQL: scopedHumansSQL}, {Version: 7, SQL: receiptOperationSQL}, {Version: 8, SQL: legacyHumanAssociationsSQL}, {Version: 9, SQL: handoffLifecycleSQL}, {Version: 10, SQL: exactSHACanarySQL}, {Version: 11, SQL: automaticMigrationAuthorizationSQL, AutomaticSafe: true}, {Version: 12, SQL: taskHierarchyPolicySQL, AutomaticSafe: true}}
	}
	output := append([]Migration(nil), input...)
	previous := 0
	for i := range output {
		if output[i].Version <= previous || output[i].SQL == "" {
			return nil, errors.New("sqlite: migrations must be ordered and nonempty")
		}
		previous = output[i].Version
		output[i].Checksum = checksumSQL(output[i].SQL)
	}
	return output, nil
}
func checksumSQL(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

const foundationSQL = `CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

CREATE TABLE command_receipts (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    outcome TEXT NOT NULL,
    result_json BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    receipt_id TEXT REFERENCES command_receipts(id),
    event_type TEXT NOT NULL,
    payload_json BLOB NOT NULL,
    occurred_at TEXT NOT NULL
);

CREATE TRIGGER audit_events_no_update
BEFORE UPDATE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TRIGGER audit_events_no_delete
BEFORE DELETE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;
`
