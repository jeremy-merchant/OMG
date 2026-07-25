package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	lineagecore "github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/platform"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/store/sqlite"
)

type nativeSessionResolverFunc func(context.Context, lineagecore.AgentSession) (ports.NativeSessionResolution, error)

func (resolve nativeSessionResolverFunc) Resolve(ctx context.Context, session lineagecore.AgentSession) (ports.NativeSessionResolution, error) {
	return resolve(ctx, session)
}

func TestDispatchLineageHumanCreateAndGet(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)

	created, handled := dispatcher.dispatchLineage(ctx, Request{
		Command:        "human.create",
		IdempotencyKey: "human-create",
		Payload:        []byte(`{"display_name":"Operator","confidence":"verified"}`),
	}, selection)
	if !handled || created.Error.Code != "" {
		t.Fatalf("human.create outcome=%+v handled=%t", created, handled)
	}
	human, ok := created.Data.(lineageHumanResult)
	if !ok || human.ID == "" {
		t.Fatalf("human.create result=%#v", created.Data)
	}

	got, handled := dispatcher.dispatchLineage(ctx, Request{
		Command: "human.get",
		Payload: []byte(`{"id":"` + human.ID + `"}`),
	}, selection)
	if !handled || got.Error.Code != "" {
		t.Fatalf("human.get outcome=%+v handled=%t", got, handled)
	}
	result, ok := got.Data.(lineageHumanResult)
	if !ok || result.ID != human.ID || result.DisplayName != "Operator" {
		t.Fatalf("human.get result=%#v", got.Data)
	}
}

func TestLineageResponsesSanitizePresentationFields(t *testing.T) {
	sensitiveHuman := lineageHumanResponse(lineagecore.Human{ID: "human-1", DisplayName: "api_key=release"})
	sensitiveSession := lineageSessionResponse(lineagecore.AgentSession{ID: "session-1", Runtime: `C:\Users\alice\private`, Role: "private_key=release"})
	sensitiveTask := lineageTaskResponse(lineagecore.Task{ID: "task-1", Title: "api-key=release"})
	for name, value := range map[string]string{
		"display name": sensitiveHuman.DisplayName,
		"runtime":      sensitiveSession.Runtime,
		"role":         sensitiveSession.Role,
		"title":        sensitiveTask.Title,
	} {
		if !strings.HasPrefix(value, "[REDACTED:") {
			t.Errorf("%s leaked sensitive presentation text: %q", name, value)
		}
	}

	if got := lineageHumanResponse(lineagecore.Human{ID: "human-1", DisplayName: "Operator"}).DisplayName; got != "Operator" {
		t.Fatalf("benign display name = %q", got)
	}
	if got := lineageSessionResponse(lineagecore.AgentSession{ID: "session-1", Runtime: "codex", Role: "operator"}); got.Runtime != "codex" || got.Role != "operator" {
		t.Fatalf("benign session result = %#v", got)
	}
	if got := lineageTaskResponse(lineagecore.Task{ID: "task-1", Title: "Ship release"}).Title; got != "Ship release" {
		t.Fatalf("benign title = %q", got)
	}
}

func TestDispatchLineageRejectsQueryIdempotencyKey(t *testing.T) {
	outcome, handled := (&ServiceDispatcher{}).dispatchLineage(context.Background(), Request{
		Command:        "human.get",
		IdempotencyKey: "not-allowed",
		Payload:        []byte(`{"id":"human-1"}`),
	}, foundation.Selection{})
	if !handled || outcome.Error.Code != "invalid_argument" || outcome.Error.Message != "application request is invalid" {
		t.Fatalf("outcome=%+v handled=%t", outcome, handled)
	}
}

