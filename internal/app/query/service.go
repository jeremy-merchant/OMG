package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	appgit "github.com/jeremy-merchant/OMG/internal/app/git"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/domain/coordination"
	gitdomain "github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/domain/reservation"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/safety"
)

// Service constructs canonical, authorized board snapshots. It deliberately
type Service struct {
	store  ports.Store
	now    func() time.Time
	native ports.NativeSessionResolver
}

func New(store ports.Store) Service { return Service{store: store, now: time.Now} }

func NewWithNativeResolver(store ports.Store, resolver ports.NativeSessionResolver) Service {
	return Service{store: store, now: time.Now, native: resolver}
}
func NewWithClock(store ports.Store, clock ports.Clock) Service {
	if clock == nil {
		return New(store)
	}
	return Service{store: store, now: clock.Now}
}

func (s Service) Query(ctx context.Context, actor domain.ActorContext, request BoardRequest) (ViewModel, error) {
	if !actor.Has(domain.CapabilityRead) {
		return ViewModel{}, domain.NewError(domain.CodeUnavailable, "read capability is required", false)
	}
	if actor.Project == "" {
		return ViewModel{}, domain.NewError(domain.CodeInvalidArgument, "project is required", false)
	}
	if err := request.Validate(); err != nil {
		return ViewModel{}, err
	}
	snapshot := emptyBoard(request, actor)
	evaluationTime := s.now().UTC()
	snapshot.GeneratedAt = evaluationTime
	var sessionsForResolution map[string]lineage.AgentSession
	err := s.store.Read(ctx, func(repos ports.Repositories) error {
		c := repos.Coordination()
		cursor, err := repos.Audit().LatestCursor(ctx)
		if err != nil {
			return err
		}
		snapshot.SnapshotCursor = fmt.Sprintf("audit:%d", cursor.Sequence)
		allSessions, err := c.ListSessions(ctx, lineage.ID(actor.Project))
		if err != nil {
			return err
		}
		allTasks, err := c.ListTasks(ctx, actor.Project)
		if err != nil {
			return err
		}
		sessions := map[string]lineage.AgentSession{}
		for _, v := range allSessions {
			sessions[string(v.ID)] = v
		}
		tasks := map[string]lineage.Task{}
		for _, v := range allTasks {
			tasks[string(v.ID)] = v
		}
		sessionsForResolution = sessions
		selectedSessions := sessions
		selectedTasks := tasks
		switch request.Mode {
		case BoardMe:
			current, ok := sessions[request.SessionID]
			if !ok || current.ProjectID != lineage.ID(actor.Project) {
				return domain.NewError(domain.CodeNotFound, "session was not found in project", false)
			}
			selectedSessions = map[string]lineage.AgentSession{request.SessionID: current}
			selectedTasks = relevantTasks(tasks, request.SessionID)
		case BoardTask:
			task, ok := tasks[request.TaskID]
			if !ok || task.ProjectID != lineage.ID(actor.Project) {
				return domain.NewError(domain.CodeNotFound, "task was not found in project", false)
			}
			selectedTasks = map[string]lineage.Task{request.TaskID: task}
			selectedSessions = map[string]lineage.AgentSession{}
			for _, candidate := range sessions {
				if candidate.TaskID == lineage.ID(request.TaskID) {
					selectedSessions[string(candidate.ID)] = candidate
				}
			}
		}

		allRuns := map[string]lineage.TaskRun{}
		for _, session := range allSessions {
			runs, err := c.ListRunsForSession(ctx, lineage.ID(actor.Project), session.ID)
			if err != nil {
				return err
			}
			for _, run := range runs {
				allRuns[string(run.ID)] = run
			}
		}
		if request.Mode == BoardMe {
			current := sessions[request.SessionID]
			if current.TaskID != "" {
				if task, ok := tasks[string(current.TaskID)]; ok {
					selectedTasks[string(task.ID)] = task
				}
			}
			for _, run := range allRuns {
				if run.SessionID == current.ID {
					if task, ok := tasks[string(run.TaskID)]; ok {
						selectedTasks[string(task.ID)] = task
					}
				}
			}
		}
		if request.Mode == BoardTask {
			for _, run := range allRuns {
				if string(run.TaskID) == request.TaskID {
					if session, ok := sessions[string(run.SessionID)]; ok {
						selectedSessions[string(session.ID)] = session
					}
				}
			}
		}
		if request.Mode == BoardMe {
			identity, err := identityFor(ctx, c, sessions[request.SessionID], actor.Project)
			if err != nil {
				return err
			}
			snapshot.Identity = &identity
		}
		for _, v := range selectedSessions {
			identity, err := identityFor(ctx, c, v, actor.Project)
			if err != nil {
				return err
			}
			if request.Mode == BoardGit {
				identity = gitIdentityScope(identity)
			}
			snapshot.Sessions = append(snapshot.Sessions, identity)
		}
		if request.Mode != BoardGit {
			for _, v := range selectedTasks {
				snapshot.Tasks = append(snapshot.Tasks, taskView(v))
			}
			for _, run := range allRuns {
				if selectedTasks[string(run.TaskID)].ID != "" || selectedSessions[string(run.SessionID)].ID != "" {
					snapshot.Runs = append(snapshot.Runs, runView(run))
				}
			}
		}
		if request.Mode != BoardGit {
			dependencies, err := c.ListDependencies(ctx, string(actor.Project))
			if err != nil {
				return err
			}
			for _, dependency := range dependencies {
				if selectedTasks[dependency.DependentTaskID].ID != "" || selectedTasks[dependency.PrerequisiteTaskID].ID != "" {
					snapshot.Dependencies = append(snapshot.Dependencies, dependencyView(dependency, tasks))
				}
			}
		}
		if includesPrivateCoordination(request.Mode) {
			for taskID := range selectedTasks {
				progress, err := c.ListProgress(ctx, taskID)
				if err != nil {
					return err
				}
				for _, item := range progress {
					snapshot.Progress = append(snapshot.Progress, progressView(item))
				}
				handoffs, err := c.ListHandoffs(ctx, taskID)
				if err != nil {
					return err
				}
				for _, handoff := range handoffs {
					decision, ok, err := c.GetHandoffDecision(ctx, handoff.ID)
					if err != nil {
						return err
					}
					var decisionView *HandoffDecisionView
					if ok {
						decisionView = handoffDecisionView(decision)
					}
					lifecycle, err := c.ListHandoffLifecycleEvents(ctx, handoff.ID)
					if err != nil {
						return err
					}
					snapshot.Handoffs = append(snapshot.Handoffs, handoffView(handoff, string(allRuns[handoff.RunID].State), decisionView, lifecycle))
				}
			}
			for _, target := range boardRecipientTargets(selectedSessions, selectedTasks, sessions) {
				inbox, err := c.ListInbox(ctx, string(actor.Project), target)
				if err != nil {
					return err
				}
				for _, message := range inbox {
					if request.Mode == BoardTask && !messageRelatedOrAddressedToTask(message, target, request.TaskID) {
						continue
					}
					delivery, ok, err := c.GetDelivery(ctx, message.ID, target)
					if err != nil {
						return err
					}
					if ok {
						snapshot.Inbox = append(snapshot.Inbox, inboxView(message, delivery))
					}
				}
			}
			reservations, err := repos.Reservations().List(ctx, actor.Project)
			if err != nil {
				return err
			}
			for _, item := range reservations {
				if reservationRelevant(item, selectedTasks, selectedSessions, request.Mode) {
					snapshot.Reservations = append(snapshot.Reservations, reservationView(item, reservations, evaluationTime))
				}
			}
		}
		if request.Mode != BoardTree {
			latest, hasGit, err := repos.Git().LatestSnapshot(ctx, actor.Project)
			if err != nil {
				return err
			}
			if hasGit {
				latest, err = appgit.ReconcileCurrent(ctx, repos, latest)
				if err != nil {
					return err
				}
			}
			if hasGit {
				snapshot.Git = gitView(latest)
				annotateIdentityGit(&snapshot, selectedSessions, latest)
				if request.Mode == BoardMe || request.Mode == BoardTask {
					snapshot.Git.Assets = filterGitAssets(snapshot.Git.Assets, selectedSessions, selectedTasks)
				} else if request.Mode == BoardGit {
					gitIdentityScopeView(snapshot.Git)
				}
			} else if request.Mode == BoardGit {
				snapshot.Warnings = append(snapshot.Warnings, "git_snapshot_unavailable")
			}
		}
		return nil
	})
	if err != nil {
		return ViewModel{}, err
	}
	if err := s.resolveNativeSessions(ctx, &snapshot, sessionsForResolution); err != nil {
		return ViewModel{}, err
	}
	dedupeAndAdvise(&snapshot)
	sort.Slice(snapshot.SuggestedActions, func(i, j int) bool {
		if snapshot.SuggestedActions[i].Code == snapshot.SuggestedActions[j].Code {
			return snapshot.SuggestedActions[i].Command < snapshot.SuggestedActions[j].Command
		}
		return snapshot.SuggestedActions[i].Code < snapshot.SuggestedActions[j].Code
	})
	canonicalize(&snapshot)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return ViewModel{}, fmt.Errorf("marshal board snapshot: %w", err)
	}
	snapshot.Redaction.ContentRedacted = strings.Contains(string(raw), "[REDACTED:")
	if snapshot.Redaction.ContentRedacted {
		raw, err = json.Marshal(snapshot)
		if err != nil {
			return ViewModel{}, fmt.Errorf("marshal board snapshot: %w", err)
		}
	}
	return NewViewModel("board", snapshot.SnapshotCursor, raw)
}

