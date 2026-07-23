package reservation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"example.invalid/coordledger/internal/app/testsupport"
	"example.invalid/coordledger/internal/domain"
	core "example.invalid/coordledger/internal/domain/lineage"
	res "example.invalid/coordledger/internal/domain/reservation"
	"example.invalid/coordledger/internal/ports"
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
