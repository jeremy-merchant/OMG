package progress

import (
	"context"
	dep "github.com/jeremy-merchant/OMG/internal/app/dependency"
	ho "github.com/jeremy-merchant/OMG/internal/app/handoff"
	msg "github.com/jeremy-merchant/OMG/internal/app/message"
	"github.com/jeremy-merchant/OMG/internal/app/testsupport"
	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	"testing"
	"time"
)

func TestForeignProjectReferencesRejectWithoutDomainWrites(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	testsupport.SeedForeign(t, s, db, now)
	progressSvc := New(s, func() time.Time { return now })
	dependencySvc := dep.New(s, func() time.Time { return now })
	messageSvc := msg.New(s, func() time.Time { return now })
	handoffSvc := ho.New(s, func() time.Time { return now })
	cases := []struct {
		name, table string
		run         func() error
	}{{"dependency", "task_dependencies", func() error {
		_, e := dependencySvc.Add(ctx, "foreign-dep", coord.Dependency{ID: "foreign-dep", PrerequisiteTaskID: "a", DependentTaskID: "foreign-task", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete})
		return e
	}}, {"message", "messages", func() error {
		_, e := messageSvc.Send(ctx, "foreign-message", testsupport.Project, coord.MailMessage{ID: "foreign-message", Type: coord.MessageNotice, ThreadID: "t", SenderSessionID: "foreign-session", Recipients: []coord.RecipientTarget{{SessionID: "foreign-session"}}, Body: "body", CreatedAt: now})
		return e
	}}, {"handoff", "handoffs", func() error {
		_, e := handoffSvc.Submit(ctx, "foreign-handoff", testsupport.Project, coord.Handoff{ID: "foreign-handoff", TaskID: "a", RunID: "run", SourceSessionID: "source", TargetSessionID: "foreign-session", Summary: "summary", FinalOutput: coord.SensitiveText{Hash: "hash", Policy: coord.FinalOutputHashOnly}})
		return e
	}}, {"progress", "progress_updates", func() error {
		_, e := progressSvc.Append(ctx, domain.IdempotencyKey("foreign-progress"), coord.Progress{ID: "foreign-progress", TaskID: "a", RunID: "foreign-run", SessionID: "foreign-session", Phase: coord.PhasePlan, Done: []string{"d"}, Doing: []string{"x"}, Next: []string{"n"}})
		return e
	}}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("foreign reference accepted")
			}
			var n int
			if err := db.QueryRow("SELECT COUNT(*) FROM "+tc.table+" WHERE id=?", "foreign-"+tc.name).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("foreign command wrote %d rows", n)
			}
		})
	}
}