func emptyBoard(request BoardRequest, actor domain.ActorContext) BoardSnapshot {
	selector := request.SessionID
	if request.Mode == BoardTask {
		selector = request.TaskID
	}
	return BoardSnapshot{
		SchemaVersion: BoardSchemaVersion,
		ViewVersion:   ViewVersion,
		Scope: BoardScope{
			ProjectID:   string(actor.Project),
			WorkspaceID: string(actor.Workspace),
			Mode:        request.Mode,
			Selector:    selector,
		},
		Mode:             request.Mode,
		ProjectID:        string(actor.Project),
		Redaction:        RedactionView{PolicyName: BoardRedactionPolicyName, PolicyVersion: BoardRedactionPolicyVersion, ContentOmitted: true},
		Sessions:         []IdentityView{},
		Tasks:            []TaskView{},
		Runs:             []RunView{},
		Progress:         []ProgressView{},
		Dependencies:     []DependencyView{},
		Inbox:            []InboxItemView{},
		Handoffs:         []HandoffView{},
		Reservations:     []ReservationView{},
		Warnings:         []string{"git_observation_advisory_non_authorizing"},
		SuggestedActions: []SuggestedActionView{},
	}
}

func includesPrivateCoordination(mode BoardMode) bool {
	return mode == BoardAll || mode == BoardMe || mode == BoardTask
}

