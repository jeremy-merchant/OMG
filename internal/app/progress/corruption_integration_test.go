package progress

import (
	"context"
	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"testing"
	"time"
)

func TestHistoryFailsClosedForCorruptProgressJSONAndTimestamp(t *testing.T) {
	for _, column := range []string{"done_json", "doing_json", "next_json", "created_at"} {
		t.Run(column, func(t *testing.T) {
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			s, db := testsupport.Store(t, now)
			testsupport.Seed(t, s, now)
			svc := New(s, func() time.Time { return now })
			p := coord.Progress{ID: "p", TaskID: "a", SessionID: "source", Phase: coord.PhasePlan, Done: []string{"d"}, Doing: []string{"x"}, Next: []string{"n"}}
			if _, err := svc.Append(context.Background(), domain.IdempotencyKey("p"), p); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("DROP TRIGGER progress_updates_no_update"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE progress_updates SET "+column+"=? WHERE id='p'", "not-valid"); err != nil {
				t.Fatal(err)
			}
			got, err := svc.History(context.Background(), "a")
			if err == nil || got != nil {
				t.Fatalf("partial corrupt read: got=%+v err=%v", got, err)
			}
		})
	}
}
