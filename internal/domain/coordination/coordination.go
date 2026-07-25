// Package coordination defines pure, append-only coordination records and decisions.
package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
)

var ErrInvalid = errors.New("coordination: invalid record")

const (
	maxProgressItems   = 32
	maxProgressItemLen = 512
	maxRecipients      = 32
	maxSubjectLen      = 256
	maxBodyLen         = 16 * 1024
	maxSummaryLen      = 4 * 1024
	maxListItems       = 128
)

type Phase string

const (
	PhaseInspect   Phase = "inspect"
	PhasePlan      Phase = "plan"
	PhaseImplement Phase = "implement"
	PhaseTest      Phase = "test"
	PhaseReview    Phase = "review"
	PhaseWait      Phase = "wait"
)

// Progress is an immutable, append-only report. SupersedesID only describes a correction.
type Progress struct {
	ID, TaskID, RunID, SessionID string
	Phase                        Phase
	Done, Doing, Next            []string
	CreatedAt                    time.Time
	SupersedesID                 string
}

func (p Progress) Validate() error {
	if !stableID(p.ID) || !stableID(p.TaskID) || (p.RunID != "" && !stableID(p.RunID)) || !stableID(p.SessionID) || (p.SupersedesID != "" && !stableID(p.SupersedesID)) || !validPhase(p.Phase) || !utc(p.CreatedAt) || !boundedStrings(p.Done, maxProgressItems, maxProgressItemLen) || !boundedStrings(p.Doing, maxProgressItems, maxProgressItemLen) || !boundedStrings(p.Next, maxProgressItems, maxProgressItemLen) {
		return ErrInvalid
	}
	return nil
}

type DependencyKind string
type UnblockCriterion string

const (
	DependencyHard          DependencyKind   = "hard"
	DependencySoft          DependencyKind   = "soft"
	DependencyInformational DependencyKind   = "informational"
	UnblockWorkComplete     UnblockCriterion = "work_complete"
	UnblockVerifiedDone     UnblockCriterion = "verified_done"
)

// Dependency is a directed edge from PrerequisiteTaskID to DependentTaskID.
type Dependency struct {
	ID, PrerequisiteTaskID, DependentTaskID string
	Kind                                    DependencyKind
	Criterion                               UnblockCriterion
}

func (d Dependency) Validate() error {
	if !stableID(d.ID) || !stableID(d.PrerequisiteTaskID) || !stableID(d.DependentTaskID) || d.PrerequisiteTaskID == d.DependentTaskID || !validDependencyKind(d.Kind) || !validCriterion(d.Criterion) {
		return ErrInvalid
	}
	return nil
}

// AddDependency validates a graph before returning a new edge list; it never changes edges.
func AddDependency(edges []Dependency, candidate Dependency) ([]Dependency, error) {
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	for _, edge := range edges {
		if err := edge.Validate(); err != nil {
			return nil, err
		}
		if edge.ID == candidate.ID || (edge.PrerequisiteTaskID == candidate.PrerequisiteTaskID && edge.DependentTaskID == candidate.DependentTaskID) {
			return nil, ErrInvalid
		}
	}
	if reachable(edges, candidate.DependentTaskID, candidate.PrerequisiteTaskID) {
		return nil, ErrInvalid
	}
	result := make([]Dependency, len(edges)+1)
	copy(result, edges)
	result[len(edges)] = candidate
	return result, nil
}

func reachable(edges []Dependency, start, target string) bool {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == target {
			return true
		}
		for _, edge := range edges {
			if edge.PrerequisiteTaskID == current && !seen[edge.DependentTaskID] {
				seen[edge.DependentTaskID] = true
				queue = append(queue, edge.DependentTaskID)
			}
		}
	}
	return false
}

// SatisfactionFact is a new fact. NotificationKey supports durable exactly-once delivery.
type SatisfactionFact struct {
	DependencyID      string
	PrerequisiteState lineage.TaskState
	Satisfied         bool
	NotificationKey   string
}

func DecideSatisfaction(dependency Dependency, state lineage.TaskState) SatisfactionFact {
	satisfied := state == lineage.TaskVerifiedDone || (dependency.Criterion == UnblockWorkComplete && state == lineage.TaskWorkComplete)
	fact := SatisfactionFact{DependencyID: dependency.ID, PrerequisiteState: state, Satisfied: satisfied}
	if satisfied {
		fact.NotificationKey = SatisfactionNotificationKey(dependency.ID, dependency.Criterion)
	}
	return fact
}

