package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	lineage "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/platform"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/store/sqlite"
	"path/filepath"
)

func TestDispatchCoordinationQueryDoesNotLeakMessageBody(t *testing.T) {
	ctx, dispatcher, selection := coordinationDispatcher(t)
	seedCoordination(t, ctx, dispatcher.service, selection)

	sent := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion, Command: "message.send", Project: selection.Project, Workspace: selection.Workspace, Store: selection.Store, IdempotencyKey: "message-send", Payload: []byte(`{"id":"message-1","type":"NOTICE","thread_id":"thread-1","sender_session_id":"source","recipients":[{"session_id":"target"}],"subject":"/private/subject","body":"private body"}`),
	})
	if sent.Error.Code != "" {
		t.Fatalf("message.send outcome=%+v", sent)
	}
	delivered, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "message.deliver", IdempotencyKey: "message-lifecycle", Payload: []byte(`{"message_id":"message-1","recipient":{"session_id":"target"}}`),
	}, selection)
	if !handled || delivered.Error.Code != "" {
		t.Fatalf("message.deliver outcome=%+v handled=%t", delivered, handled)
	}
	read, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "message.read", IdempotencyKey: "message-lifecycle", Payload: []byte(`{"message_id":"message-1","recipient":{"session_id":"target"}}`),
	}, selection)
	if !handled || read.Error.Code != domain.CodeConflict {
		t.Fatalf("message.read reused delivery key outcome=%+v handled=%t", read, handled)
	}

	outcome, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "message.inbox", Payload: []byte(`{"recipient":{"session_id":"target"}}`),
	}, selection)
	if !handled || outcome.Error.Code != "" {
		t.Fatalf("message.inbox outcome=%+v handled=%t", outcome, handled)
	}
	encoded := mustJSON(t, outcome.Data)
	if strings.Contains(string(encoded), "private body") || strings.Contains(string(encoded), "/private/subject") || strings.Contains(string(encoded), `"body"`) {
		t.Fatalf("message inbox leaked body: %s", encoded)
	}
}

func TestSafeCoordinationProgressRedactsPrivateText(t *testing.T) {
	result := safeCoordinationProgress(coord.Progress{
		ID: "progress", TaskID: "task", SessionID: "session", Phase: coord.PhaseImplement,
		Done: []string{"/private/done"}, Doing: []string{"password=secret"}, Next: []string{"benign"},
	})
	encoded := string(mustJSON(t, result))
	if strings.Contains(encoded, "/private/done") || strings.Contains(encoded, "password=secret") || !strings.Contains(encoded, "benign") {
		t.Fatalf("unsafe progress projection: %s", encoded)
	}
}

func TestDispatchCoordinationRejectsSplitDelegationTokenProgressPayload(t *testing.T) {
	ctx, dispatcher, selection := coordinationDispatcher(t)
	seedCoordination(t, ctx, dispatcher.service, selection)
	token := "omgdt_v1_" + strings.Repeat("a", 43)
	payload := []byte(`{"id":"progress-split-token","task_id":"a","session_id":"source","phase":"implement","done":["` + token[:17] + `","` + token[17:] + `"],"doing":[],"next":[]}`)

	outcome, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "progress.add", IdempotencyKey: "progress-split-token", Payload: payload,
	}, selection)
	if !handled || outcome.Error.Code != domain.CodeInvalidArgument {
		t.Fatalf("progress.add outcome=%+v handled=%t", outcome, handled)
	}
	if encoded := string(mustJSON(t, outcome)); strings.Contains(encoded, token) {
		t.Fatalf("rejected payload leaked token: %s", encoded)
	}
}

