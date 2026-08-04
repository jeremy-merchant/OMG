package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	dependencyapp "github.com/jeremy-merchant/oh-my-group/internal/app/dependency"
	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	handoffapp "github.com/jeremy-merchant/oh-my-group/internal/app/handoff"
	messageapp "github.com/jeremy-merchant/oh-my-group/internal/app/message"
	progressapp "github.com/jeremy-merchant/oh-my-group/internal/app/progress"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	lineage "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

func (d *ServiceDispatcher) dispatchCoordination(ctx context.Context, request Request, selection foundation.Selection) (Outcome, bool) {
	mutation := coordinationMutation(request.Command)
	if !coordinationCommand(request.Command) {
		return Outcome{}, false
	}
	if len(request.Payload) == 0 || len(request.Payload) > 1<<20 || (mutation && request.IdempotencyKey == "") || (!mutation && request.IdempotencyKey != "") {
		return Outcome{Error: invalidRequest()}, true
	}

	switch request.Command {
	case "progress.add":
		var payload coordinationProgressPayload
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationProgressResult
		err := d.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			item, err := progressapp.New(store, nil).Append(ctx, domain.IdempotencyKey(request.IdempotencyKey), coord.Progress{ID: payload.ID, TaskID: payload.TaskID, RunID: payload.RunID, SessionID: payload.SessionID, Phase: coord.Phase(payload.Phase), Done: payload.Done, Doing: payload.Doing, Next: payload.Next, SupersedesID: payload.SupersedesID})
			if err == nil {
				result = safeCoordinationProgress(item)
			}
			return err
		})
		return outcome(result, err), true
	case "progress.history":
		var payload coordinationTaskIDPayload
		if !decodePayload(request.Payload, &payload) || payload.TaskID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		var result []coordinationProgressResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			items, err := progressapp.New(store, nil).History(ctx, payload.TaskID)
			if err == nil {
				result = safeCoordinationProgresses(items)
			}
			return err
		})
		return outcome(result, err), true
	case "dependency.add":
		var payload coordinationDependencyPayload
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationDependencyResult
		err := d.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			item, err := dependencyapp.New(store, nil).Add(ctx, domain.IdempotencyKey(request.IdempotencyKey), coord.Dependency{ID: payload.ID, PrerequisiteTaskID: payload.PrerequisiteTaskID, DependentTaskID: payload.DependentTaskID, Kind: coord.DependencyKind(payload.Kind), Criterion: coord.UnblockCriterion(payload.Criterion)})
			if err == nil {
				result = safeCoordinationDependency(item)
			}
			return err
		})
		return outcome(result, err), true
	case "dependency.list":
		var payload struct{}
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result []coordinationDependencyResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			items, err := dependencyapp.New(store, nil).List(ctx, string(resolved.Project))
			if err == nil {
				result = safeCoordinationDependencies(items)
			}
			return err
		})
		return outcome(result, err), true
	case "message.inbox":
		var payload coordinationInboxPayload
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result []coordinationMessageResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			items, err := messageapp.New(store, nil).Inbox(ctx, string(resolved.Project), coordinationRecipient(payload.Recipient))
			if err == nil {
				result = safeCoordinationMessages(items)
			}
			return err
		})
		return outcome(result, err), true
	case "message.thread":
		var payload coordinationThreadPayload
		if !decodePayload(request.Payload, &payload) || payload.ThreadID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		var result []coordinationMessageResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			items, err := messageapp.New(store, nil).Thread(ctx, payload.ThreadID)
			if err == nil {
				result = safeCoordinationMessages(items)
			}
			return err
		})
		return outcome(result, err), true
	case "message.deliver", "message.read", "message.ack":
		var payload coordinationMessageRecipientPayload
		if !decodePayload(request.Payload, &payload) || payload.MessageID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationDeliveryResult
		err := d.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			svc := messageapp.New(store, nil)
			var item coord.RecipientDelivery
			var err error
			switch request.Command {
			case "message.deliver":
				item, err = svc.Deliver(ctx, domain.IdempotencyKey(request.IdempotencyKey), payload.MessageID, coordinationRecipient(payload.Recipient))
			case "message.read":
				item, err = svc.Read(ctx, domain.IdempotencyKey(request.IdempotencyKey), payload.MessageID, coordinationRecipient(payload.Recipient))
			default:
				item, err = svc.Acknowledge(ctx, domain.IdempotencyKey(request.IdempotencyKey), payload.MessageID, coordinationRecipient(payload.Recipient))
			}
			if err == nil {
				result = safeCoordinationDelivery(item)
			}
			return err
		})
		return outcome(result, err), true
	case "handoff.show":
		var payload coordinationHandoffIDPayload
		if !decodePayload(request.Payload, &payload) || payload.HandoffID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationHandoffResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			item, err := handoffapp.New(store, nil).Get(ctx, payload.HandoffID)
			if err == nil {
				result, err = safeCoordinationHandoffWithDecision(ctx, store, item)
			}
			return err
		})
		return outcome(result, err), true
	case "handoff.history":
		var payload coordinationTaskIDPayload
		if !decodePayload(request.Payload, &payload) || payload.TaskID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		var result []coordinationHandoffResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			items, err := handoffapp.New(store, nil).History(ctx, payload.TaskID)
			if err != nil {
				return err
			}
			result = make([]coordinationHandoffResult, len(items))
			for i := range items {
				result[i], err = safeCoordinationHandoffWithDecision(ctx, store, items[i])
				if err != nil {
					return err
				}
			}
			return nil
		})
		return outcome(result, err), true
	case "handoff.supersede":
		var payload coordinationSupersedePayload
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationHandoffResult
		err := d.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			item, err := handoffapp.New(store, nil).Supersede(ctx, domain.IdempotencyKey(request.IdempotencyKey), payload.HandoffID, payload.NewID, payload.Summary)
			if err == nil {
				result = safeCoordinationHandoff(item)
			}
			return err
		})
		return outcome(result, err), true
	case "handoff.accept", "handoff.reject":
		var payload coordinationDecisionPayload
		if !decodePayload(request.Payload, &payload) || payload.HandoffID == "" || payload.ActorSessionID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		decision := coord.HandoffAccepted
		if request.Command == "handoff.reject" {
			decision = coord.HandoffRejected
		}
		decisionID := payload.DecisionID
		if decisionID == "" {
			decisionID = coordinationDecisionID(request.IdempotencyKey, payload.HandoffID, request.Command[len("handoff."):])
		}
		var result coordinationDecisionResult
		err := d.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			item, err := handoffapp.New(store, nil).Decide(ctx, domain.IdempotencyKey(request.IdempotencyKey), payload.HandoffID, string(decision), decisionID, payload.ActorSessionID)
			if err == nil {
				result = safeCoordinationDecision(item)
			}
			return err
		})
		return outcome(result, err), true
	case "handoff.advance":
		var payload coordinationLifecyclePayload
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationLifecycleResult
		err := d.service.WithCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			item, err := handoffapp.New(store, nil).Advance(ctx, domain.IdempotencyKey(request.IdempotencyKey), coord.HandoffLifecycleEvent{
				ID: payload.ID, HandoffID: payload.HandoffID, ActorSessionID: payload.ActorSessionID,
				State: coord.IntegrationState(payload.State), SourceCommit: payload.SourceCommit,
				SourceTree: payload.SourceTree, IntegrationCommit: payload.IntegrationCommit,
				CanaryRunID: payload.CanaryRunID, CanaryIntegrationRef: payload.CanaryIntegrationRef,
				CanaryTargetSHA: payload.CanaryTargetSHA, CanaryTargetTree: payload.CanaryTargetTree, CanaryResult: payload.CanaryResult,
				CanaryCommand: payload.CanaryCommand, CanaryExecutionKind: payload.CanaryExecutionKind, CanaryEnvironmentFingerprint: payload.CanaryEnvironmentFingerprint,
				CanaryHeadBefore: payload.CanaryHeadBefore, CanaryHeadAfter: payload.CanaryHeadAfter,
				CanaryRefFingerprintBefore: payload.CanaryRefFingerprintBefore, CanaryRefFingerprintAfter: payload.CanaryRefFingerprintAfter,
				CanaryExitCode: payload.CanaryExitCode, CanaryPassedCount: payload.CanaryPassedCount, CanaryFailedCount: payload.CanaryFailedCount, CanarySkippedCount: payload.CanarySkippedCount,
				CanaryStartedAt: payload.CanaryStartedAt, CanaryFinishedAt: payload.CanaryFinishedAt, CanaryEvidencePath: payload.CanaryEvidencePath,
				SourceWorktreeCleaned: payload.SourceWorktreeCleaned, SourceBranchCleaned: payload.SourceBranchCleaned,
				Note: payload.Note,
			})
			if err == nil {
				result = safeCoordinationLifecycle(item)
			}
			return err
		})
		return outcome(result, err), true
	case "handoff.lifecycle":
		var payload coordinationHandoffIDPayload
		if !decodePayload(request.Payload, &payload) || payload.HandoffID == "" {
			return Outcome{Error: invalidRequest()}, true
		}
		var result []coordinationLifecycleResult
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			items, err := handoffapp.New(store, nil).Lifecycle(ctx, payload.HandoffID)
			if err == nil {
				result = make([]coordinationLifecycleResult, len(items))
				for i := range items {
					result[i] = safeCoordinationLifecycle(items[i])
				}
			}
			return err
		})
		return outcome(result, err), true
	case "handoff.adopt":
		var payload coordinationAdoptionPayload
		if !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}, true
		}
		var result coordinationAdoptionResult
		err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			adoption, ok := coordinationAdoption(payload, string(resolved.Project))
			if !ok {
				return invalidRequest()
			}
			item, err := handoffapp.New(store, nil).Adopt(ctx, domain.IdempotencyKey(request.IdempotencyKey), adoption)
			if err == nil {
				result = safeCoordinationAdoption(item, payload.EntityKind, payload.EntityID)
			}
			return err
		})
		return outcome(result, err), true
	}
	return Outcome{}, false
}

