// Package lineage exposes typed coordination commands without SQL leakage.
package lineage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"crypto/pbkdf2"
	"github.com/jeremy-merchant/OMG/internal/domain"
	core "github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/safety"
)

const tokenIterations = 100000

type Options struct {
	MaxDelegationDepth int
}

type Service struct {
	store              ports.Store
	now                func() time.Time
	maxDelegationDepth int
}

func New(store ports.Store, now func() time.Time) *Service {
	return NewWithOptions(store, now, Options{})
}

func NewWithOptions(store ports.Store, now func() time.Time, options Options) *Service {
	if now == nil {
		now = time.Now
	}
	if options.MaxDelegationDepth <= 0 {
		options.MaxDelegationDepth = 8
	}
	return &Service{store: store, now: now, maxDelegationDepth: options.MaxDelegationDepth}
}

type TokenIssue struct {
	Token    core.DelegationToken
	RawToken string
}

// CheckpointResult is the safe, durable identity of a recorded checkpoint.
// It intentionally excludes private heartbeat detail and observation metadata.
type CheckpointResult struct {
	ID        core.ID       `json:"id"`
	SessionID core.ID       `json:"session_id"`
	Liveness  core.Liveness `json:"liveness"`
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid lineage request", false)
}
func conflict() error {
	return domain.NewError(domain.CodeConflict, "lineage ownership conflict", false)
}
func notFound() error { return domain.NewError(domain.CodeNotFound, "lineage record not found", false) }
func humanNotFound() error {
	return domain.NewError(domain.CodeNotFound, "human_id is not registered in the selected project", false)
}
func unavailable() error {
	return domain.NewError(domain.CodeUnavailable, "lineage store unavailable", true)
}

func terminalTask(state core.TaskState) bool {
	return state == core.TaskVerifiedDone || state == core.TaskCancelled || state == core.TaskFailed || state == core.TaskAbandoned
}

func terminalSession(session core.AgentSession) bool {
	return session.EndedAt != nil || session.InterruptedAt != nil || session.Liveness == core.Interrupted
}

func knownRunState(state core.RunState) bool {
	switch state {
	case core.RunRunning, core.RunWaiting, core.RunBlocked, core.RunRework, core.RunWorkComplete, core.RunVerifiedDone, core.RunFailed, core.RunAbandoned, core.RunInterrupted, core.RunStale, core.RunCancelled:
		return true
	default:
		return false
	}
}
func id(prefix string) (core.ID, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return core.ID(prefix + base64.RawURLEncoding.EncodeToString(b)), nil
}
func derive(password string, salt []byte, iterations, length int) []byte {
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, length)
	if err != nil {
		return nil
	}
	return key
}

type sessionCreatedSummary struct {
	SessionID         core.ID                `json:"session_id"`
	ProjectID         core.ID                `json:"project_id"`
	LineageKind       core.LineageKind       `json:"lineage_kind"`
	NativeAccessState core.NativeAccessState `json:"native_access_state"`
}

func sessionSummary(x core.AgentSession) sessionCreatedSummary {
	return sessionCreatedSummary{SessionID: x.ID, ProjectID: x.ProjectID, LineageKind: x.Kind, NativeAccessState: x.NativeAccessState}
}

type humanCreatedSummary struct {
	HumanID    core.ID                   `json:"human_id"`
	Confidence core.ProvenanceConfidence `json:"confidence"`
}

func humanSummary(x core.Human) humanCreatedSummary {
	return humanCreatedSummary{HumanID: x.ID, Confidence: x.Confidence}
}

type taskCreatedSummary struct {
	TaskID        core.ID        `json:"task_id"`
	ProjectID     core.ID        `json:"project_id"`
	State         core.TaskState `json:"state"`
	DisplayNumber int64          `json:"display_number"`
}

func taskSummary(x core.Task) taskCreatedSummary {
	return taskCreatedSummary{TaskID: x.ID, ProjectID: x.ProjectID, State: x.State, DisplayNumber: x.DisplayNumber}
}

type runCreatedSummary struct {
	RunID  core.ID       `json:"run_id"`
	TaskID core.ID       `json:"task_id"`
	State  core.RunState `json:"state"`
}

func runSummary(x core.TaskRun) runCreatedSummary {
	return runCreatedSummary{RunID: x.ID, TaskID: x.TaskID, State: x.State}
}

type tokenIssuedSummary struct {
	TokenID core.ID `json:"token_id"`
}

