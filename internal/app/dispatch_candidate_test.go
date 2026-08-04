package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestCandidateCloseBeforeSourceCleanedIsReadOnly(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, false)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)

	outcome, handled := dispatcher.dispatchCandidate(ctx, candidateCloseRequest("candidate-close-blocked"), selection)
	if !handled || outcome.Error.Code != "" {
		t.Fatalf("candidate.close outcome=%+v handled=%t", outcome, handled)
	}
	result, ok := outcome.Data.(candidateCloseResult)
	if !ok {
		t.Fatalf("candidate.close result=%#v", outcome.Data)
	}
	if result.Closed || result.ReadyToClose || result.CurrentState != string(coord.IntegrationSubmitted) || len(result.MissingEvidence) == 0 || len(result.AllowedTransitions) == 0 || len(result.NextArgv) == 0 || result.GitMutated {
		t.Fatalf("blocked candidate result=%+v", result)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("blocked candidate created a receipt: %d -> %d", before, after)
	}
}

func TestCandidateCloseAtomicallyFinalizesAndReplays(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, true)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)
	request := candidateCloseRequest("candidate-close-ready")

	first, handled := dispatcher.dispatchCandidate(ctx, request, selection)
	if !handled || first.Error.Code != "" {
		t.Fatalf("candidate.close outcome=%+v handled=%t", first, handled)
	}
	result, ok := first.Data.(candidateCloseResult)
	if !ok {
		t.Fatalf("candidate.close result=%#v", first.Data)
	}
	if !result.Closed || !result.ReadyToClose || result.CurrentState != string(coord.IntegrationSourceCleaned) || result.TaskState != string(lineage.TaskVerifiedDone) || result.RunState != string(lineage.RunVerifiedDone) || result.ReservationsReleased != 1 || !result.SessionArchived || result.GitMutated || len(result.NextArgv) != 0 {
		t.Fatalf("closed candidate result=%+v", result)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before+1 {
		t.Fatalf("candidate close receipt count=%d, want %d", after, before+1)
	}
	assertCandidateClosedState(t, ctx, dispatcher, selection)

	replay, handled := dispatcher.dispatchCandidate(ctx, request, selection)
	if !handled || replay.Error.Code != "" || !reflect.DeepEqual(replay.Data, first.Data) {
		t.Fatalf("candidate.close replay=%+v handled=%t, want %#v", replay, handled, first.Data)
	}
	if afterReplay := candidateReceiptCount(t, ctx, dispatcher, selection); afterReplay != before+1 {
		t.Fatalf("candidate close replay created another receipt: %d", afterReplay)
	}
	assertCandidateClosedState(t, ctx, dispatcher, selection)
}

func TestCandidateNextActionRecordsLifecycleFacts(t *testing.T) {
	payload := candidateClosePayload{HandoffID: "handoff", ActorSessionID: "actor"}
	acceptedArgv, _ := candidateNextAction(payload, coord.IntegrationAccepted, candidateCloseResult{})
	if len(acceptedArgv) < 3 || acceptedArgv[1] != "handoff" || acceptedArgv[2] != "advance" || !containsCandidateArg(acceptedArgv, string(coord.IntegrationIntegrated)) {
		t.Fatalf("accepted next argv=%v", acceptedArgv)
	}
	passedArgv, _ := candidateNextAction(payload, coord.IntegrationCanaryPassed, candidateCloseResult{})
	if len(passedArgv) < 3 || passedArgv[1] != "handoff" || passedArgv[2] != "advance" || !containsCandidateArg(passedArgv, string(coord.IntegrationSourceCleaned)) {
		t.Fatalf("canary-passed next argv=%v", passedArgv)
	}
}