func coordinationCommand(command string) bool {
	switch command {
	case "progress.add", "progress.history", "dependency.add", "dependency.list", "message.inbox", "message.thread", "message.deliver", "message.read", "message.ack", "handoff.show", "handoff.history", "handoff.supersede", "handoff.accept", "handoff.reject", "handoff.advance", "handoff.lifecycle", "handoff.adopt":
		return true
	}
	return false
}
func coordinationMutation(command string) bool {
	switch command {
	case "progress.add", "dependency.add", "message.deliver", "message.read", "message.ack", "handoff.supersede", "handoff.accept", "handoff.reject", "handoff.advance", "handoff.adopt":
		return true
	}
	return false
}
func coordinationDecisionID(key, handoffID, decision string) string {
	sum := sha256.Sum256([]byte(key + "\x00" + handoffID + "\x00" + decision))
	return "decision-" + hex.EncodeToString(sum[:])
}

type coordinationProgressPayload struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id"`
	RunID        string   `json:"run_id,omitempty"`
	SessionID    string   `json:"session_id"`
	Phase        string   `json:"phase"`
	Done         []string `json:"done"`
	Doing        []string `json:"doing"`
	Next         []string `json:"next"`
	SupersedesID string   `json:"supersedes_id,omitempty"`
}
type coordinationTaskIDPayload struct {
	TaskID string `json:"task_id"`
}
type coordinationDependencyPayload struct {
	ID                 string `json:"id"`
	PrerequisiteTaskID string `json:"prerequisite_task_id"`
	DependentTaskID    string `json:"dependent_task_id"`
	Kind               string `json:"kind"`
	Criterion          string `json:"criterion"`
}
type coordinationRecipientPayload struct {
	SessionID string `json:"session_id,omitempty"`
	HumanID   string `json:"human_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Role      string `json:"role,omitempty"`
}
type coordinationInboxPayload struct {
	Recipient coordinationRecipientPayload `json:"recipient"`
}
type coordinationThreadPayload struct {
	ThreadID string `json:"thread_id"`
}
type coordinationMessageRecipientPayload struct {
	MessageID string                       `json:"message_id"`
	Recipient coordinationRecipientPayload `json:"recipient"`
}
type coordinationHandoffIDPayload struct {
	HandoffID string `json:"handoff_id"`
}
type coordinationSupersedePayload struct {
	HandoffID string `json:"handoff_id"`
	NewID     string `json:"new_id"`
	Summary   string `json:"summary"`
}
type coordinationDecisionPayload struct {
	HandoffID      string `json:"handoff_id"`
	DecisionID     string `json:"decision_id,omitempty"`
	ActorSessionID string `json:"actor_session_id"`
}
type coordinationLifecyclePayload struct {
	ID                           string     `json:"id"`
	HandoffID                    string     `json:"handoff_id"`
	ActorSessionID               string     `json:"actor_session_id"`
	State                        string     `json:"state"`
	SourceCommit                 string     `json:"source_commit,omitempty"`
	SourceTree                   string     `json:"source_tree,omitempty"`
	IntegrationCommit            string     `json:"integration_commit,omitempty"`
	CanaryTargetSHA              string     `json:"canary_target_sha,omitempty"`
	CanaryRunID                  string     `json:"canary_run_id,omitempty"`
	CanaryIntegrationRef         string     `json:"canary_integration_ref,omitempty"`
	CanaryTargetTree             string     `json:"canary_target_tree,omitempty"`
	CanaryResult                 string     `json:"canary_result,omitempty"`
	CanaryCommand                string     `json:"canary_command,omitempty"`
	CanaryExecutionKind          string     `json:"canary_execution_kind,omitempty"`
	CanaryEnvironmentFingerprint string     `json:"canary_environment_fingerprint,omitempty"`
	CanaryHeadBefore             string     `json:"canary_head_before,omitempty"`
	CanaryHeadAfter              string     `json:"canary_head_after,omitempty"`
	CanaryRefFingerprintBefore   string     `json:"canary_ref_fingerprint_before,omitempty"`
	CanaryRefFingerprintAfter    string     `json:"canary_ref_fingerprint_after,omitempty"`
	CanaryExitCode               *int       `json:"canary_exit_code,omitempty"`
	CanaryPassedCount            int        `json:"canary_passed_count,omitempty"`
	CanaryFailedCount            int        `json:"canary_failed_count,omitempty"`
	CanarySkippedCount           int        `json:"canary_skipped_count,omitempty"`
	CanaryStartedAt              *time.Time `json:"canary_started_at,omitempty"`
	CanaryFinishedAt             *time.Time `json:"canary_finished_at,omitempty"`
	CanaryEvidencePath           string     `json:"canary_evidence_path,omitempty"`
	SourceWorktreeCleaned        bool       `json:"source_worktree_cleaned,omitempty"`
	SourceBranchCleaned          bool       `json:"source_branch_cleaned,omitempty"`
	Note                         string     `json:"note,omitempty"`
}
type coordinationAdoptionPayload struct {
	ID                string `json:"id"`
	EntityKind        string `json:"entity_kind"`
	EntityID          string `json:"entity_id"`
	NewOwnerSessionID string `json:"new_owner_session_id"`
	Reason            string `json:"reason"`
}

type coordinationProgressResult struct {
	ID        string   `json:"id"`
	TaskID    string   `json:"task_id"`
	RunID     string   `json:"run_id,omitempty"`
	SessionID string   `json:"session_id"`
	Phase     string   `json:"phase"`
	Done      []string `json:"done"`
	Doing     []string `json:"doing"`
	Next      []string `json:"next"`
}
type coordinationDependencyResult struct {
	ID                 string `json:"id"`
	PrerequisiteTaskID string `json:"prerequisite_task_id"`
	DependentTaskID    string `json:"dependent_task_id"`
	Kind               string `json:"kind"`
	Criterion          string `json:"criterion"`
}
type coordinationMessageResult struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ThreadID        string `json:"thread_id"`
	SenderSessionID string `json:"sender_session_id"`
	Subject         string `json:"subject,omitempty"`
	RelatedTaskID   string `json:"related_task_id,omitempty"`
}
type coordinationDeliveryResult struct {
	MessageID    string `json:"message_id"`
	Recipient    string `json:"recipient"`
	Delivered    bool   `json:"delivered"`
	Read         bool   `json:"read"`
	Acknowledged bool   `json:"acknowledged"`
}
type coordinationHandoffDecisionResult struct {
	ID             string    `json:"id"`
	Decision       string    `json:"decision"`
	ActorSessionID string    `json:"actor_session_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type coordinationVerificationEvidenceResult struct {
	Summary string `json:"summary"`
	Hash    string `json:"hash"`
}

