package query

import (
	"reflect"
	"testing"
	"time"
)

func TestClassifySessionsSeparatesActionableOpenSessionStates(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	idle := now.Add(-20 * time.Minute)
	old := now.Add(-2 * time.Hour)
	future := now.Add(time.Minute)
	closedAt := now.Add(-time.Hour)
	interruptedAt := now.Add(-30 * time.Minute)
	snapshot := BoardSnapshot{
		GeneratedAt: now,
		Sessions: []IdentityView{
			{ID: "alive", Runtime: "codex", Role: "worker", TaskID: "task-alive", StartedAt: old, Liveness: SessionLivenessAlive, HeartbeatAt: &recent},
			{ID: "idle-aged", Runtime: "codex", Role: "worker", StartedAt: old, Liveness: SessionLivenessAlive, HeartbeatAt: &idle},
			{ID: "idle-no-run", Runtime: "codex", Role: "reviewer", StartedAt: old, Liveness: SessionLivenessAlive, HeartbeatAt: &recent},
			{ID: "stale-by-age", Runtime: "codex", Role: "worker", StartedAt: old, Liveness: SessionLivenessAlive, HeartbeatAt: &old},
			{ID: "stale-explicit", Runtime: "codex", Role: "worker", StartedAt: old, Liveness: SessionLivenessStale, HeartbeatAt: &recent},
			{ID: "unobservable", Runtime: "unknown", Role: "worker", StartedAt: old, Liveness: SessionLivenessNoSignal},
			{ID: "finished", Runtime: "codex", Role: "worker", StartedAt: old, Liveness: SessionLivenessNoSignal},
			{ID: "future-heartbeat", Runtime: "codex", Role: "worker", StartedAt: recent, Liveness: SessionLivenessAlive, HeartbeatAt: &future},
			{ID: "closed-stale", StartedAt: old, Liveness: SessionLivenessStale, HeartbeatAt: &old, EndedAt: &closedAt},
			{ID: "interrupted-stale", StartedAt: old, Liveness: SessionLivenessStale, HeartbeatAt: &old, InterruptedAt: &interruptedAt},
		},
		Runs: []RunView{
			{ID: "run-alive", TaskID: "task-alive", SessionID: "alive", State: "RUNNING", StartedAt: old},
			{ID: "run-idle", TaskID: "task-idle", SessionID: "idle-aged", State: "WAITING", StartedAt: old},
			{ID: "run-stale-age", TaskID: "task-stale-age", SessionID: "stale-by-age", State: "BLOCKED", StartedAt: old},
			{ID: "run-stale-explicit", TaskID: "task-stale-explicit", SessionID: "stale-explicit", State: "RUNNING", StartedAt: old},
			{ID: "run-unobservable", TaskID: "task-unobservable", SessionID: "unobservable", State: "REWORK", StartedAt: old},
			{ID: "run-finished", TaskID: "task-finished", SessionID: "finished", State: "WORK_COMPLETE", StartedAt: old},
			{ID: "run-future", TaskID: "task-future", SessionID: "future-heartbeat", State: "RUNNING", StartedAt: recent},
		},
	}

	got := ClassifySessions(snapshot)
	if got.IdleAfterSeconds != 900 || got.StaleAfterSeconds != 3600 {
		t.Fatalf("thresholds = idle %d stale %d", got.IdleAfterSeconds, got.StaleAfterSeconds)
	}
	wantCounts := SessionClassificationCounts{Alive: 2, Idle: 2, Stale: 2, RuntimeUnobservable: 1, FinishedUnclosed: 1}
	if got.Counts != wantCounts {
		t.Fatalf("counts = %#v, want %#v", got.Counts, wantCounts)
	}
	wantOrder := []string{"finished", "stale-by-age", "stale-explicit", "unobservable", "idle-aged", "idle-no-run", "alive", "future-heartbeat"}
	order := make([]string, 0, len(got.Sessions))
	byID := make(map[string]SessionClassificationView, len(got.Sessions))
	for _, session := range got.Sessions {
		order = append(order, session.SessionID)
		byID[session.SessionID] = session
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	if byID["finished"].Classification != SessionClassificationFinishedUnclosed || byID["finished"].RecommendedAction != "archive_session" || byID["finished"].TaskID != "task-finished" {
		t.Fatalf("finished = %#v", byID["finished"])
	}
	if byID["unobservable"].Classification != SessionClassificationRuntimeUnobservable || byID["unobservable"].LastHeartbeatAt != nil {
		t.Fatalf("unobservable = %#v", byID["unobservable"])
	}
	if byID["idle-no-run"].Classification != SessionClassificationIdle || len(byID["idle-no-run"].RunStates) != 0 {
		t.Fatalf("idle without run = %#v", byID["idle-no-run"])
	}
	if byID["future-heartbeat"].ElapsedSeconds != 0 {
		t.Fatalf("future elapsed = %d", byID["future-heartbeat"].ElapsedSeconds)
	}
	if _, found := byID["closed-stale"]; found {
		t.Fatal("closed session was included")
	}
	if _, found := byID["interrupted-stale"]; found {
		t.Fatal("interrupted session was included")
	}
}

func TestSummarizeDoesNotCountClosedStaleSessionsAsActionable(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	closedAt := now.Add(-time.Hour)
	snapshot := BoardSnapshot{
		GeneratedAt: now,
		Sessions: []IdentityView{
			{ID: "closed", StartedAt: old, Liveness: SessionLivenessStale, HeartbeatAt: &old, EndedAt: &closedAt},
			{ID: "open", StartedAt: old, Liveness: SessionLivenessStale, HeartbeatAt: &old},
		},
	}

	got := Summarize(snapshot)
	if got.ActiveSessions != 1 || got.StaleSessions != 1 {
		t.Fatalf("summary = %#v", got)
	}
}
