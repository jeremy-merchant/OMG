package app

import (
	"context"
	"encoding/json"
	"time"

	"example.invalid/coordledger/internal/app/foundation"
	gitapp "example.invalid/coordledger/internal/app/git"
	handoffapp "example.invalid/coordledger/internal/app/handoff"
	reservationapp "example.invalid/coordledger/internal/app/reservation"
	"example.invalid/coordledger/internal/domain"
	coord "example.invalid/coordledger/internal/domain/coordination"
	gitobs "example.invalid/coordledger/internal/domain/git"
	"example.invalid/coordledger/internal/domain/lineage"
	res "example.invalid/coordledger/internal/domain/reservation"
	"example.invalid/coordledger/internal/ports"
	"example.invalid/coordledger/internal/safety"
)

type reserveAddPayload struct {
	ID              string `json:"id"`
	PatternKind     string `json:"pattern_kind"`
	Pattern         string `json:"pattern"`
	CaseSensitivity string `json:"case_sensitivity"`
	Mode            string `json:"mode"`
	HumanID         string `json:"human_id"`
	SessionID       string `json:"session_id"`
	TaskID          string `json:"task_id"`
	RunID           string `json:"run_id"`
	Intent          string `json:"intent"`
	TTLSeconds      int64  `json:"ttl_seconds"`
}
type reserveIDPayload struct {
	ReservationID string `json:"reservation_id"`
}
type reserveRenewPayload struct {
	ReservationID string `json:"reservation_id"`
	CheckpointID  string `json:"checkpoint_id"`
	TTLSeconds    int64  `json:"ttl_seconds"`
}
type reserveReleasePayload struct {
	ReservationID string `json:"reservation_id"`
	Reason        string `json:"reason"`
}
type reserveOverridePayload struct {
	ReservationID string `json:"reservation_id"`
	HumanID       string `json:"human_id"`
	Reason        string `json:"reason"`
}
type gitInventoryPayload struct {
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	Directory string `json:"directory"`
}
type gitDiffPayload struct {
	Before string `json:"before"`
	After  string `json:"after"`
}
type gitCleanupPayload struct {
	Fingerprint string `json:"fingerprint"`
}
type gitAdoptPayload struct {
	ID                string `json:"id"`
	GitAssetID        string `json:"git_asset_id"`
	NewOwnerSessionID string `json:"new_owner_session_id"`
	Reason            string `json:"reason"`
}

type ReservationResult struct {
	ID        string `json:"id"`
	Pattern   string `json:"pattern"`
	Kind      string `json:"kind"`
	Mode      string `json:"mode"`
	ExpiresAt string `json:"expires_at"`
}
type ReservationMutationResult struct {
	ReservationID string   `json:"reservation_id"`
	Warnings      []string `json:"warnings,omitempty"`
}
type ReservationHistoryResult struct {
	ID           string `json:"id"`
	RenewalCount int    `json:"renewal_count"`
	Released     bool   `json:"released"`
	Overridden   bool   `json:"overridden"`
}
type GitSnapshotResult struct {
	ObservationID   string `json:"observation_id"`
	Sequence        int64  `json:"sequence,omitempty"`
	Hash            string `json:"hash"`
	RepositoryState string `json:"repository_state"`
	Confidence      string `json:"confidence"`
	AssetCount      int    `json:"asset_count"`
}
type GitDiffResult struct {
	NewCount     int `json:"new_count"`
	MissingCount int `json:"missing_count"`
	ChangedCount int `json:"changed_count"`
}
type GitCleanupPlanResult struct {
	Advisory        bool           `json:"advisory"`
	CandidateCount  int            `json:"candidate_count"`
	BlockedCount    int            `json:"blocked_count"`
	Classifications map[string]int `json:"classifications"`
	BlockReasons    map[string]int `json:"block_reasons"`
}
type GitAdoptionResult struct {
	ID                string `json:"id"`
	GitAssetID        string `json:"git_asset_id"`
	NewOwnerSessionID string `json:"new_owner_session_id"`
}