func rejectWrite(key domain.IdempotencyKey, values ...any) error {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, values...) != nil {
		return invalid()
	}
	return nil
}
func (s *Service) CreateHuman(ctx context.Context, key domain.IdempotencyKey, h core.Human) (core.Human, error) {
	if err := rejectWrite(key, h); err != nil {
		return h, err
	}
	if h.ID == "" {
		v, e := id("human_")
		if e != nil {
			return h, unavailable()
		}
		h.ID = v
	}
	h.CreatedAt = s.now().UTC()
	if (h.Confidence != core.ConfidenceExplicit && h.Confidence != core.ConfidenceVerified) || h.Validate() != nil {
		return h, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "human.create", func(r ports.Repositories) (domain.Result, error) {
		e := r.Coordination().CreateHuman(ctx, h)
		return domain.Result{ID: domain.ResultID(h.ID), Outcome: domain.OutcomeOK, Data: humanSummary(h)}, e
	})
	if e != nil {
		return h, mapErr(e)
	}
	return s.Human(ctx, core.ID(result.ID))
}
func (s *Service) Human(ctx context.Context, id core.ID) (core.Human, error) {
	var out core.Human
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var e error
		out, ok, e = r.Coordination().GetHuman(ctx, id)
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) RegisterHumanDirect(ctx context.Context, key domain.IdempotencyKey, x core.AgentSession) (core.AgentSession, error) {
	x.Kind = core.HumanDirect
	x.Source = core.SourceHuman
	return s.createSession(ctx, key, x, "session.create")
}
func (s *Service) createSession(ctx context.Context, key domain.IdempotencyKey, x core.AgentSession, operation string) (core.AgentSession, error) {
	if err := rejectWrite(key, x); err != nil {
		return x, err
	}
	if x.ID == "" {
		v, e := id("session_")
		if e != nil {
			return x, unavailable()
		}
		x.ID = v
	}
	x.StartedAt = s.now().UTC()
	if x.RootID == "" {
		x.RootID = x.ID
	}
	if x.NativeAccessState == "" {
		x.NativeAccessState = core.NativeAccessUnsupported
	}
	if e := x.Validate(); e != nil {
		return x, invalid()
	}
	if x.HumanID != "" {
		var found bool
		readErr := s.store.Read(ctx, func(r ports.Repositories) error {
			_, exists, err := r.Coordination().GetHuman(ctx, x.HumanID)
			found = exists
			return err
		})
		if readErr != nil {
			return x, mapErr(readErr)
		}
		if !found {
			return x, humanNotFound()
		}
	}
	_, result, e := s.store.Write(ctx, key, operation, func(r ports.Repositories) (domain.Result, error) {
		e := r.Coordination().CreateSession(ctx, x)
		return domain.Result{ID: domain.ResultID(x.ID), Outcome: domain.OutcomeOK, Data: sessionSummary(x)}, e
	})
	if e != nil {
		return x, mapErr(e)
	}
	var out core.AgentSession
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, e = r.Coordination().GetSession(ctx, core.ID(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) IssueToken(ctx context.Context, key domain.IdempotencyKey, project, task, parent core.ID, ttl time.Duration) (TokenIssue, error) {
	if err := rejectWrite(key, project, task, parent); err != nil {
		return TokenIssue{}, err
	}
	if project == "" || parent == "" || ttl <= 0 || ttl > core.MaxDelegationTTL {
		return TokenIssue{}, invalid()
	}
	raw := make([]byte, 32)
	salt := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return TokenIssue{}, unavailable()
	}
	if _, err := rand.Read(salt); err != nil {
		return TokenIssue{}, unavailable()
	}
	rawToken := "omgdt_v1_" + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now().UTC()
	tokenID, err := id("delegation_")
	if err != nil {
		return TokenIssue{}, unavailable()
	}
	token := core.DelegationToken{ID: tokenID, ProjectID: project, TaskID: task, ParentSessionID: parent, Algorithm: "PBKDF2-HMAC-SHA256", Iterations: tokenIterations, Salt: salt, Verifier: derive(rawToken, salt, tokenIterations, 32), IssuedAt: now, ExpiresAt: now.Add(ttl)}
	if err := token.Validate(); err != nil {
		return TokenIssue{}, invalid()
	}
	issued := false
	_, result, err := s.store.Write(ctx, key, "delegate.issue", func(repositories ports.Repositories) (domain.Result, error) {
		depth, err := s.delegationDepth(ctx, repositories, project, parent)
		if err != nil {
			return domain.Result{}, err
		}
		if depth >= s.maxDelegationDepth {
			return domain.Result{}, conflict()
		}
		issued = true
		if err := repositories.Coordination().IssueToken(ctx, token); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: domain.ResultID(token.ID), Outcome: domain.OutcomeOK, Data: tokenIssuedSummary{TokenID: token.ID}}, nil
	})
	if err != nil {
		return TokenIssue{}, mapErr(err)
	}
	var canonical core.DelegationToken
	if err := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		canonical, ok, err = r.Coordination().GetToken(ctx, core.ID(result.ID))
		if err != nil {
			return err
		}
		if !ok {
			return notFound()
		}
		return nil
	}); err != nil {
		return TokenIssue{}, mapErr(err)
	}
	canonical.Salt = nil
	canonical.Verifier = nil
	out := TokenIssue{Token: canonical}
	if issued {
		out.RawToken = rawToken
	}
	return out, nil
}
func (s *Service) RevokeToken(ctx context.Context, key domain.IdempotencyKey, id core.ID) error {
	if err := rejectWrite(key, id); err != nil {
		return err
	}
	_, _, err := s.store.Write(ctx, key, "delegate.revoke", func(r ports.Repositories) (domain.Result, error) {
		err := r.Coordination().RevokeToken(ctx, id, s.now().UTC())
		return domain.Result{ID: domain.ResultID(id), Outcome: domain.OutcomeOK}, err
	})
	return mapErr(err)
}
func (s *Service) RegisterDelegated(ctx context.Context, key domain.IdempotencyKey, raw string, session core.AgentSession, project, task, parent core.ID) (core.AgentSession, error) {
	if err := rejectWrite(key, session, project, task, parent); err != nil {
		return session, err
	}
	if raw == "" || project == "" || parent == "" {
		return session, invalid()
	}
	if session.ID == "" {
		value, err := id("session_")
		if err != nil {
			return session, unavailable()
		}
		session.ID = value
	}
	now := s.now().UTC()
	session.Kind = core.AgentDelegated
	session.Source = core.SourceDelegationToken
	session.ProjectID = project
	session.TaskID = task
	session.ParentID = parent
	session.StartedAt = now
	if session.NativeAccessState == "" {
		session.NativeAccessState = core.NativeAccessUnsupported
	}
	match := core.ID("")
	if err := s.store.Read(ctx, func(repositories ports.Repositories) error {
		parentSession, ok, err := repositories.Coordination().GetSession(ctx, parent)
		if err != nil {
			return err
		}
		if !ok || parentSession.ProjectID != project {
			return notFound()
		}
		tokens, err := repositories.Coordination().FindTokenByVerifier(ctx, project, task, parent)
		if err != nil {
			return err
		}
		for _, token := range tokens {
			verifier := derive(raw, token.Salt, token.Iterations, len(token.Verifier))
			if subtle.ConstantTimeCompare(verifier, token.Verifier) == 1 {
				match = token.ID
			}
		}
		return nil
	}); err != nil {
		return session, mapErr(err)
	}
	if match == "" {
		return session, conflict()
	}
	_, result, err := s.store.Write(ctx, key, "delegate.register", func(repositories ports.Repositories) (domain.Result, error) {
		depth, err := s.delegationDepth(ctx, repositories, project, parent)
		if err != nil {
			return domain.Result{}, err
		}
		if depth >= s.maxDelegationDepth {
			return domain.Result{}, conflict()
		}
		parentSession, ok, err := repositories.Coordination().GetSession(ctx, parent)
		if err != nil {
			return domain.Result{}, err
		}
		if !ok || parentSession.ProjectID != project {
			return domain.Result{}, notFound()
		}
		session.RootID = parentSession.RootID
		if session.RootID == "" {
			session.RootID = parentSession.ID
		}
		session.HumanID = parentSession.HumanID
		if err := session.Validate(); err != nil {
			return domain.Result{}, invalid()
		}
		token, ok, err := repositories.Coordination().GetToken(ctx, match)
		if err != nil {
			return domain.Result{}, err
		}
		if !ok || token.ProjectID != project || token.TaskID != task || token.ParentSessionID != parent ||
			token.RevokedAt != nil || token.ConsumedAt != nil || !token.ExpiresAt.After(now) {
			return domain.Result{}, conflict()
		}
		if err := repositories.Coordination().CreateSession(ctx, session); err != nil {
			return domain.Result{}, err
		}
		if err := repositories.Coordination().ConsumeToken(ctx, match, session.ID, now); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: domain.ResultID(session.ID), Outcome: domain.OutcomeOK, Data: sessionSummary(session)}, nil
	})
	if err != nil {
		return session, mapErr(err)
	}
	var out core.AgentSession
	err = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, err = r.Coordination().GetSession(ctx, core.ID(result.ID))
		if err != nil {
			return err
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return out, mapErr(err)
}
func (s *Service) Checkpoint(ctx context.Context, key domain.IdempotencyKey, h core.Heartbeat) error {
	_, err := s.CheckpointResult(ctx, key, h)
	return err
}

