package query

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	appgit "github.com/jeremy-merchant/OMG/internal/app/git"
	"github.com/jeremy-merchant/OMG/internal/app/handoff"
	messageapp "github.com/jeremy-merchant/OMG/internal/app/message"
	"github.com/jeremy-merchant/OMG/internal/app/progress"
	"github.com/jeremy-merchant/OMG/internal/app/testsupport"
	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	gitdomain "github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/domain/reservation"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

func boardActor(caps ...domain.Capability) domain.ActorContext {
	return domain.NewActorContext("scope", testsupport.Project, "workspace", domain.InvocationCLI, caps)
}

type nativeResolverFunc func(context.Context, lineage.AgentSession) (ports.NativeSessionResolution, error)

func (resolve nativeResolverFunc) Resolve(ctx context.Context, session lineage.AgentSession) (ports.NativeSessionResolution, error) {
	return resolve(ctx, session)
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func decodeBoard(t *testing.T, view ViewModel) BoardSnapshot {
	t.Helper()
	var snapshot BoardSnapshot
	if err := json.Unmarshal(view.Data(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestBoardQueryAuthorizationModesFilteringAndDeterminism(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := NewWithClock(store, fixedClock{now: now})
	ctx := context.Background()
	if _, err := service.Query(ctx, boardActor(), BoardRequest{Mode: BoardAll}); err == nil {
		t.Fatal("query without read capability succeeded")
	}
	actor := boardActor(domain.CapabilityRead)
	cases := []BoardRequest{{Mode: BoardMe, SessionID: "source"}, {Mode: BoardTree}, {Mode: BoardTask, TaskID: "a"}, {Mode: BoardAll}, {Mode: BoardGit}}
	for _, request := range cases {
		t.Run(string(request.Mode), func(t *testing.T) {
			first, err := service.Query(ctx, actor, request)
			if err != nil {
				t.Fatal(err)
			}
			second, err := service.Query(ctx, actor, request)
			if err != nil {
				t.Fatal(err)
			}
			if string(first.Data()) != string(second.Data()) || first.SnapshotCursor() != second.SnapshotCursor() {
				t.Fatal("unchanged state did not replay deterministically")
			}
			snapshot := decodeBoard(t, first)
			if first.Kind() != "board" || snapshot.SchemaVersion != BoardSchemaVersion || snapshot.ViewVersion != ViewVersion || snapshot.Mode != request.Mode || snapshot.SnapshotCursor != first.SnapshotCursor() {
				t.Fatalf("invalid board view: %#v", snapshot)
			}
			wantSelector := request.SessionID
			if request.Mode == BoardTask {
				wantSelector = request.TaskID
			}
			if snapshot.GeneratedAt.Location() != time.UTC || !strings.HasPrefix(snapshot.SnapshotCursor, "audit:") || snapshot.Scope.ProjectID != string(actor.Project) || snapshot.Scope.WorkspaceID != string(actor.Workspace) || snapshot.Scope.Mode != request.Mode || snapshot.Scope.Selector != wantSelector || snapshot.Redaction.PolicyName != BoardRedactionPolicyName || snapshot.Redaction.PolicyVersion != BoardRedactionPolicyVersion || !snapshot.Redaction.ContentOmitted || !strings.Contains(strings.Join(snapshot.Warnings, ","), "git_observation_advisory_non_authorizing") {
				t.Fatalf("ADR 0003 metadata is incomplete: %#v", snapshot)
			}
			for _, field := range [][]any{toAny(snapshot.Sessions), toAny(snapshot.Tasks), toAny(snapshot.Runs), toAny(snapshot.Progress), toAny(snapshot.Dependencies), toAny(snapshot.Inbox), toAny(snapshot.Handoffs), toAny(snapshot.Reservations), toAny(snapshot.Warnings), toAny(snapshot.SuggestedActions)} {
				if field == nil {
					t.Fatal("nil repeated field")
				}
			}
			if request.Mode == BoardMe && (len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "source" || snapshot.Identity == nil || len(snapshot.Tasks) != 3) {
				t.Fatalf("me filtering incorrect: %#v", snapshot)
			}
			if request.Mode == BoardTask && (len(snapshot.Tasks) != 1 || snapshot.Tasks[0].ID != "a") {
				t.Fatalf("task filtering incorrect: %#v", snapshot.Tasks)
			}
			if request.Mode == BoardGit && snapshot.Git != nil {
				t.Fatal("fixture has no Git snapshot")
			}
		})
	}
}
func TestBoardQueryProjectsModeSpecificFactsAndClock(t *testing.T) {
	now := time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)
	evaluationTime := now.Add(17 * time.Minute)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	if _, _, err := store.Write(ctx, "seed-board-projection-dependency", "test.seed", func(r ports.Repositories) (domain.Result, error) {
		dependency := coord.Dependency{ID: "a-needs-b", PrerequisiteTaskID: "b", DependentTaskID: "a", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}
		if err := r.Coordination().CreateDependency(ctx, dependency, now); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "seed-board-projection-dependency", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	messages := messageapp.New(store, func() time.Time { return now })
	for _, message := range []coord.MailMessage{
		{ID: "message-for-a", Type: coord.MessageNotice, ThreadID: "thread-a", SenderSessionID: "source", Recipients: []coord.RecipientTarget{{SessionID: "source"}}, Subject: "A", Body: "A", RelatedTaskID: "a"},
		{ID: "message-for-b", Type: coord.MessageNotice, ThreadID: "thread-b", SenderSessionID: "source", Recipients: []coord.RecipientTarget{{SessionID: "source"}}, Subject: "B", Body: "B", RelatedTaskID: "b"},
	} {
		if _, err := messages.Send(ctx, domain.IdempotencyKey("send-"+message.ID), testsupport.Project, message); err != nil {
			t.Fatal(err)
		}
	}
	seedBoardOrphanObservation(t, store, now)
	service := NewWithClock(store, fixedClock{now: evaluationTime})
	actor := boardActor(domain.CapabilityRead)
	all, err := service.Query(ctx, actor, BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := service.Query(ctx, actor, BoardRequest{Mode: BoardTree})
	if err != nil {
		t.Fatal(err)
	}
	git, err := service.Query(ctx, actor, BoardRequest{Mode: BoardGit})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.Query(ctx, actor, BoardRequest{Mode: BoardTask, TaskID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	allSnapshot := decodeBoard(t, all)
	treeSnapshot := decodeBoard(t, tree)
	gitSnapshot := decodeBoard(t, git)
	taskSnapshot := decodeBoard(t, task)
	if allSnapshot.Git == nil || len(allSnapshot.Tasks) == 0 || len(allSnapshot.Runs) == 0 || len(allSnapshot.Dependencies) == 0 || len(allSnapshot.Inbox) != 2 {
		t.Fatalf("mixed all projection missing seeded facts: %#v", allSnapshot)
	}
	if len(treeSnapshot.Sessions) == 0 || len(treeSnapshot.Tasks) == 0 || len(treeSnapshot.Runs) == 0 || len(treeSnapshot.Dependencies) == 0 || treeSnapshot.Git != nil || len(treeSnapshot.Progress) != 0 || len(treeSnapshot.Inbox) != 0 || len(treeSnapshot.Handoffs) != 0 || len(treeSnapshot.Reservations) != 0 {
		t.Fatalf("tree projection included non-tree facts: %#v", treeSnapshot)
	}
	if gitSnapshot.Git == nil || len(gitSnapshot.Sessions) == 0 || len(gitSnapshot.Tasks) != 0 || len(gitSnapshot.Runs) != 0 || len(gitSnapshot.Progress) != 0 || len(gitSnapshot.Dependencies) != 0 || len(gitSnapshot.Inbox) != 0 || len(gitSnapshot.Handoffs) != 0 || len(gitSnapshot.Reservations) != 0 {
		t.Fatalf("git projection included non-git facts: %#v", gitSnapshot)
	}
	messageIDs := map[string]bool{}
	for _, message := range taskSnapshot.Inbox {
		messageIDs[message.MessageID] = true
	}
	for _, identity := range gitSnapshot.Sessions {
		if identity.TaskID != "" || identity.PreviousTaskID != "" {
			t.Fatalf("git projection leaked task identity data: %#v", identity)
		}
	}
	for _, asset := range gitSnapshot.Git.Assets {
		if asset.OwnerTaskID != "" {
			t.Fatalf("git projection leaked task ownership data: %#v", asset)
		}
	}
	if !messageIDs["message-for-a"] || messageIDs["message-for-b"] {
		t.Fatalf("task projection did not scope messages to task a: %#v", taskSnapshot.Inbox)
	}
	if !allSnapshot.GeneratedAt.Equal(evaluationTime) || !treeSnapshot.GeneratedAt.Equal(evaluationTime) || !gitSnapshot.GeneratedAt.Equal(evaluationTime) || !taskSnapshot.GeneratedAt.Equal(evaluationTime) {
		t.Fatalf("generated_at did not use evaluation clock: all=%s tree=%s git=%s task=%s", allSnapshot.GeneratedAt, treeSnapshot.GeneratedAt, gitSnapshot.GeneratedAt, taskSnapshot.GeneratedAt)
	}
	emptyStore, _ := testsupport.Store(t, now)
	empty := NewWithClock(emptyStore, fixedClock{now: evaluationTime})
	first, err := empty.Query(ctx, actor, BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	second, err := empty.Query(ctx, actor, BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	if !decodeBoard(t, first).GeneratedAt.Equal(evaluationTime) || first.SnapshotCursor() != second.SnapshotCursor() {
		t.Fatalf("empty snapshot clock/cursor contract failed: first=%#v second=%#v", decodeBoard(t, first), decodeBoard(t, second))
	}
}

func TestBoardQueryStrictlyValidatesModeSelectors(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store)
	actor := boardActor(domain.CapabilityRead)
	cases := []struct {
		name    string
		request BoardRequest
		wantErr bool
	}{
		{name: "me requires session", request: BoardRequest{Mode: BoardMe}, wantErr: true},
		{name: "me rejects task", request: BoardRequest{Mode: BoardMe, SessionID: "source", TaskID: "a"}, wantErr: true},
		{name: "me accepts only session", request: BoardRequest{Mode: BoardMe, SessionID: "source"}},
		{name: "task requires task", request: BoardRequest{Mode: BoardTask}, wantErr: true},
		{name: "task rejects session", request: BoardRequest{Mode: BoardTask, SessionID: "source", TaskID: "a"}, wantErr: true},
		{name: "task accepts only task", request: BoardRequest{Mode: BoardTask, TaskID: "a"}},
		{name: "all rejects session", request: BoardRequest{Mode: BoardAll, SessionID: "source"}, wantErr: true},
		{name: "all rejects task", request: BoardRequest{Mode: BoardAll, TaskID: "a"}, wantErr: true},
		{name: "all accepts no selector", request: BoardRequest{Mode: BoardAll}},
		{name: "tree rejects selector", request: BoardRequest{Mode: BoardTree, SessionID: "source"}, wantErr: true},
		{name: "git rejects selector", request: BoardRequest{Mode: BoardGit, TaskID: "a"}, wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Query(context.Background(), actor, test.request)
			if (err != nil) != test.wantErr {
				t.Fatalf("Query(%#v) error = %v, want error=%t", test.request, err, test.wantErr)
			}
		})
	}
}

func TestBoardIdentityUsesLatestRunAndSafeWorktreeFingerprint(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	if _, _, err := store.Write(ctx, "identity-history", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		session := lineage.AgentSession{
			ID: "history", ProjectID: lineage.ID(testsupport.Project), HumanID: "human", Kind: lineage.HumanDirect,
			Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "fixture",
			RootID: "history", TaskID: "a", WorktreeRef: "/private/worktree", StartedAt: now,
		}
		if err := c.CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		for i, taskID := range []lineage.ID{"b", "c"} {
			if err := c.CreateRun(ctx, lineage.TaskRun{ID: lineage.ID("history-run-" + string(rune('1'+i))), TaskID: taskID, SessionID: session.ID, State: lineage.RunRunning, StartedAt: now.Add(time.Duration(i+1) * time.Minute)}); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "identity-history", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	view, err := New(store).Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardMe, SessionID: "history"})
	if err != nil {
		t.Fatal(err)
	}
	identity := decodeBoard(t, view).Identity
	if identity == nil || identity.TaskID != "c" || identity.PreviousTaskID != "b" || !identity.WorktreeBound || identity.WorktreeFingerprint != fingerprint("/private/worktree") {
		t.Fatalf("identity history/worktree projection = %#v", identity)
	}
	if strings.Contains(string(view.Data()), "/private/worktree") {
		t.Fatal("private worktree path leaked")
	}
}

func TestBoardIdentityProjectsCheckpointHeartbeatInEveryMode(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	heartbeatAt := now.Add(time.Minute)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	if _, _, err := store.Write(ctx, "checkpoint-source", "test.write", func(r ports.Repositories) (domain.Result, error) {
		if err := r.Coordination().RecordHeartbeat(ctx, lineage.Heartbeat{
			ID: "checkpoint-source-heartbeat", SessionID: "source", ObservedAt: heartbeatAt, Liveness: lineage.Alive, Detail: []byte("{}"),
		}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "checkpoint-source", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, request := range []BoardRequest{
		{Mode: BoardMe, SessionID: "source"},
		{Mode: BoardTree},
		{Mode: BoardTask, TaskID: "a"},
		{Mode: BoardAll},
		{Mode: BoardGit},
	} {
		t.Run(string(request.Mode), func(t *testing.T) {
			view, err := New(store).Query(ctx, boardActor(domain.CapabilityRead), request)
			if err != nil {
				t.Fatal(err)
			}
			board := decodeBoard(t, view)
			identity := board.Identity
			if identity == nil || identity.ID != "source" {
				for i := range board.Sessions {
					if board.Sessions[i].ID == "source" {
						identity = &board.Sessions[i]
						break
					}
				}
			}
			if identity == nil || identity.ID != "source" || identity.HeartbeatAt == nil || !identity.HeartbeatAt.Equal(heartbeatAt) || identity.Liveness != SessionLivenessAlive {
				t.Fatalf("source heartbeat projection = %#v", identity)
			}
			if !strings.Contains(string(view.Data()), `"heartbeat_at":"2026-07-22T12:01:00Z"`) || !strings.Contains(string(view.Data()), `"liveness":"alive"`) {
				t.Fatalf("canonical JSON model omits live checkpoint status: %s", view.Data())
			}
		})
	}
}

func TestBoardIdentityProjectsStaleAndNoSignalLiveness(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	if _, _, err := store.Write(ctx, "stale-source", "test.write", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().RecordHeartbeat(ctx, lineage.Heartbeat{
			ID: "stale-source-heartbeat", SessionID: "source", ObservedAt: now.Add(time.Minute), Liveness: lineage.Stale, Detail: []byte(`{"private":"must-not-leak"}`),
		})
		return domain.Result{ID: "stale-source", Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}

	stale, err := New(store).Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardMe, SessionID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	staleBoard := decodeBoard(t, stale)
	if staleBoard.Identity == nil || staleBoard.Identity.Liveness != SessionLivenessStale {
		t.Fatalf("stale checkpoint projection = %#v", staleBoard.Identity)
	}
	if strings.Contains(string(stale.Data()), "must-not-leak") {
		t.Fatalf("private heartbeat detail leaked in board: %s", stale.Data())
	}

	noSignalStore, _ := testsupport.Store(t, now)
	testsupport.Seed(t, noSignalStore, now)
	noSignal, err := New(noSignalStore).Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardMe, SessionID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	if board := decodeBoard(t, noSignal); board.Identity == nil || board.Identity.Liveness != SessionLivenessNoSignal {
		t.Fatalf("no-signal projection = %#v", board.Identity)
	}
}

func TestAnnotateIdentityGitSelectsOnlyCanonicalMatchingWorktree(t *testing.T) {
	const sessionID = "owner"
	worktree := "/private/primary"
	sessions := map[string]lineage.AgentSession{
		sessionID: {ID: sessionID, WorktreeRef: worktree},
	}
	snapshot := BoardSnapshot{
		Identity: &IdentityView{ID: sessionID},
		Sessions: []IdentityView{{ID: sessionID}},
	}
	git := gitdomain.Snapshot{Assets: []gitdomain.AssetRecord{
		{Asset: gitdomain.Asset{Facts: gitdomain.AssetFacts{WorktreePath: worktree, Branch: "matching"}}, OwnerSessionID: sessionID},
		{Asset: gitdomain.Asset{Facts: gitdomain.AssetFacts{WorktreePath: "/private/adopted", Branch: "adopted"}}, OwnerSessionID: sessionID},
	}}

	annotateIdentityGit(&snapshot, sessions, git)

	if snapshot.Identity.Branch != "matching" || snapshot.Sessions[0].Branch != "matching" {
		t.Fatalf("identity branches = %#v, %#v", snapshot.Identity.Branch, snapshot.Sessions[0].Branch)
	}
}

func TestBoardQueryUsesCanonicalAuditCursorInsteadOfVisibleContentHash(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	actor := boardActor(domain.CapabilityRead)
	service := NewWithClock(store, fixedClock{now: now})

	first, err := service.Query(ctx, actor, BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "board-audit-position", "test.write", func(ports.Repositories) (domain.Result, error) {
		return domain.Result{ID: "board-audit-position", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	second, err := service.Query(ctx, actor, BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	before, after := decodeBoard(t, first), decodeBoard(t, second)
	if before.SnapshotCursor == after.SnapshotCursor {
		t.Fatal("canonical audit cursor did not advance after an invisible audit event")
	}
	if string(first.Data()) == string(second.Data()) {
		t.Fatal("canonical payload did not expose its new audit position")
	}
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Sessions, after.Sessions) || !reflect.DeepEqual(before.Runs, after.Runs) {
		t.Fatal("invisible audit event changed visible board facts")
	}
}

func TestBoardQueryRedactsArbitraryTextAndPrivateSessionFields(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	view, err := New(store).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	payload := string(view.Data())
	for _, forbidden := range []string{"runtime_home", "native_session", "worktree_ref", "source_ref", "evidence", "heartbeat", "final_output_text", "pattern_value", "/private/", "secret"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("private value %q leaked in %s", forbidden, payload)
		}
	}
	snapshot := decodeBoard(t, view)
	if !snapshot.Redaction.ContentOmitted || snapshot.Redaction.ContentRedacted {
		t.Fatalf("redaction metadata did not accurately report omitted/redacted content: %#v", snapshot.Redaction)
	}
	if len(snapshot.Tasks) == 0 || snapshot.Tasks[0].Title != "a" {
		t.Fatalf("benign task text was not preserved: %#v", snapshot.Tasks)
	}
}

func TestBoardQueryResolvesNativeStateOnDemandWithoutLeakingLocator(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	session := lineage.AgentSession{
		ID: "native-session", ProjectID: lineage.ID(testsupport.Project), Kind: lineage.Imported,
		Runtime: "runtime-a", Role: "reviewer", Source: lineage.SourceImport, SourceRef: "fixture",
		RootID: "native-session", StartedAt: now, NativeAccessState: lineage.NativeAccessAvailable,
		RuntimeHome: "/private/runtime-home", NativeSessionID: "native-1", NativeSessionRef: "opaque-private-ref",
	}
	session.NativeSessionFingerprint = lineage.NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, nil)
	if _, _, err := store.Write(context.Background(), "native-query-fixture", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		if err := repositories.Coordination().CreateSession(context.Background(), session); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "native-query-fixture", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	var calls int
	resolver := nativeResolverFunc(func(_ context.Context, got lineage.AgentSession) (ports.NativeSessionResolution, error) {
		calls++
		if got.RuntimeHome != session.RuntimeHome || got.NativeSessionRef != session.NativeSessionRef {
			t.Fatal("query did not delegate the stored locator to the resolver")
		}
		return ports.NativeSessionResolution{AccessState: lineage.NativeAccessMissing}, nil
	})
	view, err := NewWithNativeResolver(store, resolver).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decodeBoard(t, view)
	var found bool
	for _, identity := range snapshot.Sessions {
		if identity.ID == string(session.ID) {
			found = true
			if identity.NativeAccessState != string(lineage.NativeAccessMissing) {
				t.Fatalf("resolved access state = %q", identity.NativeAccessState)
			}
		}
	}
	if !found || calls != 1 {
		t.Fatalf("native identity found=%t resolver calls=%d", found, calls)
	}
	for _, private := range []string{session.RuntimeHome, session.NativeSessionRef, session.NativeSessionFingerprint} {
		if strings.Contains(string(view.Data()), private) {
			t.Fatalf("private locator leaked: %q", private)
		}
	}

	view, err = New(store).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = decodeBoard(t, view)
	for _, identity := range snapshot.Sessions {
		if identity.ID == string(session.ID) && identity.NativeAccessState != string(lineage.NativeAccessUnsupported) {
			t.Fatalf("missing adapter state = %q", identity.NativeAccessState)
		}
	}

	privateFailure := errors.New("fingerprint mismatch at /private/runtime-home")
	_, err = NewWithNativeResolver(store, nativeResolverFunc(func(context.Context, lineage.AgentSession) (ports.NativeSessionResolution, error) {
		return ports.NativeSessionResolution{}, privateFailure
	})).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err == nil || strings.Contains(err.Error(), "/private/") {
		t.Fatalf("unsafe resolver error = %v", err)
	}
}

func TestBoardQueryTerminatesNativeResolutionCancellation(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	session := lineage.AgentSession{
		ID: "native-cancelled", ProjectID: lineage.ID(testsupport.Project), Kind: lineage.Imported,
		Runtime: "runtime-a", Role: "reviewer", Source: lineage.SourceImport, SourceRef: "fixture",
		RootID: "native-cancelled", StartedAt: now, NativeAccessState: lineage.NativeAccessAvailable,
		RuntimeHome: "/private/runtime-home", NativeSessionID: "native-cancelled-1", NativeSessionRef: "opaque-native-ref",
	}
	session.NativeSessionFingerprint = lineage.NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, nil)
	if _, _, err := store.Write(context.Background(), "native-cancelled-fixture", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		if err := repositories.Coordination().CreateSession(context.Background(), session); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "native-cancelled-fixture", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			view, err := NewWithNativeResolver(store, nativeResolverFunc(func(context.Context, lineage.AgentSession) (ports.NativeSessionResolution, error) {
				return ports.NativeSessionResolution{}, want
			})).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
			if err != want {
				t.Fatalf("error = %v, want exact %v", err, want)
			}
			if view.Kind() != "" || view.SnapshotCursor() != "" || len(view.Data()) != 0 {
				t.Fatalf("cancellation produced board = %#v", view)
			}
		})
	}
}

func TestBoardQueryResolvesEachNativeSessionOncePerSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	session := lineage.AgentSession{
		ID: "native-duplicate", ProjectID: lineage.ID(testsupport.Project), Kind: lineage.Imported,
		Runtime: "runtime-a", Role: "reviewer", Source: lineage.SourceImport, SourceRef: "fixture",
		RootID: "native-duplicate", StartedAt: now, NativeAccessState: lineage.NativeAccessAvailable,
		RuntimeHome: "/private/runtime-home", NativeSessionID: "native-duplicate-1", NativeSessionRef: "opaque-native-ref",
	}
	session.NativeSessionFingerprint = lineage.NativeSessionFingerprint(session.Runtime, session.NativeSessionID, session.NativeSessionRef, nil)
	if _, _, err := store.Write(context.Background(), "native-duplicate-fixture", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		if err := repositories.Coordination().CreateSession(context.Background(), session); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "native-duplicate-fixture", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	view, err := NewWithNativeResolver(store, nativeResolverFunc(func(context.Context, lineage.AgentSession) (ports.NativeSessionResolution, error) {
		calls++
		if calls == 1 {
			return ports.NativeSessionResolution{AccessState: lineage.NativeAccessAvailable}, nil
		}
		return ports.NativeSessionResolution{AccessState: lineage.NativeAccessMissing}, nil
	})).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardMe, SessionID: string(session.ID)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decodeBoard(t, view)
	if calls != 1 || snapshot.Identity == nil || snapshot.Identity.NativeAccessState != string(lineage.NativeAccessAvailable) || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].NativeAccessState != string(lineage.NativeAccessAvailable) {
		t.Fatalf("native resolution was not immutable per snapshot: calls=%d identity=%#v sessions=%#v", calls, snapshot.Identity, snapshot.Sessions)
	}
}

func TestBoardProjectsImmutableHandoffDecisionWithoutChangingStatus(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	if _, _, err := store.Write(ctx, "handoff-decision-board", "test.write", func(r ports.Repositories) (domain.Result, error) {
		handoff := coord.Handoff{ID: "handoff-decision", TaskID: "a", RunID: "run", SourceSessionID: "source", Summary: "review", FinalOutput: coord.SensitiveText{Policy: coord.FinalOutputNone}, Status: coord.HandoffSubmitted, CreatedAt: now}
		if err := r.Coordination().CreateHandoff(ctx, handoff); err != nil {
			return domain.Result{}, err
		}
		if err := r.Coordination().CreateHandoffDecision(ctx, coord.HandoffDecision{ID: "decision-rejected", HandoffID: handoff.ID, Decision: coord.HandoffRejected, DecidedBySessionID: "target", CreatedAt: now.Add(time.Second)}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "handoff-decision-board", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	view, err := New(store).Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardTask, TaskID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decodeBoard(t, view)
	if len(snapshot.Handoffs) != 1 || snapshot.Handoffs[0].Status != string(coord.HandoffSubmitted) || snapshot.Handoffs[0].RunState != string(lineage.RunWorkComplete) || snapshot.Handoffs[0].Decision == nil || snapshot.Handoffs[0].Decision.Decision != string(coord.HandoffRejected) || snapshot.Handoffs[0].Decision.ActorSessionID != "target" {
		t.Fatalf("handoff decision projection = %#v", snapshot.Handoffs)
	}
}

func TestSuggestedActionsBindOriginScopeWithoutLeakingPrivateSelectors(t *testing.T) {
	snapshot := BoardSnapshot{
		Scope:        BoardScope{ProjectID: "project-origin", WorkspaceID: "workspace-origin", Mode: BoardTask, Selector: "task-origin"},
		Tasks:        []TaskView{{ID: "task-origin"}},
		Handoffs:     []HandoffView{{ID: "handoff-origin"}},
		Reservations: []ReservationView{{ID: "reservation-origin"}},
		Git:          &GitView{Assets: []GitAssetView{{Fingerprint: "asset-origin", Classification: []string{"dirty_unowned"}}}},
	}
	dedupeAndAdviseForOS(&snapshot, "darwin")
	if len(snapshot.SuggestedActions) != 4 {
		t.Fatalf("suggested actions = %#v", snapshot.SuggestedActions)
	}
	wantCommands := map[string]string{
		"show_task":           "omg board task --project '<PROJECT_PATH>' --task task-origin",
		"show_handoff":        `omg handoff show --project '<PROJECT_PATH>' --payload '{"handoff_id":"handoff-origin"}'`,
		"reservation_history": `omg reserve history --project '<PROJECT_PATH>' --payload '{"reservation_id":"reservation-origin"}'`,
		"git_cleanup_plan":    `omg git cleanup-plan --project '<PROJECT_PATH>' --payload '{"fingerprint":"asset-origin"}'`,
	}
	for _, action := range snapshot.SuggestedActions {
		if action.Command == "" || action.Shell != "" || len(action.Argv) == 0 {
			t.Fatalf("suggested action must be a reviewable non-shell template: %#v", action)
		}
		if !strings.Contains(action.Command, "'<PROJECT_PATH>'") || !slices.Contains(action.Argv, "<PROJECT_PATH>") {
			t.Fatalf("suggested action lacks explicit project placeholder: %#v", action)
		}
		if action.Command != wantCommands[action.Code] {
			t.Fatalf("suggested action command for %s = %q; want %q", action.Code, action.Command, wantCommands[action.Code])
		}
		delete(wantCommands, action.Code)
		if action.Scope != (BoardScope{ProjectID: "project-origin", WorkspaceID: "workspace-origin", Mode: BoardTask}) {
			t.Fatalf("suggested action origin scope = %#v", action.Scope)
		}
	}
	if len(wantCommands) != 0 {
		t.Fatalf("missing suggested actions: %#v", wantCommands)
	}

	private := BoardSnapshot{
		Scope: BoardScope{ProjectID: "/private/project", WorkspaceID: `C:\private\workspace`, Mode: BoardAll},
		Tasks: []TaskView{{ID: "task-private"}},
	}
	dedupeAndAdviseForOS(&private, "windows")
	action := private.SuggestedActions[0]
	if action.Command == "" || action.Shell != "" || len(action.Argv) == 0 || action.Scope.ProjectID != "" || action.Scope.WorkspaceID != "" {
		t.Fatalf("private selection handling in suggested action = %#v", action)
	}
	if !strings.Contains(action.Command, "'<PROJECT_PATH>'") || !slices.Contains(action.Argv, "<PROJECT_PATH>") {
		t.Fatalf("private selection action lacks explicit placeholder: %#v", action)
	}
	encoded, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "/private/") || strings.Contains(string(encoded), `C:\private`) {
		t.Fatalf("private selection leaked into JSON: %s", encoded)
	}
}

func TestBoardQueryPreservesDistinctRecipientDeliveryState(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	messages := messageapp.New(store, func() time.Time { return now })
	message := coord.MailMessage{
		ID: "recipient-state", Type: coord.MessageNotice, ThreadID: "thread", SenderSessionID: "source",
		Recipients: []coord.RecipientTarget{{SessionID: "source"}, {SessionID: "target"}},
		Subject:    "state", Body: "inert",
	}
	if _, err := messages.Send(ctx, "recipient-state-send", testsupport.Project, message); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.Read(ctx, "recipient-state-read-source", message.ID, coord.RecipientTarget{SessionID: "source"}); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.Read(ctx, "recipient-state-read-target", message.ID, coord.RecipientTarget{SessionID: "target"}); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.Acknowledge(ctx, "recipient-state-ack-target", message.ID, coord.RecipientTarget{SessionID: "target"}); err != nil {
		t.Fatal(err)
	}

	service := NewWithClock(store, fixedClock{now: now})
	first, err := service.Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Data()) != string(second.Data()) {
		t.Fatal("unchanged recipient delivery state rendered non-deterministically")
	}
	inbox := decodeBoard(t, first).Inbox
	var source, target *InboxItemView
	for i := range inbox {
		if inbox[i].MessageID != message.ID {
			continue
		}
		switch inbox[i].Recipient {
		case "session:source":
			source = &inbox[i]
		case "session:target":
			target = &inbox[i]
		}
	}
	if source == nil || target == nil || source.Acknowledgement != "read" || target.Acknowledgement != "acknowledged" {
		t.Fatalf("recipient delivery states were lost: %#v", inbox)
	}
}

func TestBoardQueryIncludesTaskHumanAndRoleRecipientInbox(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	messages := messageapp.New(store, func() time.Time { return now })
	for _, message := range []coord.MailMessage{
		testsupport.Message("dependency-unblocked", coord.MessageDependency, coord.RecipientTarget{TaskID: "a"}, "unblocked"),
		testsupport.Message("human-notice", coord.MessageNotice, coord.RecipientTarget{HumanID: "human"}, "human"),
		testsupport.Message("review-notice", coord.MessageNotice, coord.RecipientTarget{Role: "reviewer"}, "review"),
	} {
		if _, err := messages.Send(ctx, domain.IdempotencyKey("send-"+message.ID), testsupport.Project, message); err != nil {
			t.Fatal(err)
		}
	}
	privateRole := "/Users/alice/private/reviewer"
	privateMessage := testsupport.Message("private-role-notice", coord.MessageNotice, coord.RecipientTarget{Role: privateRole}, "private-role")
	if _, _, err := store.Write(ctx, "seed-private-role", "test.seed", func(repositories ports.Repositories) (domain.Result, error) {
		err := repositories.Coordination().CreateMessage(ctx, testsupport.Project, privateMessage)
		return domain.Result{ID: domain.ResultID(privateMessage.ID), Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	service := NewWithClock(store, fixedClock{now: now})
	taskView, err := service.Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardTask, TaskID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	taskRecipients := map[string]bool{}
	for _, item := range decodeBoard(t, taskView).Inbox {
		taskRecipients[item.Recipient] = true
	}
	if !taskRecipients["task:a"] || !taskRecipients["human:human"] {
		t.Fatalf("task board omitted relevant recipient inbox: %#v", taskRecipients)
	}
	allView, err := service.Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	allRecipients := map[string]bool{}
	for _, item := range decodeBoard(t, allView).Inbox {
		allRecipients[item.Recipient] = true
	}
	payload := string(allView.Data())
	if strings.Contains(payload, privateRole) {
		t.Fatalf("role recipient leaked a private path: %s", payload)
	}
	for _, recipient := range []string{"task:a", "human:human", "role:reviewer"} {
		if !allRecipients[recipient] {
			t.Fatalf("all board omitted %s recipient inbox: %#v", recipient, allRecipients)
		}
	}
}

func TestAnnotateIdentityGitNormalizesWindowsWorktreeSeparatorsAndCase(t *testing.T) {
	snapshot := BoardSnapshot{Identity: &IdentityView{ID: "owner"}, Sessions: []IdentityView{{ID: "owner"}}}
	sessions := map[string]lineage.AgentSession{"owner": {ID: "owner", WorktreeRef: `C:/Repo/Work`}}
	git := gitdomain.Snapshot{Assets: []gitdomain.AssetRecord{{Asset: gitdomain.Asset{Facts: gitdomain.AssetFacts{WorktreePath: `c:\repo\work`, Branch: "matching"}}, OwnerSessionID: "owner"}}}
	annotateIdentityGitForOS(&snapshot, sessions, git, "windows")
	if snapshot.Identity.Branch != "matching" || snapshot.Sessions[0].Branch != "matching" {
		t.Fatalf("separator- and case-equivalent Windows worktree did not annotate branch: %#v", snapshot)
	}
	if path, ok := canonicalWorktreeRefForOS(`C:/Repo/Work`, "windows"); !ok || path != `c:\repo\work` {
		t.Fatalf("Windows worktree canonicalization = %q, %t", path, ok)
	}
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i := range in {
		out[i] = in[i]
	}
	return out
}

func TestBoardQueryRedactsSeededLegacyDelegationToken(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	raw := "omgdt_v1_" + strings.Repeat("a", 43)
	if _, err := db.Exec("UPDATE tasks SET title=? WHERE id='a'", "legacy "+raw); err != nil {
		t.Fatal(err)
	}
	view, err := New(store).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardTask, TaskID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	payload := string(view.Data())
	if strings.Contains(payload, raw) || !strings.Contains(payload, "[REDACTED:OMG_DELEGATION_TOKEN]") {
		t.Fatalf("legacy delegation token was not redacted: %s", payload)
	}
	if !decodeBoard(t, view).Redaction.ContentRedacted {
		t.Fatal("redaction metadata did not report redacted legacy delegation token")
	}
}

type boardGitScanner struct{ observation gitdomain.Observation }

func (s boardGitScanner) Scan(context.Context, string) (gitdomain.Observation, error) {
	return s.observation, nil
}

func seedBoardGitAdopter(t *testing.T, store ports.Store, now time.Time, id string, state lineage.RunState) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := store.Write(ctx, domain.IdempotencyKey("seed-board-"+id), "test.write", func(r ports.Repositories) (domain.Result, error) {
		session := lineage.AgentSession{
			ID: lineage.ID(id), ProjectID: lineage.ID(testsupport.Project), HumanID: "human",
			Kind: lineage.HumanDirect, Runtime: "test", Role: "owner",
			Source: lineage.SourceHuman, SourceRef: "fixture", RootID: lineage.ID(id),
			TaskID: "a", StartedAt: now,
		}
		if err := r.Coordination().CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		if err := r.Coordination().CreateRun(ctx, lineage.TaskRun{ID: lineage.ID(id + "-run"), TaskID: "a", SessionID: session.ID, State: state, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: domain.ResultID("seed-board-" + id), Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func seedBoardOrphanObservation(t *testing.T, store ports.Store, now time.Time) gitdomain.Snapshot {
	t.Helper()
	asset := gitdomain.Asset{
		Facts:          gitdomain.AssetFacts{Confidence: gitdomain.ConfidenceObserved, WorktreePath: "/private/orphan", Status: gitdomain.Status{Confidence: gitdomain.ConfidenceObserved}},
		Worktree:       gitdomain.Worktree{Path: "/private/orphan"},
		Classification: gitdomain.AssetClassification{Labels: []gitdomain.Classification{gitdomain.ClassUnknown}, Confidence: gitdomain.ConfidenceObserved},
	}
	observation := gitdomain.Observation{
		Revision: gitdomain.ObservationRevision, Repository: gitdomain.RepoWorktree,
		Confidence: gitdomain.ConfidenceObserved, CommonDir: "/private/.git",
		TopLevel: "/private", Assets: []gitdomain.Asset{asset},
	}
	observation.Hash = gitdomain.HashObservation(observation)
	if _, err := appgit.New(store, boardGitScanner{observation: observation}, func() time.Time { return now }).Scan(
		context.Background(), "board-git-scan",
		appgit.ScanRequest{ProjectID: testsupport.Project, SessionID: "source", TaskID: "a", RunID: "run", Directory: "/private"},
	); err != nil {
		t.Fatal(err)
	}
	var snapshot gitdomain.Snapshot
	if err := store.Read(context.Background(), func(r ports.Repositories) error {
		var ok bool
		var err error
		snapshot, ok, err = r.Git().LatestSnapshot(context.Background(), testsupport.Project)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("Git observation was not persisted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func boardGitOwner(t *testing.T, service Service, mode BoardMode) string {
	t.Helper()
	view, err := service.Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decodeBoard(t, view)
	if snapshot.Git == nil || len(snapshot.Git.Assets) != 1 {
		t.Fatalf("%s Git projection = %#v", mode, snapshot.Git)
	}
	return snapshot.Git.Assets[0].OwnerSessionID
}

func TestBoardGitOwnershipOverlaysCanonicalAdoptionsWithoutMutatingObservation(t *testing.T) {
	now := time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	persisted := seedBoardOrphanObservation(t, store, now)
	fingerprint := persisted.Assets[0].Fingerprint
	for _, mode := range []BoardMode{BoardAll, BoardGit} {
		view, err := New(store).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		asset := decodeBoard(t, view).Git.Assets[0]
		if asset.OwnerSessionID != "" || asset.OwnerState != string(gitdomain.OwnerUnknown) {
			t.Fatalf("%s did not fail closed for unknown ownership: %#v", mode, asset)
		}
	}
	seedBoardGitAdopter(t, store, now, "adopter-one", lineage.RunRunning)
	seedBoardGitAdopter(t, store, now, "adopter-two", lineage.RunRunning)

	adoptions := handoff.New(store, func() time.Time { return now.Add(time.Minute) })
	if _, err := adoptions.Adopt(context.Background(), "adopt-orphan", coord.Adoption{ID: "adopt-orphan", ProjectID: string(testsupport.Project), GitAssetID: fingerprint, NewOwnerSessionID: "adopter-one", Reason: "owner lost"}); err != nil {
		t.Fatal(err)
	}
	service := New(store)
	for _, mode := range []BoardMode{BoardAll, BoardGit} {
		if owner := boardGitOwner(t, service, mode); owner != "adopter-one" {
			t.Fatalf("%s did not project adopted owner: %q", mode, owner)
		}
	}
	current, err := appgit.New(store, nil, nil).RecordedCurrent(context.Background(), testsupport.Project)
	if err != nil || current.Assets[0].OwnerSessionID != "adopter-one" {
		t.Fatalf("git current disagrees after adoption: %+v, %v", current.Assets, err)
	}
	if _, err := handoff.New(store, func() time.Time { return now.Add(2 * time.Minute) }).Adopt(context.Background(), "reassign-orphan", coord.Adoption{ID: "reassign-orphan", ProjectID: string(testsupport.Project), GitAssetID: fingerprint, NewOwnerSessionID: "adopter-two", Reason: "owner reassigned"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []BoardMode{BoardAll, BoardGit} {
		if owner := boardGitOwner(t, service, mode); owner != "adopter-two" {
			t.Fatalf("%s did not project reassigned owner: %q", mode, owner)
		}
	}
	var after gitdomain.Snapshot
	if err := store.Read(context.Background(), func(r ports.Repositories) error {
		var ok bool
		var err error
		after, ok, err = r.Git().LatestSnapshot(context.Background(), testsupport.Project)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("persisted observation disappeared")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, after) {
		t.Fatalf("board query mutated immutable Git observation: before=%+v after=%+v", persisted, after)
	}
}

func TestBoardGitOwnershipFailsClosedForStaleAdoption(t *testing.T) {
	now := time.Date(2026, 7, 23, 15, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	persisted := seedBoardOrphanObservation(t, store, now)
	seedBoardGitAdopter(t, store, now, "stale-adopter", lineage.RunStale)
	if _, err := handoff.New(store, func() time.Time { return now.Add(time.Minute) }).Adopt(context.Background(), "adopt-stale", coord.Adoption{ID: "adopt-stale", ProjectID: string(testsupport.Project), GitAssetID: persisted.Assets[0].Fingerprint, NewOwnerSessionID: "stale-adopter", Reason: "stale owner"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []BoardMode{BoardAll, BoardGit} {
		view, err := New(store).Query(context.Background(), boardActor(domain.CapabilityRead), BoardRequest{Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		asset := decodeBoard(t, view).Git.Assets[0]
		if asset.OwnerSessionID != "stale-adopter" || asset.OwnerState != string(gitdomain.OwnerStale) {
			t.Fatalf("%s failed closed stale ownership projection: %#v", mode, asset)
		}
	}
	current, err := appgit.New(store, nil, nil).RecordedCurrent(context.Background(), testsupport.Project)
	if err != nil || len(current.Assets) != 1 || current.Assets[0].OwnerSessionID != "stale-adopter" || current.Assets[0].Facts.Owner.State != gitdomain.OwnerStale {
		t.Fatalf("git current did not report stale ownership: %+v, %v", current.Assets, err)
	}
}

func TestBoardTaskPreservesProgressHistoryWithoutRun(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	if _, _, err := store.Write(ctx, "seed-child-task", "test.write", func(r ports.Repositories) (domain.Result, error) {
		task := lineage.Task{
			ID: "child", ProjectID: lineage.ID(testsupport.Project), DisplayNumber: 4, Title: "child",
			State: lineage.TaskReady, CreatedBySessionID: "source", ParentTaskID: "a", CreatedAt: now, UpdatedAt: now,
		}
		if _, err := r.Coordination().CreateTask(ctx, task); err != nil {
			return domain.Result{}, err
		}
		if _, won, err := r.Coordination().ClaimTask(ctx, task.ID, "target", now); err != nil || !won {
			return domain.Result{}, err
		}
		return domain.Result{ID: "seed-child-task", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, update := range []struct {
		id string
		at time.Time
	}{
		{id: "progress-first", at: now.Add(time.Minute)},
		{id: "progress-second", at: now.Add(2 * time.Minute)},
	} {
		if _, err := progress.New(store, func() time.Time { return update.at }).Append(ctx, domain.IdempotencyKey(update.id), coord.Progress{
			ID: update.id, TaskID: "child", SessionID: "target", Phase: coord.PhaseImplement,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []BoardRequest{
		{Mode: BoardTask, TaskID: "child"},
		{Mode: BoardAll},
	} {
		t.Run(string(request.Mode), func(t *testing.T) {
			view, err := NewWithClock(store, fixedClock{now}).Query(ctx, boardActor(domain.CapabilityRead), request)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := decodeBoard(t, view)
			if request.Mode == BoardTask && (len(snapshot.Tasks) != 1 || snapshot.Tasks[0].CreatedBySessionID != "source" || snapshot.Tasks[0].ClaimedBySessionID != "target" || snapshot.Tasks[0].ParentTaskID != "a") {
				t.Fatalf("task ownership/tree projection = %#v", snapshot.Tasks)
			}
			if len(snapshot.Progress) != 2 || snapshot.Progress[0].ID != "progress-first" || snapshot.Progress[1].ID != "progress-second" {
				t.Fatalf("progress history export = %#v", snapshot.Progress)
			}
		})
	}
}

func TestReservationViewRejectsUnsafeLegacyPatternValue(t *testing.T) {
	v := reservationView(reservation.Reservation{
		ID:      "reservation-hostile",
		Pattern: reservation.Pattern{Kind: reservation.Exact, Value: "/private/worktree/secret.go", CaseSensitivity: reservation.CaseSensitive},
	}, nil, time.Time{})
	if v.Pattern != "" || strings.Contains(v.Pattern, "/private/") {
		t.Fatalf("unsafe reservation pattern projected: %#v", v)
	}
	secret := reservationView(reservation.Reservation{
		ID:      "reservation-secret",
		Pattern: reservation.Pattern{Kind: reservation.Exact, Value: "api_key=release", CaseSensitivity: reservation.CaseSensitive},
	}, nil, time.Time{})
	if strings.Contains(secret.Pattern, "api_key=release") || !strings.HasPrefix(secret.Pattern, "[REDACTED:") {
		t.Fatalf("secret-bearing reservation pattern was not redacted: %#v", secret)
	}
}

func TestBoardTaskProjectsSafeNormalizedReservationPattern(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	ctx := context.Background()
	pattern, err := reservation.NewPattern(reservation.Exact, `src\./safe.go`, reservation.CaseSensitive)
	if err != nil {
		t.Fatal(err)
	}
	item, err := reservation.New(reservation.ReservationInput{
		ID: "reservation-safe", Owner: reservation.Owner{HumanID: "human", SessionID: "source", TaskID: "a", RunID: "run"},
		Pattern: pattern, Mode: reservation.Exclusive, Intent: "edit", ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "seed-safe-reservation", "test.write", func(r ports.Repositories) (domain.Result, error) {
		if err := r.Reservations().Create(ctx, testsupport.Project, item, now); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "reservation-safe", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	view, err := NewWithClock(store, fixedClock{now}).Query(ctx, boardActor(domain.CapabilityRead), BoardRequest{Mode: BoardTask, TaskID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := decodeBoard(t, view)
	if len(snapshot.Reservations) != 1 || snapshot.Reservations[0].Pattern != "src/safe.go" {
		t.Fatalf("safe normalized reservation pattern = %#v", snapshot.Reservations)
	}
	if strings.Contains(string(view.Data()), `src\./safe.go`) || strings.Contains(string(view.Data()), "/private/") {
		t.Fatalf("unsafe reservation path leaked: %s", view.Data())
	}
}
