package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

func TestSQLiteConfiguresForeignKeysBusyTimeoutAndJournalPolicy(t *testing.T) {
	root := canonicalTempDir(t)
	walStore, status, err := Open(context.Background(), filepath.Join(root, "wal.db"), OpenOptions{
		WALEligible: func(string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer walStore.Close()
	if status.JournalMode != "wal" || walStore.journalMode != "wal" {
		t.Fatalf("eligible local store journal mode = %q/%q; want wal", status.JournalMode, walStore.journalMode)
	}

	var foreignKeys, busyTimeout int
	var journalMode string
	if err := walStore.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := walStore.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if err := walStore.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != busyTimeoutMS || journalMode != "wal" {
		t.Fatalf("pragmas foreign_keys=%d busy_timeout=%d journal_mode=%q", foreignKeys, busyTimeout, journalMode)
	}

	deleteStore, deleteStatus, err := Open(context.Background(), filepath.Join(root, "delete.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer deleteStore.Close()
	if deleteStatus.JournalMode != "delete" || deleteStore.journalMode != "delete" {
		t.Fatalf("explicit DELETE store journal mode = %q/%q; want delete", deleteStatus.JournalMode, deleteStore.journalMode)
	}
	if !sidecarsPossible(root) || sidecarsPossible(filepath.Join(root, "not-a-directory")) {
		t.Fatal("sidecar capability probe did not distinguish a writable directory from an unsupported path")
	}
}

func TestSQLiteAuditsWALEligibilityFallbackAfterSchemaReadiness(t *testing.T) {
	store := migratedStore(t, OpenOptions{
		WALEligible: func(string) bool { return false },
	})

	if store.journalMode != "delete" {
		t.Fatalf("ineligible store journal mode = %q; want delete", store.journalMode)
	}
	var reason string
	if err := store.db.QueryRow(`SELECT json_extract(payload_json, '$.reason') FROM audit_events WHERE event_type = 'journal_mode_fallback'`).Scan(&reason); err != nil {
		t.Fatalf("fallback audit event: %v", err)
	}
	if reason != "wal ineligible for state path" {
		t.Fatalf("fallback audit reason = %q", reason)
	}
}

func TestSQLiteDoesNotAuditExplicitDELETE(t *testing.T) {
	store := migratedStore(t, OpenOptions{})

	var fallbackEvents int
	if err := store.db.QueryRow(`SELECT count(*) FROM audit_events WHERE event_type = 'journal_mode_fallback'`).Scan(&fallbackEvents); err != nil {
		t.Fatal(err)
	}
	if fallbackEvents != 0 {
		t.Fatalf("explicit DELETE fallback events = %d; want 0", fallbackEvents)
	}
}

func TestOpenExistingOnlyInspectsWithoutCreatingState(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "absent", "state.db")

	store, status, err := Open(context.Background(), path, OpenOptions{ExistingOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if store != nil || status.Exists {
		t.Fatalf("existing-only open = store %v, exists %t; want absent", store, status.Exists)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("existing-only inspection created state directory: %v", err)
	}
}

func TestExistingOnlyWritableURIRefusesToCreateDeletedState(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "deleted.db")
	db, err := sql.Open("sqlite", sqliteURI(path, false, true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err == nil {
		t.Fatal("existing-only writable URI created missing state")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("existing-only writable URI created state: %v", err)
	}
}

func TestOpenExistingOnlyRejectsUnsafeFileAndLink(t *testing.T) {
	root := canonicalTempDir(t)
	unsafe := filepath.Join(root, "unsafe.db")
	if err := os.WriteFile(unsafe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(context.Background(), unsafe, OpenOptions{ExistingOnly: true}); err == nil {
		t.Fatal("existing-only inspection accepted broad file")
	}

	target := filepath.Join(root, "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(context.Background(), link, OpenOptions{ExistingOnly: true}); err == nil {
		t.Fatal("existing-only inspection accepted link")
	}
}

func TestSQLiteRetryPolicyIsDeterministicBoundedAndCancelable(t *testing.T) {
	key := domain.IdempotencyKey("retry-policy")
	var total time.Duration
	for attempt := range 8 {
		first := retryDelay(key, attempt)
		second := retryDelay(key, attempt)
		if first != second || first <= 0 {
			t.Fatalf("attempt %d delay = %v/%v", attempt, first, second)
		}
		total += first
	}
	if total >= 4*time.Second {
		t.Fatalf("retry delay budget = %v; want less than 4s", total)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := sleepRetry(ctx, key, 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
		t.Fatalf("canceled retry took %v", elapsed)
	}

	for _, message := range []string{"database is locked", "database is busy", "SQLITE_BUSY", "SQLITE_LOCKED"} {
		if !transient(errors.New(message)) {
			t.Fatalf("transient busy error rejected: %q", message)
		}
	}
	if transient(errors.New("disk I/O error")) {
		t.Fatal("non-transient storage error was marked retryable")
	}
}

func TestSQLiteRetriesTransientCallbackErrors(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	_, _, retry, err := store.writeOnce(context.Background(), "project", "busy-callback", "test.write", func(ports.Repositories) (domain.Result, error) {
		return domain.Result{}, errors.New("SQLITE_BUSY")
	})
	if err == nil || !retry {
		t.Fatalf("callback busy error=%v retry=%t; want transient retry", err, retry)
	}
}

func TestSQLiteRejectsNewerAndChecksumDivergentSchemaState(t *testing.T) {
	t.Run("newer version", func(t *testing.T) {
		store := migratedStore(t, OpenOptions{})
		if _, err := store.db.Exec(`INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(9,'sha256:future','2026-07-23T00:00:00Z')`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pending(context.Background()); !errors.Is(err, ErrMigrationState) {
			t.Fatalf("newer schema error = %v", err)
		}
	})

	t.Run("checksum divergence", func(t *testing.T) {
		store := migratedStore(t, OpenOptions{})
		if _, err := store.db.Exec(`UPDATE schema_migrations SET checksum='sha256:wrong' WHERE version=1`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pending(context.Background()); !errors.Is(err, ErrMigrationState) {
			t.Fatalf("checksum divergence error = %v", err)
		}
	})
}
