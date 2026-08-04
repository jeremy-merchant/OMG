package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestApprovedMigrationBootstrapsProjectForCoordinationWrites(t *testing.T) {
	ctx := context.Background()
	s, _, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	plan, _, approval := migrationApproval(t, s, "approved-project")
	if err := s.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id=?", "approved-project").Scan(&count); err != nil || count != 1 {
		t.Fatalf("project bootstrap: count=%d err=%v", count, err)
	}
	now := time.Now().UTC()
	_, _, err = s.Write(ctx, "coordination-write", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		h := core.Human{ID: "h", DisplayName: "Human", Confidence: core.ConfidenceExplicit, CreatedAt: now}
		if err := c.CreateHuman(ctx, h); err != nil {
			return domain.Result{}, err
		}
		session := core.AgentSession{ID: "s", ProjectID: "approved-project", HumanID: "h", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "test", RootID: "s", StartedAt: now}
		if err := c.CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		_, err := c.CreateTask(ctx, core.Task{ID: "t", ProjectID: "approved-project", Title: "task", State: core.TaskReady, CreatedAt: now, UpdatedAt: now})
		return domain.Result{ID: "ok", Outcome: domain.OutcomeOK}, err
	})
	if err != nil {
		t.Fatal(err)
	}
}
