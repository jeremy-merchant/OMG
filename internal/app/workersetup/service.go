// Package workersetup creates or validates one worker execution unit atomically.
package workersetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	lineageapp "github.com/jeremy-merchant/oh-my-group/internal/app/lineage"
	reservationapp "github.com/jeremy-merchant/oh-my-group/internal/app/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

const operation = "worker.setup"

type Service struct {
	store ports.Store
	now   func() time.Time
}

func New(store ports.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}

type Request struct {
	ProjectID           domain.ProjectID
	ProjectRoot         string
	HumanID             core.ID
	ControllerSessionID core.ID
	SessionID           core.ID
	Runtime             string
	Role                string
	TaskID              core.ID
	TaskTitle           string
	ParentTaskID        core.ID
	CompletionPolicy    core.TaskCompletionPolicy
	ParentRequirement   core.TaskParentRequirement
	RunID               core.ID
	Reservations        []reservationapp.BatchCreateItem
}

type Result struct {
	ProjectID           string   `json:"project_id"`
	HumanID             string   `json:"human_id"`
	ControllerSessionID string   `json:"controller_session_id"`
	SessionID           string   `json:"session_id"`
	TaskID              string   `json:"task_id"`
	RunID               string   `json:"run_id"`
	SessionCreated      bool     `json:"session_created"`
	TaskCreated         bool     `json:"task_created"`
	TaskClaimed         bool     `json:"task_claimed"`
	RunCreated          bool     `json:"run_created"`
	TaskState           string   `json:"task_state"`
	RunState            string   `json:"run_state"`
	ReservationIDs      []string `json:"reservation_ids"`
	Warnings            []string `json:"warnings"`
}

type receiptData struct {
	Result
	PayloadSHA256 string `json:"_payload_sha256,omitempty"`
}

type fingerprintReservation struct {
	ID              string `json:"id"`
	PatternKind     string `json:"pattern_kind"`
	Pattern         string `json:"pattern"`
	CaseSensitivity string `json:"case_sensitivity"`
	Mode            string `json:"mode"`
	Intent          string `json:"intent"`
	TTLNanoseconds  int64  `json:"ttl_nanoseconds"`
}

type fingerprintInput struct {
	ProjectID           string                   `json:"project_id"`
	ProjectRoot         string                   `json:"project_root"`
	HumanID             string                   `json:"human_id"`
	ControllerSessionID string                   `json:"controller_session_id"`
	SessionID           string                   `json:"session_id"`
	Runtime             string                   `json:"runtime"`
	Role                string                   `json:"role"`
	TaskID              string                   `json:"task_id"`
	TaskTitle           string                   `json:"task_title"`
	ParentTaskID        string                   `json:"parent_task_id"`
	CompletionPolicy    string                   `json:"completion_policy"`
	ParentRequirement   string                   `json:"parent_requirement"`
	RunID               string                   `json:"run_id"`
	Reservations        []fingerprintReservation `json:"reservations"`
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid worker setup request", false)
}

func missing(message string) error {
	return domain.NewError(domain.CodeNotFound, message, false)
}

func conflict(message string) error {
	return domain.NewError(domain.CodeConflict, message, false)
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var domainErr domain.DomainError
	if errors.As(err, &domainErr) {
		return domainErr
	}
	return domain.NewError(domain.CodeUnavailable, "worker setup store unavailable", true)
}

