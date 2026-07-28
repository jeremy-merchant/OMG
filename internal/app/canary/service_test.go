package canary

import (
	"context"
	"testing"
	"time"

	handoffapp "github.com/jeremy-merchant/OMG/internal/app/handoff"
	"github.com/jeremy-merchant/OMG/internal/app/testsupport"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
)

type verifierStub struct {
	revision gitobs.RevisionEvidence
}

func (v *verifierStub) ResolveRevision(context.Context, string, string) (gitobs.RevisionEvidence, error) {
	return v.revision, nil
}
func (v *verifierStub) Reconcile(context.Context, string, string, string, string, string) (gitobs.ReconcileEvidence, error) {
	return gitobs.ReconcileEvidence{}, nil
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