func messageRelatedOrAddressedToTask(message coordination.MailMessage, target coordination.RecipientTarget, taskID string) bool {
	return message.RelatedTaskID == taskID || target.TaskID == taskID
}

func gitIdentityScope(identity IdentityView) IdentityView {
	identity.TaskID = ""
	identity.PreviousTaskID = ""
	return identity
}

func gitIdentityScopeView(git *GitView) {
	for i := range git.Assets {
		git.Assets[i].OwnerTaskID = ""
	}
}
func relevantTasks(tasks map[string]lineage.Task, session string) map[string]lineage.Task {
	out := map[string]lineage.Task{}
	for id, t := range tasks {
		if string(t.CreatedBySessionID) == session || string(t.ClaimedBySessionID) == session {
			out[id] = t
		}
	}
	return out
}

func boardRecipientTargets(sessions map[string]lineage.AgentSession, tasks map[string]lineage.Task, allSessions map[string]lineage.AgentSession) []coordination.RecipientTarget {
	targets := make(map[string]coordination.RecipientTarget, len(sessions)*3+len(tasks))
	add := func(target coordination.RecipientTarget) {
		targets[recipientViewKey(target)] = target
	}
	for _, session := range sessions {
		add(coordination.RecipientTarget{SessionID: string(session.ID)})
		if session.HumanID != "" {
			add(coordination.RecipientTarget{HumanID: string(session.HumanID)})
		}
		if root, ok := allSessions[string(session.RootID)]; ok && root.HumanID != "" {
			add(coordination.RecipientTarget{HumanID: string(root.HumanID)})
		}
		if session.Role != "" {
			add(coordination.RecipientTarget{Role: session.Role})
		}
	}
	for _, task := range tasks {
		add(coordination.RecipientTarget{TaskID: string(task.ID)})
	}
	out := make([]coordination.RecipientTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return recipientViewKey(out[i]) < recipientViewKey(out[j]) })
	return out
}
func dedupeAndAdvise(snapshot *BoardSnapshot) {
	dedupeAndAdviseForOS(snapshot, runtime.GOOS)
}

