package importrecord

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
)

func TestApplyCreatesCanonicalRecordsAndReplaysIdempotently(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	service := New(store, func() time.Time { return now })
	record := Record{SourceRecordID: "external-42", SourceState: StateActive, Title: "Normalize this", Runtime: "generic", Role: "worker"}
	var baselineEvents int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&baselineEvents); err != nil {
		t.Fatal(err)
	}

	first, err := service.Apply(context.Background(), "import-42", testsupport.Project, record)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Apply(context.Background(), "import-42", testsupport.Project, record)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID == "" || first.TaskID == "" || first.SessionID != second.SessionID || first.TaskID != second.TaskID {
		t.Fatalf("results = %+v, %+v", first, second)
	}
	if first.State != core.TaskInProgress || first.Classification != ClassificationImportedVerified {
		t.Fatalf("result = %+v", first)
	}
	var sessions, tasks, receipts, events int
	for _, check := range []struct {
		query string
		out   *int
	}{
		{"SELECT COUNT(*) FROM agent_sessions", &sessions},
		{"SELECT COUNT(*) FROM tasks", &tasks},
		{"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key='import-42'", &receipts},
		{"SELECT COUNT(*) FROM audit_events", &events},
	} {
		if err := db.QueryRow(check.query).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if sessions != 1 || tasks != 1 || receipts != 1 || events != baselineEvents+1 {
		t.Fatalf("sessions=%d tasks=%d receipts=%d events=%d baseline_events=%d", sessions, tasks, receipts, events, baselineEvents)
	}
	var sourceRef, nativeID, nativeRef sql.NullString
	if err := db.QueryRow("SELECT source_ref,native_session_id,native_session_ref FROM agent_sessions WHERE id=?", first.SessionID).Scan(&sourceRef, &nativeID, &nativeRef); err != nil {
		t.Fatal(err)
	}
	if sourceRef.String != record.SourceRecordID || nativeID.Valid || nativeRef.Valid {
		t.Fatalf("source_ref=%q native_id=%+v native_ref=%+v", sourceRef.String, nativeID, nativeRef)
	}
}

func TestApplyAmbiguousIsImportedUnverified(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	result, err := New(store, func() time.Time { return now }).Apply(context.Background(), "ambiguous", testsupport.Project, Record{SourceRecordID: "opaque-7", SourceState: StateAmbiguous, Title: "Review", Runtime: "generic", Role: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != core.TaskReady || result.Classification != ClassificationImportedUnverified {
		t.Fatalf("result = %+v", result)
	}
}

func TestApplyInvalidRecordDoesNotMutate(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	tables := []string{"agent_sessions", "tasks", "command_receipts", "audit_events"}
	before := make(map[string]int, len(tables))
	for _, table := range tables {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		before[table] = count
	}
	_, err := New(store, func() time.Time { return now }).Apply(context.Background(), "invalid", testsupport.Project, Record{SourceRecordID: "opaque-8", SourceState: State("unknown"), Title: "Bad", Runtime: "generic", Role: "worker"})
	if !errors.Is(err, domain.NewError(domain.CodeInvalidArgument, "invalid import record", false)) {
		t.Fatalf("error = %v", err)
	}
	for _, table := range tables {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != before[table] {
			t.Fatalf("%s count changed: %d -> %d", table, before[table], count)
		}
	}
}

func TestApplyRejectsSecretLikeStableMetadataWithoutMutation(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	for _, record := range []Record{
		{SourceRecordID: "password-source", SourceState: StateActive, Title: "password is valid content", Runtime: "generic", Role: "worker"},
		{SourceRecordID: "source-1", SourceState: StateActive, Title: "valid title", Runtime: "generic", Role: "worker", ParentTaskID: "/private/parent"},
	} {
		store, db := testsupport.Store(t, now)
		for _, table := range []string{"agent_sessions", "tasks", "command_receipts", "audit_events"} {
			var before int
			if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if _, err := New(store, func() time.Time { return now }).Apply(context.Background(), "import-safe-key", testsupport.Project, record); !errors.Is(err, domain.NewError(domain.CodeInvalidArgument, "invalid import record", false)) {
				t.Fatalf("Apply error = %v", err)
			}
			var after int
			if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("%s count changed: %d -> %d", table, before, after)
			}
		}
	}
}

func TestApplyAllowsSecretLikeTitleAsContent(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	result, err := New(store, func() time.Time { return now }).Apply(context.Background(), "import-content-1", testsupport.Project, Record{SourceRecordID: "source-1", SourceState: StateActive, Title: "rotate password after review", Runtime: "generic", Role: "worker"})
	if err != nil {
		t.Fatalf("Apply rejected content text: %v", err)
	}
	if result.SessionID == "" || result.TaskID == "" {
		t.Fatalf("result = %+v", result)
	}
}
