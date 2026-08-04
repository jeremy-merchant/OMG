package workersetup

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestSetupCreatesAndReplaysOneAtomicExecutionUnit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)

	before := tableCounts(t, db)
	first, err := service.Setup(ctx, "worker-setup-one", request)
	if err != nil {
		t.Fatal(err)
	}
	if !first.SessionCreated || !first.TaskCreated || !first.TaskClaimed || !first.RunCreated {
		t.Fatalf("fresh setup flags = %+v", first)
	}
	if first.TaskState != string(core.TaskClaimed) || first.RunState != string(core.RunRunning) || !reflect.DeepEqual(first.ReservationIDs, []string{"setup-app", "setup-test"}) {
		t.Fatalf("fresh setup result = %+v", first)
	}
	after := tableCounts(t, db)
	if after.sessions != before.sessions+1 || after.tasks != before.tasks+1 || after.runs != before.runs+1 || after.reservations != before.reservations+2 || after.receipts != before.receipts+1 || after.audit != before.audit+1 {
		t.Fatalf("fresh setup counts = before %+v after %+v", before, after)
	}

	replay, err := service.Setup(ctx, "worker-setup-one", request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, replay) {
		t.Fatalf("replay = %+v; want %+v", replay, first)
	}
	if got := tableCounts(t, db); got != after {
		t.Fatalf("replay mutated state: %+v -> %+v", after, got)
	}

	changed := request
	changed.Reservations = append([]reservation.BatchCreateItem(nil), request.Reservations...)
	changed.Reservations[1].Intent = "different intent"
	if _, err := service.Setup(ctx, "worker-setup-one", changed); !hasCode(err, domain.CodeConflict) {
		t.Fatalf("changed-payload replay error = %v", err)
	}
	if got := tableCounts(t, db); got != after {
		t.Fatalf("changed-payload replay mutated state: %+v -> %+v", after, got)
	}
}

func TestSetupReusesExactExistingExecutionUnitWithNewKey(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)
	if _, err := service.Setup(ctx, "worker-setup-first", request); err != nil {
		t.Fatal(err)
	}
	before := tableCounts(t, db)

	reused, err := service.Setup(ctx, "worker-setup-second", request)
	if err != nil {
		t.Fatal(err)
	}
	if reused.SessionCreated || reused.TaskCreated || reused.TaskClaimed || reused.RunCreated {
		t.Fatalf("reuse flags = %+v", reused)
	}
	if !reflect.DeepEqual(reused.ReservationIDs, []string{"setup-app", "setup-test"}) {
		t.Fatalf("reuse reservation IDs = %#v", reused.ReservationIDs)
	}
	after := tableCounts(t, db)
	if after.sessions != before.sessions || after.tasks != before.tasks || after.runs != before.runs || after.reservations != before.reservations || after.receipts != before.receipts+1 || after.audit != before.audit+1 {
		t.Fatalf("reuse counts = before %+v after %+v", before, after)
	}
}

func TestSetupReservationConflictRollsBackSessionTaskAndRun(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	seedCompetingReservation(t, store, now)
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)
	request.Reservations[0].Pattern = mustPattern(t, "internal/app/setup.go")
	before := tableCounts(t, db)

	if _, err := service.Setup(ctx, "worker-setup-conflict", request); !hasCode(err, domain.CodeConflict) {
		t.Fatalf("reservation conflict error = %v", err)
	}
	if got := tableCounts(t, db); got != before {
		t.Fatalf("conflicting setup partially persisted: %+v -> %+v", before, got)
	}
	assertMissingExecutionUnit(t, db, request)
}

func TestSetupMissingParentRollsBackSession(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)
	request.ParentTaskID = "missing-parent"
	request.Reservations = nil
	before := tableCounts(t, db)

	if _, err := service.Setup(ctx, "worker-setup-parent-missing", request); !hasCode(err, domain.CodeNotFound) {
		t.Fatalf("missing parent error = %v", err)
	}
	if got := tableCounts(t, db); got != before {
		t.Fatalf("missing-parent setup partially persisted: %+v -> %+v", before, got)
	}
	assertMissingExecutionUnit(t, db, request)
}

