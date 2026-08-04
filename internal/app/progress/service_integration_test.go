package progress

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
)

func TestAppendHistoryIsOrderedImmutableAndSupportsOptionalRun(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	svc := New(s, func() time.Time { return now })
	for _, p := range []coord.Progress{{ID: "p1", TaskID: "a", SessionID: "source", RunID: "run", Phase: coord.PhasePlan, Done: []string{"one"}, Doing: []string{"one"}, Next: []string{"two"}}, {ID: "p2", TaskID: "a", SessionID: "source", Phase: coord.PhaseTest, Done: []string{"two"}, Doing: []string{"two"}, Next: []string{"three"}}} {
		if _, err := svc.Append(context.Background(), domain.IdempotencyKey(p.ID), p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.History(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p2" {
		t.Fatalf("history=%+v", got)
	}
	if _, err := db.Exec("UPDATE progress_updates SET phase='test' WHERE id='p1'"); err == nil {
		t.Fatal("UPDATE unexpectedly accepted")
	}
	if _, err := db.Exec("DELETE FROM progress_updates WHERE id='p1'"); err == nil {
		t.Fatal("DELETE unexpectedly accepted")
	}
}

func TestAppendRejectsSplitDelegationTokenBeforePersistence(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		done []string
	}{
		{name: "two parts", done: []string{sampleDelegationToken()[:17], sampleDelegationToken()[17:]}},
		{name: "three parts", done: []string{sampleDelegationToken()[:9], sampleDelegationToken()[9:31], sampleDelegationToken()[31:]}},
		{name: "incomplete candidate before complete token", done: []string{"omgdt_v1_" + strings.Repeat("a", 42), sampleDelegationToken()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, db := testsupport.Store(t, now)
			testsupport.Seed(t, store, now)
			_, err := New(store, func() time.Time { return now }).Append(context.Background(), "reject-token", coord.Progress{
				ID: "reject-token", TaskID: "a", SessionID: "source", Phase: coord.PhasePlan, Done: test.done,
			})
			if err == nil {
				t.Fatal("split delegation token progress accepted")
			}
			for _, query := range []string{
				"SELECT COUNT(*) FROM progress_updates WHERE id='reject-token'",
				"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key='reject-token'",
				"SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key='reject-token'",
			} {
				var count int
				if err := db.QueryRow(query).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("rejected split token persisted %d rows for %q", count, query)
				}
			}
		})
	}
}

func sampleDelegationToken() string {
	return "omgdt_v1_" + strings.Repeat("a", 43)
}

func TestAppendRejectsDelegationTokenSplitBetweenKeyAndProgressBeforePersistence(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	key := domain.IdempotencyKey("omgdt_v1_")
	progressID := strings.Repeat("a", 43)

	_, err := New(store, func() time.Time { return now }).Append(context.Background(), key, coord.Progress{
		ID: progressID, TaskID: "a", SessionID: "source", Phase: coord.PhasePlan,
	})
	if err == nil {
		t.Fatal("delegation token split across key and progress accepted")
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM progress_updates WHERE id=?",
		"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key=?",
		"SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key=?",
	} {
		var count int
		value := progressID
		if strings.Contains(query, "idempotency_key") {
			value = string(key)
		}
		if err := db.QueryRow(query, value).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rejected composite token persisted %d rows", count)
		}
	}
}

func TestAppendRejectsDelegationTokenSplitAcrossKeyAndProgressLeavesBeforePersistence(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	key := domain.IdempotencyKey("omgdt_")

	_, err := New(store, func() time.Time { return now }).Append(context.Background(), key, coord.Progress{
		ID:        "v1_" + strings.Repeat("a", 20),
		TaskID:    "a",
		SessionID: "source",
		Phase:     coord.PhasePlan,
		Done:      []string{strings.Repeat("a", 23)},
	})
	if err == nil {
		t.Fatal("delegation token split across key and progress leaves accepted")
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM progress_updates WHERE id=?",
		"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key=?",
		"SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key=?",
	} {
		var count int
		value := "v1_" + strings.Repeat("a", 20)
		if strings.Contains(query, "idempotency_key") {
			value = string(key)
		}
		if err := db.QueryRow(query, value).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rejected composite token persisted %d rows", count)
		}
	}
}

func TestAppendRejectsSecretBearingStableMetadataBeforePersistence(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	svc := New(store, func() time.Time { return now })
	for _, test := range []struct {
		name string
		key  domain.IdempotencyKey
		id   string
	}{
		{name: "idempotency key", key: "password=release-secret", id: "progress-key"},
		{name: "progress id", key: "progress-id", id: "password=release-secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := svc.Append(context.Background(), test.key, coord.Progress{ID: test.id, TaskID: "a", SessionID: "source", Phase: coord.PhasePlan, Done: []string{"password=release-secret"}})
			if err == nil {
				t.Fatal("secret-bearing stable metadata accepted")
			}
			for _, query := range []string{
				"SELECT COUNT(*) FROM progress_updates WHERE id=?",
				"SELECT COUNT(*) FROM command_receipts WHERE idempotency_key=?",
				"SELECT COUNT(*) FROM audit_events a JOIN command_receipts r ON r.id=a.receipt_id WHERE r.idempotency_key=?",
			} {
				var count int
				value := test.id
				if strings.Contains(query, "idempotency_key") {
					value = string(test.key)
				}
				if err := db.QueryRow(query, value).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("rejected %s persisted %d rows", test.name, count)
				}
			}
		})
	}
}