func dedupeAndAdviseForOS(snapshot *BoardSnapshot, goos string) {
	_ = goos
	dependencies := make(map[string]struct{}, len(snapshot.Dependencies))
	dedupedDependencies := make([]DependencyView, 0, len(snapshot.Dependencies))
	for _, v := range snapshot.Dependencies {
		if _, ok := dependencies[v.ID]; !ok {
			dependencies[v.ID] = struct{}{}
			dedupedDependencies = append(dedupedDependencies, v)
		}
	}
	snapshot.Dependencies = dedupedDependencies
	inbox := make(map[string]struct{}, len(snapshot.Inbox))
	dedupedInbox := make([]InboxItemView, 0, len(snapshot.Inbox))
	for _, v := range snapshot.Inbox {
		if _, ok := inbox[v.MessageID+"\x00"+v.Recipient]; !ok {
			dedupedInbox = append(dedupedInbox, v)
			inbox[v.MessageID+"\x00"+v.Recipient] = struct{}{}
		}
	}
	snapshot.Inbox = dedupedInbox
	handoffs := make(map[string]struct{}, len(snapshot.Handoffs))
	dedupedHandoffs := make([]HandoffView, 0, len(snapshot.Handoffs))
	for _, v := range snapshot.Handoffs {
		if _, ok := handoffs[v.ID]; !ok {
			handoffs[v.ID] = struct{}{}
			dedupedHandoffs = append(dedupedHandoffs, v)
		}
	}
	snapshot.Handoffs = dedupedHandoffs
	warnings := map[string]struct{}{}
	for _, warning := range snapshot.Warnings {
		warnings[warning] = struct{}{}
	}
	addWarning := func(value string) { warnings[value] = struct{}{} }
	for _, v := range snapshot.Reservations {
		for _, conflict := range v.ConflictIDs {
			addWarning("reservation_conflict:" + v.ID + ":" + conflict)
		}
	}
	if snapshot.Git != nil {
		for _, asset := range snapshot.Git.Assets {
			for _, label := range asset.Classification {
				switch label {
				case "dirty_unowned", "unpushed", "diverged", "orphaned_worktree", "detached_unowned":
					addWarning("git_risk:" + asset.Fingerprint[:min(16, len(asset.Fingerprint))] + ":" + label)
				}
			}
		}
	}
	snapshot.Warnings = make([]string, 0, len(warnings))
	for warning := range warnings {
		snapshot.Warnings = append(snapshot.Warnings, warning)
	}
	actions := map[string]SuggestedActionView{}
	for _, task := range snapshot.Tasks {
		actions["show-task:"+task.ID] = suggestedAction(snapshot.Scope, "show_task", task.ID)
	}
	for _, handoff := range snapshot.Handoffs {
		actions["show-handoff:"+handoff.ID] = suggestedAction(snapshot.Scope, "show_handoff", handoff.ID)
	}
	for _, reservation := range snapshot.Reservations {
		actions["reservation-history:"+reservation.ID] = suggestedAction(snapshot.Scope, "reservation_history", reservation.ID)
	}
	if snapshot.Git != nil {
		for _, asset := range snapshot.Git.Assets {
			for _, label := range asset.Classification {
				switch label {
				case "dirty_unowned", "unpushed", "diverged", "orphaned_worktree", "detached_unowned":
					key := "git-cleanup-plan:" + asset.Fingerprint
					actions[key] = suggestedAction(snapshot.Scope, "git_cleanup_plan", asset.Fingerprint)
					goto nextAsset
				}
			}
		nextAsset:
		}
	}
	snapshot.SuggestedActions = make([]SuggestedActionView, 0, len(actions))
	for _, action := range actions {
		snapshot.SuggestedActions = append(snapshot.SuggestedActions, action)
	}
}

// suggestedAction emits a reviewable command template rather than an ambient
// command. The explicit, quoted <PROJECT_PATH> placeholder prevents a copied
// command from silently acting on the caller's current directory. Canonical
// private paths are never retained in the snapshot or command template.
func suggestedAction(scope BoardScope, code, selector string) SuggestedActionView {
	const projectPlaceholder = "<PROJECT_PATH>"
	var argv []string
	switch code {
	case "show_task":
		argv = []string{"omg", "board", "task", "--project", projectPlaceholder, "--task", selector}
	case "show_handoff":
		argv = []string{"omg", "handoff", "show", "--project", projectPlaceholder, "--payload", actionPayload("handoff_id", selector)}
	case "reservation_history":
		argv = []string{"omg", "reserve", "history", "--project", projectPlaceholder, "--payload", actionPayload("reservation_id", selector)}
	case "git_cleanup_plan":
		argv = []string{"omg", "git", "cleanup-plan", "--project", projectPlaceholder, "--payload", actionPayload("fingerprint", selector)}
	default:
		return SuggestedActionView{Code: code, Argv: []string{}, Scope: safeSuggestedActionScope(scope)}
	}
	return SuggestedActionView{
		Code:    code,
		Command: actionCommand(argv),
		Argv:    argv,
		Scope:   safeSuggestedActionScope(scope),
	}
}

func actionPayload(key, value string) string {
	encoded, _ := json.Marshal(map[string]string{key: value})
	return string(encoded)
}

func actionCommand(argv []string) string {
	parts := make([]string, len(argv))
	for index, value := range argv {
		switch {
		case value == "<PROJECT_PATH>":
			parts[index] = "'<PROJECT_PATH>'"
		case strings.HasPrefix(value, "{"):
			parts[index] = "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
		default:
			parts[index] = value
		}
	}
	return strings.Join(parts, " ")
}