func TestDispatchCoordinationHandoffAcceptGeneratesStableDecisionID(t *testing.T) {
	ctx, dispatcher, selection := coordinationDispatcher(t)
	seedCoordination(t, ctx, dispatcher.service, selection)
	createHandoff(t, ctx, dispatcher, selection)

	outcome, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "handoff.accept", IdempotencyKey: "accept-handoff", Payload: []byte(`{"handoff_id":"handoff-1","actor_session_id":"target"}`),
	}, selection)
	if !handled || outcome.Error.Code != "" {
		t.Fatalf("handoff.accept outcome=%+v handled=%t", outcome, handled)
	}
	decision, ok := outcome.Data.(coordinationDecisionResult)
	if !ok || decision.ID != coordinationDecisionID("accept-handoff", "handoff-1", "accept") || decision.Decision != string(coord.HandoffAccepted) {
		t.Fatalf("decision=%#v", outcome.Data)
	}
	rejected, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "handoff.reject", IdempotencyKey: "accept-handoff", Payload: []byte(`{"handoff_id":"handoff-1","actor_session_id":"target"}`),
	}, selection)
	if !handled || rejected.Error.Code != domain.CodeConflict {
		t.Fatalf("handoff.reject reused accept key outcome=%+v handled=%t", rejected, handled)
	}

	show, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "handoff.show", Payload: []byte(`{"handoff_id":"handoff-1"}`),
	}, selection)
	if !handled || show.Error.Code != "" {
		t.Fatalf("handoff.show outcome=%+v handled=%t", show, handled)
	}
	handoff, ok := show.Data.(coordinationHandoffResult)
	if !ok || handoff.Status != string(coord.HandoffSubmitted) || handoff.Decision == nil || handoff.Decision.Decision != string(coord.HandoffAccepted) || handoff.Decision.ActorSessionID != "target" {
		t.Fatalf("handoff.show did not preserve immutable decision fact: %#v", show.Data)
	}

	history, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "handoff.history", Payload: []byte(`{"task_id":"a"}`),
	}, selection)
	if !handled || history.Error.Code != "" {
		t.Fatalf("handoff.history outcome=%+v handled=%t", history, handled)
	}
	handoffs, ok := history.Data.([]coordinationHandoffResult)
	if !ok || len(handoffs) != 1 || handoffs[0].Decision == nil || handoffs[0].Decision.ID != handoff.Decision.ID {
		t.Fatalf("handoff.history did not project decision fact: %#v", history.Data)
	}
}

