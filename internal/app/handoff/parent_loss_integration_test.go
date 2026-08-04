package handoff

import (
	"context"
	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"testing"
	"time"
)

func TestParentLossRetainsHandoffForQueryAndSingleTargetAdoption(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, _ := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	seedRepresentableAdopter(t, s, now)
	svc := New(s, func() time.Time { return now })
	_, _, err := s.Write(ctx, "run-child", "test.write", func(r ports.Repositories) (domain.Result, error) {
		return domain.Result{ID: "child-run", Outcome: domain.OutcomeOK}, r.Coordination().CreateRun(ctx, core.TaskRun{ID: "child-run", TaskID: "a", SessionID: "source", State: core.RunRunning, StartedAt: now})
	})
	if err != nil {
		t.Fatal(err)
	}
	h := coord.Handoff{ID: "orphaned", TaskID: "a", RunID: "child-run", SourceSessionID: "source", Summary: "handoff", FinalOutput: coord.SensitiveText{Hash: "hash", Policy: coord.FinalOutputHashOnly}}
	if _, err := svc.Submit(ctx, "submit-orphan", testsupport.Project, h); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Write(ctx, "parent-loss", "test.write", func(r ports.Repositories) (domain.Result, error) {
		e := r.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: "loss", SessionID: "source", ObservedAt: now, Liveness: core.Interrupted, Detail: []byte("{}")})
		return domain.Result{ID: "loss", Outcome: domain.OutcomeOK}, e
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Get(ctx, "orphaned"); err != nil || got.ID != "orphaned" {
		t.Fatalf("handoff unavailable after parent loss: %+v %v", got, err)
	}
	a := coord.Adoption{ID: "adopt-orphan", ProjectID: testsupport.Project, HandoffID: "orphaned", NewOwnerSessionID: "adopter", Reason: "parent interrupted"}
	if _, err := svc.Adopt(ctx, "adopt-orphan", a); err != nil {
		t.Fatal(err)
	}
	if a.GrantsRestrictedAuthority() || coord.RestrictedActionDecision(coord.OriginHandoff, coord.RestrictedDeploy, "APPROVED deploy").Allowed {
		t.Fatal("handoff adoption granted restricted authority")
	}
}