func (d *ServiceDispatcher) dispatchRecovery(ctx context.Context, request Request, selection foundation.Selection) (Outcome, bool) {
	mutating := request.Command == "reserve.add" || request.Command == "reserve.renew" || request.Command == "reserve.release" || request.Command == "reserve.override" || request.Command == "git.inventory" || request.Command == "git.adopt"
	query := request.Command == "reserve.list" || request.Command == "reserve.active" || request.Command == "reserve.history" || request.Command == "git.current" || request.Command == "git.latest" || request.Command == "git.history" || request.Command == "git.diff" || request.Command == "git.cleanup-plan"
	if !mutating && !query {
		return Outcome{}, false
	}
	if (mutating && request.IdempotencyKey == "") || (query && request.IdempotencyKey != "") {
		return Outcome{Error: invalidRequest()}, true
	}
	withStore := d.service.WithCurrentStore
	if query {
		withStore = d.service.WithReadOnlyCurrentStore
	}

	var result any
	err := withStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		project := domain.ProjectID(resolved.Project)
		if request.Command == "reserve.add" || request.Command == "reserve.list" || request.Command == "reserve.active" || request.Command == "reserve.history" || request.Command == "reserve.renew" || request.Command == "reserve.release" || request.Command == "reserve.override" {
			svc := reservationapp.New(store, nil)
			switch request.Command {
			case "reserve.add":
				var p reserveAddPayload
				if !decodePayload(request.Payload, &p) {
					return invalidRequest()
				}
				pattern, e := res.NewPattern(res.PatternKind(p.PatternKind), p.Pattern, res.CaseSensitivity(p.CaseSensitivity))
				if e != nil {
					return invalidRequest()
				}
				out, e := svc.Create(ctx, domain.IdempotencyKey(request.IdempotencyKey), reservationapp.CreateRequest{ProjectID: project, ID: p.ID, Pattern: pattern, Mode: res.Mode(p.Mode), Owner: res.Owner{HumanID: p.HumanID, SessionID: p.SessionID, TaskID: p.TaskID, RunID: p.RunID}, Intent: p.Intent, TTL: durationFromSeconds(p.TTLSeconds)})
				if e != nil {
					return e
				}
				result = ReservationMutationResult{ReservationID: string(out.ID), Warnings: out.Warnings}
			case "reserve.list", "reserve.active":
				var p struct{}
				if !decodePayload(request.Payload, &p) {
					return invalidRequest()
				}
				var records []res.Reservation
				var e error
				if request.Command == "reserve.list" {
					records, e = svc.List(ctx, project)
				} else {
					records, e = svc.Active(ctx, project)
				}
				if e != nil {
					return e
				}
				result = safeReservations(records)
			case "reserve.history":
				var p reserveIDPayload
				if !decodePayload(request.Payload, &p) || p.ReservationID == "" {
					return invalidRequest()
				}
				history, e := svc.History(ctx, project, p.ReservationID)
				if e != nil {
					return e
				}
				result = ReservationHistoryResult{ID: history.Current.ID, RenewalCount: len(history.Renewals), Released: history.Release != nil, Overridden: history.Override != nil}
			case "reserve.renew":
				var p reserveRenewPayload
				if !decodePayload(request.Payload, &p) || p.ReservationID == "" || p.CheckpointID == "" {
					return invalidRequest()
				}
				out, e := svc.Renew(ctx, domain.IdempotencyKey(request.IdempotencyKey), reservationapp.RenewRequest{ProjectID: project, ReservationID: p.ReservationID, CheckpointID: p.CheckpointID, TTL: durationFromSeconds(p.TTLSeconds)})
				if e != nil {
					return e
				}
				result = ReservationMutationResult{ReservationID: string(out.ID)}
			case "reserve.release":
				var p reserveReleasePayload
				if !decodePayload(request.Payload, &p) || p.ReservationID == "" || p.Reason == "" {
					return invalidRequest()
				}
				out, e := svc.Release(ctx, domain.IdempotencyKey(request.IdempotencyKey), reservationapp.ReleaseRequest{ProjectID: project, ReservationID: p.ReservationID, Reason: p.Reason})
				if e != nil {
					return e
				}
				result = ReservationMutationResult{ReservationID: string(out.ID)}
			case "reserve.override":
				var p reserveOverridePayload
				if !decodePayload(request.Payload, &p) || p.ReservationID == "" || p.HumanID == "" || p.Reason == "" {
					return invalidRequest()
				}
				out, e := svc.Override(ctx, domain.IdempotencyKey(request.IdempotencyKey), reservationapp.OverrideRequest{ProjectID: project, ReservationID: p.ReservationID, HumanID: p.HumanID, Reason: p.Reason})
				if e != nil {
					return e
				}
				result = ReservationMutationResult{ReservationID: string(out.ID)}
			}
			return nil
		}
		if request.Command == "git.inventory" && d.scanner == nil {
			return domain.NewError(domain.CodeUnavailable, "git scanner is unavailable", true)
		}
		if request.Command == "git.inventory" && d.pathInspector == nil {
			return domain.NewError(domain.CodeUnavailable, "path inspector is unavailable", true)
		}
		svc := gitapp.New(store, d.scanner, nil)
		switch request.Command {
		case "git.inventory":
			var p gitInventoryPayload
			if !decodePayload(request.Payload, &p) || p.Directory == "" {
				return invalidRequest()
			}
			if !d.pathInspector.SameDirectory(p.Directory, resolved.ProjectRoot) {
				return invalidRequest()
			}
			hasAttribution := p.SessionID != "" || p.TaskID != "" || p.RunID != ""
			if hasAttribution && (p.SessionID == "" || p.TaskID == "" || p.RunID == "") {
				return invalidRequest()
			}
			out, e := svc.Scan(ctx, domain.IdempotencyKey(request.IdempotencyKey), gitapp.ScanRequest{ProjectID: project, SessionID: lineage.ID(p.SessionID), TaskID: lineage.ID(p.TaskID), RunID: lineage.ID(p.RunID), Directory: p.Directory})
			if e != nil {
				return e
			}
			snapshot, ok := canonicalGitSnapshotResult(out.Data)
			if !ok {
				return domain.NewError(domain.CodeInternal, "git observation is unavailable", false)
			}
			result = snapshot
		case "git.current", "git.latest":
			var p struct{}
			if !decodePayload(request.Payload, &p) {
				return invalidRequest()
			}
			var snapshot gitobs.Snapshot
			var e error
			if request.Command == "git.current" {
				snapshot, e = svc.Current(ctx, project)
			} else {
				snapshot, e = svc.Latest(ctx, project)
			}
			if e != nil {
				return e
			}
			result = safeGitSnapshot(snapshot)
		case "git.history":
			var p struct{}
			if !decodePayload(request.Payload, &p) {
				return invalidRequest()
			}
			snapshots, e := svc.History(ctx, project)
			if e != nil {
				return e
			}
			out := make([]GitSnapshotResult, len(snapshots))
			for i := range snapshots {
				out[i] = safeGitSnapshot(snapshots[i])
			}
			result = out
		case "git.diff":
			var p gitDiffPayload
			if !decodePayload(request.Payload, &p) || p.Before == "" || p.After == "" {
				return invalidRequest()
			}
			diff, e := svc.Diff(ctx, project, p.Before, p.After)
			if e != nil {
				return e
			}
			result = GitDiffResult{NewCount: len(diff.New), MissingCount: len(diff.Missing), ChangedCount: len(diff.Changed)}
		case "git.cleanup-plan":
			var p gitCleanupPayload
			if !decodePayload(request.Payload, &p) {
				return invalidRequest()
			}
			plan, e := svc.CleanupPlan(ctx, project, p.Fingerprint)
			if e != nil {
				return e
			}
			result = safeGitCleanupPlan(plan)
		case "git.adopt":
			var p gitAdoptPayload
			if !decodePayload(request.Payload, &p) || p.ID == "" || p.GitAssetID == "" || p.NewOwnerSessionID == "" || p.Reason == "" {
				return invalidRequest()
			}
			adoption, e := handoffapp.New(store, nil).Adopt(ctx, domain.IdempotencyKey(request.IdempotencyKey), coord.Adoption{ID: p.ID, ProjectID: string(project), GitAssetID: p.GitAssetID, NewOwnerSessionID: p.NewOwnerSessionID, Reason: p.Reason})
			if e != nil {
				return e
			}
			result = GitAdoptionResult{ID: adoption.ID, GitAssetID: adoption.GitAssetID, NewOwnerSessionID: adoption.NewOwnerSessionID}
		}
		return nil
	})
	if err.Code != "" {
		return outcome(result, err), true
	}
	return Outcome{Data: result}, true
}