func TestDispatchCoordinationHandoffQueriesExposeSafeImmutableEvidenceAndRunState(t *testing.T) {
	ctx, dispatcher, selection := coordinationDispatcher(t)
	seedCoordination(t, ctx, dispatcher.service, selection)
	privatePath := "/private/customer/release.go"
	if err := dispatcher.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
		_, _, err := store.Write(ctx, "seed-verified-handoff-run", "test.write", func(r ports.Repositories) (domain.Result, error) {
			err := r.Coordination().CreateRun(ctx, lineage.TaskRun{ID: "verified-run", TaskID: "a", SessionID: "source", State: lineage.RunVerifiedDone, Evidence: []byte("verified"), StartedAt: time.Now().UTC()})
			return domain.Result{ID: "seed-verified-handoff-run", Outcome: domain.OutcomeOK}, err
		})
		return err
	}); err.Code != "" {
		t.Fatal(err)
	}
	verified := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion, Command: "handoff.create", Project: selection.Project, Workspace: selection.Workspace, Store: selection.Store, IdempotencyKey: "verified-handoff-create",
		Payload: []byte(`{"id":"handoff-verified","task_id":"a","run_id":"verified-run","source_session_id":"source","summary":"verified handoff","final_output_policy":"none","verification_evidence":[{"summary":"tests passed","hash":"sha256:verified"}]}`),
	})
	if verified.Error.Code != "" {
		t.Fatalf("verified handoff.create outcome=%+v", verified)
	}

	credentialText := "password=not-for-output"
	create := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion, Command: "handoff.create", Project: selection.Project, Workspace: selection.Workspace, Store: selection.Store, IdempotencyKey: "handoff-safe-evidence-create",
		Payload: []byte(`{"id":"handoff-evidence","task_id":"a","run_id":"run","source_session_id":"source","summary":"handoff","final_output_policy":"hash_only","final_output_hash":"sha256:final-output","changed_files":["` + privatePath + `"],"commits":["` + credentialText + `"],"verification_evidence":[{"summary":"` + privatePath + `","hash":"sha256:evidence"}],"remaining_risks":["` + credentialText + `"],"suggested_actions":["` + privatePath + `"]}`),
	})
	if create.Error.Code != "" {
		t.Fatalf("handoff.create outcome=%+v", create)
	}
	supersede, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "handoff.supersede", IdempotencyKey: "handoff-safe-evidence-supersede", Payload: []byte(`{"handoff_id":"handoff-evidence","new_id":"handoff-evidence-v2","summary":"revised"}`),
	}, selection)
	if !handled || supersede.Error.Code != "" {
		t.Fatalf("handoff.supersede outcome=%+v handled=%t", supersede, handled)
	}

	history, handled := dispatcher.dispatchCoordination(ctx, Request{
		Command: "handoff.history", Payload: []byte(`{"task_id":"a"}`),
	}, selection)
	if !handled || history.Error.Code != "" {
		t.Fatalf("handoff.history outcome=%+v handled=%t", history, handled)
	}
	handoffs, ok := history.Data.([]coordinationHandoffResult)
	if !ok || len(handoffs) != 3 {
		t.Fatalf("handoff.history = %#v", history.Data)
	}
	var original, revised, verifiedHandoff coordinationHandoffResult
	for _, handoff := range handoffs {
		switch handoff.ID {
		case "handoff-evidence":
			original = handoff
		case "handoff-evidence-v2":
			revised = handoff
		case "handoff-verified":
			verifiedHandoff = handoff
		}
	}
	if original.RunState != string(lineage.RunWorkComplete) || revised.RunState != string(lineage.RunWorkComplete) || verifiedHandoff.RunState != string(lineage.RunVerifiedDone) || revised.SupersedesID != original.ID {
		t.Fatalf("handoff run/supersede projection original=%#v revised=%#v verified=%#v", original, revised, verifiedHandoff)
	}
	if original.FinalOutputPolicy != string(coord.FinalOutputHashOnly) || original.FinalOutputHash != "sha256:final-output" {
		t.Fatalf("handoff output policy/hash changed: %#v", original)
	}
	if len(original.ChangedFiles) != 1 || len(original.Commits) != 1 || len(original.VerificationEvidence) != 1 || len(original.RemainingRisks) != 1 || len(original.SuggestedActions) != 1 {
		t.Fatalf("handoff evidence omitted from history: %#v", original)
	}
	encoded := string(mustJSON(t, history.Data))
	for _, unsafe := range []string{privatePath, credentialText} {
		if strings.Contains(encoded, unsafe) {
			t.Fatalf("handoff history leaked %q: %s", unsafe, encoded)
		}
	}
}

func TestDispatchCoordinationRejectsInvalidKeyShape(t *testing.T) {
	ctx := context.Background()
	dispatcher := &ServiceDispatcher{}
	selection := foundation.Selection{}
	for _, request := range []Request{
		{Command: "dependency.list", IdempotencyKey: "query-key", Payload: []byte(`{}`)},
		{Command: "progress.add", Payload: []byte(`{"id":"p","task_id":"a","session_id":"source","phase":"implement","done":[],"doing":[],"next":[]}`)},
	} {
		outcome, handled := dispatcher.dispatchCoordination(ctx, request, selection)
		if !handled || outcome.Error.Code != domain.CodeInvalidArgument {
			t.Fatalf("request=%+v outcome=%+v handled=%t", request, outcome, handled)
		}
	}
}

