package app

import (
	"context"
	"encoding/json"
	"time"

	dependencyapp "github.com/jeremy-merchant/oh-my-group/internal/app/dependency"
	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	lineageapp "github.com/jeremy-merchant/oh-my-group/internal/app/lineage"
	queryapp "github.com/jeremy-merchant/oh-my-group/internal/app/query"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	lineage "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

type lineageHumanPayload struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"display_name"`
	Confidence  string `json:"confidence"`
	Supersedes  string `json:"supersedes_id,omitempty"`
}

type lineageHumanGetPayload struct {
	ID string `json:"id"`
}

type lineageSessionPayload struct {
	ID        string `json:"id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	HumanID   string `json:"human_id,omitempty"`
	Runtime   string `json:"runtime"`
	Role      string `json:"role"`
	SourceRef string `json:"source_ref"`
	// Projection-only fields are tolerated as inert compatibility hints. Canonical
	// provenance always comes from lineage and the linked human record.
	IgnoredInstructionSource    string     `json:"instruction_source,omitempty"`
	IgnoredProvenanceConfidence string     `json:"provenance_confidence,omitempty"`
	ParentSessionID             string     `json:"parent_session_id,omitempty"`
	ContinuationOfID            string     `json:"continuation_of_id,omitempty"`
	TaskID                      string     `json:"task_id,omitempty"`
	WorktreeRef                 string     `json:"worktree_ref,omitempty"`
	NativeAccessState           string     `json:"native_access_state"`
	RuntimeHome                 string     `json:"runtime_home,omitempty"`
	NativeSessionID             string     `json:"native_session_id,omitempty"`
	NativeSessionRef            string     `json:"native_session_ref,omitempty"`
	NativeSessionStartedAt      *time.Time `json:"native_session_started_at,omitempty"`
	NativeParentSessionID       string     `json:"native_parent_session_id,omitempty"`
}

type lineageDelegateIssuePayload struct {
	TaskID          string `json:"task_id,omitempty"`
	ParentSessionID string `json:"parent_session_id"`
	TTLSeconds      int64  `json:"ttl_seconds"`
}

type lineageDelegateRegisterPayload struct {
	RawToken        string                `json:"raw_token"`
	TaskID          string                `json:"task_id,omitempty"`
	ParentSessionID string                `json:"parent_session_id"`
	Session         lineageSessionPayload `json:"session"`
}

