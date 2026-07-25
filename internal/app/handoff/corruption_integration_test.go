package handoff

import (
	"context"
	"github.com/jeremy-merchant/OMG/internal/app/testsupport"
	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	"testing"
	"time"
)

func TestGetFailsClosedForCorruptHandoffJSONAndTimestamp(t *testing.T) {
	for _, column := range []string{"commits_json", "verification_json", "risks_json", "actions_json", "created_at"} {
		t.Run(column, func(t *testing.T) {
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			s, db := testsupport.Store(t, now)
			testsupport.Seed(t, s, now)
			svc := New(s, func() time.Time { return now })
			h := coord.Handoff{ID: "h", TaskID: "a", RunID: "run", SourceSessionID: "source", Summary: "summary", FinalOutput: coord.SensitiveText{Hash: "hash", Policy: coord.FinalOutputHashOnly}}
			if _, err := svc.Submit(context.Background(), domain.IdempotencyKey("h"), testsupport.Project, h); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DROP TRIGGER handoffs_no_update"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE handoffs SET "+column+"=? WHERE id='h'", "not-valid"); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Get(context.Background(), "h")
			if err == nil || got.ID != "" {
				t.Fatalf("partial corrupt read: got=%+v err=%v", got, err)
			}
		})
	}
}