// CheckpointResult records a heartbeat once and always returns the durable
// outcome associated with the idempotency key, including on replay.
func (s *Service) CheckpointResult(ctx context.Context, key domain.IdempotencyKey, h core.Heartbeat) (CheckpointResult, error) {
	if err := rejectWrite(key, h); err != nil {
		return CheckpointResult{}, err
	}
	h.ObservedAt = s.now().UTC()
	if h.ID == "" {
		v, e := id("heartbeat_")
		if e != nil {
			return CheckpointResult{}, unavailable()
		}
		h.ID = v
	}
	if e := h.Validate(); e != nil {
		return CheckpointResult{}, invalid()
	}
	expected := CheckpointResult{ID: h.ID, SessionID: h.SessionID, Liveness: h.Liveness}
	_, recorded, e := s.store.Write(ctx, key, "checkpoint.record", func(r ports.Repositories) (domain.Result, error) {
		e := r.Coordination().RecordHeartbeat(ctx, h)
		return domain.Result{ID: domain.ResultID(h.ID), Outcome: domain.OutcomeOK, Data: expected}, e
	})
	if e != nil {
		return CheckpointResult{}, mapErr(e)
	}
	canonical, e := checkpointResultFromData(recorded.Data)
	if e != nil {
		return CheckpointResult{}, e
	}
	return canonical, nil
}