func SatisfactionNotificationKey(dependencyID string, criterion UnblockCriterion) string {
	digest := sha256.Sum256([]byte(dependencyID + "\x00" + string(criterion)))
	return "dependency-satisfied:" + hex.EncodeToString(digest[:])
}

type MessageType string

const (
	MessageNotice     MessageType = "NOTICE"
	MessageQuestion   MessageType = "QUESTION"
	MessageDependency MessageType = "DEPENDENCY"
	MessageConflict   MessageType = "CONFLICT"
	MessageHandoff    MessageType = "HANDOFF"
	MessageDone       MessageType = "DONE"
	MessageBlocked    MessageType = "BLOCKED"
	MessageCancel     MessageType = "CANCEL"
	MessageACK        MessageType = "ACK"
)

func AllMessageTypes() []MessageType {
	return []MessageType{MessageNotice, MessageQuestion, MessageDependency, MessageConflict, MessageHandoff, MessageDone, MessageBlocked, MessageCancel, MessageACK}
}

// RecipientTarget has exactly one address; content cannot establish authority.
type RecipientTarget struct{ SessionID, HumanID, TaskID, Role string }

func (r RecipientTarget) Validate() error {
	count := 0
	for _, value := range []string{r.SessionID, r.HumanID, r.TaskID, r.Role} {
		if stableID(value) {
			count++
		} else if strings.TrimSpace(value) != "" {
			return ErrInvalid
		}
	}
	if count != 1 {
		return ErrInvalid
	}
	return nil
}

// MailMessage treats Subject and Body as inert data only.
type MailMessage struct {
	ID                           string
	Type                         MessageType
	ThreadID, SenderSessionID    string
	Recipients                   []RecipientTarget
	Subject, Body, RelatedTaskID string
	CreatedAt                    time.Time
}