type lineageDelegateRevokePayload struct {
	TokenID string `json:"token_id"`
}
type lineageCheckpointPayload struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Liveness  string `json:"liveness"`
	Detail    string `json:"detail,omitempty"`
}
type lineageSessionArchivePayload struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id"`
	ActorSessionID string `json:"actor_session_id"`
	Reason         string `json:"reason"`
}
type lineageTaskGetPayload struct {
	TaskID string `json:"task_id"`
}
type lineageTaskClaimPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
}
type lineageTaskTransitionPayload struct {
	TaskID         string `json:"task_id"`
	State          string `json:"state"`
	Evidence       string `json:"evidence,omitempty"`
	ActorSessionID string `json:"actor_session_id,omitempty"`
}
type lineageRunCreatePayload struct {
	ID        string `json:"id,omitempty"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
}
type lineageRunTransitionPayload struct {
	RunID    string `json:"run_id"`
	State    string `json:"state"`
	Evidence string `json:"evidence,omitempty"`
}
type lineageFinishLitePayload struct {
	TaskID         string `json:"task_id"`
	RunID          string `json:"run_id"`
	SessionID      string `json:"session_id"`
	ActorSessionID string `json:"actor_session_id"`
	ArchiveEventID string `json:"archive_event_id"`
	Evidence       string `json:"evidence"`
}

type lineageHumanResult struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Confidence  string `json:"confidence"`
	Supersedes  string `json:"supersedes_id,omitempty"`
}
type lineageSessionResult struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	HumanID           string    `json:"human_id,omitempty"`
	Kind              string    `json:"lineage_kind"`
	Runtime           string    `json:"runtime"`
	Role              string    `json:"role"`
	Source            string    `json:"instruction_source"`
	ParentID          string    `json:"parent_session_id,omitempty"`
	RootID            string    `json:"root_session_id"`
	ContinuationOfID  string    `json:"continuation_of_id,omitempty"`
	TaskID            string    `json:"task_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	NativeAccessState string    `json:"native_access_state"`
}
type lineageTaskResult struct {
	ID                 string `json:"id"`
	DisplayNumber      int64  `json:"display_number"`
	Title              string `json:"title"`
	State              string `json:"state"`
	CreatedBySessionID string `json:"created_by_session_id"`
	ClaimedBySessionID string `json:"claimed_by_session_id,omitempty"`
	ParentTaskID       string `json:"parent_task_id,omitempty"`
	CompletionPolicy   string `json:"completion_policy"`
	ParentRequirement  string `json:"parent_requirement"`
}
type lineageRunResult struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}
type lineageFinishLiteResult struct {
	TaskID               string `json:"task_id"`
	TaskState            string `json:"task_state"`
	RunID                string `json:"run_id"`
	RunState             string `json:"run_state"`
	SessionID            string `json:"session_id"`
	ReservationsReleased int    `json:"reservations_released"`
	SessionArchived      bool   `json:"session_archived"`
}
type lineageDelegateIssueResult struct {
	TokenID   string    `json:"token_id"`
	ExpiresAt time.Time `json:"expires_at"`
	RawToken  string    `json:"raw_token"`
}
type lineageDelegateRevokeResult struct {
	TokenID string `json:"token_id"`
	Revoked bool   `json:"revoked"`
}
type lineageCheckpointResult struct {
	ID               string                     `json:"id"`
	SessionID        string                     `json:"session_id"`
	Liveness         string                     `json:"liveness"`
	RefreshAvailable bool                       `json:"refresh_available"`
	Warnings         []string                   `json:"warnings"`
	Identity         *queryapp.IdentityView     `json:"identity"`
	Inbox            []queryapp.InboxItemView   `json:"inbox"`
	Dependencies     []queryapp.DependencyView  `json:"dependencies"`
	Reservations     []queryapp.ReservationView `json:"reservations"`
}

func unavailableCheckpointRefresh(checkpoint lineageapp.CheckpointResult) lineageCheckpointResult {
	return lineageCheckpointResult{
		ID:               string(checkpoint.ID),
		SessionID:        string(checkpoint.SessionID),
		Liveness:         string(checkpoint.Liveness),
		RefreshAvailable: false,
		Warnings:         []string{"refresh_unavailable"},
		Inbox:            []queryapp.InboxItemView{},
		Dependencies:     []queryapp.DependencyView{},
		Reservations:     []queryapp.ReservationView{},
	}
}