func checkpointResultFromData(data any) (CheckpointResult, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return CheckpointResult{}, conflict()
	}
	var result CheckpointResult
	if err := json.Unmarshal(encoded, &result); err != nil || result.ID == "" || result.SessionID == "" || result.Liveness == "" {
		return CheckpointResult{}, conflict()
	}
	return result, nil
}
func (s *Service) Resume(ctx context.Context, key domain.IdempotencyKey, x core.AgentSession) (core.AgentSession, error) {
	x.Kind = core.Resumed
	x.Source = core.SourceResume
	return s.inheritSession(ctx, key, x, x.ContinuationOfID, "session.resume")
}
func (s *Service) Adopt(ctx context.Context, key domain.IdempotencyKey, x core.AgentSession) (core.AgentSession, error) {
	x.Kind = core.Adopted
	x.Source = core.SourceAdoption
	return s.inheritSession(ctx, key, x, x.ParentID, "session.adopt")
}
func (s *Service) inheritSession(ctx context.Context, key domain.IdempotencyKey, x core.AgentSession, reference core.ID, operation string) (core.AgentSession, error) {
	if err := rejectWrite(key, x); err != nil {
		return x, err
	}
	if x.ID == "" {
		v, e := id("session_")
		if e != nil {
			return x, unavailable()
		}
		x.ID = v
	}
	_, result, e := s.store.Write(ctx, key, operation, func(r ports.Repositories) (domain.Result, error) {
		if reference == "" {
			return domain.Result{}, invalid()
		}
		prior, ok, e := r.Coordination().GetSession(ctx, reference)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, notFound()
		}
		if x.ProjectID != "" && x.ProjectID != prior.ProjectID {
			return domain.Result{}, invalid()
		}
		x.ProjectID = prior.ProjectID
		x.RootID = prior.RootID
		if x.RootID == "" {
			x.RootID = prior.ID
		}
		x.HumanID = prior.HumanID
		x.StartedAt = s.now().UTC()
		if x.NativeAccessState == "" {
			x.NativeAccessState = core.NativeAccessUnsupported
		}
		if e := x.Validate(); e != nil {
			return domain.Result{}, invalid()
		}
		if e := r.Coordination().CreateSession(ctx, x); e != nil {
			return domain.Result{}, e
		}
		return domain.Result{ID: domain.ResultID(x.ID), Outcome: domain.OutcomeOK, Data: sessionSummary(x)}, nil
	})
	if e != nil {
		return x, mapErr(e)
	}
	var out core.AgentSession
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, e = r.Coordination().GetSession(ctx, core.ID(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) Import(ctx context.Context, key domain.IdempotencyKey, x core.AgentSession) (core.AgentSession, error) {
	x.Kind = core.Imported
	x.Source = core.SourceImport
	return s.createSession(ctx, key, x, "session.import")
}
func (s *Service) Interrupt(ctx context.Context, key domain.IdempotencyKey, h core.Heartbeat) error {
	h.Liveness = core.Interrupted
	return s.Checkpoint(ctx, key, h)
}
func (s *Service) MarkParentLoss(ctx context.Context, key domain.IdempotencyKey, run core.ID, h core.Heartbeat) (core.TaskRun, error) {
	if err := rejectWrite(key, run, h); err != nil {
		return core.TaskRun{}, err
	}
	h.ObservedAt = s.now().UTC()
	if h.ID == "" {
		v, e := id("heartbeat_")
		if e != nil {
			return core.TaskRun{}, unavailable()
		}
		h.ID = v
	}
	if h.Liveness != core.Stale && h.Liveness != core.Interrupted {
		return core.TaskRun{}, invalid()
	}
	if e := h.Validate(); e != nil {
		return core.TaskRun{}, invalid()
	}
	var out core.TaskRun
	_, result, e := s.store.Write(ctx, key, "run.parent_loss", func(p ports.Repositories) (domain.Result, error) {
		if _, ok, err := p.Coordination().GetRun(ctx, run); err != nil {
			return domain.Result{}, err
		} else if !ok {
			return domain.Result{}, notFound()
		}
		updated, err := p.Coordination().RecordParentLoss(ctx, run, h)
		out = updated
		return domain.Result{ID: domain.ResultID(run), Outcome: domain.OutcomeOK, Data: runSummary(out)}, err
	})
	if e != nil {
		return out, mapErr(e)
	}
	var canonical core.TaskRun
	e = s.store.Read(ctx, func(p ports.Repositories) error {
		var ok bool
		canonical, ok, e = p.Coordination().GetRun(ctx, core.ID(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return canonical, mapErr(e)
}
func (s *Service) CreateTask(ctx context.Context, key domain.IdempotencyKey, t core.Task) (core.Task, error) {
	if err := rejectWrite(key, t); err != nil {
		return t, err
	}
	if t.ID == "" {
		v, e := id("task_")
		if e != nil {
			return t, unavailable()
		}
		t.ID = v
	}
	now := s.now().UTC()
	t.State = core.TaskReady
	t.CreatedAt = now
	t.UpdatedAt = now
	t.DisplayNumber = 1
	if e := t.Validate(); e != nil {
		return t, invalid()
	}
	var out core.Task
	_, result, e := s.store.Write(ctx, key, "task.create", func(r ports.Repositories) (domain.Result, error) {
		var e error
		out, e = r.Coordination().CreateTask(ctx, t)
		return domain.Result{ID: domain.ResultID(out.ID), Outcome: domain.OutcomeOK, Data: taskSummary(out)}, e
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.Task(ctx, core.ID(result.ID))
}
func (s *Service) Task(ctx context.Context, id core.ID) (core.Task, error) {
	var out core.Task
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var e error
		out, ok, e = r.Coordination().GetTask(ctx, id)
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) Claim(ctx context.Context, key domain.IdempotencyKey, task, session core.ID) (core.Task, error) {
	if err := rejectWrite(key, task, session); err != nil {
		return core.Task{}, err
	}
	var out core.Task
	_, result, e := s.store.Write(ctx, key, "task.claim", func(r ports.Repositories) (domain.Result, error) {
		var winner bool
		var e error
		out, winner, e = r.Coordination().ClaimTask(ctx, task, session, s.now().UTC())
		if e != nil {
			return domain.Result{}, e
		}
		if !winner {
			if out.ID == "" {
				return domain.Result{}, notFound()
			}
			return domain.Result{}, conflict()
		}
		return domain.Result{ID: domain.ResultID(out.ID), Outcome: domain.OutcomeOK, Data: taskSummary(out)}, nil
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.Task(ctx, core.ID(result.ID))
}
func (s *Service) TransitionTask(ctx context.Context, key domain.IdempotencyKey, id core.ID, to core.TaskState, evidence []byte) (core.Task, error) {
	if err := rejectWrite(key, id, to, evidence); err != nil {
		return core.Task{}, err
	}
	var out core.Task
	_, result, e := s.store.Write(ctx, key, "task.transition", func(r ports.Repositories) (domain.Result, error) {
		var e error
		out, e = r.Coordination().TransitionTask(ctx, id, to, evidence, s.now().UTC())
		if e != nil {
			return domain.Result{}, e
		}
		if out.ID == "" {
			return domain.Result{}, notFound()
		}
		return domain.Result{ID: domain.ResultID(id), Outcome: domain.OutcomeOK, Data: taskSummary(out)}, nil
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.Task(ctx, core.ID(result.ID))
}
func (s *Service) CreateRun(ctx context.Context, key domain.IdempotencyKey, r core.TaskRun) (core.TaskRun, error) {
	if err := rejectWrite(key, r); err != nil {
		return r, err
	}
	if r.ID == "" {
		v, e := id("run_")
		if e != nil {
			return r, unavailable()
		}
		r.ID = v
	}
	r.State = core.RunRunning
	r.StartedAt = s.now().UTC()
	if e := r.Validate(); e != nil {
		return r, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "task.run-create", func(p ports.Repositories) (domain.Result, error) {
		task, ok, e := p.Coordination().GetTask(ctx, r.TaskID)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, notFound()
		}
		if terminalTask(task.State) {
			return domain.Result{}, conflict()
		}
		session, ok, e := p.Coordination().GetSession(ctx, r.SessionID)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, notFound()
		}
		if terminalSession(session) {
			return domain.Result{}, conflict()
		}
		e = p.Coordination().CreateRun(ctx, r)
		return domain.Result{ID: domain.ResultID(r.ID), Outcome: domain.OutcomeOK, Data: runSummary(r)}, e
	})
	if e != nil {
		return r, mapErr(e)
	}
	var out core.TaskRun
	e = s.store.Read(ctx, func(p ports.Repositories) error {
		var ok bool
		out, ok, e = p.Coordination().GetRun(ctx, core.ID(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) TransitionRun(ctx context.Context, key domain.IdempotencyKey, id core.ID, to core.RunState, evidence []byte) (core.TaskRun, error) {
	if err := rejectWrite(key, id, to, evidence); err != nil {
		return core.TaskRun{}, err
	}
	if !knownRunState(to) || (to == core.RunVerifiedDone && len(evidence) == 0) {
		return core.TaskRun{}, invalid()
	}
	var out core.TaskRun
	_, result, e := s.store.Write(ctx, key, "task.run-transition", func(p ports.Repositories) (domain.Result, error) {
		run, ok, e := p.Coordination().GetRun(ctx, id)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, notFound()
		}
		if !core.CanTransitionRun(run.State, to, evidence) {
			return domain.Result{}, conflict()
		}
		if to == core.RunWorkComplete || to == core.RunVerifiedDone {
			owner, ok, e := p.Coordination().GetSession(ctx, run.SessionID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok {
				return domain.Result{}, notFound()
			}
			if owner.Liveness == core.Stale || owner.Liveness == core.Interrupted {
				return domain.Result{}, invalid()
			}
		}
		out, e = p.Coordination().TransitionRun(ctx, id, to, evidence, s.now().UTC())
		return domain.Result{ID: domain.ResultID(id), Outcome: domain.OutcomeOK, Data: runSummary(out)}, e
	})
	if e != nil {
		return out, mapErr(e)
	}
	var canonical core.TaskRun
	e = s.store.Read(ctx, func(p ports.Repositories) error {
		var ok bool
		canonical, ok, e = p.Coordination().GetRun(ctx, core.ID(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return notFound()
		}
		return nil
	})
	return canonical, mapErr(e)
}
func mapErr(e error) error {
	if e == nil {
		return nil
	}
	var de domain.DomainError
	if errors.As(e, &de) {
		return de
	}
	return unavailable()
}

func (s *Service) delegationDepth(ctx context.Context, repositories ports.Repositories, project, parent core.ID) (int, error) {
	depth := 0
	seen := map[core.ID]struct{}{}
	for parent != "" {
		if _, exists := seen[parent]; exists {
			return 0, conflict()
		}
		seen[parent] = struct{}{}
		session, ok, err := repositories.Coordination().GetSession(ctx, parent)
		if err != nil {
			return 0, err
		}
		if !ok || session.ProjectID != project {
			return 0, notFound()
		}
		if session.ParentID == "" {
			return depth, nil
		}
		depth++
		parent = session.ParentID
	}
	return 0, notFound()
}