func safeSuggestedActionScope(scope BoardScope) BoardScope {
	scope.Selector = ""
	if filepath.IsAbs(scope.ProjectID) || windowsAbsolutePath(scope.ProjectID) {
		scope.ProjectID = ""
	}
	if filepath.IsAbs(scope.WorkspaceID) || windowsAbsolutePath(scope.WorkspaceID) {
		scope.WorkspaceID = ""
	}
	return scope
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func (s Service) resolveNativeSessions(ctx context.Context, snapshot *BoardSnapshot, sessions map[string]lineage.AgentSession) error {
	resolved := make(map[string]string, len(snapshot.Sessions)+1)
	resolve := func(identity *IdentityView) error {
		session, ok := sessions[identity.ID]
		if !ok || session.NativeAccessState == lineage.NativeAccessUnsupported {
			return nil
		}
		if state, ok := resolved[identity.ID]; ok {
			identity.NativeAccessState = state
			return nil
		}
		if s.native == nil {
			state := string(lineage.NativeAccessUnsupported)
			resolved[identity.ID] = state
			identity.NativeAccessState = state
			return nil
		}
		resolution, err := s.native.Resolve(ctx, session)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return domain.NewError(domain.CodeConflict, "native session identity could not be verified", false)
		}
		state := string(lineage.NativeAccessUnreadable)
		switch resolution.AccessState {
		case lineage.NativeAccessAvailable, lineage.NativeAccessMissing, lineage.NativeAccessUnreadable, lineage.NativeAccessUnsupported:
			state = string(resolution.AccessState)
		}
		resolved[identity.ID] = state
		identity.NativeAccessState = state
		return nil
	}
	if snapshot.Identity != nil {
		if err := resolve(snapshot.Identity); err != nil {
			return err
		}
	}
	for index := range snapshot.Sessions {
		if err := resolve(&snapshot.Sessions[index]); err != nil {
			return err
		}
	}
	return nil
}

func identityFor(ctx context.Context, c ports.CoordinationRepository, session lineage.AgentSession, project domain.ProjectID) (IdentityView, error) {
	v := IdentityView{ID: string(session.ID), Kind: string(session.Kind), Runtime: safeText(session.Runtime), Role: safeText(session.Role), InstructionSource: string(session.Source), ProvenanceConfidence: "unknown", HumanID: string(session.HumanID), ParentSessionID: string(session.ParentID), RootSessionID: string(session.RootID), ContinuationOfID: string(session.ContinuationOfID), TaskID: string(session.TaskID), WorktreeBound: session.WorktreeRef != "", NativeAccessState: string(session.NativeAccessState), StartedAt: session.StartedAt, Liveness: sessionLiveness(session), HeartbeatAt: session.HeartbeatAt, EndedAt: session.EndedAt, InterruptedAt: session.InterruptedAt}
	if session.WorktreeRef != "" {
		v.WorktreeFingerprint = fingerprint(session.WorktreeRef)
	}
	humanID := session.HumanID
	if root, ok, err := c.GetSession(ctx, session.RootID); err != nil {
		return IdentityView{}, err
	} else if ok && root.ProjectID == lineage.ID(project) {
		v.RootHumanID = string(root.HumanID)
		if humanID == "" {
			humanID = root.HumanID
		}
	}
	if humanID != "" {
		human, ok, err := c.GetHuman(ctx, humanID)
		if err != nil {
			return IdentityView{}, err
		}
		if !ok {
			return IdentityView{}, domain.NewError(domain.CodeNotFound, "referenced human was not found", false)
		}
		v.ProvenanceConfidence = string(human.Confidence)
	}
	if runs, err := c.ListRunsForSession(ctx, lineage.ID(project), session.ID); err != nil {
		return IdentityView{}, err
	} else {
		history := make([]lineage.ID, 0, len(runs)+1)
		if session.TaskID != "" {
			history = append(history, session.TaskID)
		}
		for _, run := range runs {
			if run.TaskID != "" && (len(history) == 0 || history[len(history)-1] != run.TaskID) {
				history = append(history, run.TaskID)
			}
		}
		if len(history) > 0 {
			v.TaskID = string(history[len(history)-1])
		}
		if len(history) > 1 {
			v.PreviousTaskID = string(history[len(history)-2])
		}
	}
	return v, nil
}

