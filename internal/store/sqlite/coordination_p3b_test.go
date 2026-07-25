package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	core "github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

func TestP3BProgressAppendOnlyAndOrdered(t *testing.T) {
	ctx := context.Background()
	s, _, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	plan, _, approval := migrationApproval(t, s, "p3b-progress")
	if err = s.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Nanosecond)
	_, _, err = s.Write(ctx, "seed", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		if err := c.CreateHuman(ctx, core.Human{ID: "h", DisplayName: "human", Confidence: core.ConfidenceExplicit, CreatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateSession(ctx, core.AgentSession{ID: "s", ProjectID: "p3b-progress", HumanID: "h", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "test", RootID: "s", StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		_, err := c.CreateTask(ctx, core.Task{ID: "t", ProjectID: "p3b-progress", Title: "task", State: core.TaskReady, CreatedAt: now, UpdatedAt: now})
		return domain.Result{ID: "seed", Outcome: domain.OutcomeOK}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Write(ctx, "append", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		for _, p := range []coord.Progress{{ID: "p2", TaskID: "t", SessionID: "s", Phase: coord.PhasePlan, Done: []string{"b"}, Doing: []string{"b"}, Next: []string{"b"}, CreatedAt: now.Add(time.Second)}, {ID: "p1", TaskID: "t", SessionID: "s", Phase: coord.PhasePlan, Done: []string{"a"}, Doing: []string{"a"}, Next: []string{"a"}, CreatedAt: now}} {
			if err := c.CreateProgress(ctx, p); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "append", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []coord.Progress
	if err = s.Read(ctx, func(r ports.Repositories) error {
		var e error
		got, e = r.Coordination().ListProgress(ctx, "t")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p2" {
		t.Fatalf("ordered progress=%+v", got)
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE progress_updates SET phase='test' WHERE id='p1'"); err == nil {
		t.Fatal("progress update unexpectedly mutable")
	}
}