func (m MailMessage) Validate() error {
	if !stableID(m.ID) || !validMessageType(m.Type) || !stableID(m.ThreadID) || !stableID(m.SenderSessionID) || (m.RelatedTaskID != "" && !stableID(m.RelatedTaskID)) || !optionalBoundedText(m.Subject, maxSubjectLen) || !boundedText(m.Body, maxBodyLen) || len(m.Recipients) == 0 || len(m.Recipients) > maxRecipients || !utc(m.CreatedAt) {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(m.Recipients))
	for _, recipient := range m.Recipients {
		if recipient.Validate() != nil {
			return ErrInvalid
		}
		key := recipient.key()
		if _, exists := seen[key]; exists {
			return ErrInvalid
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (r RecipientTarget) key() string {
	return r.SessionID + "\x00" + r.HumanID + "\x00" + r.TaskID + "\x00" + r.Role
}

// RecipientDelivery records monotonic recipient lifecycle facts in a value.
type RecipientDelivery struct {
	MessageID       string
	Recipient       RecipientTarget
	DeliveredAt     time.Time
	ReadAt, AckedAt *time.Time
}

func DeliverRecipient(messageID string, recipient RecipientTarget, deliveredAt time.Time) (RecipientDelivery, error) {
	if !stableID(messageID) || recipient.Validate() != nil || !utc(deliveredAt) {
		return RecipientDelivery{}, ErrInvalid
	}
	return RecipientDelivery{MessageID: messageID, Recipient: recipient, DeliveredAt: deliveredAt}, nil
}

func MarkRecipientRead(delivery RecipientDelivery, readAt time.Time) (RecipientDelivery, error) {
	if !validDelivery(delivery) || !utc(readAt) || readAt.Before(delivery.DeliveredAt) {
		return RecipientDelivery{}, ErrInvalid
	}
	if delivery.ReadAt != nil {
		return delivery, nil
	}
	result := delivery
	stamp := readAt
	result.ReadAt = &stamp
	return result, nil
}

func AcknowledgeRecipient(delivery RecipientDelivery, ackedAt time.Time) (RecipientDelivery, error) {
	if !validDelivery(delivery) || delivery.ReadAt == nil || !utc(ackedAt) || ackedAt.Before(*delivery.ReadAt) {
		return RecipientDelivery{}, ErrInvalid
	}
	if delivery.AckedAt != nil {
		return delivery, nil
	}
	result := delivery
	stamp := ackedAt
	result.AckedAt = &stamp
	return result, nil
}

func validDelivery(d RecipientDelivery) bool {
	if !stableID(d.MessageID) || d.Recipient.Validate() != nil || !utc(d.DeliveredAt) {
		return false
	}
	if d.ReadAt != nil && (!utc(*d.ReadAt) || d.ReadAt.Before(d.DeliveredAt)) {
		return false
	}
	return d.AckedAt == nil || (d.ReadAt != nil && utc(*d.AckedAt) && !d.AckedAt.Before(*d.ReadAt))
}

type HandoffStatus string

const (
	HandoffSubmitted  HandoffStatus = "submitted"
	HandoffAccepted   HandoffStatus = "accepted"
	HandoffRejected   HandoffStatus = "rejected"
	HandoffSuperseded HandoffStatus = "superseded"
)

// FinalOutputPolicy controls whether a handoff persists raw, redacted, hash-only, or no final output.
type FinalOutputPolicy string

const (
	FinalOutputNone     FinalOutputPolicy = "none"
	FinalOutputHashOnly FinalOutputPolicy = "hash_only"
	FinalOutputRedacted FinalOutputPolicy = "redacted"
	FinalOutputFull     FinalOutputPolicy = "full"
)

// SensitiveText distinguishes restricted raw content from safe summary and hash fields.
type SensitiveText struct {
	Text, Hash string
	Policy     FinalOutputPolicy
}
type SafeEvidence struct{ Summary, Hash string }

// Handoff is immutable. Status is submitted at creation; outcome facts are separate.
type Handoff struct {
	ID, TaskID, RunID, SourceSessionID string
	TargetSessionID, TargetTaskID      string
	Summary                            string
	FinalOutput                        SensitiveText
	ChangedFiles, Commits              []string
	VerificationEvidence               []SafeEvidence
	RemainingRisks, SuggestedActions   []string
	Status                             HandoffStatus
	CreatedAt                          time.Time
	SupersedesID                       string
}

func (h Handoff) Validate(runState lineage.RunState) error {
	if !stableID(h.ID) || !stableID(h.TaskID) || !stableID(h.RunID) || !stableID(h.SourceSessionID) || !boundedText(h.Summary, maxSummaryLen) || !validSensitiveText(h.FinalOutput) || !boundedStrings(h.ChangedFiles, maxListItems, maxProgressItemLen) || !boundedStrings(h.Commits, maxListItems, maxProgressItemLen) || !boundedStrings(h.RemainingRisks, maxListItems, maxProgressItemLen) || !boundedStrings(h.SuggestedActions, maxListItems, maxProgressItemLen) || !utc(h.CreatedAt) || h.Status != HandoffSubmitted {
		return ErrInvalid
	}
	if h.TargetSessionID != "" && !stableID(h.TargetSessionID) || h.TargetTaskID != "" && !stableID(h.TargetTaskID) || h.SupersedesID != "" && !stableID(h.SupersedesID) {
		return ErrInvalid
	}
	if runState == lineage.RunVerifiedDone && len(h.VerificationEvidence) == 0 {
		return ErrInvalid
	}
	for _, evidence := range h.VerificationEvidence {
		if !boundedText(evidence.Summary, maxSummaryLen) || !stableID(evidence.Hash) {
			return ErrInvalid
		}
	}
	return nil
}

func validSensitiveText(value SensitiveText) bool {
	switch value.Policy {
	case FinalOutputNone:
		return value.Text == "" && value.Hash == ""
	case FinalOutputHashOnly:
		return value.Text == "" && stableID(value.Hash)
	case FinalOutputRedacted:
		return stableID(value.Hash)
	case FinalOutputFull:
		return value.Text == "" || stableID(value.Hash)
	default:
		return false
	}
}

// HandoffDecision records acceptance or rejection without modifying a submitted handoff.
type HandoffDecision struct {
	ID, HandoffID      string
	Decision           HandoffStatus
	DecidedBySessionID string
	CreatedAt          time.Time
}

func DecideHandoff(handoff Handoff, decision HandoffStatus, decisionID, bySessionID string, at time.Time) (HandoffDecision, error) {
	if handoff.Status != HandoffSubmitted || !stableID(handoff.ID) || !stableID(decisionID) || (decision != HandoffAccepted && decision != HandoffRejected) || !stableID(bySessionID) || !utc(at) {
		return HandoffDecision{}, ErrInvalid
	}
	return HandoffDecision{ID: decisionID, HandoffID: handoff.ID, Decision: decision, DecidedBySessionID: bySessionID, CreatedAt: at}, nil
}

// SupersedeHandoff returns a distinct submitted record that links back to the original.
func SupersedeHandoff(original Handoff, newID, summary string, at time.Time) (Handoff, error) {
	if original.Status != HandoffSubmitted || !stableID(newID) || newID == original.ID || !boundedText(summary, maxSummaryLen) || !utc(at) {
		return Handoff{}, ErrInvalid
	}
	replacement := original
	replacement.ID = newID
	replacement.Summary = summary
	replacement.Status = HandoffSubmitted
	replacement.CreatedAt = at
	replacement.SupersedesID = original.ID
	return replacement, nil
}

// Adoption selects exactly one orphan coordination target and grants no authority.
type Adoption struct {
	ID, ProjectID, SessionID, TaskID, HandoffID, GitAssetID, NewOwnerSessionID, Reason string
	CreatedAt                                                                          time.Time
}

func (a Adoption) Validate() error {
	if !stableID(a.ID) || !stableID(a.ProjectID) || !stableID(a.NewOwnerSessionID) || !boundedText(a.Reason, maxSummaryLen) || !utc(a.CreatedAt) {
		return ErrInvalid
	}
	count := 0
	for _, target := range []string{a.SessionID, a.TaskID, a.HandoffID, a.GitAssetID} {
		if stableID(target) {
			count++
		} else if strings.TrimSpace(target) != "" {
			return ErrInvalid
		}
	}
	if count != 1 {
		return ErrInvalid
	}
	return nil
}

func (Adoption) GrantsRestrictedAuthority() bool { return false }

type Origin string
type RestrictedAction string

const (
	OriginDelegated          Origin           = "delegated"
	OriginMessage            Origin           = "message"
	OriginHandoff            Origin           = "handoff"
	RestrictedCommit         RestrictedAction = "commit"
	RestrictedPush           RestrictedAction = "push"
	RestrictedDeploy         RestrictedAction = "deploy"
	RestrictedCredential     RestrictedAction = "credential"
	RestrictedProduction     RestrictedAction = "production"
	RestrictedDeletion       RestrictedAction = "deletion"
	RestrictedPublication    RestrictedAction = "publication"
	RestrictedDestructiveGit RestrictedAction = "destructive_git"
)

func AllRestrictedActions() []RestrictedAction {
	return []RestrictedAction{RestrictedCommit, RestrictedPush, RestrictedDeploy, RestrictedCredential, RestrictedProduction, RestrictedDeletion, RestrictedPublication, RestrictedDestructiveGit}
}

type ActionDecision struct {
	Allowed bool
	Reason  string
}

// RestrictedActionDecision is deliberately content-blind: untrusted text cannot confer authority.
func RestrictedActionDecision(origin Origin, action RestrictedAction, untrustedText string) ActionDecision {
	_ = untrustedText
	if origin == OriginDelegated || origin == OriginMessage || origin == OriginHandoff {
		return ActionDecision{Allowed: false, Reason: "restricted action requires separate human authority"}
	}
	return ActionDecision{Allowed: false, Reason: "restricted action requires separate human authority"}
}

func stableID(value string) bool                       { return id(value) && domain.IsSecretFreeStableMetadata(value) }
func id(value string) bool                             { return strings.TrimSpace(value) != "" }
func utc(value time.Time) bool                         { return !value.IsZero() && value.Location() == time.UTC }
func boundedText(value string, limit int) bool         { return id(value) && len(value) <= limit }
func optionalBoundedText(value string, limit int) bool { return value == "" || len(value) <= limit }
func boundedStrings(values []string, maxItems, maxLen int) bool {
	if len(values) > maxItems {
		return false
	}
	for _, value := range values {
		if !boundedText(value, maxLen) {
			return false
		}
	}
	return true
}
func validPhase(value Phase) bool {
	return value == PhaseInspect || value == PhasePlan || value == PhaseImplement || value == PhaseTest || value == PhaseReview || value == PhaseWait
}
func validDependencyKind(value DependencyKind) bool {
	return value == DependencyHard || value == DependencySoft || value == DependencyInformational
}
func validCriterion(value UnblockCriterion) bool {
	return value == UnblockWorkComplete || value == UnblockVerifiedDone
}
func validMessageType(value MessageType) bool {
	switch value {
	case MessageNotice, MessageQuestion, MessageDependency, MessageConflict, MessageHandoff, MessageDone, MessageBlocked, MessageCancel, MessageACK:
		return true
	default:
		return false
	}
}
