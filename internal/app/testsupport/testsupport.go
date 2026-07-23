package testsupport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"example.invalid/coordledger/internal/domain"
	coord "example.invalid/coordledger/internal/domain/coordination"
	core "example.invalid/coordledger/internal/domain/lineage"
	"example.invalid/coordledger/internal/ports"
	"example.invalid/coordledger/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

const Project = "p3b-test"

func Store(t *testing.T, now time.Time) (*sqlite.SQLiteStore, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, _, err := sqlite.Open(ctx, path, sqlite.OpenOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanMigrations(ctx, Project)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := s.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	approval := sqlite.Approval{ApprovalID: "test-approval-" + Project, ApprovedBy: "test", EvidenceReference: "test", PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: now, ExpiresAt: now.Add(5 * time.Minute)}
	if err := s.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = s.Close() })
	return s, db
}

func Seed(t *testing.T, s *sqlite.SQLiteStore, now time.Time) {
	t.Helper()
	ctx := context.Background()
	_, _, err := s.Write(ctx, "seed", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		if err := c.CreateHuman(ctx, core.Human{ID: "human", DisplayName: "Human", Confidence: core.ConfidenceExplicit, CreatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		for _, x := range []core.AgentSession{{ID: "source", ProjectID: Project, HumanID: "human", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "test", RootID: "source", StartedAt: now}, {ID: "target", ProjectID: Project, HumanID: "human", Kind: core.HumanDirect, Runtime: "test", Role: "reviewer", Source: core.SourceHuman, SourceRef: "test", RootID: "target", StartedAt: now}} {
			if err := c.CreateSession(ctx, x); err != nil {
				return domain.Result{}, err
			}
		}
		for i, id := range []string{"a", "b", "c"} {
			if _, err := c.CreateTask(ctx, core.Task{ID: core.ID(id), ProjectID: Project, DisplayNumber: int64(i + 1), Title: id, State: core.TaskReady, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			if _, won, err := c.ClaimTask(ctx, core.ID(id), "source", now); err != nil || !won {
				return domain.Result{}, err
			}
		}
		if err := c.CreateRun(ctx, core.TaskRun{ID: "run", TaskID: "a", SessionID: "source", State: core.RunWorkComplete, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "seed", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func Message(id string, typ coord.MessageType, recipient coord.RecipientTarget, body string) coord.MailMessage {
	return coord.MailMessage{ID: id, Type: typ, ThreadID: "thread", SenderSessionID: "source", Recipients: []coord.RecipientTarget{recipient}, Subject: "subject", Body: body, RelatedTaskID: "a"}
}

func SeedForeign(t *testing.T, s *sqlite.SQLiteStore, db *sql.DB, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Exec("INSERT INTO projects(id,created_at) VALUES(?,?)", "foreign", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Write(ctx, "seed-foreign", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		session := core.AgentSession{ID: "foreign-session", ProjectID: "foreign", HumanID: "human", Kind: core.HumanDirect, Runtime: "test", Role: "owner", Source: core.SourceHuman, SourceRef: "test", RootID: "foreign-session", StartedAt: now}
		if err := c.CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		if _, err := c.CreateTask(ctx, core.Task{ID: "foreign-task", ProjectID: "foreign", DisplayNumber: 1, Title: "foreign", State: core.TaskClaimed, CreatedAt: now, UpdatedAt: now}); err != nil {
			return domain.Result{}, err
		}
		if err := c.CreateRun(ctx, core.TaskRun{ID: "foreign-run", TaskID: "foreign-task", SessionID: "foreign-session", State: core.RunWorkComplete, StartedAt: now}); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: "seed-foreign", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
