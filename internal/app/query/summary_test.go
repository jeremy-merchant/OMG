package query

import (
	"reflect"
	"testing"
	"time"
)

func TestSummarizeAndIntegrationQueueExposeBottleneckAndEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 28, 2, 0, 0, 0, time.UTC)
	staleHeartbeat := now.Add(-2 * time.Hour)
	snapshot := BoardSnapshot{
		GeneratedAt: now,
		Sessions: []IdentityView{
			{ID: "alive", Liveness: SessionLivenessAlive, HeartbeatAt: &now},
			{ID: "unknown-runtime", Liveness: SessionLivenessNoSignal},
			{ID: "stale", Liveness: SessionLivenessStale, HeartbeatAt: &staleHeartbeat},
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
	if summary.ActiveSessions != 1 || summary.OpenSessions != 3 || summary.AliveSessions != 0 || summary.IdleSessions != 1 || summary.StaleSessions != 1 || summary.RuntimeUnobservableSessions != 1 || summary.FinishedUnclosedSessions != 0 || summary.Conflicts != 1 || summary.OwnershipConflicts != 1 || summary.GitRisks != 0 || summary.IntegrationQueue != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Housekeeping.StaleSessions != 1 || summary.Housekeeping.RuntimeUnobservableSessions != 1 || summary.Housekeeping.FinishedUnclosedSessions != 0 || summary.Housekeeping.IntegrationQueue != 2 {
		t.Fatalf("housekeeping = %#v", summary.Housekeeping)
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

func TestSummarizeDoesNotCountUnobservableOrFinishedUnclosedSessionsAsActive(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	snapshot := BoardSnapshot{
		GeneratedAt: now,
		Sessions: []IdentityView{
			{ID: "unobservable", StartedAt: now.Add(-time.Hour), Liveness: SessionLivenessNoSignal},
			{ID: "finished-unclosed", StartedAt: now.Add(-time.Hour), Liveness: SessionLivenessNoSignal},
		},
		Runs: []RunView{
			{ID: "running", TaskID: "task-running", SessionID: "unobservable", State: "RUNNING", StartedAt: now.Add(-time.Hour)},
			{ID: "finished", TaskID: "task-finished", SessionID: "finished-unclosed", State: "WORK_COMPLETE", StartedAt: now.Add(-time.Hour)},
		},
	}

	summary := Summarize(snapshot)
	if summary.ActiveSessions != 0 || summary.OpenSessions != 2 || summary.AliveSessions != 0 || summary.IdleSessions != 0 || summary.StaleSessions != 0 || summary.RuntimeUnobservableSessions != 1 || summary.FinishedUnclosedSessions != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSummarizeIgnoresConflictsWithInactiveReservations(t *testing.T) {
	snapshot := BoardSnapshot{Reservations: []ReservationView{
		{ID: "active", Lifecycle: "active", ConflictIDs: []string{"overridden", "released", "expired"}},
		{ID: "overridden", Lifecycle: "overridden", ConflictIDs: []string{"active"}},
		{ID: "released", Lifecycle: "released", ConflictIDs: []string{"active"}},
		{ID: "expired", Lifecycle: "expired", ConflictIDs: []string{"active"}},
	}}
	if got := Summarize(snapshot).Conflicts; got != 0 {
		t.Fatalf("inactive reservation conflicts = %d, want 0", got)
	}
}

func TestSummarizeSeparatesOwnershipConflictsFromGitRisks(t *testing.T) {
	snapshot := BoardSnapshot{
		Reservations: []ReservationView{{ID: "r1", ConflictIDs: []string{"r2"}}, {ID: "r2", ConflictIDs: []string{"r1"}}},
		Warnings:     []string{"git_risk:dirty_tree", "git_risk:dirty_tree", "other_warning"},
	}
	summary := Summarize(snapshot)
	if summary.OwnershipConflicts != 1 || summary.GitRisks != 1 || summary.Conflicts != 2 {
		t.Fatalf("summary = %#v", summary)
	}
}