// dispatchLineage owns the command boundary for lineage operations. It never exposes
// runtime-home or native-session locators in a response.
func (d *ServiceDispatcher) dispatchLineage(ctx context.Context, request Request, selection foundation.Selection) (Outcome, bool) {
	query := request.Command == "human.get" || request.Command == "task.get"
	mutations := map[string]bool{
		"human.create": true, "session.create": true, "session.resume": true, "session.adopt": true, "session.import": true, "session.archive": true,
		"delegate.issue": true, "delegate.register": true, "delegate.revoke": true, "checkpoint.record": true,
		"task.claim": true, "task.transition": true, "task.run-create": true, "task.run-transition": true, "task.finish-lite": true,
	}
	if !query && !mutations[request.Command] {
		return Outcome{}, false
	}
	if (query && request.IdempotencyKey != "") || (!query && request.IdempotencyKey == "") {
		return Outcome{Error: invalidRequest()}, true
	}

	withStore := d.service.WithCurrentStore
	if query {
		withStore = d.service.WithReadOnlyCurrentStore
	}

	var result any
	err := withStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		service := lineageapp.New(store, nil)
		project := lineage.ID(resolved.Project)
		key := domain.IdempotencyKey(request.IdempotencyKey)
		switch request.Command {
		case "human.create":
			var payload lineageHumanPayload
			if !decodePayload(request.Payload, &payload) {
				return invalidRequest()
			}
			human, err := service.CreateHuman(ctx, key, lineage.Human{ID: lineage.ID(payload.ID), DisplayName: payload.DisplayName, Confidence: lineage.ProvenanceConfidence(payload.Confidence), Supersedes: lineage.ID(payload.Supersedes)})
			if err == nil {
				result = lineageHumanResponse(human)
			}
			return err
		case "human.get":
			var payload lineageHumanGetPayload
			if !decodePayload(request.Payload, &payload) || payload.ID == "" {
				return invalidRequest()
			}
			human, err := service.Human(ctx, lineage.ID(payload.ID))
			if err == nil {
				result = lineageHumanResponse(human)
			}
			return err
		case "session.create", "session.resume", "session.adopt", "session.import":
			var payload lineageSessionPayload
			if !decodePayload(request.Payload, &payload) {
				return invalidRequest()
			}
			if !d.service.WorktreeBelongsToProject(ctx, payload.WorktreeRef, resolved.Project) {
				return domain.NewError(domain.CodeInvalidArgument, "worktree_ref is outside the selected project", false)
			}
			if request.Command == "session.create" && payload.SourceRef == "" {
				payload.SourceRef = "session.create"
			}
			session := lineageSessionRequest(payload, project)
			var err error
			switch request.Command {
			case "session.create":
				session, err = service.RegisterHumanDirect(ctx, key, session)
			case "session.resume":
				session, err = service.Resume(ctx, key, session)
			case "session.adopt":
				session, err = service.Adopt(ctx, key, session)
			case "session.import":
				session, err = service.Import(ctx, key, session)
			}
			if err == nil {
				result = lineageSessionResponse(session)
			}
			return err
		case "session.archive":
			var payload lineageSessionArchivePayload
			if !decodePayload(request.Payload, &payload) || payload.ID == "" || payload.SessionID == "" || payload.ActorSessionID == "" || payload.Reason == "" {
				return invalidRequest()
			}
			var archived lineageCheckpointResult
			_, recorded, err := store.Write(ctx, key, "session.archive", func(repositories ports.Repositories) (domain.Result, error) {
				target, found, err := repositories.Coordination().GetSession(ctx, lineage.ID(payload.SessionID))
				if err != nil {
					return domain.Result{}, err
				}
				if !found || target.ProjectID != project {
					return domain.Result{}, domain.NewError(domain.CodeNotFound, "session to archive was not found in project", false)
				}
				actor, found, err := repositories.Coordination().GetSession(ctx, lineage.ID(payload.ActorSessionID))
				if err != nil {
					return domain.Result{}, err
				}
				if !found || actor.ProjectID != project {
					return domain.Result{}, domain.NewError(domain.CodeNotFound, "archive actor session was not found in project", false)
				}
				runs, err := repositories.Coordination().ListRunsForSession(ctx, project, lineage.ID(payload.SessionID))
				if err != nil {
					return domain.Result{}, err
				}
				for _, run := range runs {
					if !lineage.RunHasEnded(run.State) {
						return domain.Result{}, domain.NewError(domain.CodeConflict, "session still owns a non-terminal run", false)
					}
				}
				heartbeat := lineage.Heartbeat{ID: lineage.ID(payload.ID), SessionID: lineage.ID(payload.SessionID), ObservedAt: time.Now().UTC(), Liveness: lineage.Interrupted, Detail: []byte(`{"archived":true}`)}
				if heartbeat.Validate() != nil {
					return domain.Result{}, invalidRequest()
				}
				if err := repositories.Coordination().RecordHeartbeat(ctx, heartbeat); err != nil {
					return domain.Result{}, err
				}
				value := lineageCheckpointResult{ID: payload.ID, SessionID: payload.SessionID, Liveness: "archived", RefreshAvailable: false, Warnings: []string{}, Inbox: []queryapp.InboxItemView{}, Dependencies: []queryapp.DependencyView{}, Reservations: []queryapp.ReservationView{}}
				return domain.Result{ID: domain.ResultID(payload.ID), Outcome: domain.OutcomeOK, Data: value}, nil
			})
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(recorded.Data)
			if err != nil || json.Unmarshal(encoded, &archived) != nil {
				return domain.NewError(domain.CodeInternal, "session archive receipt is invalid", false)
			}
			result = archived
			return nil
		case "delegate.issue":
			var payload lineageDelegateIssuePayload
			if !decodePayload(request.Payload, &payload) || payload.ParentSessionID == "" || payload.TTLSeconds <= 0 {
				return invalidRequest()
			}
			issue, err := service.IssueToken(ctx, key, project, lineage.ID(payload.TaskID), lineage.ID(payload.ParentSessionID), time.Duration(payload.TTLSeconds)*time.Second)
			if err == nil {
				result = lineageDelegateIssueResult{TokenID: string(issue.Token.ID), ExpiresAt: issue.Token.ExpiresAt, RawToken: issue.RawToken}
			}
			return err
		case "delegate.register":
			var payload lineageDelegateRegisterPayload
			if !decodePayload(request.Payload, &payload) || payload.RawToken == "" || payload.ParentSessionID == "" {
				return invalidRequest()
			}
			session, err := service.RegisterDelegated(ctx, key, payload.RawToken, lineageSessionRequest(payload.Session, project), project, lineage.ID(payload.TaskID), lineage.ID(payload.ParentSessionID))
			if err == nil {
				result = lineageSessionResponse(session)
			}
			return err
		case "delegate.revoke":
			var payload lineageDelegateRevokePayload
			if !decodePayload(request.Payload, &payload) || payload.TokenID == "" {
				return invalidRequest()
			}
			err := service.RevokeToken(ctx, key, lineage.ID(payload.TokenID))
			if err == nil {
				result = lineageDelegateRevokeResult{TokenID: payload.TokenID, Revoked: true}
			}
			return err
		case "checkpoint.record":
			var payload lineageCheckpointPayload
			if !decodePayload(request.Payload, &payload) || payload.ID == "" || payload.SessionID == "" {
				return invalidRequest()
			}
			checkpoint, err := service.CheckpointResult(ctx, key, lineage.Heartbeat{ID: lineage.ID(payload.ID), SessionID: lineage.ID(payload.SessionID), Liveness: lineage.Liveness(payload.Liveness), Detail: []byte(payload.Detail)})
			if err != nil {
				return err
			}
			actor := domain.NewActorContext(domain.ScopeID(resolved.Project), resolved.Project, resolved.Workspace, domain.InvocationCLI, []domain.Capability{domain.CapabilityRead})
			model, err := queryapp.NewWithNativeResolver(store, d.service.NativeSessionResolver()).Query(ctx, actor, queryapp.BoardRequest{Mode: queryapp.BoardMe, SessionID: string(checkpoint.SessionID)})
			if err != nil {
				if refreshErr, ok := err.(domain.DomainError); ok && refreshErr.Code == domain.CodeConflict {
					result = unavailableCheckpointRefresh(checkpoint)
					return nil
				}
				return err
			}
			var board queryapp.BoardSnapshot
			if err := json.Unmarshal(model.Data(), &board); err != nil {
				return err
			}
			result = lineageCheckpointResult{
				ID:               string(checkpoint.ID),
				SessionID:        string(checkpoint.SessionID),
				Liveness:         string(checkpoint.Liveness),
				RefreshAvailable: true,
				Warnings:         []string{},
				Identity:         board.Identity,
				Inbox:            board.Inbox,
				Dependencies:     board.Dependencies,
				Reservations:     board.Reservations,
			}
			return nil
		case "task.get":
			var payload lineageTaskGetPayload
			if !decodePayload(request.Payload, &payload) || payload.TaskID == "" {
				return invalidRequest()
			}
			task, err := service.Task(ctx, lineage.ID(payload.TaskID))
			if err == nil {
				result = lineageTaskResponse(task)
			}
			return err
		case "task.claim":
			var payload lineageTaskClaimPayload
			if !decodePayload(request.Payload, &payload) || payload.TaskID == "" || payload.SessionID == "" {
				return invalidRequest()
			}
			task, err := service.Claim(ctx, key, lineage.ID(payload.TaskID), lineage.ID(payload.SessionID))
			if err == nil {
				result = lineageTaskResponse(task)
			}
			return err
		case "task.transition":
			var payload lineageTaskTransitionPayload
			if !decodePayload(request.Payload, &payload) || payload.TaskID == "" {
				return invalidRequest()
			}
			state, evidence := lineage.TaskState(payload.State), []byte(payload.Evidence)
			task, err := dependencyapp.New(store, nil).TransitionAndReconcile(ctx, key, string(project), payload.TaskID, payload.ActorSessionID, state, evidence)
			if err == nil {
				result = lineageTaskResponse(task)
			}
			return err
		case "task.run-create":
			var payload lineageRunCreatePayload
			if !decodePayload(request.Payload, &payload) || payload.TaskID == "" || payload.SessionID == "" {
				return invalidRequest()
			}
			run, err := service.CreateRun(ctx, key, lineage.TaskRun{ID: lineage.ID(payload.ID), TaskID: lineage.ID(payload.TaskID), SessionID: lineage.ID(payload.SessionID)})
			if err == nil {
				result = lineageRunResponse(run)
			}
			return err
		case "task.run-transition":
			var payload lineageRunTransitionPayload
			if !decodePayload(request.Payload, &payload) || payload.RunID == "" {
				return invalidRequest()
			}
			run, err := service.TransitionRun(ctx, key, lineage.ID(payload.RunID), lineage.RunState(payload.State), []byte(payload.Evidence))
			if err == nil {
				result = lineageRunResponse(run)
			}
			return err
		case "task.finish-lite":
			var payload lineageFinishLitePayload
			if !decodePayload(request.Payload, &payload) || payload.TaskID == "" || payload.RunID == "" || payload.SessionID == "" || payload.ActorSessionID == "" || payload.ArchiveEventID == "" || payload.Evidence == "" || payload.ActorSessionID != payload.SessionID {
				return invalidRequest()
			}
			var completed lineageFinishLiteResult
			_, recorded, err := store.Write(ctx, key, "task.finish-lite", func(repositories ports.Repositories) (domain.Result, error) {
				now := time.Now().UTC()
				coordination := repositories.Coordination()
				session, found, err := coordination.GetSession(ctx, lineage.ID(payload.SessionID))
				if err != nil {
					return domain.Result{}, err
				}
				if !found || session.ProjectID != project {
					return domain.Result{}, domain.NewError(domain.CodeNotFound, "work-lite session was not found in project", false)
				}
				if session.Liveness == lineage.Stale || session.Liveness == lineage.Interrupted {
					return domain.Result{}, domain.NewError(domain.CodeConflict, "work-lite session is not live", false)
				}
				task, found, err := coordination.GetTask(ctx, lineage.ID(payload.TaskID))
				if err != nil {
					return domain.Result{}, err
				}
				if !found || task.ProjectID != project || task.ClaimedBySessionID != session.ID {
					return domain.Result{}, domain.NewError(domain.CodeConflict, "work-lite task is not owned by the session", false)
				}
				run, found, err := coordination.GetRun(ctx, lineage.ID(payload.RunID))
				if err != nil {
					return domain.Result{}, err
				}
				if !found || run.TaskID != task.ID || run.SessionID != session.ID || !lineage.CanTransitionRun(run.State, lineage.RunWorkComplete, []byte(payload.Evidence)) {
					return domain.Result{}, domain.NewError(domain.CodeConflict, "work-lite run cannot be completed", false)
				}
				run, err = coordination.TransitionRun(ctx, run.ID, lineage.RunWorkComplete, []byte(payload.Evidence), now)
				if err != nil {
					return domain.Result{}, err
				}
				task, err = dependencyapp.TransitionAndReconcileRepositories(ctx, repositories, now, string(project), payload.TaskID, payload.ActorSessionID, lineage.TaskWorkComplete, []byte(payload.Evidence))
				if err != nil {
					return domain.Result{}, err
				}
				runs, err := coordination.ListRunsForSession(ctx, project, session.ID)
				if err != nil {
					return domain.Result{}, err
				}
				for _, ownedRun := range runs {
					if !lineage.RunHasEnded(ownedRun.State) {
						return domain.Result{}, domain.NewError(domain.CodeConflict, "work-lite session still owns a non-terminal run", false)
					}
				}
				released, err := repositories.Reservations().ReleaseForTask(ctx, domain.ProjectID(resolved.Project), task.ID, now, "work-lite completed")
				if err != nil {
					return domain.Result{}, err
				}
				heartbeat := lineage.Heartbeat{ID: lineage.ID(payload.ArchiveEventID), SessionID: session.ID, ObservedAt: now, Liveness: lineage.Interrupted, Detail: []byte(`{"archived":true,"mode":"WORK_LITE"}`)}
				if heartbeat.Validate() != nil {
					return domain.Result{}, invalidRequest()
				}
				if err := coordination.RecordHeartbeat(ctx, heartbeat); err != nil {
					return domain.Result{}, err
				}
				value := lineageFinishLiteResult{TaskID: string(task.ID), TaskState: string(task.State), RunID: string(run.ID), RunState: string(run.State), SessionID: string(session.ID), ReservationsReleased: len(released), SessionArchived: true}
				return domain.Result{ID: domain.ResultID(task.ID), Outcome: domain.OutcomeOK, Data: value}, nil
			})
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(recorded.Data)
			if err != nil || json.Unmarshal(encoded, &completed) != nil {
				return domain.NewError(domain.CodeInternal, "work-lite completion receipt is invalid", false)
			}
			result = completed
			return nil
		}
		return invalidRequest()
	})
	if err.Code != "" {
		return outcome(result, err), true
	}
	return outcome(result, nil), true
}