func safeReservations(records []res.Reservation) []ReservationResult {
	out := make([]ReservationResult, len(records))
	for i, record := range records {
		out[i] = ReservationResult{ID: record.ID, Pattern: safety.SafeText(record.Pattern.Value), Kind: string(record.Pattern.Kind), Mode: string(record.Mode), ExpiresAt: record.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	}
	return out
}

func canonicalGitSnapshotResult(data any) (GitSnapshotResult, bool) {
	switch summary := data.(type) {
	case gitapp.ScanSummary:
		return GitSnapshotResult{ObservationID: summary.ObservationID, Hash: summary.Hash, RepositoryState: string(summary.RepositoryState), Confidence: string(summary.Confidence), AssetCount: summary.AssetCount}, true
	case GitSnapshotResult:
		return summary, true
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return GitSnapshotResult{}, false
	}
	var result GitSnapshotResult
	if err := json.Unmarshal(encoded, &result); err != nil || result.ObservationID == "" {
		return GitSnapshotResult{}, false
	}
	return result, true
}
func safeGitSnapshot(snapshot gitobs.Snapshot) GitSnapshotResult {
	return GitSnapshotResult{ObservationID: snapshot.ID, Sequence: snapshot.SequenceNo, Hash: snapshot.Observation.Hash, RepositoryState: string(snapshot.Observation.Repository), Confidence: string(snapshot.Observation.Confidence), AssetCount: len(snapshot.Assets)}
}
func safeGitCleanupPlan(plan gitobs.CleanupPlan) GitCleanupPlanResult {
	out := GitCleanupPlanResult{Advisory: !plan.Mutating, CandidateCount: len(plan.Candidates), BlockedCount: len(plan.Blocked), Classifications: make(map[string]int), BlockReasons: make(map[string]int)}
	for _, candidate := range plan.Candidates {
		out.Classifications[string(candidate.Classification)]++
	}
	for _, blocked := range plan.Blocked {
		out.BlockReasons[blocked.Reason]++
	}
	return out
}
func durationFromSeconds(seconds int64) time.Duration {
	max := time.Duration(1<<63 - 1)
	min := -max - 1
	if seconds > int64(max/time.Second) {
		return max
	}
	if seconds < int64(min/time.Second) {
		return min
	}
	return time.Duration(seconds) * time.Second
}