func TestSetupRejectsMismatchedExistingSessionWithoutMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)
	if _, err := service.Setup(ctx, "worker-setup-existing", request); err != nil {
		t.Fatal(err)
	}
	before := tableCounts(t, db)
	request.Runtime = "different-runtime"

	if _, err := service.Setup(ctx, "worker-setup-mismatch", request); !hasCode(err, domain.CodeConflict) {
		t.Fatalf("mismatched session error = %v", err)
	}
	if got := tableCounts(t, db); got != before {
		t.Fatalf("mismatched setup mutated state: %+v -> %+v", before, got)
	}
}

func TestSetupRejectsForeignControllerWithoutMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	seedForeignController(t, store, db, now)
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)
	request.ControllerSessionID = "foreign-controller"
	before := tableCounts(t, db)

	if _, err := service.Setup(ctx, "worker-setup-foreign", request); !hasCode(err, domain.CodeNotFound) {
		t.Fatalf("foreign controller error = %v", err)
	}
	if got := tableCounts(t, db); got != before {
		t.Fatalf("foreign-controller setup mutated state: %+v -> %+v", before, got)
	}
	assertMissingExecutionUnit(t, db, request)
}

func TestSetupRejectsStaleControllerWithoutMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	seedController(t, store, now)
	if _, _, err := store.Scope(testsupport.Project).Write(ctx, "mark-setup-controller-stale", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		err := repositories.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: "setup-controller-stale", SessionID: "setup-controller", ObservedAt: now, Liveness: core.Stale, Detail: []byte("{}")})
		return domain.Result{ID: "setup-controller-stale", Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	service := New(store.Scope(testsupport.Project), func() time.Time { return now })
	request := setupRequest(t)
	before := tableCounts(t, db)
	if _, err := service.Setup(ctx, "worker-setup-stale-controller", request); !hasCode(err, domain.CodeConflict) {
		t.Fatalf("stale controller error = %v", err)
	}
	if got := tableCounts(t, db); got != before {
		t.Fatalf("stale-controller setup mutated state: %+v -> %+v", before, got)
	}
	assertMissingExecutionUnit(t, db, request)
}

func setupRequest(t *testing.T) Request {
	t.Helper()
	return Request{
		ProjectID: testsupport.Project, ProjectRoot: "/project", HumanID: "worker-human",
		ControllerSessionID: "setup-controller", SessionID: "setup-worker", Runtime: "test-runtime", Role: "worker",
		TaskID: "setup-task", TaskTitle: "Atomic worker setup", RunID: "setup-run",
		Reservations: []reservation.BatchCreateItem{
			{ID: "setup-app", Pattern: mustPattern(t, "internal/app/setup.go"), Mode: res.Exclusive, Intent: "edit setup logic", TTL: time.Hour},
			{ID: "setup-test", Pattern: mustPattern(t, "internal/app/setup_test.go"), Mode: res.Exclusive, Intent: "test setup logic", TTL: time.Hour},
		},
	}
}