type coordinationHandoffResult struct {
	ID                   string                                   `json:"id"`
	TaskID               string                                   `json:"task_id"`
	RunID                string                                   `json:"run_id"`
	RunState             string                                   `json:"run_state"`
	SourceSessionID      string                                   `json:"source_session_id"`
	TargetSessionID      string                                   `json:"target_session_id,omitempty"`
	TargetTaskID         string                                   `json:"target_task_id,omitempty"`
	Summary              string                                   `json:"summary"`
	FinalOutputPolicy    string                                   `json:"final_output_policy"`
	FinalOutputHash      string                                   `json:"final_output_hash,omitempty"`
	ChangedFiles         []string                                 `json:"changed_files"`
	Commits              []string                                 `json:"commits"`
	VerificationEvidence []coordinationVerificationEvidenceResult `json:"verification_evidence"`
	RemainingRisks       []string                                 `json:"remaining_risks"`
	SuggestedActions     []string                                 `json:"suggested_actions"`
	Status               string                                   `json:"status"`
	IntegrationState     string                                   `json:"integration_state"`
	Lifecycle            []coordinationLifecycleResult            `json:"lifecycle"`
	Decision             *coordinationHandoffDecisionResult       `json:"decision,omitempty"`
	SupersedesID         string                                   `json:"supersedes_id,omitempty"`
}
type coordinationDecisionResult struct {
	ID             string `json:"id"`
	HandoffID      string `json:"handoff_id"`
	Decision       string `json:"decision"`
	ActorSessionID string `json:"actor_session_id"`
}
type coordinationLifecycleResult struct {
	ID                           string     `json:"id"`
	HandoffID                    string     `json:"handoff_id"`
	ActorSessionID               string     `json:"actor_session_id"`
	State                        string     `json:"state"`
	SourceCommit                 string     `json:"source_commit,omitempty"`
	SourceTree                   string     `json:"source_tree,omitempty"`
	IntegrationCommit            string     `json:"integration_commit,omitempty"`
	CanaryTargetSHA              string     `json:"canary_target_sha,omitempty"`
	CanaryRunID                  string     `json:"canary_run_id,omitempty"`
	CanaryIntegrationRef         string     `json:"canary_integration_ref,omitempty"`
	CanaryTargetTree             string     `json:"canary_target_tree,omitempty"`
	CanaryResult                 string     `json:"canary_result,omitempty"`
	CanaryCommand                string     `json:"canary_command,omitempty"`
	CanaryExecutionKind          string     `json:"canary_execution_kind,omitempty"`
	CanaryEnvironmentFingerprint string     `json:"canary_environment_fingerprint,omitempty"`
	CanaryHeadBefore             string     `json:"canary_head_before,omitempty"`
	CanaryHeadAfter              string     `json:"canary_head_after,omitempty"`
	CanaryRefFingerprintBefore   string     `json:"canary_ref_fingerprint_before,omitempty"`
	CanaryRefFingerprintAfter    string     `json:"canary_ref_fingerprint_after,omitempty"`
	CanaryExitCode               *int       `json:"canary_exit_code,omitempty"`
	CanaryPassedCount            int        `json:"canary_passed_count,omitempty"`
	CanaryFailedCount            int        `json:"canary_failed_count,omitempty"`
	CanarySkippedCount           int        `json:"canary_skipped_count,omitempty"`
	CanaryStartedAt              *time.Time `json:"canary_started_at,omitempty"`
	CanaryFinishedAt             *time.Time `json:"canary_finished_at,omitempty"`
	CanaryEvidencePath           string     `json:"canary_evidence_path,omitempty"`
	SourceWorktreeCleaned        bool       `json:"source_worktree_cleaned"`
	SourceBranchCleaned          bool       `json:"source_branch_cleaned"`
	Note                         string     `json:"note,omitempty"`
	CreatedAt                    time.Time  `json:"created_at"`
}
type coordinationAdoptionResult struct {
	ID                string `json:"id"`
	EntityKind        string `json:"entity_kind"`
	EntityID          string `json:"entity_id"`
	NewOwnerSessionID string `json:"new_owner_session_id"`
}