func coordinationDispatcher(t *testing.T) (context.Context, *ServiceDispatcher, foundation.Selection) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".omg"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolver := coordinationResolver{resolved: ports.ResolvedStore{Path: filepath.Join(root, ".omg", "state.db"), Project: "coordination-test", ProjectRoot: root}}
	service := foundation.New(foundation.Dependencies{
		Resolver:          resolver,
		ConfigInitializer: platform.NewProjectConfigInitializer(),
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			store, status, err := sqlite.Open(ctx, path, options)
			if err != nil {
				return nil, ports.OpenStatus{}, err
			}
			return store, status, nil
		},
	})
	selection := foundation.Selection{Project: root}
	if _, err := service.Init(ctx, selection); err.Code != "" {
		t.Fatal(err)
	}
	plan, err := service.Plan(ctx, selection)
	if err.Code != "" {
		t.Fatal(err)
	}
	backup, err := service.Backup(ctx, selection, &plan)
	if err.Code != "" {
		t.Fatal(err)
	}
	approval := foundation.ApprovalFile{ApprovalID: "coordination-dispatch-approval", ApprovedBy: "test", EvidenceReference: "coordination-dispatch-test", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano), ExpiresAtRaw: now.Add(5 * time.Minute).Format(time.RFC3339Nano)}
	if err := service.Apply(ctx, selection, plan, approval); err.Code != "" {
		t.Fatal(err)
	}
	return ctx, NewDispatcher(service), selection
}

type coordinationResolver struct{ resolved ports.ResolvedStore }

func (r coordinationResolver) Resolve(context.Context, ports.ResolveRequest) (ports.ResolvedStore, error) {
	return r.resolved, nil
}

func seedCoordination(t *testing.T, ctx context.Context, service *foundation.Service, selection foundation.Selection) {
	t.Helper()
	err := service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		now := time.Now().UTC()
		_, _, err := store.Write(ctx, "seed-coordination", "test.write", func(r ports.Repositories) (domain.Result, error) {
			c := r.Coordination()
			if err := c.CreateHuman(ctx, lineage.Human{ID: "human", DisplayName: "Human", Confidence: lineage.ConfidenceExplicit, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			for _, session := range []lineage.AgentSession{{ID: "source", ProjectID: lineage.ID(resolved.Project), HumanID: "human", Kind: lineage.HumanDirect, Runtime: "test", Role: "owner", Source: lineage.SourceHuman, SourceRef: "test", RootID: "source", StartedAt: now}, {ID: "target", ProjectID: lineage.ID(resolved.Project), HumanID: "human", Kind: lineage.HumanDirect, Runtime: "test", Role: "reviewer", Source: lineage.SourceHuman, SourceRef: "test", RootID: "target", StartedAt: now}} {
				if err := c.CreateSession(ctx, session); err != nil {
					return domain.Result{}, err
				}
			}
			if _, err := c.CreateTask(ctx, lineage.Task{ID: "a", ProjectID: lineage.ID(resolved.Project), DisplayNumber: 1, Title: "a", State: lineage.TaskClaimed, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if err := c.CreateRun(ctx, lineage.TaskRun{ID: "run", TaskID: "a", SessionID: "source", State: lineage.RunWorkComplete, StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			return domain.Result{ID: "seed-coordination", Outcome: domain.OutcomeOK}, nil
		})
		return err
	})
	if err.Code != "" {
		t.Fatal(err)
	}
}

func createHandoff(t *testing.T, ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection) {
	t.Helper()
	outcome := dispatcher.Dispatch(ctx, Request{Version: RequestVersion, Command: "handoff.create", Project: selection.Project, Workspace: selection.Workspace, Store: selection.Store, IdempotencyKey: "create-handoff", Payload: []byte(`{"id":"handoff-1","task_id":"a","run_id":"run","source_session_id":"source","summary":"handoff","final_output_policy":"none"}`)})
	if outcome.Error.Code != "" {
		t.Fatalf("handoff.create outcome=%+v", outcome)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
