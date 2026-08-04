package canary

import (
	"context"
	"strings"
	"testing"
	"time"

	handoffapp "github.com/jeremy-merchant/oh-my-group/internal/app/handoff"
	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	gitobs "github.com/jeremy-merchant/oh-my-group/internal/domain/git"
)

type verifierStub struct {
	revision         gitobs.RevisionEvidence
	localIntegration gitobs.LocalIntegrationEvidence
}

func (v *verifierStub) ResolveRevision(context.Context, string, string) (gitobs.RevisionEvidence, error) {
	return v.revision, nil
}
func (v *verifierStub) Reconcile(context.Context, string, string, string, string, string) (gitobs.ReconcileEvidence, error) {
	return gitobs.ReconcileEvidence{}, nil
}
func (v *verifierStub) VerifyLocalIntegration(context.Context, string, string, string) (gitobs.LocalIntegrationEvidence, error) {
	return v.localIntegration, nil
}

func integratedFixture(t *testing.T, now time.Time) (*Service, *verifierStub, func(time.Time)) {
	t.Helper()
	ctx := context.Background()
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	clock := now
	handoffService := handoffapp.New(store, func() time.Time { return clock })
	handoff := coord.Handoff{ID: "canary-handoff", TaskID: "a", RunID: "run", SourceSessionID: "source", TargetSessionID: "target", Summary: "ready", FinalOutput: coord.SensitiveText{Policy: coord.FinalOutputNone}, SourceCommit: "source-commit", SourceTree: "source-tree"}
	if _, err := handoffService.Submit(ctx, "canary-submit", testsupport.Project, handoff); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := handoffService.Advance(ctx, "canary-review", coord.HandoffLifecycleEvent{ID: "canary-review", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationReviewing}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := handoffService.Decide(ctx, "canary-accept", handoff.ID, string(coord.HandoffAccepted), "canary-decision", "target"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := handoffService.Advance(ctx, "canary-integrated", coord.HandoffLifecycleEvent{ID: "canary-integrated", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationIntegrated, IntegrationCommit: "integration-sha"}); err != nil {
		t.Fatal(err)
	}
	verifier := &verifierStub{revision: gitobs.RevisionEvidence{Commit: "integration-sha", Tree: "integration-tree", RefFingerprint: "ref-history-1"}}
	service := New(store, verifier, func() time.Time { return clock })
	return service, verifier, func(value time.Time) { clock = value }
}

func acceptedFixture(t *testing.T, now time.Time) (*Service, *verifierStub) {
	t.Helper()
	ctx := context.Background()
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	clock := now
	handoffService := handoffapp.New(store, func() time.Time { return clock })
	handoff := coord.Handoff{ID: "local-canary-handoff", TaskID: "a", RunID: "run", SourceSessionID: "source", TargetSessionID: "target", Summary: "ready", FinalOutput: coord.SensitiveText{Policy: coord.FinalOutputNone}, SourceCommit: "source-commit", SourceTree: "source-tree"}
	if _, err := handoffService.Submit(ctx, "local-canary-submit", testsupport.Project, handoff); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := handoffService.Advance(ctx, "local-canary-review", coord.HandoffLifecycleEvent{ID: "local-canary-review", HandoffID: handoff.ID, ActorSessionID: "target", State: coord.IntegrationReviewing}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if _, err := handoffService.Decide(ctx, "local-canary-accept", handoff.ID, string(coord.HandoffAccepted), "local-canary-decision", "target"); err != nil {
		t.Fatal(err)
	}
	verifier := &verifierStub{
		revision: gitobs.RevisionEvidence{Commit: "rolling-head", Tree: "rolling-tree", RefFingerprint: "rolling-history"},
		localIntegration: gitobs.LocalIntegrationEvidence{
			Candidate:          gitobs.RevisionEvidence{Commit: "candidate-sha", Tree: "candidate-tree"},
			Rolling:            gitobs.RevisionEvidence{Commit: "rolling-head", Tree: "rolling-tree", RefFingerprint: "rolling-history"},
			CandidateReachable: true,
			WorktreeClean:      true,
		},
	}
	return New(store, verifier, func() time.Time { return clock }), verifier
}

func TestExactSHACanaryPassesOnlyForUnchangedRealVerification(t *testing.T) {
	now := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	service, verifier, setClock := integratedFixture(t, now)
	ctx := context.Background()
	start, err := service.Start(ctx, "canary-start-key", StartRequest{ProjectID: testsupport.Project, Directory: "/project", HandoffID: "canary-handoff", ActorSessionID: "target", IntegrationRef: "refs/heads/main", Command: "go test ./...", ExecutionKind: "real", EnvironmentFingerprint: "env-hash"})
	if err != nil {
		t.Fatal(err)
	}
	if start.State != coord.IntegrationCanaryRunning || start.CanaryTargetSHA != "integration-sha" || start.CanaryTargetTree != "integration-tree" {
		t.Fatalf("start = %+v", start)
	}
	verifier.revision = gitobs.RevisionEvidence{Commit: "moved-after-start", Tree: "moved-tree", RefFingerprint: "ref-history-2"}
	replayed, err := service.Start(ctx, "canary-start-key", StartRequest{ProjectID: testsupport.Project, Directory: "/project", HandoffID: "canary-handoff", ActorSessionID: "target", IntegrationRef: "refs/heads/main", Command: "changed input is ignored by the same idempotency key", ExecutionKind: "real", EnvironmentFingerprint: "other-env"})
	if err != nil || replayed.ID != start.ID || replayed.CanaryTargetSHA != "integration-sha" {
		t.Fatalf("idempotent start replay = %+v err=%v", replayed, err)
	}
	verifier.revision = gitobs.RevisionEvidence{Commit: "integration-sha", Tree: "integration-tree", RefFingerprint: "ref-history-1"}
	setClock(now.Add(time.Minute))
	finish, err := service.Finish(ctx, "canary-finish-key", FinishRequest{ProjectID: testsupport.Project, Directory: "/project", CanaryRunID: start.CanaryRunID, ActorSessionID: "target", ExitCode: 0, PassedCount: 4, EvidencePath: "/logs/canary.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if finish.State != coord.IntegrationCanaryPassed || finish.CanaryResult != "PASS_REAL" || finish.CanaryHeadBefore != finish.CanaryHeadAfter || finish.CanaryPassedCount != 4 {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestExactSHACanaryInvalidatesWhenRefMovesEvenIfTestsPass(t *testing.T) {
	now := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	service, verifier, setClock := integratedFixture(t, now)
	ctx := context.Background()
	start, err := service.Start(ctx, "canary-start-moved", StartRequest{ProjectID: testsupport.Project, Directory: "/project", HandoffID: "canary-handoff", ActorSessionID: "target", IntegrationRef: "refs/heads/main", Command: "go test ./...", ExecutionKind: "real", EnvironmentFingerprint: "env-hash"})
	if err != nil {
		t.Fatal(err)
	}
	verifier.revision = gitobs.RevisionEvidence{Commit: "new-head", Tree: "new-tree", RefFingerprint: "ref-history-2"}
	setClock(now.Add(time.Minute))
	finish, err := service.Finish(ctx, "canary-finish-moved", FinishRequest{ProjectID: testsupport.Project, Directory: "/project", CanaryRunID: start.CanaryRunID, ActorSessionID: "target", ExitCode: 0, PassedCount: 4})
	if err != nil {
		t.Fatal(err)
	}
	if finish.State != coord.IntegrationCanaryInvalid || finish.CanaryResult != "INCONCLUSIVE" {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestLocalIntegrationCanaryUsesGitEvidenceAndRecordsLedgerWarning(t *testing.T) {
	now := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
	service, _ := acceptedFixture(t, now)
	ctx := context.Background()
	request := StartRequest{ProjectID: testsupport.Project, Directory: "/project", HandoffID: "local-canary-handoff", ActorSessionID: "target", IntegrationRef: "refs/heads/rolling", Mode: string(ModeLocalIntegration), CandidateSHA: "candidate-sha", Command: "go test ./...", ExecutionKind: "real", EnvironmentFingerprint: "env-hash"}
	start, err := service.Start(ctx, "local-canary-start", request)
	if err != nil {
		t.Fatal(err)
	}
	if start.State != coord.IntegrationCanaryRunning || start.CanaryTargetSHA != "rolling-head" || !strings.Contains(start.Note, "ledger_status=pending_ledger_reconciliation") || !strings.Contains(start.Note, "ledger_warning=missing_integrated_event") || !strings.Contains(start.Note, "candidate_sha=candidate-sha") {
		t.Fatalf("start = %+v", start)
	}
	replayed, err := service.Start(ctx, "local-canary-start", request)
	if err != nil || replayed.ID != start.ID || replayed.Note != start.Note {
		t.Fatalf("replayed = %+v, err = %v", replayed, err)
	}
	finish, err := service.Finish(ctx, "local-canary-finish", FinishRequest{ProjectID: testsupport.Project, Directory: "/project", CanaryRunID: start.CanaryRunID, ActorSessionID: "target", ExitCode: 0, PassedCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if finish.State != coord.IntegrationCanaryPassed || finish.Note != start.Note {
		t.Fatalf("finish = %+v", finish)
	}
}

func TestLocalIntegrationCanaryBlocksUnreachableOrDirtyGitEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*verifierStub)
	}{
		{name: "unreachable", mutate: func(v *verifierStub) { v.localIntegration.CandidateReachable = false }},
		{name: "dirty", mutate: func(v *verifierStub) { v.localIntegration.WorktreeClean = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, verifier := acceptedFixture(t, time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC))
			tc.mutate(verifier)
			_, err := service.Start(context.Background(), domain.IdempotencyKey("local-canary-"+tc.name), StartRequest{ProjectID: testsupport.Project, Directory: "/project", HandoffID: "local-canary-handoff", ActorSessionID: "target", IntegrationRef: "refs/heads/rolling", Mode: string(ModeLocalIntegration), CandidateSHA: "candidate-sha", Command: "go test ./...", ExecutionKind: "real", EnvironmentFingerprint: "env-hash"})
			if err == nil {
				t.Fatal("local canary started without required Git evidence")
			}
		})
	}
}

func TestReleaseOrProductionCanaryStillRequiresIntegratedLedgerEvent(t *testing.T) {
	service, _ := acceptedFixture(t, time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC))
	_, err := service.Start(context.Background(), "strict-canary-start", StartRequest{ProjectID: testsupport.Project, Directory: "/project", HandoffID: "local-canary-handoff", ActorSessionID: "target", IntegrationRef: "refs/heads/rolling", Mode: string(ModeReleaseOrProduction), Command: "go test ./...", ExecutionKind: "real", EnvironmentFingerprint: "env-hash"})
	if err == nil {
		t.Fatal("strict canary started without an INTEGRATED ledger event")
	}
}
