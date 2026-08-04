package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	handoffapp "github.com/jeremy-merchant/oh-my-group/internal/app/handoff"
	lineageapp "github.com/jeremy-merchant/oh-my-group/internal/app/lineage"
	messageapp "github.com/jeremy-merchant/oh-my-group/internal/app/message"
	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	lineagecore "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

const RequestVersion = 1

// Request is a versioned, transport-neutral application request.
type Request struct {
	Version        int             `json:"version"`
	Command        string          `json:"command"`
	Project        string          `json:"project,omitempty"`
	Workspace      string          `json:"workspace,omitempty"`
	Store          string          `json:"store,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
}

// ReceiptGet identifies one public receipt within the selected project.
type ReceiptGet struct {
	ID string `json:"id"`
}

// ReceiptView contains only receipt-safe public metadata.
type ReceiptView struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Operation      string    `json:"operation"`
	Outcome        string    `json:"outcome"`
	CreatedAt      time.Time `json:"created_at"`
}

func safeReceipt(receipt domain.Receipt) ReceiptView {
	return ReceiptView{
		ID:             string(receipt.ID),
		IdempotencyKey: string(receipt.IdempotencyKey),
		Operation:      receipt.Operation,
		Outcome:        string(receipt.Outcome),
		CreatedAt:      receipt.CreatedAt,
	}
}

// Outcome is a transport-neutral result. Each transport owns its envelope.
type Outcome struct {
	Data   any
	Error  domain.DomainError
	Detail *ErrorDetail
}

// Dispatcher is the application boundary shared by all transports.
type Dispatcher interface {
	Dispatch(context.Context, Request) Outcome
}

type DispatcherOptions struct {
	StrictReservationConflicts bool
}

type ServiceDispatcher struct {
	service                    *foundation.Service
	scanner                    ports.Scanner
	verifier                   ports.GitVerifier
	pathInspector              ports.PathInspector
	strictReservationConflicts bool
}

func NewDispatcher(service *foundation.Service) *ServiceDispatcher {
	return NewDispatcherWithOptions(service, DispatcherOptions{})
}

func NewDispatcherWithOptions(service *foundation.Service, options DispatcherOptions) *ServiceDispatcher {
	return NewDispatcherWithGitToolsAndOptions(service, nil, nil, nil, options)
}

func NewDispatcherWithGitScanner(service *foundation.Service, scanner ports.Scanner, pathInspector ports.PathInspector) *ServiceDispatcher {
	return NewDispatcherWithGitTools(service, scanner, nil, pathInspector)
}

func NewDispatcherWithGitTools(service *foundation.Service, scanner ports.Scanner, verifier ports.GitVerifier, pathInspector ports.PathInspector) *ServiceDispatcher {
	return NewDispatcherWithGitToolsAndOptions(service, scanner, verifier, pathInspector, DispatcherOptions{})
}

func NewDispatcherWithGitToolsAndOptions(service *foundation.Service, scanner ports.Scanner, verifier ports.GitVerifier, pathInspector ports.PathInspector, options DispatcherOptions) *ServiceDispatcher {
	return &ServiceDispatcher{
		service: service, scanner: scanner, verifier: verifier, pathInspector: pathInspector,
		strictReservationConflicts: options.StrictReservationConflicts,
	}
}

func (d *ServiceDispatcher) Dispatch(ctx context.Context, request Request) (result Outcome) {
	defer func() { d.enrichErrorOutcome(ctx, request, &result) }()
	if ctx == nil || d == nil || d.service == nil || request.Version != RequestVersion || len(request.Payload) == 0 || len(request.Payload) > 1<<20 {
		return Outcome{Error: invalidRequest()}
	}
	if payloadErr := validatePublicPayload(request.Command, request.Payload); payloadErr.Code != "" {
		return Outcome{Error: payloadErr}
	}
	selection := foundation.Selection{Project: request.Project, Workspace: request.Workspace, Store: request.Store}
	switch request.Command {
	case "preflight.query":
		return d.dispatchPreflight(ctx, request, selection)
	case "board.query":
		var payload query.BoardRequest
		if request.IdempotencyKey != "" || !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}
		}
		var model query.ViewModel
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			actor := domain.NewActorContext(domain.ScopeID(resolved.Project), resolved.Project, resolved.Workspace, domain.InvocationCLI, []domain.Capability{domain.CapabilityRead})
			var queryErr error
			model, queryErr = query.NewWithNativeResolver(store, d.service.NativeSessionResolver()).Query(ctx, actor, payload)
			return queryErr
		})
		return outcome(model, err)
	case "receipt.get":
		var payload ReceiptGet
		if request.IdempotencyKey != "" || !decodePayload(request.Payload, &payload) || payload.ID == "" {
			return Outcome{Error: invalidRequest()}
		}
		var receipt ReceiptView
		var found bool
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			return store.Read(ctx, func(repositories ports.Repositories) error {
				value, ok, err := repositories.Receipts().GetReceipt(ctx, domain.ReceiptID(payload.ID))
				if err != nil {
					return err
				}
				found = ok
				if ok {
					receipt = safeReceipt(value)
				}
				return nil
			})
		})
		if err.Code != "" {
			return Outcome{Error: err}
		}
		if !found {
			return Outcome{Error: domain.NewError(domain.CodeNotFound, "receipt not found", false)}
		}
		return Outcome{Data: receipt}
	case "receipt.list":
		if request.IdempotencyKey != "" || !decodePayload(request.Payload, &struct{}{}) {
			return Outcome{Error: invalidRequest()}
		}
		var receipts []ReceiptView
		err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(_ ports.ResolvedStore, store ports.Store) error {
			return store.Read(ctx, func(repositories ports.Repositories) error {
				values, err := repositories.Receipts().ListReceipts(ctx)
				if err != nil {
					return err
				}
				receipts = make([]ReceiptView, len(values))
				for i, value := range values {
					receipts[i] = safeReceipt(value)
				}
				return nil
			})
		})
		return outcome(receipts, err)
	case "task.create":
		var payload TaskCreate
		if request.IdempotencyKey == "" || !decodePayload(request.Payload, &payload) || payload.CreatedBySessionID == "" {
			return Outcome{Error: invalidRequest()}
		}
		var result TaskResult
		err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			task, err := lineageapp.New(store, nil).CreateTask(ctx, domain.IdempotencyKey(request.IdempotencyKey), lineagecore.Task{ProjectID: lineagecore.ID(resolved.Project), Title: payload.Title, CreatedBySessionID: lineagecore.ID(payload.CreatedBySessionID), ParentTaskID: lineagecore.ID(payload.ParentTaskID), CompletionPolicy: lineagecore.TaskCompletionPolicy(payload.CompletionPolicy), ParentRequirement: lineagecore.TaskParentRequirement(payload.ParentRequirement)})
			if err == nil {
				result = TaskResult{ID: string(task.ID), DisplayNumber: task.DisplayNumber, State: string(task.State), CompletionPolicy: string(task.CompletionPolicy), ParentRequirement: string(task.ParentRequirement)}
			}
			return err
		})
		return outcome(result, err)
	case "message.send":
		var payload MessageSend
		if request.IdempotencyKey == "" || !decodePayload(request.Payload, &payload) {
			return Outcome{Error: invalidRequest()}
		}
		recipients := make([]coord.RecipientTarget, len(payload.Recipients))
		for i, r := range payload.Recipients {
			recipients[i] = coord.RecipientTarget{SessionID: r.SessionID, HumanID: r.HumanID, TaskID: r.TaskID, Role: r.Role}
		}
		var result MessageResult
		err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			message, err := messageapp.New(store, nil).Send(ctx, domain.IdempotencyKey(request.IdempotencyKey), string(resolved.Project), coord.MailMessage{ID: payload.ID, Type: coord.MessageType(payload.Type), ThreadID: payload.ThreadID, SenderSessionID: payload.SenderSessionID, Recipients: recipients, Subject: payload.Subject, Body: payload.Body, RelatedTaskID: payload.RelatedTaskID})
			if err == nil {
				result = MessageResult{ID: message.ID, ThreadID: message.ThreadID, Type: string(message.Type)}
			}
			return err
		})
		return outcome(result, err)
	case "handoff.create":
		var payload HandoffCreate
		if request.IdempotencyKey == "" || !decodePayload(request.Payload, &payload) || payload.FinalOutputPolicy == string(coord.FinalOutputFull) {
			return Outcome{Error: invalidRequest()}
		}
		evidence := make([]coord.SafeEvidence, len(payload.VerificationEvidence))
		for i, v := range payload.VerificationEvidence {
			evidence[i] = coord.SafeEvidence{Summary: v.Summary, Hash: v.Hash}
		}
		var result HandoffResult
		err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
			handoff, err := handoffapp.New(store, nil).Submit(ctx, domain.IdempotencyKey(request.IdempotencyKey), string(resolved.Project), coord.Handoff{ID: payload.ID, TaskID: payload.TaskID, RunID: payload.RunID, SourceSessionID: payload.SourceSessionID, TargetSessionID: payload.TargetSessionID, TargetTaskID: payload.TargetTaskID, Summary: payload.Summary, FinalOutput: coord.SensitiveText{Text: payload.FinalOutputText, Hash: payload.FinalOutputHash, Policy: coord.FinalOutputPolicy(payload.FinalOutputPolicy)}, ChangedFiles: payload.ChangedFiles, Commits: payload.Commits, SourceCommit: payload.SourceCommit, SourceTree: payload.SourceTree, VerificationEvidence: evidence, RemainingRisks: payload.RemainingRisks, SuggestedActions: payload.SuggestedActions})
			if err == nil {
				result = HandoffResult{ID: handoff.ID, TaskID: handoff.TaskID, Status: string(handoff.Status)}
			}
			return err
		})
		return outcome(result, err)
	case "import.record":
		if outcome, handled := d.dispatchImport(ctx, request, selection); handled {
			return outcome
		}
		return Outcome{Error: invalidRequest()}
	default:
		if outcome, handled := d.dispatchWorker(ctx, request, selection); handled {
			return outcome
		}
		if outcome, handled := d.dispatchCandidate(ctx, request, selection); handled {
			return outcome
		}
		if outcome, handled := d.dispatchLineage(ctx, request, selection); handled {
			return outcome
		}
		if outcome, handled := d.dispatchRecovery(ctx, request, selection); handled {
			return outcome
		}
		if outcome, handled := d.dispatchCoordination(ctx, request, selection); handled {
			return outcome
		}
		return Outcome{Error: invalidRequest()}
	}
}

func outcome(data any, err error) Outcome {
	if err == nil {
		return Outcome{Data: data}
	}
	if de, ok := err.(domain.DomainError); ok {
		if de.Code == "" {
			return Outcome{Data: data}
		}
		return Outcome{Error: de}
	}
	return Outcome{Error: domain.NewError(domain.CodeInternal, "command failed", false)}
}
func invalidRequest() domain.DomainError {
	return domain.NewError(domain.CodeInvalidArgument, "application request is invalid", false)
}
func decodePayload(data []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil && decoder.Decode(&struct{}{}) == io.EOF
}

type TaskCreate struct {
	Title              string `json:"title"`
	CreatedBySessionID string `json:"created_by_session_id"`
	ParentTaskID       string `json:"parent_task_id,omitempty"`
	CompletionPolicy   string `json:"completion_policy,omitempty"`
	ParentRequirement  string `json:"parent_requirement,omitempty"`
}
type Recipient struct {
	SessionID string `json:"session_id,omitempty"`
	HumanID   string `json:"human_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Role      string `json:"role,omitempty"`
}
type MessageSend struct {
	ID              string      `json:"id"`
	Type            string      `json:"type"`
	ThreadID        string      `json:"thread_id"`
	SenderSessionID string      `json:"sender_session_id"`
	Recipients      []Recipient `json:"recipients"`
	Subject         string      `json:"subject,omitempty"`
	Body            string      `json:"body"`
	RelatedTaskID   string      `json:"related_task_id,omitempty"`
}
type Evidence struct {
	Summary string `json:"summary"`
	Hash    string `json:"hash"`
}
type HandoffCreate struct {
	ID                   string     `json:"id"`
	TaskID               string     `json:"task_id"`
	RunID                string     `json:"run_id"`
	SourceSessionID      string     `json:"source_session_id"`
	TargetSessionID      string     `json:"target_session_id,omitempty"`
	TargetTaskID         string     `json:"target_task_id,omitempty"`
	Summary              string     `json:"summary"`
	FinalOutputPolicy    string     `json:"final_output_policy"`
	FinalOutputText      string     `json:"final_output_text,omitempty"`
	FinalOutputHash      string     `json:"final_output_hash,omitempty"`
	ChangedFiles         []string   `json:"changed_files,omitempty"`
	Commits              []string   `json:"commits,omitempty"`
	SourceCommit         string     `json:"source_commit,omitempty"`
	SourceTree           string     `json:"source_tree,omitempty"`
	VerificationEvidence []Evidence `json:"verification_evidence,omitempty"`
	RemainingRisks       []string   `json:"remaining_risks,omitempty"`
	SuggestedActions     []string   `json:"suggested_actions,omitempty"`
}
type TaskResult struct {
	ID                string `json:"id"`
	DisplayNumber     int64  `json:"display_number"`
	State             string `json:"state"`
	CompletionPolicy  string `json:"completion_policy"`
	ParentRequirement string `json:"parent_requirement"`
}
type MessageResult struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
	Type     string `json:"type"`
}
type HandoffResult struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}
