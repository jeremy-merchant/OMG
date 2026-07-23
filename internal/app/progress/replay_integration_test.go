package progress

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.invalid/coordledger/internal/app/testsupport"
	"example.invalid/coordledger/internal/domain"
	coord "example.invalid/coordledger/internal/domain/coordination"
)

func TestAppendReplayReturnsCanonicalProgressWithoutLeakingPrivateFields(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	testsupport.SeedForeign(t, store, db, now)
	svc := New(store, func() time.Time { return now })

	firstInput := coord.Progress{ID: "progress-canonical", TaskID: "a", RunID: "run", SessionID: "source", Phase: coord.PhaseInspect, Done: []string{"private done: customer-secret"}, Doing: []string{"private doing: access-token"}, Next: []string{"private next: incident-plan"}}
	first, err := svc.Append(ctx, "progress-replay", firstInput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Append(ctx, "progress-replay", coord.Progress{ID: "progress-altered", TaskID: "b", SessionID: "target", Phase: coord.PhaseReview, Done: []string{"hostile second done"}, Doing: []string{"hostile second doing"}, Next: []string{"hostile second next"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("replay returned %#v; want canonical %#v", second, first)
	}
	assertProgressCount(t, db, "progress_updates", 1)
	assertProgressCount(t, db, "command_receipts", 4) // migration, seed, foreign seed, append
	assertProgressCount(t, db, "audit_events", 4)
	assertProgressPrivateAbsent(t, db, "private done: customer-secret", "private doing: access-token", "private next: incident-plan", "hostile second done", "hostile second doing", "hostile second next", "inspect", "review")

	beforeProgress, beforeReceipts, beforeAudit := progressCounts(t, db)
	_, err = svc.Append(ctx, domain.IdempotencyKey("progress-cross-project"), coord.Progress{ID: "progress-cross-project", TaskID: "a", RunID: "foreign-run", SessionID: "foreign-session", Phase: coord.PhasePlan, Done: []string{"private cross-project"}})
	if err == nil {
		t.Fatal("cross-project append succeeded")
	}
	afterProgress, afterReceipts, afterAudit := progressCounts(t, db)
	if afterProgress != beforeProgress || afterReceipts != beforeReceipts || afterAudit != beforeAudit {
		t.Fatalf("failed append left effects: progress %d→%d receipts %d→%d audit %d→%d", beforeProgress, afterProgress, beforeReceipts, afterReceipts, beforeAudit, afterAudit)
	}
}

func progressCounts(t *testing.T, db *sql.DB) (int, int, int) {
	t.Helper()
	var progress, receipts, audit int
	if err := db.QueryRow("SELECT COUNT(*) FROM progress_updates").Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&audit); err != nil {
		t.Fatal(err)
	}
	return progress, receipts, audit
}

func assertProgressCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d; want %d", table, got, want)
	}
}

func assertProgressPrivateAbsent(t *testing.T, db *sql.DB, private ...string) {
	t.Helper()
	for table, column := range map[string]string{"command_receipts": "result_json", "audit_events": "payload_json"} {
		rows, err := db.Query("SELECT " + column + " FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value []byte
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			for _, secret := range private {
				if strings.Contains(string(value), secret) {
					t.Fatalf("%s leaked %q", table, secret)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