func seedController(t *testing.T, store ports.Store, now time.Time) {
	t.Helper()
	_, _, err := store.Scope(testsupport.Project).Write(context.Background(), "seed-worker-setup-controller", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		coordination := repositories.Coordination()
		for _, human := range []core.Human{
			{ID: "controller-human", ProjectID: testsupport.Project, DisplayName: "Controller", Confidence: core.ConfidenceExplicit, CreatedAt: now},
			{ID: "worker-human", ProjectID: testsupport.Project, DisplayName: "Worker", Confidence: core.ConfidenceExplicit, CreatedAt: now},
		} {
			if err := coordination.CreateHuman(context.Background(), human); err != nil {
				return domain.Result{}, err
			}
		}
		controller := core.AgentSession{
			ID: "setup-controller", ProjectID: testsupport.Project, HumanID: "controller-human", Kind: core.HumanDirect,
			Runtime: "test-runtime", Role: "controller", Source: core.SourceHuman, SourceRef: "test", RootID: "setup-controller",
			StartedAt: now, NativeAccessState: core.NativeAccessUnsupported,
		}
		if err := coordination.CreateSession(context.Background(), controller); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "setup-controller", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedCompetingReservation(t *testing.T, store ports.Store, now time.Time) {
	t.Helper()
	ctx := context.Background()
	_, _, err := store.Scope(testsupport.Project).Write(ctx, "seed-worker-setup-conflict", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		coordination := repositories.Coordination()
		worker := core.AgentSession{
			ID: "competing-worker", ProjectID: testsupport.Project, HumanID: "worker-human", Kind: core.HumanDirect,
			Runtime: "test-runtime", Role: "worker", Source: core.SourceHuman, SourceRef: "controller:setup-controller",
			RootID: "competing-worker", TaskID: "competing-task", StartedAt: now, NativeAccessState: core.NativeAccessUnsupported,
		}
		if err := coordination.CreateSession(ctx, worker); err != nil {
			return domain.Result{}, err
		}
		task, err := coordination.CreateTask(ctx, core.Task{
			ID: "competing-task", ProjectID: testsupport.Project, DisplayNumber: 1, Title: "Competing task", State: core.TaskReady,
			CreatedBySessionID: "setup-controller", CompletionPolicy: core.TaskCompletionIndependent,
			ParentRequirement: core.TaskParentOptional, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return domain.Result{}, err
		}
		if task, won, err := coordination.ClaimTask(ctx, task.ID, worker.ID, now); err != nil || !won {
			return domain.Result{}, err
		} else if err := coordination.CreateRun(ctx, core.TaskRun{ID: "competing-run", TaskID: task.ID, SessionID: worker.ID, State: core.RunRunning, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		pattern := mustPattern(t, "internal/app/setup.go")
		record, err := res.New(res.ReservationInput{
			ID: "competing-reservation", Pattern: pattern, Mode: res.Exclusive,
			Owner:  res.Owner{HumanID: "worker-human", SessionID: "competing-worker", TaskID: "competing-task", RunID: "competing-run"},
			Intent: "competing edit", ExpiresAt: now.Add(time.Hour),
		})
		if err != nil {
			return domain.Result{}, err
		}
		if err := repositories.Reservations().Create(ctx, testsupport.Project, record, now); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "competing-reservation", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedForeignController(t *testing.T, store ports.Store, db *sql.DB, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "INSERT INTO projects(id,created_at) VALUES(?,?)", "foreign-worker-setup", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Scope("foreign-worker-setup").Write(ctx, "seed-foreign-worker-setup", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		coordination := repositories.Coordination()
		if err := coordination.CreateHuman(ctx, core.Human{ID: "foreign-controller-human", ProjectID: "foreign-worker-setup", DisplayName: "Foreign", Confidence: core.ConfidenceExplicit, CreatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		controller := core.AgentSession{
			ID: "foreign-controller", ProjectID: "foreign-worker-setup", HumanID: "foreign-controller-human", Kind: core.HumanDirect,
			Runtime: "test-runtime", Role: "controller", Source: core.SourceHuman, SourceRef: "test", RootID: "foreign-controller",
			StartedAt: now, NativeAccessState: core.NativeAccessUnsupported,
		}
		if err := coordination.CreateSession(ctx, controller); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "foreign-controller", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mustPattern(t *testing.T, value string) res.Pattern {
	t.Helper()
	pattern, err := res.NewPattern(res.Exact, value, res.CaseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	return pattern
}

type counts struct {
	sessions, tasks, runs, reservations, receipts, audit int
}

func tableCounts(t *testing.T, db *sql.DB) counts {
	t.Helper()
	var out counts
	for query, target := range map[string]*int{
		"SELECT COUNT(*) FROM agent_sessions":   &out.sessions,
		"SELECT COUNT(*) FROM tasks":            &out.tasks,
		"SELECT COUNT(*) FROM task_runs":        &out.runs,
		"SELECT COUNT(*) FROM reservations":     &out.reservations,
		"SELECT COUNT(*) FROM command_receipts": &out.receipts,
		"SELECT COUNT(*) FROM audit_events":     &out.audit,
	} {
		if err := db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func assertMissingExecutionUnit(t *testing.T, db *sql.DB, request Request) {
	t.Helper()
	for table, id := range map[string]string{"agent_sessions": string(request.SessionID), "tasks": string(request.TaskID), "task_runs": string(request.RunID)} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id=?", id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s %s unexpectedly persisted", table, id)
		}
	}
	for _, item := range request.Reservations {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM reservations WHERE id=?", item.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("reservation %s unexpectedly persisted", item.ID)
		}
	}
}

func hasCode(err error, code domain.ErrorCode) bool {
	var domainErr domain.DomainError
	return errors.As(err, &domainErr) && domainErr.Code == code && !domainErr.Retryable
}