func (s *Service) Setup(ctx context.Context, key domain.IdempotencyKey, request Request) (Result, error) {
	request = normalize(request)
	if !validRequest(key, request) || safety.RejectPrefixed(key, request) != nil {
		return Result{}, invalid()
	}
	now := s.now().UTC()
	session := core.AgentSession{
		ID: request.SessionID, ProjectID: core.ID(request.ProjectID), HumanID: request.HumanID,
		Kind: core.HumanDirect, Runtime: request.Runtime, Role: request.Role, Source: core.SourceHuman,
		SourceRef: "controller:" + string(request.ControllerSessionID), RootID: request.SessionID,
		TaskID: request.TaskID, WorktreeRef: request.ProjectRoot, StartedAt: now,
		NativeAccessState: core.NativeAccessUnsupported,
	}
	task := core.Task{
		ID: request.TaskID, ProjectID: core.ID(request.ProjectID), DisplayNumber: 1,
		Title: request.TaskTitle, State: core.TaskReady, CreatedBySessionID: request.ControllerSessionID,
		ParentTaskID: request.ParentTaskID, CompletionPolicy: request.CompletionPolicy,
		ParentRequirement: request.ParentRequirement, CreatedAt: now, UpdatedAt: now,
	}
	run := core.TaskRun{ID: request.RunID, TaskID: request.TaskID, SessionID: request.SessionID, State: core.RunRunning, StartedAt: now}
	if session.Validate() != nil || task.Validate() != nil || run.Validate() != nil {
		return Result{}, invalid()
	}

	var prepared reservationapp.PreparedBatch
	if len(request.Reservations) != 0 {
		var err error
		prepared, err = reservationapp.PrepareBatch(now, reservationapp.BatchCreateRequest{
			ProjectID: request.ProjectID,
			Owner: res.Owner{
				HumanID: string(request.HumanID), SessionID: string(request.SessionID),
				TaskID: string(request.TaskID), RunID: string(request.RunID),
			},
			Items: request.Reservations,
		})
		if err != nil {
			return Result{}, mapErr(err)
		}
	}

	payloadSHA256 := requestFingerprint(request)
	var public Result
	_, recorded, err := s.store.Write(ctx, key, operation, func(repositories ports.Repositories) (domain.Result, error) {
		value, warnings, setupErr := setupInTransaction(ctx, repositories, now, request, session, task, run, prepared)
		if setupErr != nil {
			return domain.Result{}, setupErr
		}
		value.Warnings = append([]string(nil), warnings...)
		return domain.Result{
			ID: domain.ResultID(value.RunID), Outcome: domain.OutcomeOK,
			Data: receiptData{Result: value, PayloadSHA256: payloadSHA256}, Warnings: warnings,
		}, nil
	})
	if err != nil {
		return Result{}, mapErr(err)
	}
	encoded, marshalErr := json.Marshal(recorded.Data)
	var persisted receiptData
	if marshalErr != nil || json.Unmarshal(encoded, &persisted) != nil {
		return Result{}, domain.NewError(domain.CodeInternal, "worker setup receipt is invalid", false)
	}
	if persisted.PayloadSHA256 != "" && persisted.PayloadSHA256 != payloadSHA256 {
		return Result{}, conflict("idempotency key was reused with a different worker.setup payload")
	}
	public = persisted.Result
	if public.ReservationIDs == nil {
		public.ReservationIDs = []string{}
	}
	if public.Warnings == nil {
		public.Warnings = []string{}
	}
	return public, nil
}

func normalize(request Request) Request {
	request.CompletionPolicy = core.EffectiveTaskCompletionPolicy(request.CompletionPolicy)
	if request.ParentTaskID != "" && request.ParentRequirement == "" {
		request.ParentRequirement = core.TaskParentRequired
	} else {
		request.ParentRequirement = core.EffectiveTaskParentRequirement(request.ParentRequirement)
	}
	if request.Reservations == nil {
		request.Reservations = []reservationapp.BatchCreateItem{}
	}
	return request
}

func validRequest(key domain.IdempotencyKey, request Request) bool {
	if !domain.IsSecretFreeStableMetadata(string(key)) || request.ProjectID == "" || strings.TrimSpace(request.ProjectRoot) == "" ||
		request.HumanID == "" || request.ControllerSessionID == "" || request.SessionID == "" || request.ControllerSessionID == request.SessionID ||
		request.TaskID == "" || request.RunID == "" || strings.TrimSpace(request.Runtime) == "" || strings.TrimSpace(request.Role) == "" || strings.TrimSpace(request.TaskTitle) == "" {
		return false
	}
	for _, id := range []string{string(request.HumanID), string(request.ControllerSessionID), string(request.SessionID), string(request.TaskID), string(request.ParentTaskID), string(request.RunID)} {
		if id != "" && !domain.IsSecretFreeStableMetadata(id) {
			return false
		}
	}
	return true
}