func lineageSessionRequest(payload lineageSessionPayload, project lineage.ID) lineage.AgentSession {
	fingerprint := ""
	if payload.NativeAccessState != string(lineage.NativeAccessUnsupported) {
		fingerprint = lineage.NativeSessionFingerprint(payload.Runtime, payload.NativeSessionID, payload.NativeSessionRef, payload.NativeSessionStartedAt)
	}
	return lineage.AgentSession{ID: lineage.ID(payload.ID), ProjectID: project, HumanID: lineage.ID(payload.HumanID), Runtime: payload.Runtime, Role: payload.Role, SourceRef: payload.SourceRef, ParentID: lineage.ID(payload.ParentSessionID), ContinuationOfID: lineage.ID(payload.ContinuationOfID), TaskID: lineage.ID(payload.TaskID), WorktreeRef: payload.WorktreeRef, NativeAccessState: lineage.NativeAccessState(payload.NativeAccessState), RuntimeHome: payload.RuntimeHome, NativeSessionID: payload.NativeSessionID, NativeSessionRef: payload.NativeSessionRef, NativeSessionStartedAt: payload.NativeSessionStartedAt, NativeSessionFingerprint: fingerprint, NativeParentSessionID: payload.NativeParentSessionID}
}

func lineageHumanResponse(x lineage.Human) lineageHumanResult {
	return lineageHumanResult{ID: string(x.ID), DisplayName: safety.SafeText(x.DisplayName), Confidence: string(x.Confidence), Supersedes: string(x.Supersedes)}
}
func lineageSessionResponse(x lineage.AgentSession) lineageSessionResult {
	return lineageSessionResult{ID: string(x.ID), ProjectID: string(x.ProjectID), HumanID: string(x.HumanID), Kind: string(x.Kind), Runtime: safety.SafeText(x.Runtime), Role: safety.SafeText(x.Role), Source: string(x.Source), ParentID: string(x.ParentID), RootID: string(x.RootID), ContinuationOfID: string(x.ContinuationOfID), TaskID: string(x.TaskID), StartedAt: x.StartedAt, NativeAccessState: string(x.NativeAccessState)}
}
func lineageTaskResponse(x lineage.Task) lineageTaskResult {
	return lineageTaskResult{ID: string(x.ID), DisplayNumber: x.DisplayNumber, Title: safety.SafeText(x.Title), State: string(x.State), CreatedBySessionID: string(x.CreatedBySessionID), ClaimedBySessionID: string(x.ClaimedBySessionID), ParentTaskID: string(x.ParentTaskID), CompletionPolicy: string(lineage.EffectiveTaskCompletionPolicy(x.CompletionPolicy)), ParentRequirement: string(lineage.EffectiveTaskParentRequirement(x.ParentRequirement))}
}
func lineageRunResponse(x lineage.TaskRun) lineageRunResult {
	return lineageRunResult{ID: string(x.ID), TaskID: string(x.TaskID), SessionID: string(x.SessionID), State: string(x.State)}
}
