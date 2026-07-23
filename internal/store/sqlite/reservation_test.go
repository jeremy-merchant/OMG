package sqlite

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	resapp "example.invalid/coordledger/internal/app/reservation"
	"example.invalid/coordledger/internal/domain"
	core "example.invalid/coordledger/internal/domain/lineage"
	domainreservation "example.invalid/coordledger/internal/domain/reservation"
	"example.invalid/coordledger/internal/ports"
)

const (
	baseExpiry = "2030-01-01T00:00:00.000000000Z"
	createdAt  = "2026-01-01T00:00:00.000000000Z"
)

func seedReservationRow(t *testing.T, s *SQLiteStore, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reservations(id,project_id,human_id,session_id,task_id,run_id,pattern_kind,normalized_pattern,case_sensitivity,mode,intent,expires_at,created_at) VALUES(?,'p','h','s','t','run','exact','private/pattern','sensitive','exclusive','private intent',?,?)`, id, baseExpiry, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
}

func TestScopedReservationRepositoryRejectsForeignProjects(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	ctx := context.Background()
	now := mustParseReservationTime(t, createdAt)
	const projectA = domain.ProjectID("project-a")
	const projectB = domain.ProjectID("project-b")
	if _, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,created_at) VALUES(?,?)`, projectA, utc(now)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Write(ctx, "reservation-lineage", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		if err := c.CreateHuman(ctx, core.Human{ID: "human-a", DisplayName: "human", Confidence: core.ConfidenceExplicit, CreatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateSession(ctx, core.AgentSession{ID: "session-a", ProjectID: core.ID(projectA), HumanID: "human-a", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "fixture", RootID: "session-a", StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if _, err := c.CreateTask(ctx, core.Task{ID: "task-a", ProjectID: core.ID(projectA), Title: "task", State: core.TaskClaimed, CreatedAt: now, UpdatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateRun(ctx, core.TaskRun{ID: "run-a", TaskID: "task-a", SessionID: "session-a", State: core.RunWorkComplete, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "reservation-lineage", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	pattern, err := domainreservation.NewPattern(domainreservation.Exact, "owned/file.go", domainreservation.CaseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	value, err := domainreservation.New(domainreservation.ReservationInput{ID: "reservation-a", Pattern: pattern, Mode: domainreservation.Exclusive, Owner: domainreservation.Owner{HumanID: "human-a", SessionID: "session-a", TaskID: "task-a", RunID: "run-a"}, Intent: "edit", ExpiresAt: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	scoped := s.Scope(projectA)
	if _, _, err := scoped.Write(ctx, "reservation-create-a", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Reservations().Create(ctx, projectA, value, now)
		return domain.Result{ID: "reservation-a", Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.Read(ctx, func(r ports.Repositories) error {
		repo := r.Reservations()
		if got, ok, err := repo.Get(ctx, projectA, value.ID); err != nil || !ok || got.ID != value.ID {
			t.Fatalf("same-project Get = %+v, %v, %v", got, ok, err)
		}
		if got, err := repo.List(ctx, projectA); err != nil || len(got) != 1 || got[0].ID != value.ID {
			t.Fatalf("same-project List = %+v, %v", got, err)
		}
		if _, ok, err := repo.History(ctx, projectA, value.ID); err != nil || !ok {
			t.Fatalf("same-project History = %v, %v", ok, err)
		}
		for _, call := range []func() error{
			func() error { _, _, err := repo.Get(ctx, projectB, value.ID); return err },
			func() error { _, err := repo.List(ctx, projectB); return err },
			func() error { _, _, err := repo.History(ctx, projectB, value.ID); return err },
		} {
			if err := call(); err == nil {
				t.Fatal("foreign project read succeeded")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scoped.Write(ctx, "reservation-writes-b", "test.write", func(r ports.Repositories) (domain.Result, error) {
		repo := r.Reservations()
		if err := repo.Create(ctx, projectB, value, now); err == nil {
			t.Fatal("foreign project Create succeeded")
		}
		if err := repo.Renew(ctx, projectB, value.ID, domainreservation.RenewalFact{}, now); err == nil {
			t.Fatal("foreign project Renew succeeded")
		}
		if err := repo.Release(ctx, projectB, value.ID, domainreservation.ReleaseFact{}); err == nil {
			t.Fatal("foreign project Release succeeded")
		}
		if err := repo.Override(ctx, projectB, value.ID, domainreservation.OverrideFact{}); err == nil {
			t.Fatal("foreign project Override succeeded")
		}
		if _, err := repo.ReleaseForTask(ctx, projectB, "task-a", now, "done"); err == nil {
			t.Fatal("foreign project ReleaseForTask succeeded")
		}
		return domain.Result{ID: "foreign writes rejected", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	assertCount(t, s.db, "reservations", 1)
	assertCount(t, s.db, "reservation_renewals", 0)
	assertCount(t, s.db, "reservation_releases", 0)
	assertCount(t, s.db, "reservation_overrides", 0)
}

func TestReservationFactsRejectSnapshotMutationAndInvalidFacts(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	seedReservationRow(t, s, "r")
	ctx := context.Background()
	for _, query := range []string{
		`UPDATE reservations SET expires_at='2031-01-01T00:00:00.000000000Z' WHERE id='r'`,
		`UPDATE reservations SET intent='changed' WHERE id='r'`,
		`DELETE FROM reservations WHERE id='r'`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('dangling','missing','checkpoint','2030-01-01T00:00:00.000000000Z','2031-01-01T00:00:00.000000000Z','2026-01-02T00:00:00.000000000Z')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('bad-time','r','checkpoint','2030-01-01T00:00:00.000000000Z','2031-01-01T00:00:00.000000000Z','not-a-timestamp')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('bad-month','r','checkpoint','2030-13-01T00:00:00.000000000Z','2031-01-01T00:00:00.000000000Z','2026-01-02T00:00:00.000000000Z')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('bad-day','r','checkpoint','2030-02-30T00:00:00.000000000Z','2031-01-01T00:00:00.000000000Z','2026-01-02T00:00:00.000000000Z')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('bad-hour','r','checkpoint','2030-01-01T25:00:00.000000000Z','2031-01-01T00:00:00.000000000Z','2026-01-02T00:00:00.000000000Z')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('late','r','checkpoint','2030-01-01T00:00:00.000000000Z','2031-01-01T00:00:00.000000000Z','2030-01-01T00:00:00.000000000Z')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('short','r','checkpoint','2030-01-01T00:00:00.000000000Z','2029-01-01T00:00:00.000000000Z','2026-01-02T00:00:00.000000000Z')`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err == nil {
			t.Fatalf("accepted invalid immutable-ledger SQL: %s", query)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO reservation_releases(id,reservation_id,reason,occurred_at) VALUES('release','r','done','2026-01-02T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO reservation_overrides(id,reservation_id,human_id,reason,occurred_at) VALUES('override','r','h','exception','2026-01-02T00:00:00.000000000Z')`); err == nil {
		t.Fatal("cross-terminal fact accepted")
	}
}

func TestReservationHistoryOrdersImmutableFacts(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	seedReservationRow(t, s, "r")
	ctx := context.Background()
	for _, query := range []string{
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('renewal-1','r','checkpoint-1','2030-01-01T00:00:00.000000000Z','2030-02-01T00:00:00.000000000Z','2026-01-02T00:00:00.000000000Z')`,
		`INSERT INTO reservation_renewals(id,reservation_id,checkpoint_id,previous_expires_at,expires_at,occurred_at) VALUES('renewal-2','r','checkpoint-2','2030-02-01T00:00:00.000000000Z','2030-03-01T00:00:00.000000000Z','2026-01-03T00:00:00.000000000Z')`,
		`INSERT INTO reservation_releases(id,reservation_id,reason,occurred_at) VALUES('release','r','done','2026-01-04T00:00:00.000000000Z')`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	var historyFound bool
	var currentExpiry string
	if err := s.Read(ctx, func(r ports.Repositories) error {
		history, ok, err := r.Reservations().History(ctx, "p", "r")
		if err != nil {
			return err
		}
		historyFound = ok && history.Release != nil && history.Override == nil && len(history.Renewals) == 2 && history.Renewals[0].CheckpointID == "checkpoint-1" && history.Renewals[1].CheckpointID == "checkpoint-2"
		currentExpiry = utc(history.Current.ExpiresAt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !historyFound || currentExpiry != "2030-03-01T00:00:00.000000000Z" {
		t.Fatalf("history round-trip failed: found=%v expiry=%s", historyFound, currentExpiry)
	}
}

func TestReleaseForTaskUsesExactFractionalExpiryBoundary(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, expiry string }{{"fractional-boundary", "2026-01-02T00:00:00.500000000Z"}, {"fractional-after", "2026-01-02T00:00:00.500000001Z"}} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO reservations(id,project_id,human_id,session_id,task_id,run_id,pattern_kind,normalized_pattern,case_sensitivity,mode,intent,expires_at,created_at) VALUES(?,'p','h','s','t','run','exact','private/fractional','sensitive','exclusive','private intent',?,?)`, item.id, item.expiry, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	at := mustParseReservationTime(t, "2026-01-02T00:00:00.500000000Z")
	var released int
	_, _, err := s.Write(ctx, "fractional-release", "test.write", func(r ports.Repositories) (domain.Result, error) {
		rows, err := r.Reservations().ReleaseForTask(ctx, "p", "t", at, "waiting")
		released = len(rows)
		return domain.Result{Outcome: domain.OutcomeOK}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("released %d reservations, want exactly the value strictly after .5Z", released)
	}
}

func mustParseReservationTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestShorterRenewReturnsInvalidArgument(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	seedReservationRow(t, s, "r")
	service := resapp.New(s, func() time.Time { return mustParseReservationTime(t, "2026-01-02T00:00:00.000000000Z") })
	_, err := service.Renew(context.Background(), "short-renew", resapp.RenewRequest{ProjectID: "p", ReservationID: "r", CheckpointID: "checkpoint", TTL: time.Minute})
	var domainErr domain.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeInvalidArgument || domainErr.Retryable {
		t.Fatalf("shorter renewal error = %#v, want deterministic invalid argument", err)
	}
}

func TestReservationReleaseRaceHasOneTerminalFact(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	seedReservationRow(t, s, "r")
	ctx := context.Background()
	at := mustParseReservationTime(t, "2026-01-02T00:00:00.000000000Z")
	var wait sync.WaitGroup
	results := make(chan error, 32)
	for i := range 32 {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			_, _, err := s.Write(ctx, domain.IdempotencyKey("release-race-"+string(rune('a'+i))), "test.write", func(r ports.Repositories) (domain.Result, error) {
				err := r.Reservations().Release(ctx, "p", "r", domainreservation.ReleaseFact{At: at, Reason: "race"})
				return domain.Result{Outcome: domain.OutcomeOK}, err
			})
			results <- err
		}(i)
	}
	wait.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("release winners = %d, want 1", winners)
	}
}

func TestReceiptAndAuditDoNotContainReservationPrivateFields(t *testing.T) {
	s := migratedStore(t, OpenOptions{})
	defer s.Close()
	ctx := context.Background()
	_, _, err := s.Write(ctx, "safe-result", "test.write", func(_ ports.Repositories) (domain.Result, error) {
		return domain.Result{Outcome: domain.OutcomeOK, Data: map[string]string{"reservation_id": "r"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for table, column := range map[string]string{"command_receipts": "result_json", "audit_events": "payload_json"} {
		rows, err := s.db.QueryContext(ctx, `SELECT `+column+` FROM `+table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value []byte
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"private/pattern", "private intent", "override reason"} {
				if strings.Contains(string(value), private) {
					t.Fatal("private reservation data leaked")
				}
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