func sessionLiveness(session lineage.AgentSession) SessionLiveness {
	if session.HeartbeatAt == nil {
		return SessionLivenessNoSignal
	}
	if session.Liveness == lineage.Stale || session.Liveness == lineage.Interrupted {
		return SessionLivenessStale
	}
	return SessionLivenessAlive
}
func taskView(v lineage.Task) TaskView {
	return TaskView{ID: string(v.ID), DisplayNumber: v.DisplayNumber, Title: safeText(v.Title), State: string(v.State), CreatedBySessionID: string(v.CreatedBySessionID), ClaimedBySessionID: string(v.ClaimedBySessionID), ParentTaskID: string(v.ParentTaskID), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func runView(v lineage.TaskRun) RunView {
	return RunView{ID: string(v.ID), TaskID: string(v.TaskID), SessionID: string(v.SessionID), State: string(v.State), StartedAt: v.StartedAt, EndedAt: v.EndedAt, ParentLostAt: v.ParentLostAt}
}
func progressView(v coordination.Progress) ProgressView {
	return ProgressView{ID: v.ID, TaskID: v.TaskID, RunID: v.RunID, SessionID: v.SessionID, Phase: string(v.Phase), Done: redactList(v.Done), Doing: redactList(v.Doing), Next: redactList(v.Next), CreatedAt: v.CreatedAt}
}
func dependencyView(v coordination.Dependency, tasks map[string]lineage.Task) DependencyView {
	blocker := tasks[v.PrerequisiteTaskID]
	return DependencyView{ID: v.ID, DependentTaskID: v.DependentTaskID, BlockerTaskID: v.PrerequisiteTaskID, Type: string(v.Kind), UnblockOn: string(v.Criterion), Satisfied: coordination.DecideSatisfaction(v, blocker.State).Satisfied}
}
func inboxView(v coordination.MailMessage, d coordination.RecipientDelivery) InboxItemView {
	ack := "delivered"
	if d.ReadAt != nil {
		ack = "read"
	}
	if d.AckedAt != nil {
		ack = "acknowledged"
	}
	return InboxItemView{Recipient: recipientViewKey(d.Recipient), MessageID: v.ID, Type: string(v.Type), Subject: safeText(v.Subject), SenderSessionID: v.SenderSessionID, RelatedTaskID: v.RelatedTaskID, Acknowledgement: ack, ReadAt: d.ReadAt, AcknowledgedAt: d.AckedAt, CreatedAt: v.CreatedAt}
}
func recipientViewKey(v coordination.RecipientTarget) string {
	if v.SessionID != "" {
		return "session:" + v.SessionID
	}
	if v.HumanID != "" {
		return "human:" + v.HumanID
	}
	if v.TaskID != "" {
		return "task:" + v.TaskID
	}
	return "role:" + safeText(v.Role)
}
func handoffDecisionView(v coordination.HandoffDecision) *HandoffDecisionView {
	return &HandoffDecisionView{ID: v.ID, Decision: string(v.Decision), ActorSessionID: v.DecidedBySessionID, CreatedAt: v.CreatedAt}
}
func handoffView(v coordination.Handoff, runState string, decision *HandoffDecisionView, lifecycle []coordination.HandoffLifecycleEvent) HandoffView {
	events := make([]HandoffLifecycleView, 0, len(lifecycle))
	for _, event := range lifecycle {
		events = append(events, HandoffLifecycleView{ID: event.ID, State: string(event.State), ActorSessionID: event.ActorSessionID, SourceCommit: safeText(event.SourceCommit), SourceTree: safeText(event.SourceTree), IntegrationCommit: safeText(event.IntegrationCommit), CanaryRunID: safeText(event.CanaryRunID), CanaryIntegrationRef: safeText(event.CanaryIntegrationRef), CanaryTargetSHA: safeText(event.CanaryTargetSHA), CanaryTargetTree: safeText(event.CanaryTargetTree), CanaryResult: safeText(event.CanaryResult), CanaryCommand: safeText(event.CanaryCommand), CanaryExecutionKind: safeText(event.CanaryExecutionKind), CanaryEnvironmentFingerprint: safeText(event.CanaryEnvironmentFingerprint), CanaryHeadBefore: safeText(event.CanaryHeadBefore), CanaryHeadAfter: safeText(event.CanaryHeadAfter), CanaryRefFingerprintBefore: safeText(event.CanaryRefFingerprintBefore), CanaryRefFingerprintAfter: safeText(event.CanaryRefFingerprintAfter), CanaryExitCode: event.CanaryExitCode, CanaryPassedCount: event.CanaryPassedCount, CanaryFailedCount: event.CanaryFailedCount, CanarySkippedCount: event.CanarySkippedCount, CanaryStartedAt: event.CanaryStartedAt, CanaryFinishedAt: event.CanaryFinishedAt, CanaryEvidencePath: safeText(event.CanaryEvidencePath), SourceWorktreeCleaned: event.SourceWorktreeCleaned, SourceBranchCleaned: event.SourceBranchCleaned, Note: safeText(event.Note), CreatedAt: event.CreatedAt})
	}
	var domainDecision *coordination.HandoffDecision
	if decision != nil {
		domainDecision = &coordination.HandoffDecision{Decision: coordination.HandoffStatus(decision.Decision)}
	}
	return HandoffView{ID: v.ID, TaskID: v.TaskID, RunID: v.RunID, RunState: runState, SourceSessionID: v.SourceSessionID, TargetSessionID: v.TargetSessionID, TargetTaskID: v.TargetTaskID, Summary: safeText(v.Summary), FinalOutputPolicy: string(v.FinalOutput.Policy), FinalOutputHash: safeText(v.FinalOutput.Hash), ChangedFileCount: len(v.ChangedFiles), VerificationItemCount: len(v.VerificationEvidence), Status: string(v.Status), IntegrationState: string(coordination.CurrentIntegrationState(lifecycle, domainDecision)), Lifecycle: events, Decision: decision, CreatedAt: v.CreatedAt}
}
func reservationRelevant(v reservation.Reservation, tasks map[string]lineage.Task, sessions map[string]lineage.AgentSession, mode BoardMode) bool {
	return mode != BoardMe && mode != BoardTask || tasks[v.Owner.TaskID].ID != "" || sessions[v.Owner.SessionID].ID != ""
}
func reservationView(v reservation.Reservation, all []reservation.Reservation, now time.Time) ReservationView {
	conflicts := []string{}
	for _, other := range all {
		if other.ID != v.ID && reservation.Decide(v, other, now).Conflict {
			conflicts = append(conflicts, other.ID)
		}
	}
	return ReservationView{ID: v.ID, SessionID: v.Owner.SessionID, TaskID: v.Owner.TaskID, RunID: v.Owner.RunID, PatternKind: string(v.Pattern.Kind), Pattern: safeReservationPattern(v.Pattern), PatternFingerprint: fingerprint(v.Pattern.Value), CaseSensitivity: string(v.Pattern.CaseSensitivity), Mode: string(v.Mode), Intent: safeText(v.Intent), Lifecycle: string(v.LifecycleAt(now)), ExpiresAt: v.ExpiresAt, ConflictIDs: conflicts}
}
func safeReservationPattern(pattern reservation.Pattern) string {
	normalized, err := reservation.NewPattern(pattern.Kind, pattern.Value, pattern.CaseSensitivity)
	if err != nil {
		return ""
	}
	return safeText(normalized.Value)
}
func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func gitView(v gitdomain.Snapshot) *GitView {
	out := &GitView{ObservationID: v.ID, ObservedAt: v.ObservedAt, Repository: string(v.Observation.Repository), Confidence: string(v.Observation.Confidence), DefaultBranch: safeText(v.Observation.DefaultBranch), Assets: []GitAssetView{}}
	for _, asset := range v.Assets {
		labels := make([]string, len(asset.Classification.Labels))
		for i, label := range asset.Classification.Labels {
			labels[i] = string(label)
		}
		out.Assets = append(out.Assets, GitAssetView{Fingerprint: asset.Fingerprint, Type: string(gitdomain.DeriveAssetType(v.Observation, asset.Asset)), Branch: safeText(asset.Facts.Branch), Head: safeText(asset.Facts.Status.Head), Upstream: safeText(asset.Facts.Status.Upstream), AheadDefault: asset.Facts.DefaultAhead, BehindDefault: asset.Facts.DefaultBehind, AheadUpstream: asset.Facts.Status.Ahead, BehindUpstream: asset.Facts.Status.Behind, TrackedDirty: asset.Facts.Status.TrackedDirty, UntrackedDirty: asset.Facts.Status.Untracked, Classification: labels, Confidence: string(asset.Classification.Confidence), OwnerState: string(asset.Facts.Owner.State), OwnerSessionID: string(asset.OwnerSessionID), OwnerTaskID: string(asset.OwnerTaskID)})
	}
	return out
}
func filterGitAssets(assets []GitAssetView, sessions map[string]lineage.AgentSession, tasks map[string]lineage.Task) []GitAssetView {
	out := make([]GitAssetView, 0, len(assets))
	for _, asset := range assets {
		if sessions[asset.OwnerSessionID].ID != "" || tasks[asset.OwnerTaskID].ID != "" {
			out = append(out, asset)
		}
	}
	return out
}
func annotateIdentityGit(snapshot *BoardSnapshot, sessions map[string]lineage.AgentSession, git gitdomain.Snapshot) {
	annotateIdentityGitForOS(snapshot, sessions, git, runtime.GOOS)
}

func annotateIdentityGitForOS(snapshot *BoardSnapshot, sessions map[string]lineage.AgentSession, git gitdomain.Snapshot, goos string) {
	worktrees := make(map[string]string, len(sessions))
	for id, session := range sessions {
		if path, ok := canonicalWorktreeRefForOS(session.WorktreeRef, goos); ok {
			worktrees[id] = path
		}
	}
	branches := make(map[string]string, len(git.Assets))
	for _, asset := range git.Assets {
		worktree, owned := worktrees[string(asset.OwnerSessionID)]
		assetPath, canonical := canonicalAssetWorktreeRefForOS(asset.Asset, goos)
		if owned && canonical && assetPath == worktree {
			branches[string(asset.OwnerSessionID)] = safeText(asset.Facts.Branch)
		}
	}
	annotate := func(identity *IdentityView) {
		if identity != nil {
			identity.Branch = branches[identity.ID]
		}
	}
	annotate(snapshot.Identity)
	for i := range snapshot.Sessions {
		if sessions[snapshot.Sessions[i].ID].ID != "" {
			annotate(&snapshot.Sessions[i])
		}
	}
}

func canonicalAssetWorktreeRef(asset gitdomain.Asset) (string, bool) {
	return canonicalAssetWorktreeRefForOS(asset, runtime.GOOS)
}

func canonicalAssetWorktreeRefForOS(asset gitdomain.Asset, goos string) (string, bool) {
	path := asset.Facts.WorktreePath
	if path == "" {
		path = asset.Worktree.Path
	}
	return canonicalWorktreeRefForOS(path, goos)
}

func canonicalWorktreeRef(path string) (string, bool) {
	return canonicalWorktreeRefForOS(path, runtime.GOOS)
}

func canonicalWorktreeRefForOS(path, goos string) (string, bool) {
	if path == "" {
		return "", false
	}
	if goos == "windows" {
		path = strings.ReplaceAll(path, "/", `\`)
		if !windowsAbsolutePath(path) {
			return "", false
		}
		return strings.ToLower(filepath.Clean(path)), true
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", false
	}
	return path, true
}

func windowsAbsolutePath(path string) bool {
	if runtime.GOOS == "windows" {
		return filepath.IsAbs(path)
	}
	return len(path) >= 3 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':' && path[2] == '\\'
}

func safeText(value string) string { return safety.SafeText(value) }
func redactList(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = safeText(v)
	}
	return out
}
func canonicalize(v *BoardSnapshot) {
	sort.Slice(v.Sessions, func(i, j int) bool { return v.Sessions[i].ID < v.Sessions[j].ID })
	sort.Slice(v.Tasks, func(i, j int) bool {
		if v.Tasks[i].DisplayNumber == v.Tasks[j].DisplayNumber {
			return v.Tasks[i].ID < v.Tasks[j].ID
		}
		return v.Tasks[i].DisplayNumber < v.Tasks[j].DisplayNumber
	})
	sort.Slice(v.Runs, func(i, j int) bool { return v.Runs[i].ID < v.Runs[j].ID })
	sort.Slice(v.Progress, func(i, j int) bool {
		if v.Progress[i].CreatedAt.Equal(v.Progress[j].CreatedAt) {
			return v.Progress[i].ID < v.Progress[j].ID
		}
		return v.Progress[i].CreatedAt.Before(v.Progress[j].CreatedAt)
	})
	sort.Slice(v.Dependencies, func(i, j int) bool { return v.Dependencies[i].ID < v.Dependencies[j].ID })
	sort.Slice(v.Inbox, func(i, j int) bool {
		if v.Inbox[i].MessageID == v.Inbox[j].MessageID {
			return v.Inbox[i].Recipient < v.Inbox[j].Recipient
		}
		return v.Inbox[i].MessageID < v.Inbox[j].MessageID
	})
	sort.Slice(v.Handoffs, func(i, j int) bool { return v.Handoffs[i].ID < v.Handoffs[j].ID })
	sort.Slice(v.Reservations, func(i, j int) bool { return v.Reservations[i].ID < v.Reservations[j].ID })
	for i := range v.Reservations {
		sort.Strings(v.Reservations[i].ConflictIDs)
	}
	sort.Strings(v.Warnings)
	if v.Git != nil {
		sort.Slice(v.Git.Assets, func(i, j int) bool { return v.Git.Assets[i].Fingerprint < v.Git.Assets[j].Fingerprint })
		for i := range v.Git.Assets {
			sort.Strings(v.Git.Assets[i].Classification)
		}
	}
}
