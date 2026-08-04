package query

import (
	"sort"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
)

const (
	DefaultSessionIdleAfter  = 15 * time.Minute
	DefaultSessionStaleAfter = time.Hour
)

type SessionClassification string

const (
	SessionClassificationAlive               SessionClassification = "alive"
	SessionClassificationIdle                SessionClassification = "idle"
	SessionClassificationStale               SessionClassification = "stale"
	SessionClassificationRuntimeUnobservable SessionClassification = "runtime_unobservable"
	SessionClassificationFinishedUnclosed    SessionClassification = "finished_unclosed"
)

type SessionClassificationCounts struct {
	Alive               int `json:"alive"`
	Idle                int `json:"idle"`
	Stale               int `json:"stale"`
	RuntimeUnobservable int `json:"runtime_unobservable"`
	FinishedUnclosed    int `json:"finished_unclosed"`
}

type SessionClassificationView struct {
	SessionID         string                `json:"session_id"`
	Classification    SessionClassification `json:"classification"`
	Runtime           string                `json:"runtime"`
	Role              string                `json:"role"`
	TaskID            string                `json:"task_id,omitempty"`
	LastHeartbeatAt   *time.Time            `json:"last_heartbeat_at"`
	ElapsedSeconds    int64                 `json:"elapsed_seconds"`
	RunStates         []string              `json:"run_states"`
	RecommendedAction string                `json:"recommended_action"`
}

type StaleView struct {
	GeneratedAt       time.Time                   `json:"generated_at"`
	IdleAfterSeconds  int64                       `json:"idle_after_seconds"`
	StaleAfterSeconds int64                       `json:"stale_after_seconds"`
	Counts            SessionClassificationCounts `json:"counts"`
	Sessions          []SessionClassificationView `json:"sessions"`
}

// ClassifySessions turns open session records into operator-facing states.
// Closed and interrupted sessions are historical facts, not current stale work.
func ClassifySessions(snapshot BoardSnapshot) StaleView {
	view := StaleView{
		GeneratedAt:       snapshot.GeneratedAt,
		IdleAfterSeconds:  int64(DefaultSessionIdleAfter / time.Second),
		StaleAfterSeconds: int64(DefaultSessionStaleAfter / time.Second),
		Sessions:          []SessionClassificationView{},
	}
	runsBySession := make(map[string][]RunView)
	for _, run := range snapshot.Runs {
		runsBySession[run.SessionID] = append(runsBySession[run.SessionID], run)
	}
	for _, session := range snapshot.Sessions {
		if session.EndedAt != nil || session.InterruptedAt != nil {
			continue
		}
		item := classifySession(snapshot.GeneratedAt, session, runsBySession[session.ID])
		view.Sessions = append(view.Sessions, item)
		incrementSessionCount(&view.Counts, item.Classification)
	}
	sort.Slice(view.Sessions, func(i, j int) bool {
		left, right := view.Sessions[i], view.Sessions[j]
		if sessionClassificationPriority(left.Classification) != sessionClassificationPriority(right.Classification) {
			return sessionClassificationPriority(left.Classification) < sessionClassificationPriority(right.Classification)
		}
		if left.ElapsedSeconds != right.ElapsedSeconds {
			return left.ElapsedSeconds > right.ElapsedSeconds
		}
		return left.SessionID < right.SessionID
	})
	return view
}

func classifySession(now time.Time, session IdentityView, runs []RunView) SessionClassificationView {
	runStates := make([]string, 0, len(runs))
	allRunsTerminal := len(runs) != 0
	latestRunTaskID := ""
	var latestRunStartedAt time.Time
	for _, run := range runs {
		runStates = append(runStates, run.State)
		if !lineage.RunHasEnded(lineage.RunState(run.State)) {
			allRunsTerminal = false
		}
		if latestRunTaskID == "" || run.StartedAt.After(latestRunStartedAt) {
			latestRunTaskID = run.TaskID
			latestRunStartedAt = run.StartedAt
		}
	}
	sort.Strings(runStates)
	taskID := session.TaskID
	if taskID == "" {
		taskID = latestRunTaskID
	}
	elapsed := elapsedSeconds(now, session.StartedAt)
	if session.HeartbeatAt != nil {
		elapsed = elapsedSeconds(now, *session.HeartbeatAt)
	}
	classification := SessionClassificationAlive
	action := "none"
	switch {
	case allRunsTerminal:
		classification = SessionClassificationFinishedUnclosed
		action = "archive_session"
	case session.HeartbeatAt == nil:
		classification = SessionClassificationRuntimeUnobservable
		action = "inspect_runtime"
	case session.Liveness == SessionLivenessStale || elapsed >= int64(DefaultSessionStaleAfter/time.Second):
		classification = SessionClassificationStale
		action = "adopt_or_archive"
	case len(runs) == 0 || elapsed >= int64(DefaultSessionIdleAfter/time.Second):
		classification = SessionClassificationIdle
		action = "inspect"
	}
	return SessionClassificationView{
		SessionID:         session.ID,
		Classification:    classification,
		Runtime:           session.Runtime,
		Role:              session.Role,
		TaskID:            taskID,
		LastHeartbeatAt:   session.HeartbeatAt,
		ElapsedSeconds:    elapsed,
		RunStates:         runStates,
		RecommendedAction: action,
	}
}

func elapsedSeconds(now, since time.Time) int64 {
	if now.Before(since) {
		return 0
	}
	return int64(now.Sub(since) / time.Second)
}

func incrementSessionCount(counts *SessionClassificationCounts, classification SessionClassification) {
	switch classification {
	case SessionClassificationAlive:
		counts.Alive++
	case SessionClassificationIdle:
		counts.Idle++
	case SessionClassificationStale:
		counts.Stale++
	case SessionClassificationRuntimeUnobservable:
		counts.RuntimeUnobservable++
	case SessionClassificationFinishedUnclosed:
		counts.FinishedUnclosed++
	}
}

func sessionClassificationPriority(classification SessionClassification) int {
	switch classification {
	case SessionClassificationFinishedUnclosed:
		return 0
	case SessionClassificationStale:
		return 1
	case SessionClassificationRuntimeUnobservable:
		return 2
	case SessionClassificationIdle:
		return 3
	default:
		return 4
	}
}