func TestDispatchLineageTaskCompletionReconcilesDependenciesWithOptionalActor(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	if err := seedLineageCompletionDependency(ctx, dispatcher, selection); err != nil {
		t.Fatal(err)
	}
	progress := Request{Version: RequestVersion, Command: "task.transition", IdempotencyKey: "in-progress", Payload: json.RawMessage(`{"task_id":"a","state":"IN_PROGRESS"}`)}
	if outcome, handled := dispatcher.dispatchLineage(ctx, progress, selection); !handled || outcome.Error.Code != "" {
		t.Fatalf("non-completing transition outcome=%+v handled=%t", outcome, handled)
	}
	err := dispatcher.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(r ports.Repositories) error {
			_, found, err := r.Coordination().GetMessage(ctx, coord.SatisfactionNotificationKey("a-b-work", coord.UnblockWorkComplete))
			if err != nil {
				return err
			}
			if found {
				t.Fatal("non-completing transition emitted a dependency notification")
			}
			return nil
		})
	})
	if err.Code != "" {
		t.Fatal(err)
	}

	work := Request{Version: RequestVersion, Command: "task.transition", IdempotencyKey: "work-complete", Payload: json.RawMessage(`{"task_id":"a","state":"WORK_COMPLETE"}`)}
	first, handled := dispatcher.dispatchLineage(ctx, work, selection)
	if !handled || first.Error.Code != "" {
		t.Fatalf("completion without actor outcome=%+v handled=%t", first, handled)
	}
	firstTask, ok := first.Data.(lineageTaskResult)
	if !ok || firstTask.ID != "a" || firstTask.State != string(lineagecore.TaskWorkComplete) {
		t.Fatalf("completion result=%#v", first.Data)
	}
	replay, handled := dispatcher.dispatchLineage(ctx, work, selection)
	if !handled || replay.Error.Code != "" || replay.Data != first.Data {
		t.Fatalf("completion replay outcome=%+v handled=%t", replay, handled)
	}

	verified := Request{Version: RequestVersion, Command: "task.transition", IdempotencyKey: "verified-done", Payload: json.RawMessage(`{"task_id":"a","actor_session_id":"actor","state":"VERIFIED_DONE","evidence":"verification"}`)}
	if outcome, handled := dispatcher.dispatchLineage(ctx, verified, selection); !handled || outcome.Error.Code != "" {
		t.Fatalf("completion with actor outcome=%+v handled=%t", outcome, handled)
	}

	err = dispatcher.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(r ports.Repositories) error {
			for _, assertion := range []struct {
				task, dependency, sender string
			}{
				{task: "b", dependency: "a-b-work", sender: "source"},
				{task: "c", dependency: "a-c-verified", sender: "actor"},
			} {
				task, ok, err := r.Coordination().GetTask(ctx, lineagecore.ID(assertion.task))
				if err != nil {
					return err
				}
				if !ok || task.State != lineagecore.TaskInProgress {
					t.Fatalf("dependent %s = %#v, found=%t", assertion.task, task, ok)
				}
				message, ok, err := r.Coordination().GetMessage(ctx, coord.SatisfactionNotificationKey(assertion.dependency, map[string]coord.UnblockCriterion{"a-b-work": coord.UnblockWorkComplete, "a-c-verified": coord.UnblockVerifiedDone}[assertion.dependency]))
				if err != nil {
					return err
				}
				if !ok || message.SenderSessionID != assertion.sender {
					t.Fatalf("notification %s = %#v, found=%t", assertion.dependency, message, ok)
				}
			}
			return nil
		})
	})
	if err.Code != "" {
		t.Fatal(err)
	}
}

func TestDispatchLineageCheckpointReturnsCurrentRefresh(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	if err := seedCheckpointRefresh(ctx, dispatcher, selection); err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{
		{Version: RequestVersion, Command: "message.send", IdempotencyKey: "checkpoint-message", Payload: json.RawMessage(`{"id":"checkpoint-message","type":"NOTICE","thread_id":"checkpoint-thread","sender_session_id":"checkpoint-session","recipients":[{"session_id":"checkpoint-session"}],"subject":"refresh","body":"inert","related_task_id":"checkpoint-task"}`)},
		{Version: RequestVersion, Command: "reserve.add", IdempotencyKey: "reserve-refresh", Payload: json.RawMessage(`{"id":"refresh-reservation","pattern_kind":"exact","pattern":"internal/app/lineage/service.go","case_sensitivity":"sensitive","mode":"exclusive","human_id":"checkpoint-human","session_id":"checkpoint-session","task_id":"checkpoint-task","run_id":"checkpoint-run","intent":"refresh","ttl_seconds":60}`)},
	} {
		request.Project = selection.Project
		outcome := dispatcher.Dispatch(ctx, request)
		if outcome.Error.Code != "" {
			t.Fatalf("%s outcome = %+v", request.Command, outcome)
		}
	}

	outcome, handled := dispatcher.dispatchLineage(ctx, Request{
		Version: RequestVersion, Command: "checkpoint.record", IdempotencyKey: "checkpoint-refresh",
		Payload: json.RawMessage(`{"id":"checkpoint-heartbeat","session_id":"checkpoint-session","liveness":"alive"}`),
	}, selection)
	if !handled || outcome.Error.Code != "" {
		t.Fatalf("checkpoint outcome = %+v handled=%t", outcome, handled)
	}
	result, ok := outcome.Data.(lineageCheckpointResult)
	if !ok || result.ID != "checkpoint-heartbeat" || result.Identity == nil || result.Identity.HeartbeatAt == nil ||
		len(result.Dependencies) != 1 || len(result.Inbox) != 1 || len(result.Reservations) != 1 {
		t.Fatalf("checkpoint refresh = %#v", outcome.Data)
	}
}

