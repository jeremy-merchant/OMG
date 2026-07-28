package query

import (
	"sort"
	"strings"
)

const (
	integrationSubmitted     = "SUBMITTED"
	integrationReviewing     = "REVIEWING"
	integrationAccepted      = "ACCEPTED"
	integrationIntegrated    = "INTEGRATED"
	integrationCanaryPassed  = "CANARY_PASSED"
	integrationSourceCleaned = "SOURCE_CLEANED"
	integrationRejected      = "REJECTED"
	integrationBlocked       = "BLOCKED"
)

func Summarize(snapshot BoardSnapshot) OperatorSummary {
	summary := OperatorSummary{
		TasksByState:    map[string]int{},
		HandoffsByState: map[string]int{},
		Bottlenecks:     []BottleneckView{},
	}
	for _, session := range snapshot.Sessions {
		switch session.Liveness {
		case SessionLivenessStale:
			summary.StaleSessions++
		default:
			if session.EndedAt == nil && session.InterruptedAt == nil {
				summary.ActiveSessions++
			}
		}
	}
	for _, task := range snapshot.Tasks {
		summary.TasksByState[task.State]++
	}
	for _, handoff := range snapshot.Handoffs {
		state := normalizedIntegrationState(handoff)
		summary.HandoffsByState[state]++
		if state != integrationSourceCleaned && state != integrationRejected {
			summary.IntegrationQueue++
		}
	}
	conflicts := map[string]struct{}{}
	for _, reservation := range snapshot.Reservations {
		for _, other := range reservation.ConflictIDs {
			pair := []string{reservation.ID, other}
			sort.Strings(pair)
			conflicts[pair[0]+"\x00"+pair[1]] = struct{}{}
		}
	}
	for _, warning := range snapshot.Warnings {
		if strings.HasPrefix(warning, "git_risk:") {
			conflicts[warning] = struct{}{}
		}
	}
	summary.Conflicts = len(conflicts)
	workComplete := summary.TasksByState["WORK_COMPLETE"]
	verifiedDone := summary.TasksByState["VERIFIED_DONE"]
	if workComplete != 0 || verifiedDone != 0 {
		summary.Bottlenecks = append(summary.Bottlenecks, BottleneckView{
			From: "WORK_COMPLETE", To: "VERIFIED_DONE", Waiting: workComplete, Done: verifiedDone,
		})
	}
	return summary
}

func IntegrationQueue(snapshot BoardSnapshot) []IntegrationQueueItemView {
	items := make([]IntegrationQueueItemView, 0, len(snapshot.Handoffs))
	for _, handoff := range snapshot.Handoffs {
		state := normalizedIntegrationState(handoff)
		if state == integrationSourceCleaned || state == integrationRejected {
			continue
		}
		item := IntegrationQueueItemView{
			HandoffID: handoff.ID, TaskID: handoff.TaskID, State: state,
			SourceSessionID: handoff.SourceSessionID, UpdatedAt: handoff.CreatedAt,
			MissingEvidence: []string{},
		}
		if handoff.Decision != nil {
			item.ReviewerSessionID = handoff.Decision.ActorSessionID
			if handoff.Decision.CreatedAt.After(item.UpdatedAt) {
				item.UpdatedAt = handoff.Decision.CreatedAt
			}
		}
		for _, event := range handoff.Lifecycle {
			if event.SourceCommit != "" {
				item.SourceCommit = event.SourceCommit
			}
			if event.SourceTree != "" {
				item.SourceTree = event.SourceTree
			}
			if event.IntegrationCommit != "" {
				item.IntegrationCommit = event.IntegrationCommit
			}
			if event.CanaryTargetSHA != "" {
				item.CanaryTargetSHA = event.CanaryTargetSHA
			}
			if event.CanaryResult != "" {
				item.CanaryResult = event.CanaryResult
			}
			if event.SourceWorktreeCleaned && event.SourceBranchCleaned {
				item.SourceCleanupComplete = true
			}
			if event.CreatedAt.After(item.UpdatedAt) {
				item.UpdatedAt = event.CreatedAt
			}
		}
		if item.SourceCommit == "" {
			item.MissingEvidence = append(item.MissingEvidence, "source_commit")
		}
		if item.SourceTree == "" {
			item.MissingEvidence = append(item.MissingEvidence, "source_tree")
		}
		if stateAtLeast(state, integrationIntegrated) && item.IntegrationCommit == "" {
			item.MissingEvidence = append(item.MissingEvidence, "integration_commit")
		}
		if stateAtLeast(state, integrationCanaryPassed) && (item.CanaryTargetSHA == "" || item.CanaryResult == "") {
			item.MissingEvidence = append(item.MissingEvidence, "canary_result")
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].HandoffID < items[j].HandoffID
		}
		return items[i].UpdatedAt.Before(items[j].UpdatedAt)
	})
	return items
}

func normalizedIntegrationState(handoff HandoffView) string {
	if handoff.IntegrationState != "" {
		return handoff.IntegrationState
	}
	if handoff.Decision != nil {
		switch strings.ToLower(handoff.Decision.Decision) {
		case "accepted":
			return integrationAccepted
		case "rejected":
			return integrationRejected
		}
	}
	return integrationSubmitted
}

func stateAtLeast(state, threshold string) bool {
	order := map[string]int{
		integrationSubmitted: 0, integrationReviewing: 1, integrationAccepted: 2,
		integrationIntegrated: 3, integrationCanaryPassed: 4, integrationSourceCleaned: 5,
	}
	value, ok := order[state]
	minimum, thresholdOK := order[threshold]
	return ok && thresholdOK && value >= minimum
}
