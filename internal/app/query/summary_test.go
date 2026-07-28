package query

import (
	"reflect"
	"testing"
	"time"
)

func TestSummarizeAndIntegrationQueueExposeBottleneckAndEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 28, 2, 0, 0, 0, time.UTC)
	snapshot := BoardSnapshot{
		Sessions: []IdentityView{
			{ID: "alive", Liveness: SessionLivenessAlive},
			{ID: "unknown-runtime", Liveness: SessionLivenessNoSignal},
			{ID: "stale", Liveness: SessionLivenessStale},
		},
		Tasks: []TaskView{
			{ID: "wc-1", State: "WORK_COMPLETE"}, {ID: "wc-2", State: "WORK_COMPLETE"},
			{ID: "done", State: "VERIFIED_DONE"},
		},
		Handoffs: []HandoffView{
			{ID: "queued", TaskID: "wc-1", SourceSessionID: "alive", IntegrationState: "INTEGRATED", CreatedAt: now, Lifecycle: []HandoffLifecycleView{{State: "SUBMITTED", SourceCommit: "abc", SourceTree: "def", CreatedAt: now}, {State: "INTEGRATED", IntegrationCommit: "fed", CreatedAt: now.Add(time.Minute)}}},
			{ID: "missing", TaskID: "wc-2", SourceSessionID: "unknown-runtime", IntegrationState: "SUBMITTED", CreatedAt: now.Add(2 * time.Minute)},
			{ID: "cleaned", TaskID: "done", IntegrationState: "SOURCE_CLEANED", CreatedAt: now},
		},
		Reservations: []ReservationView{{ID: "r1", ConflictIDs: []string{"r2"}}, {ID: "r2", ConflictIDs: []string{"r1"}}},
	}
	summary := Summarize(snapshot)
	if summary.ActiveSessions != 2 || summary.StaleSessions != 1 || summary.Conflicts != 1 || summary.IntegrationQueue != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	if !reflect.DeepEqual(summary.Bottlenecks, []BottleneckView{{From: "WORK_COMPLETE", To: "VERIFIED_DONE", Waiting: 2, Done: 1}}) {
		t.Fatalf("bottlenecks = %#v", summary.Bottlenecks)
	}
	queue := IntegrationQueue(snapshot)
	if len(queue) != 2 || queue[0].State != "INTEGRATED" || queue[0].SourceCommit != "abc" || queue[0].IntegrationCommit != "fed" || len(queue[0].MissingEvidence) != 0 {
		t.Fatalf("queue = %#v", queue)
	}
	if queue[1].HandoffID != "missing" || !reflect.DeepEqual(queue[1].MissingEvidence, []string{"source_commit", "source_tree"}) {
		t.Fatalf("missing evidence queue item = %#v", queue[1])
	}
}