func coordinationRecipient(v coordinationRecipientPayload) coord.RecipientTarget {
	return coord.RecipientTarget{SessionID: v.SessionID, HumanID: v.HumanID, TaskID: v.TaskID, Role: v.Role}
}
func coordinationAdoption(v coordinationAdoptionPayload, project string) (coord.Adoption, bool) {
	out := coord.Adoption{ID: v.ID, ProjectID: project, NewOwnerSessionID: v.NewOwnerSessionID, Reason: v.Reason}
	switch v.EntityKind {
	case "session":
		out.SessionID = v.EntityID
	case "task":
		out.TaskID = v.EntityID
	case "handoff":
		out.HandoffID = v.EntityID
	case "git_asset":
		out.GitAssetID = v.EntityID
	default:
		return coord.Adoption{}, false
	}
	return out, true
}
func safeCoordinationTexts(values []string) []string {
	out := make([]string, len(values))
	for i := range values {
		out[i] = safety.SafeText(values[i])
	}
	return out
}

func safeCoordinationProgress(v coord.Progress) coordinationProgressResult {
	return coordinationProgressResult{ID: v.ID, TaskID: v.TaskID, RunID: v.RunID, SessionID: v.SessionID, Phase: string(v.Phase), Done: safeCoordinationTexts(v.Done), Doing: safeCoordinationTexts(v.Doing), Next: safeCoordinationTexts(v.Next)}
}
func safeCoordinationProgresses(values []coord.Progress) []coordinationProgressResult {
	out := make([]coordinationProgressResult, len(values))
	for i := range values {
		out[i] = safeCoordinationProgress(values[i])
	}
	return out
}
func safeCoordinationDependency(v coord.Dependency) coordinationDependencyResult {
	return coordinationDependencyResult{ID: v.ID, PrerequisiteTaskID: v.PrerequisiteTaskID, DependentTaskID: v.DependentTaskID, Kind: string(v.Kind), Criterion: string(v.Criterion)}
}
func safeCoordinationDependencies(values []coord.Dependency) []coordinationDependencyResult {
	out := make([]coordinationDependencyResult, len(values))
	for i := range values {
		out[i] = safeCoordinationDependency(values[i])
	}
	return out
}
func safeCoordinationMessage(v coord.MailMessage) coordinationMessageResult {
	return coordinationMessageResult{ID: v.ID, Type: string(v.Type), ThreadID: v.ThreadID, SenderSessionID: v.SenderSessionID, Subject: safety.SafeText(v.Subject), RelatedTaskID: v.RelatedTaskID}
}
func safeCoordinationMessages(values []coord.MailMessage) []coordinationMessageResult {
	out := make([]coordinationMessageResult, len(values))
	for i := range values {
		out[i] = safeCoordinationMessage(values[i])
	}
	return out
}
func safeCoordinationDelivery(v coord.RecipientDelivery) coordinationDeliveryResult {
	return coordinationDeliveryResult{MessageID: v.MessageID, Recipient: coordinationRecipientKey(v.Recipient), Delivered: !v.DeliveredAt.IsZero(), Read: v.ReadAt != nil, Acknowledged: v.AckedAt != nil}
}
func coordinationRecipientKey(v coord.RecipientTarget) string {
	if v.SessionID != "" {
		return "session:" + v.SessionID
	}
	if v.HumanID != "" {
		return "human:" + v.HumanID
	}
	if v.TaskID != "" {
		return "task:" + v.TaskID
	}
	return "role:" + v.Role
}
func safeCoordinationHandoff(v coord.Handoff) coordinationHandoffResult {
	evidence := make([]coordinationVerificationEvidenceResult, len(v.VerificationEvidence))
	for i, item := range v.VerificationEvidence {
		evidence[i] = coordinationVerificationEvidenceResult{Summary: safety.SafeText(item.Summary), Hash: safety.SafeText(item.Hash)}
	}
	return coordinationHandoffResult{
		ID: v.ID, TaskID: v.TaskID, RunID: v.RunID, SourceSessionID: v.SourceSessionID, TargetSessionID: v.TargetSessionID, TargetTaskID: v.TargetTaskID,
		Summary: safety.SafeText(v.Summary), FinalOutputPolicy: string(v.FinalOutput.Policy), FinalOutputHash: safety.SafeText(v.FinalOutput.Hash),
		ChangedFiles: safeCoordinationTexts(v.ChangedFiles), Commits: safeCoordinationTexts(v.Commits), VerificationEvidence: evidence,
		RemainingRisks: safeCoordinationTexts(v.RemainingRisks), SuggestedActions: safeCoordinationTexts(v.SuggestedActions),
		Status: string(v.Status), IntegrationState: string(coord.IntegrationSubmitted), Lifecycle: []coordinationLifecycleResult{}, SupersedesID: v.SupersedesID,
	}
}
func safeCoordinationHandoffWithDecision(ctx context.Context, store ports.Store, handoff coord.Handoff) (coordinationHandoffResult, error) {
	result := safeCoordinationHandoff(handoff)
	err := store.Read(ctx, func(r ports.Repositories) error {
		run, ok, err := r.Coordination().GetRun(ctx, lineage.ID(handoff.RunID))
		if err != nil {
			return err
		}
		if ok {
			result.RunState = string(run.State)
		}
		decision, ok, err := r.Coordination().GetHandoffDecision(ctx, handoff.ID)
		if err != nil {
			return err
		}
		if ok {
			result.Decision = &coordinationHandoffDecisionResult{ID: decision.ID, Decision: string(decision.Decision), ActorSessionID: decision.DecidedBySessionID, CreatedAt: decision.CreatedAt}
		}
		events, err := r.Coordination().ListHandoffLifecycleEvents(ctx, handoff.ID)
		if err != nil {
			return err
		}
		result.Lifecycle = make([]coordinationLifecycleResult, len(events))
		for i := range events {
			result.Lifecycle[i] = safeCoordinationLifecycle(events[i])
		}
		var decisionPtr *coord.HandoffDecision
		if ok {
			decisionPtr = &decision
		}
		result.IntegrationState = string(coord.CurrentIntegrationState(events, decisionPtr))
		return nil
	})
	return result, err
}
func safeCoordinationDecision(v coord.HandoffDecision) coordinationDecisionResult {
	return coordinationDecisionResult{ID: v.ID, HandoffID: v.HandoffID, Decision: string(v.Decision), ActorSessionID: v.DecidedBySessionID}
}
func safeCoordinationLifecycle(v coord.HandoffLifecycleEvent) coordinationLifecycleResult {
	return coordinationLifecycleResult{ID: v.ID, HandoffID: v.HandoffID, ActorSessionID: v.ActorSessionID, State: string(v.State), SourceCommit: safety.SafeText(v.SourceCommit), SourceTree: safety.SafeText(v.SourceTree), IntegrationCommit: safety.SafeText(v.IntegrationCommit), CanaryRunID: safety.SafeText(v.CanaryRunID), CanaryIntegrationRef: safety.SafeText(v.CanaryIntegrationRef), CanaryTargetSHA: safety.SafeText(v.CanaryTargetSHA), CanaryTargetTree: safety.SafeText(v.CanaryTargetTree), CanaryResult: safety.SafeText(v.CanaryResult), CanaryCommand: safety.SafeText(v.CanaryCommand), CanaryExecutionKind: safety.SafeText(v.CanaryExecutionKind), CanaryEnvironmentFingerprint: safety.SafeText(v.CanaryEnvironmentFingerprint), CanaryHeadBefore: safety.SafeText(v.CanaryHeadBefore), CanaryHeadAfter: safety.SafeText(v.CanaryHeadAfter), CanaryRefFingerprintBefore: safety.SafeText(v.CanaryRefFingerprintBefore), CanaryRefFingerprintAfter: safety.SafeText(v.CanaryRefFingerprintAfter), CanaryExitCode: v.CanaryExitCode, CanaryPassedCount: v.CanaryPassedCount, CanaryFailedCount: v.CanaryFailedCount, CanarySkippedCount: v.CanarySkippedCount, CanaryStartedAt: v.CanaryStartedAt, CanaryFinishedAt: v.CanaryFinishedAt, CanaryEvidencePath: safety.SafeText(v.CanaryEvidencePath), SourceWorktreeCleaned: v.SourceWorktreeCleaned, SourceBranchCleaned: v.SourceBranchCleaned, Note: safety.SafeText(v.Note), CreatedAt: v.CreatedAt}
}
func safeCoordinationAdoption(v coord.Adoption, kind, entityID string) coordinationAdoptionResult {
	return coordinationAdoptionResult{ID: v.ID, EntityKind: kind, EntityID: entityID, NewOwnerSessionID: v.NewOwnerSessionID}
}