func TestDispatchLineageCheckpointKeepsDurableSuccessWhenRefreshConflicts(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcherWithNativeResolver(t, nativeSessionResolverFunc(func(context.Context, lineagecore.AgentSession) (ports.NativeSessionResolution, error) {
		return ports.NativeSessionResolution{}, errors.New("native fingerprint mismatch")
	}))
	if err := seedCheckpointRefresh(ctx, dispatcher, selection); err != nil {
		t.Fatal(err)
	}
	request := Request{
		Version: RequestVersion, Command: "checkpoint.record", IdempotencyKey: "checkpoint-refresh-conflict",
		Payload: json.RawMessage(`{"id":"checkpoint-refresh-conflict","session_id":"checkpoint-session","liveness":"alive"}`),
	}
	first, handled := dispatcher.dispatchLineage(ctx, request, selection)
	if !handled || first.Error.Code != "" {
		t.Fatalf("checkpoint outcome=%+v handled=%t", first, handled)
	}
	result, ok := first.Data.(lineageCheckpointResult)
	if !ok || result.ID != "checkpoint-refresh-conflict" || result.SessionID != "checkpoint-session" || result.Liveness != "alive" || result.RefreshAvailable || !reflect.DeepEqual(result.Warnings, []string{"refresh_unavailable"}) {
		t.Fatalf("partial checkpoint result=%#v", first.Data)
	}
	err := dispatcher.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(repositories ports.Repositories) error {
			session, found, err := repositories.Coordination().GetSession(ctx, "checkpoint-session")
			if err != nil {
				return err
			}
			if !found || session.HeartbeatAt == nil || session.Liveness != lineagecore.Alive {
				t.Fatalf("durable checkpoint missing after refresh conflict: %#v found=%t", session, found)
			}
			receipt, recorded, err := repositories.Receipts().FindReceipt(ctx, domain.IdempotencyKey(request.IdempotencyKey))
			if err != nil {
				return err
			}
			if !recorded || receipt.Operation != "checkpoint.record" || receipt.Outcome != domain.OutcomeOK {
				t.Fatalf("durable checkpoint receipt=%#v recorded=%t", receipt, recorded)
			}
			return nil
		})
	})
	if err.Code != "" {
		t.Fatal(err)
	}
	exact, handled := dispatcher.dispatchLineage(ctx, request, selection)
	if !handled || exact.Error.Code != "" || !reflect.DeepEqual(exact.Data, first.Data) {
		t.Fatalf("exact replay outcome=%+v handled=%t, want %#v", exact, handled, first.Data)
	}
}

func TestDispatchLineageCheckpointReplayUsesCanonicalRecordedSession(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	if err := seedCheckpointRefresh(ctx, dispatcher, selection); err != nil {
		t.Fatal(err)
	}
	firstRequest := Request{
		Version: RequestVersion, Command: "checkpoint.record", IdempotencyKey: "checkpoint-canonical",
		Payload: json.RawMessage(`{"id":"checkpoint-a","session_id":"checkpoint-session","liveness":"alive"}`),
	}
	first, handled := dispatcher.dispatchLineage(ctx, firstRequest, selection)
	if !handled || first.Error.Code != "" {
		t.Fatalf("first checkpoint outcome=%+v handled=%t", first, handled)
	}
	firstResult, ok := first.Data.(lineageCheckpointResult)
	if !ok || firstResult.ID != "checkpoint-a" || firstResult.SessionID != "checkpoint-session" || firstResult.Liveness != "alive" || firstResult.Identity == nil || firstResult.Identity.HeartbeatAt == nil {
		t.Fatalf("first checkpoint result=%#v", first.Data)
	}

	replayed := firstRequest
	replayed.Payload = json.RawMessage(`{"id":"checkpoint-b","session_id":"checkpoint-session-b","liveness":"stale"}`)
	second, handled := dispatcher.dispatchLineage(ctx, replayed, selection)
	if !handled || second.Error.Code != "" {
		t.Fatalf("conflicting replay outcome=%+v handled=%t", second, handled)
	}
	secondResult, ok := second.Data.(lineageCheckpointResult)
	if !ok || secondResult.ID != firstResult.ID || secondResult.SessionID != firstResult.SessionID || secondResult.Liveness != firstResult.Liveness || secondResult.Identity == nil || secondResult.Identity.ID != firstResult.Identity.ID {
		t.Fatalf("conflicting replay result=%#v, want canonical %#v", second.Data, firstResult)
	}

	exact, handled := dispatcher.dispatchLineage(ctx, firstRequest, selection)
	if !handled || exact.Error.Code != "" || !reflect.DeepEqual(exact.Data, first.Data) {
		t.Fatalf("exact replay outcome=%+v handled=%t, want %#v", exact, handled, first)
	}
	err := dispatcher.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(r ports.Repositories) error {
			session, found, err := r.Coordination().GetSession(ctx, "checkpoint-session-b")
			if err != nil {
				return err
			}
			if !found || session.HeartbeatAt != nil {
				t.Fatalf("replayed checkpoint refreshed session B: %#v found=%t", session, found)
			}
			return nil
		})
	})
	if err.Code != "" {
		t.Fatal(err)
	}
}

