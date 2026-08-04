package reservation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestUserSuppliedReservationIDsRejectSecretLikeMetadataWithoutMutation(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, func() time.Time { return now })
	pattern, err := res.NewPattern(res.Exact, "src/file.go", res.CaseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{ProjectID: testsupport.Project, Pattern: pattern, Mode: res.Exclusive, Owner: res.Owner{HumanID: "human", SessionID: "source", TaskID: "a", RunID: "run"}, Intent: "edit", TTL: time.Minute}
	beforeReceipts, beforeEvents := reservationMutationCounts(t, db)
	if _, err := service.Create(context.Background(), "create-secret-id", CreateRequest{ID: "password-reservation", TTL: time.Minute}); !isInvalidReservationRequest(err) {
		t.Fatalf("Create error = %v", err)
	}
	assertReservationMutationCounts(t, db, beforeReceipts, beforeEvents)

	request.ID = "reservation-1"
	if _, err := service.Create(context.Background(), "create-benign-id", request); err != nil {
		t.Fatalf("Create rejected benign ID: %v", err)
	}
	beforeReceipts, beforeEvents = reservationMutationCounts(t, db)
	for _, invoke := range []func() error{
		func() error {
			_, err := service.Renew(context.Background(), "renew-secret-id", RenewRequest{ProjectID: testsupport.Project, ReservationID: "token-reservation", CheckpointID: "checkpoint-1", TTL: 2 * time.Minute})
			return err
		},
		func() error {
			_, err := service.Renew(context.Background(), "renew-secret-checkpoint", RenewRequest{ProjectID: testsupport.Project, ReservationID: "reservation-1", CheckpointID: "api_key=release", TTL: 2 * time.Minute})
			return err
		},
		func() error {
			_, err := service.Release(context.Background(), "release-secret-id", ReleaseRequest{ProjectID: testsupport.Project, ReservationID: "/private/reservation", Reason: "done"})
			return err
		},
		func() error {
			_, err := service.Override(context.Background(), "override-secret-id", OverrideRequest{ProjectID: testsupport.Project, ReservationID: "secret-reservation", HumanID: "human", Reason: "approved"})
			return err
		},
		func() error {
			_, err := service.History(context.Background(), testsupport.Project, "password-reservation")
			return err
		},
	} {
		if err := invoke(); !isInvalidReservationRequest(err) {
			t.Fatalf("secret-like reservation ID error = %v", err)
		}
		assertReservationMutationCounts(t, db, beforeReceipts, beforeEvents)
	}
}

func isInvalidReservationRequest(err error) bool {
	var domainErr domain.DomainError
	return errors.As(err, &domainErr) && domainErr.Code == domain.CodeInvalidArgument && !domainErr.Retryable
}

func reservationMutationCounts(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	var receipts, events int
	if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	return receipts, events
}

func assertReservationMutationCounts(t *testing.T, db *sql.DB, wantReceipts, wantEvents int) {
	t.Helper()
	receipts, events := reservationMutationCounts(t, db)
	if receipts != wantReceipts || events != wantEvents {
		t.Fatalf("receipts/events changed: %d/%d -> %d/%d", wantReceipts, wantEvents, receipts, events)
	}
}

func TestCreateRejectsForeignHumanWithoutMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	for _, project := range []string{testsupport.Project, "reservation-foreign"} {
		if project != testsupport.Project {
			if _, err := db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", project, now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
		}
		scoped := store.Scope(domain.ProjectID(project))
		if _, _, err := scoped.Write(ctx, domain.IdempotencyKey("seed-"+project), "test.write", func(r ports.Repositories) (domain.Result, error) {
			c := r.Coordination()
			humanID := core.ID("human-" + project)
			sessionID := core.ID("session-" + project)
			taskID := core.ID("task-" + project)
			if err := c.CreateHuman(ctx, core.Human{ID: humanID, ProjectID: core.ID(project), DisplayName: "Human", Confidence: core.ConfidenceExplicit, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateSession(ctx, core.AgentSession{ID: sessionID, ProjectID: core.ID(project), HumanID: humanID, Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: sessionID, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if _, err := c.CreateTask(ctx, core.Task{ID: taskID, ProjectID: core.ID(project), Title: "task", State: core.TaskClaimed, CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateRun(ctx, core.TaskRun{ID: core.ID("run-" + project), TaskID: taskID, SessionID: sessionID, State: core.RunWorkComplete, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			return domain.Result{Outcome: domain.OutcomeOK}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	pattern, err := res.NewPattern(res.Exact, "src/file.go", res.CaseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := CreateRequest{ID: "foreign-human", ProjectID: testsupport.Project, Pattern: pattern, Mode: res.Exclusive, Owner: res.Owner{HumanID: "human-reservation-foreign", SessionID: "session-" + testsupport.Project, TaskID: "task-" + testsupport.Project, RunID: "run-" + testsupport.Project}, Intent: "edit", TTL: time.Minute}
	beforeReceipts, beforeEvents := reservationMutationCounts(t, db)
	if _, err := service.Create(ctx, "foreign-human", request); err == nil {
		t.Fatal("foreign human reservation was created")
	}
	assertReservationMutationCounts(t, db, beforeReceipts, beforeEvents)
	if n := reservationRowCount(t, db, "foreign-human"); n != 0 {
		t.Fatalf("foreign human reservation persisted: %d", n)
	}
	request.ID = "same-project-human"
	request.Owner.HumanID = "human-" + testsupport.Project
	if _, err := service.Create(ctx, "same-project-human", request); err != nil {
		t.Fatalf("same-project human reservation rejected: %v", err)
	}
}

func reservationRowCount(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM reservations WHERE id=?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBatchCreateIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 6, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, func() time.Time { return now })
	first := mustReservationPattern(t, "internal/app/a.go")
	second := mustReservationPattern(t, "internal/app/b.go")
	request := BatchCreateRequest{
		ProjectID: testsupport.Project,
		Owner:     res.Owner{HumanID: "human", SessionID: "source", TaskID: "a", RunID: "run"},
		Items: []BatchCreateItem{
			{ID: "batch-a", Pattern: first, Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
			{ID: "batch-b", Pattern: second, Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
		},
	}
	result, err := service.BatchCreate(ctx, "batch-key", request)
	if err != nil {
		t.Fatal(err)
	}
	if got := batchReservationIDs(t, result.Data); len(got) != 2 || got[0] != "batch-a" || got[1] != "batch-b" {
		t.Fatalf("batch IDs = %#v", got)
	}
	if count := reservationTableCount(t, db); count != 2 {
		t.Fatalf("reservation count = %d; want 2", count)
	}
	replay, err := service.BatchCreate(ctx, "batch-key", request)
	if err != nil {
		t.Fatal(err)
	}
	if got := batchReservationIDs(t, replay.Data); len(got) != 2 || got[0] != "batch-a" || got[1] != "batch-b" {
		t.Fatalf("replay IDs = %#v", got)
	}
	if count := reservationTableCount(t, db); count != 2 {
		t.Fatalf("replay created duplicate rows: %d", count)
	}
}

func TestBatchCreateStrictConflictRollsBackWholeBatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 6, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	pattern := mustReservationPattern(t, "internal/app/conflict.go")
	owner := res.Owner{HumanID: "human", SessionID: "source", TaskID: "a", RunID: "run"}
	if _, _, err := store.Write(ctx, "seed-run-b", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		if err := repositories.Coordination().CreateRun(ctx, core.TaskRun{ID: "run-b", TaskID: "b", SessionID: "source", State: core.RunRunning, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "run-b", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	existingOwner := res.Owner{HumanID: "human", SessionID: "source", TaskID: "b", RunID: "run-b"}
	if _, err := New(store, func() time.Time { return now }).Create(ctx, "existing-key", CreateRequest{ProjectID: testsupport.Project, ID: "existing", Pattern: pattern, Mode: res.Exclusive, Owner: existingOwner, Intent: "edit", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	before := reservationTableCount(t, db)
	service := NewWithOptions(store, func() time.Time { return now }, Options{StrictConflicts: true})
	request := BatchCreateRequest{ProjectID: testsupport.Project, Owner: owner, Items: []BatchCreateItem{
		{ID: "would-create", Pattern: mustReservationPattern(t, "internal/app/clean.go"), Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
		{ID: "would-conflict", Pattern: pattern, Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
	}}
	if _, err := service.BatchCreate(ctx, "strict-batch", request); err == nil {
		t.Fatal("strict conflicting batch succeeded")
	}
	if count := reservationTableCount(t, db); count != before {
		t.Fatalf("partial batch persisted: %d -> %d", before, count)
	}
	if reservationRowCount(t, db, "would-create") != 0 || reservationRowCount(t, db, "would-conflict") != 0 {
		t.Fatal("strict conflict left batch rows")
	}
}

func TestBatchCreateRejectsInvalidOrDuplicateItemsBeforeMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 6, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	owner := res.Owner{HumanID: "human", SessionID: "source", TaskID: "a", RunID: "run"}
	service := NewWithOptions(store, func() time.Time { return now }, Options{StrictConflicts: true})
	beforeReceipts, beforeEvents := reservationMutationCounts(t, db)
	invalid := BatchCreateRequest{ProjectID: testsupport.Project, Owner: owner, Items: []BatchCreateItem{
		{ID: "valid-first", Pattern: mustReservationPattern(t, "internal/app/valid.go"), Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
		{ID: "invalid-second", Pattern: mustReservationPattern(t, "internal/app/invalid.go"), Mode: res.Exclusive, Intent: "edit", TTL: 0},
	}}
	if _, err := service.BatchCreate(ctx, "invalid-batch", invalid); !isInvalidReservationRequest(err) {
		t.Fatalf("invalid batch error = %v", err)
	}
	assertReservationMutationCounts(t, db, beforeReceipts, beforeEvents)
	if reservationTableCount(t, db) != 0 {
		t.Fatal("invalid batch persisted rows")
	}
	duplicate := BatchCreateRequest{ProjectID: testsupport.Project, Owner: owner, Items: []BatchCreateItem{
		{ID: "first", Pattern: mustReservationPattern(t, "internal/app/duplicate.go"), Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
		{ID: "second", Pattern: mustReservationPattern(t, "internal/app/duplicate.go"), Mode: res.Exclusive, Intent: "edit", TTL: time.Hour},
	}}
	if _, err := service.BatchCreate(ctx, "duplicate-batch", duplicate); !isInvalidReservationRequest(err) {
		t.Fatalf("duplicate batch error = %v", err)
	}
	if reservationTableCount(t, db) != 0 {
		t.Fatal("duplicate batch persisted rows")
	}
}

func mustReservationPattern(t *testing.T, value string) res.Pattern {
	return mustReservationPatternKind(t, res.Exact, value)
}

func mustReservationPatternKind(t *testing.T, kind res.PatternKind, value string) res.Pattern {
	t.Helper()
	pattern, err := res.NewPattern(kind, value, res.CaseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	return pattern
}

func batchReservationIDs(t *testing.T, value any) []string {
	t.Helper()
	if typed, ok := value.(BatchCreateData); ok {
		return typed.ReservationIDs
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var data BatchCreateData
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	return data.ReservationIDs
}

func reservationTableCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM reservations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