func containsCandidateArg(values []string, needle string) bool {
	for _, value := range values {
		if value == needle || strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func candidateCloseRequest(key string) Request {
	payload, err := json.Marshal(candidateClosePayload{
		HandoffID:      "candidate-handoff",
		ActorSessionID: "candidate-actor",
		ArchiveEventID: "candidate-archive",
		Evidence:       "exact real Canary passed and source cleanup verified",
	})
	if err != nil {
		panic(err)
	}
	return Request{Command: "candidate.close", IdempotencyKey: key, Payload: payload}
}

func seedCandidateClose(t *testing.T, ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection, ready bool) {
	t.Helper()
	mapped := dispatcher.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		_, _, err := store.Write(ctx, domain.IdempotencyKey("seed-candidate-close"), "test.write", func(repositories ports.Repositories) (domain.Result, error) {
			now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
			coordination := repositories.Coordination()
			if err := coordination.CreateHuman(ctx, lineage.Human{ID: "candidate-human", DisplayName: "Operator", Confidence: lineage.ConfidenceExplicit, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			for _, session := range []lineage.AgentSession{
				{ID: "candidate-source", ProjectID: lineage.ID(resolved.Project), HumanID: "candidate-human", Kind: lineage.HumanDirect, Runtime: "test", Role: "worker", Source: lineage.SourceHuman, SourceRef: "test", RootID: "candidate-source", TaskID: "candidate-task", StartedAt: now},
				{ID: "candidate-actor", ProjectID: lineage.ID(resolved.Project), HumanID: "candidate-human", Kind: lineage.HumanDirect, Runtime: "test", Role: "reviewer", Source: lineage.SourceHuman, SourceRef: "test", RootID: "candidate-actor", StartedAt: now},
			} {
				if err := coordination.CreateSession(ctx, session); err != nil {
					return domain.Result{}, err
				}
			}
			task, err := coordination.CreateTask(ctx, lineage.Task{ID: "candidate-task", ProjectID: lineage.ID(resolved.Project), DisplayNumber: 1, Title: "candidate task", State: lineage.TaskReady, CreatedBySessionID: "candidate-source", CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return domain.Result{}, err
			}
			if _, won, err := coordination.ClaimTask(ctx, task.ID, "candidate-source", now); err != nil {
				return domain.Result{}, err
			} else if !won {
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate task claim failed", false)
			}
			if _, err := coordination.TransitionTask(ctx, task.ID, lineage.TaskWorkComplete, []byte("worker verification passed"), now); err != nil {
				return domain.Result{}, err
			}
			if err := coordination.CreateRun(ctx, lineage.TaskRun{ID: "candidate-run", TaskID: task.ID, SessionID: "candidate-source", State: lineage.RunWorkComplete, Evidence: []byte("worker verification passed"), StartedAt: now}); err != nil {
				return domain.Result{}, err
			}
			handoff := coord.Handoff{
				ID: "candidate-handoff", TaskID: string(task.ID), RunID: "candidate-run", SourceSessionID: "candidate-source", TargetSessionID: "candidate-actor",
				Summary: "candidate implementation complete", FinalOutput: coord.SensitiveText{Policy: coord.FinalOutputNone},
				SourceCommit: "source-commit", SourceTree: "source-tree", Status: coord.HandoffSubmitted, CreatedAt: now,
			}
			if err := coordination.CreateHandoff(ctx, handoff); err != nil {
				return domain.Result{}, err
			}
			if err := coordination.CreateHandoffLifecycleEvent(ctx, coord.HandoffLifecycleEvent{ID: "candidate-submitted", HandoffID: handoff.ID, ActorSessionID: handoff.SourceSessionID, State: coord.IntegrationSubmitted, SourceCommit: handoff.SourceCommit, SourceTree: handoff.SourceTree, CreatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			pattern, err := res.NewPattern(res.Exact, "internal/candidate.go", res.CaseSensitive)
			if err != nil {
				return domain.Result{}, err
			}
			reservation, err := res.New(res.ReservationInput{ID: "candidate-reservation", Pattern: pattern, Mode: res.Exclusive, Owner: res.Owner{HumanID: "candidate-human", SessionID: "candidate-source", TaskID: string(task.ID), RunID: "candidate-run"}, Intent: "candidate implementation", ExpiresAt: time.Now().UTC().Add(time.Hour)})
			if err != nil {
				return domain.Result{}, err
			}
			if err := repositories.Reservations().Create(ctx, domain.ProjectID(resolved.Project), reservation, now); err != nil {
				return domain.Result{}, err
			}
			if !ready {
				return domain.Result{ID: "seed-candidate-close", Outcome: domain.OutcomeOK}, nil
			}
			decision, err := coord.DecideHandoff(handoff, coord.HandoffAccepted, "candidate-decision", "candidate-actor", now.Add(time.Minute))
			if err != nil {
				return domain.Result{}, err
			}
			if err := coordination.CreateHandoffDecision(ctx, decision); err != nil {
				return domain.Result{}, err
			}
			started := now.Add(3 * time.Minute)
			finished := now.Add(4 * time.Minute)
			exitCode := 0
			events := []coord.HandoffLifecycleEvent{
				{ID: "candidate-integrated", HandoffID: handoff.ID, ActorSessionID: "candidate-actor", State: coord.IntegrationIntegrated, IntegrationCommit: "integration-commit", CreatedAt: now.Add(2 * time.Minute)},
				{ID: "candidate-canary-running", HandoffID: handoff.ID, ActorSessionID: "candidate-actor", State: coord.IntegrationCanaryRunning, CanaryRunID: "candidate-canary", CanaryIntegrationRef: "refs/heads/main", CanaryTargetSHA: "integration-commit", CanaryTargetTree: "integration-tree", CanaryCommand: "go test ./...", CanaryExecutionKind: "real", CanaryEnvironmentFingerprint: "environment-fingerprint", CanaryHeadBefore: "integration-commit", CanaryRefFingerprintBefore: "ref-fingerprint", CanaryStartedAt: &started, CreatedAt: started},
				{ID: "candidate-canary-passed", HandoffID: handoff.ID, ActorSessionID: "candidate-actor", State: coord.IntegrationCanaryPassed, CanaryRunID: "candidate-canary", CanaryIntegrationRef: "refs/heads/main", CanaryTargetSHA: "integration-commit", CanaryTargetTree: "integration-tree", CanaryResult: "PASS_REAL", CanaryCommand: "go test ./...", CanaryExecutionKind: "real", CanaryEnvironmentFingerprint: "environment-fingerprint", CanaryHeadBefore: "integration-commit", CanaryHeadAfter: "integration-commit", CanaryRefFingerprintBefore: "ref-fingerprint", CanaryRefFingerprintAfter: "ref-fingerprint", CanaryExitCode: &exitCode, CanaryPassedCount: 1, CanaryStartedAt: &started, CanaryFinishedAt: &finished, CreatedAt: finished},
				{ID: "candidate-source-cleaned", HandoffID: handoff.ID, ActorSessionID: "candidate-actor", State: coord.IntegrationSourceCleaned, SourceWorktreeCleaned: true, SourceBranchCleaned: true, CreatedAt: now.Add(5 * time.Minute)},
			}
			for _, event := range events {
				if event.Validate() != nil {
					return domain.Result{}, domain.NewError(domain.CodeInvalidArgument, "candidate test lifecycle event is invalid", false)
				}
				if err := coordination.CreateHandoffLifecycleEvent(ctx, event); err != nil {
					return domain.Result{}, err
				}
			}
			return domain.Result{ID: "seed-candidate-close", Outcome: domain.OutcomeOK}, nil
		})
		return err
	})
	if mapped.Code != "" {
		t.Fatalf("seed candidate close: %+v", mapped)
	}
}

func candidateReceiptCount(t *testing.T, ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection) int {
	t.Helper()
	count := 0
	mapped := dispatcher.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(repositories ports.Repositories) error {
			receipts, err := repositories.Receipts().ListReceipts(ctx)
			count = len(receipts)
			return err
		})
	})
	if mapped.Code != "" {
		t.Fatalf("count candidate receipts: %+v", mapped)
	}
	return count
}

func assertCandidateClosedState(t *testing.T, ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection) {
	t.Helper()
	mapped := dispatcher.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(repositories ports.Repositories) error {
			coordination := repositories.Coordination()
			task, found, err := coordination.GetTask(ctx, "candidate-task")
			if err != nil || !found || task.State != lineage.TaskVerifiedDone {
				t.Fatalf("candidate task=%+v found=%t err=%v", task, found, err)
			}
			run, found, err := coordination.GetRun(ctx, "candidate-run")
			if err != nil || !found || run.State != lineage.RunVerifiedDone {
				t.Fatalf("candidate run=%+v found=%t err=%v", run, found, err)
			}
			session, found, err := coordination.GetSession(ctx, "candidate-source")
			if err != nil || !found || session.InterruptedAt == nil || session.Liveness != lineage.Interrupted {
				t.Fatalf("candidate session=%+v found=%t err=%v", session, found, err)
			}
			reservation, found, err := repositories.Reservations().Get(ctx, domain.ProjectID(resolved.Project), "candidate-reservation")
			if err != nil || !found || reservation.LifecycleAt(time.Now().UTC()) != res.Released {
				t.Fatalf("candidate reservation=%+v found=%t err=%v", reservation, found, err)
			}
			return nil
		})
	})
	if mapped.Code != "" {
		t.Fatalf("read candidate close state: %+v", mapped)
	}
}