func seedCheckpointRefresh(ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection) error {
	err := dispatcher.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		_, _, err := store.Write(ctx, "seed-checkpoint-refresh", "test.write", func(r ports.Repositories) (domain.Result, error) {
			now := time.Now().UTC()
			coordination := r.Coordination()
			if err := coordination.CreateHuman(ctx, lineagecore.Human{ID: "checkpoint-human", DisplayName: "Operator", Confidence: lineagecore.ConfidenceExplicit, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			session := lineagecore.AgentSession{ID: "checkpoint-session", ProjectID: lineagecore.ID(resolved.Project), HumanID: "checkpoint-human", Kind: lineagecore.HumanDirect, Runtime: "test", Role: "owner", Source: lineagecore.SourceHuman, SourceRef: "test", RootID: "checkpoint-session", TaskID: "checkpoint-task", StartedAt: now, NativeAccessState: lineagecore.NativeAccessAvailable, NativeSessionID: "native-checkpoint", NativeSessionRef: "checkpoint-ref"}
			session.NativeSessionFingerprint = lineagecore.NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, session.NativeSessionStartedAt)
			for _, session := range []lineagecore.AgentSession{
				session,
				{ID: "checkpoint-session-b", ProjectID: lineagecore.ID(resolved.Project), HumanID: "checkpoint-human", Kind: lineagecore.HumanDirect, Runtime: "test", Role: "observer", Source: lineagecore.SourceHuman, SourceRef: "test", RootID: "checkpoint-session-b", StartedAt: now},
			} {
				if err := coordination.CreateSession(ctx, session); err != nil {
					return domain.Result{}, err
				}
			}
			for _, task := range []lineagecore.Task{
				{ID: "checkpoint-task", ProjectID: lineagecore.ID(resolved.Project), DisplayNumber: 1, Title: "checkpoint task", State: lineagecore.TaskReady, CreatedBySessionID: session.ID, CreatedAt: now, UpdatedAt: now},
				{ID: "checkpoint-dependent", ProjectID: lineagecore.ID(resolved.Project), DisplayNumber: 2, Title: "dependent task", State: lineagecore.TaskReady, CreatedBySessionID: session.ID, CreatedAt: now, UpdatedAt: now},
			} {
				if _, err := coordination.CreateTask(ctx, task); err != nil {
					return domain.Result{}, err
				}
			}
			if err := coordination.CreateRun(ctx, lineagecore.TaskRun{ID: "checkpoint-run", TaskID: "checkpoint-task", SessionID: "checkpoint-session", State: lineagecore.RunRunning, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := coordination.CreateDependency(ctx, coord.Dependency{ID: "checkpoint-dependency", PrerequisiteTaskID: "checkpoint-task", DependentTaskID: "checkpoint-dependent", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}, now); err != nil {
				return domain.Result{}, err
			}
			return domain.Result{ID: "seed-checkpoint-refresh", Outcome: domain.OutcomeOK}, nil
		})
		return err
	})
	if err.Code != "" {
		return err
	}
	return nil
}

func seedLineageCompletionDependency(ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection) error {
	err := dispatcher.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		_, _, err := store.Write(ctx, "seed-completion-dependency", "test.write", func(r ports.Repositories) (domain.Result, error) {
			coordination := r.Coordination()
			now := time.Now().UTC()
			if err := coordination.CreateHuman(ctx, lineagecore.Human{ID: "human", DisplayName: "Operator", Confidence: lineagecore.ConfidenceExplicit, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			for _, session := range []lineagecore.AgentSession{
				{ID: "source", ProjectID: lineagecore.ID(resolved.Project), HumanID: "human", Kind: lineagecore.HumanDirect, Runtime: "test", Role: "owner", Source: lineagecore.SourceHuman, SourceRef: "test", RootID: "source", StartedAt: now},
				{ID: "actor", ProjectID: lineagecore.ID(resolved.Project), HumanID: "human", Kind: lineagecore.HumanDirect, Runtime: "test", Role: "reviewer", Source: lineagecore.SourceHuman, SourceRef: "test", RootID: "actor", StartedAt: now},
			} {
				if err := coordination.CreateSession(ctx, session); err != nil {
					return domain.Result{}, err
				}
			}
			for _, task := range []lineagecore.Task{
				{ID: "a", ProjectID: lineagecore.ID(resolved.Project), DisplayNumber: 1, Title: "prerequisite", State: lineagecore.TaskReady, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now},
				{ID: "b", ProjectID: lineagecore.ID(resolved.Project), DisplayNumber: 2, Title: "work dependent", State: lineagecore.TaskReady, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now},
				{ID: "c", ProjectID: lineagecore.ID(resolved.Project), DisplayNumber: 3, Title: "verified dependent", State: lineagecore.TaskReady, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now},
			} {
				if _, err := coordination.CreateTask(ctx, task); err != nil {
					return domain.Result{}, err
				}
				if _, won, err := coordination.ClaimTask(ctx, task.ID, "source", now); err != nil {
					return domain.Result{}, err
				} else if !won {
					return domain.Result{}, errors.New("seed task claim was not acquired")
				}
			}
			for _, taskID := range []lineagecore.ID{"b", "c"} {
				if _, err := coordination.TransitionTask(ctx, taskID, lineagecore.TaskBlocked, nil, now); err != nil {
					return domain.Result{}, err
				}
			}
			for _, dependency := range []coord.Dependency{
				{ID: "a-b-work", PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete},
				{ID: "a-c-verified", PrerequisiteTaskID: "a", DependentTaskID: "c", Kind: coord.DependencyHard, Criterion: coord.UnblockVerifiedDone},
			} {
				if err := coordination.CreateDependency(ctx, dependency, now); err != nil {
					return domain.Result{}, err
				}
			}
			return domain.Result{ID: "seed-completion-dependency", Outcome: domain.OutcomeOK}, nil
		})
		return err
	})
	if err.Code != "" {
		return err
	}
	return nil
}

func lineageDispatcher(t *testing.T) (context.Context, *ServiceDispatcher, foundation.Selection) {
	return lineageDispatcherWithOpener(t, lineageSQLiteOpener)
}

func lineageDispatcherWithOpener(t *testing.T, opener func(context.Context, string, ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error)) (context.Context, *ServiceDispatcher, foundation.Selection) {
	return lineageDispatcherWithDependencies(t, opener, nil)
}

func lineageDispatcherWithNativeResolver(t *testing.T, native ports.NativeSessionResolver) (context.Context, *ServiceDispatcher, foundation.Selection) {
	return lineageDispatcherWithDependencies(t, lineageSQLiteOpener, native)
}

func lineageDispatcherWithDependencies(t *testing.T, opener func(context.Context, string, ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error), native ports.NativeSessionResolver) (context.Context, *ServiceDispatcher, foundation.Selection) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".omg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".omg", "project.toml"), []byte("# OMG project configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := platform.NewResolver(platform.Dependencies{
		Git: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("not a Git repository")
		},
		UserConfigDir: func() (string, error) { return dataRoot, nil },
		Environment:   func(string) string { return "" },
	})
	service := foundation.New(foundation.Dependencies{
		Resolver:          resolver,
		Open:              opener,
		ConfigInitializer: platform.NewProjectConfigInitializer(),
		NativeResolver:    native,
	})
	selection := foundation.Selection{Project: root}
	if _, err := service.Init(ctx, selection); err.Code != "" {
		t.Fatalf("init: %v", err)
	}
	plan, err := service.Plan(ctx, selection)
	if err.Code != "" {
		t.Fatalf("plan: %v", err)
	}
	backup, err := service.Backup(ctx, selection, &plan)
	if err.Code != "" {
		t.Fatalf("backup: %v", err)
	}
	approval := foundation.ApprovalFile{ApprovalID: "approval-1", ApprovedBy: "test", EvidenceReference: "lineage-dispatch-test", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), ExpiresAtRaw: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)}
	if err := service.Apply(ctx, selection, plan, approval); err.Code != "" {
		t.Fatalf("apply: %v", err)
	}
	return ctx, NewDispatcher(service), selection
}

func lineageSQLiteOpener(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
	store, status, err := sqlite.Open(ctx, path, options)
	if err != nil {
		return nil, ports.OpenStatus{}, err
	}
	return store, status, nil
}