func setupInTransaction(ctx context.Context, repositories ports.Repositories, now time.Time, request Request, desiredSession core.AgentSession, desiredTask core.Task, desiredRun core.TaskRun, prepared reservationapp.PreparedBatch) (Result, []string, error) {
	coordination := repositories.Coordination()
	controller, found, err := coordination.GetSession(ctx, request.ControllerSessionID)
	if err != nil {
		return Result{}, nil, err
	}
	if !found || controller.ProjectID != core.ID(request.ProjectID) {
		return Result{}, nil, missing("worker setup controller session was not found in project")
	}
	if sessionEnded(controller) || controller.Liveness == core.Stale {
		return Result{}, nil, conflict("worker setup controller session is not live")
	}
	if _, found, err := coordination.GetHuman(ctx, request.HumanID); err != nil {
		return Result{}, nil, err
	} else if !found {
		return Result{}, nil, missing("worker setup human was not found in project")
	}

	result := Result{
		ProjectID: string(request.ProjectID), HumanID: string(request.HumanID),
		ControllerSessionID: string(request.ControllerSessionID), SessionID: string(request.SessionID),
		TaskID: string(request.TaskID), RunID: string(request.RunID),
		ReservationIDs: []string{}, Warnings: []string{},
	}

	worker, found, err := coordination.GetSession(ctx, request.SessionID)
	if err != nil {
		return Result{}, nil, err
	}
	if !found {
		if err := lineageapp.CreateSessionInTransaction(ctx, repositories, desiredSession); err != nil {
			return Result{}, nil, err
		}
		worker = desiredSession
		result.SessionCreated = true
	} else if err := validateExistingSession(worker, desiredSession, request.ControllerSessionID); err != nil {
		return Result{}, nil, err
	}

	task, found, err := coordination.GetTask(ctx, request.TaskID)
	if err != nil {
		return Result{}, nil, err
	}
	if !found {
		task, err = lineageapp.CreateTaskInTransaction(ctx, repositories, desiredTask)
		if err != nil {
			return Result{}, nil, err
		}
		result.TaskCreated = true
	} else if err := validateExistingTask(task, desiredTask); err != nil {
		return Result{}, nil, err
	}

	switch task.State {
	case core.TaskReady:
		var won bool
		task, won, err = lineageapp.ClaimTaskInTransaction(ctx, repositories, now, task.ID, worker.ID)
		if err != nil {
			return Result{}, nil, err
		}
		if !won {
			return Result{}, nil, conflict("worker setup task claim was lost")
		}
		result.TaskClaimed = true
	case core.TaskClaimed, core.TaskInProgress, core.TaskWaiting, core.TaskBlocked, core.TaskRework:
		if task.ClaimedBySessionID != worker.ID {
			return Result{}, nil, conflict("worker setup task is owned by another session")
		}
	default:
		return Result{}, nil, conflict("worker setup task is not active")
	}

	run, found, err := coordination.GetRun(ctx, request.RunID)
	if err != nil {
		return Result{}, nil, err
	}
	if !found {
		if err := lineageapp.CreateRunInTransaction(ctx, repositories, desiredRun); err != nil {
			return Result{}, nil, err
		}
		run = desiredRun
		result.RunCreated = true
	} else if err := validateExistingRun(run, desiredRun); err != nil {
		return Result{}, nil, err
	}

	warnings := []string{}
	if len(request.Reservations) != 0 {
		data, reservationWarnings, err := reservationapp.EnsurePreparedBatchInTransaction(ctx, repositories, now, true, prepared)
		if err != nil {
			return Result{}, nil, err
		}
		result.ReservationIDs = append([]string(nil), data.ReservationIDs...)
		warnings = append(warnings, reservationWarnings...)
	}
	result.TaskState = string(task.State)
	result.RunState = string(run.State)
	return result, warnings, nil
}

func validateExistingSession(existing, desired core.AgentSession, controllerID core.ID) error {
	if existing.ProjectID != desired.ProjectID || existing.HumanID != desired.HumanID || existing.TaskID != desired.TaskID || existing.Runtime != desired.Runtime || existing.Role != desired.Role || sessionEnded(existing) || existing.Liveness == core.Stale {
		return conflict("worker setup session does not match requested ownership")
	}
	if existing.ParentID != "" {
		if existing.ParentID != controllerID {
			return conflict("worker setup session belongs to another controller")
		}
		return nil
	}
	if existing.Kind != core.HumanDirect || existing.Source != core.SourceHuman || existing.SourceRef != "controller:"+string(controllerID) || existing.RootID != existing.ID {
		return conflict("worker setup session has incompatible provenance")
	}
	return nil
}

func validateExistingTask(existing, desired core.Task) error {
	if existing.ProjectID != desired.ProjectID || existing.Title != desired.Title || existing.CreatedBySessionID != desired.CreatedBySessionID || existing.ParentTaskID != desired.ParentTaskID ||
		core.EffectiveTaskCompletionPolicy(existing.CompletionPolicy) != desired.CompletionPolicy || core.EffectiveTaskParentRequirement(existing.ParentRequirement) != desired.ParentRequirement {
		return conflict("worker setup task does not match requested intent")
	}
	return nil
}

func validateExistingRun(existing, desired core.TaskRun) error {
	if existing.TaskID != desired.TaskID || existing.SessionID != desired.SessionID || !activeRun(existing.State) {
		return conflict("worker setup run does not match active task ownership")
	}
	return nil
}

func sessionEnded(session core.AgentSession) bool {
	return session.EndedAt != nil || session.InterruptedAt != nil || session.Liveness == core.Interrupted
}

func activeRun(state core.RunState) bool {
	return state == core.RunRunning || state == core.RunWaiting || state == core.RunBlocked || state == core.RunRework
}

func requestFingerprint(request Request) string {
	reservations := make([]fingerprintReservation, len(request.Reservations))
	for i, item := range request.Reservations {
		reservations[i] = fingerprintReservation{
			ID: item.ID, PatternKind: string(item.Pattern.Kind), Pattern: item.Pattern.Value,
			CaseSensitivity: string(item.Pattern.CaseSensitivity), Mode: string(item.Mode),
			Intent: item.Intent, TTLNanoseconds: int64(item.TTL),
		}
	}
	input := fingerprintInput{
		ProjectID: string(request.ProjectID), ProjectRoot: request.ProjectRoot, HumanID: string(request.HumanID),
		ControllerSessionID: string(request.ControllerSessionID), SessionID: string(request.SessionID),
		Runtime: request.Runtime, Role: request.Role, TaskID: string(request.TaskID), TaskTitle: request.TaskTitle,
		ParentTaskID: string(request.ParentTaskID), CompletionPolicy: string(request.CompletionPolicy),
		ParentRequirement: string(request.ParentRequirement), RunID: string(request.RunID), Reservations: reservations,
	}
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
