// Package git inspects live Git state and optionally persists explicit
// point-in-time observations as advisory evidence.
package git

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/safety"
)

type Service struct {
	store    ports.Store
	scanner  ports.Scanner
	verifier ports.GitVerifier
	now      func() time.Time
}

func New(store ports.Store, scanner ports.Scanner, now func() time.Time) *Service {
	return NewWithVerifier(store, scanner, nil, now)
}

func NewWithVerifier(store ports.Store, scanner ports.Scanner, verifier ports.GitVerifier, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, scanner: scanner, verifier: verifier, now: now}
}

type ScanRequest struct {
	ProjectID                domain.ProjectID
	SessionID, TaskID, RunID lineage.ID
	Directory                string
}
type ScanSummary struct {
	ObservationID   string            `json:"observation_id"`
	Hash            string            `json:"hash"`
	RepositoryState gitobs.RepoState  `json:"repository_state"`
	Confidence      gitobs.Confidence `json:"confidence"`
	AssetCount      int               `json:"asset_count"`
}

type HandoffReconcileItem struct {
	HandoffID         string                    `json:"handoff_id"`
	TaskID            string                    `json:"task_id"`
	LifecycleState    coord.IntegrationState    `json:"lifecycle_state"`
	SourceCommit      string                    `json:"source_commit,omitempty"`
	SourceTree        string                    `json:"source_tree,omitempty"`
	IntegrationCommit string                    `json:"integration_commit,omitempty"`
	Evidence          *gitobs.ReconcileEvidence `json:"evidence,omitempty"`
	Ready             bool                      `json:"ready"`
	Blockers          []string                  `json:"blockers"`
}

type ReconcileReport struct {
	IntegrationRef  string                  `json:"integration_ref"`
	IntegrationHead gitobs.RevisionEvidence `json:"integration_head"`
	Items           []HandoffReconcileItem  `json:"items"`
	ReadyCount      int                     `json:"ready_count"`
	BlockedCount    int                     `json:"blocked_count"`
}

type OrphanRisk struct {
	Code              string `json:"code"`
	Severity          string `json:"severity"`
	HandoffID         string `json:"handoff_id,omitempty"`
	Fingerprint       string `json:"fingerprint,omitempty"`
	Worktree          string `json:"worktree,omitempty"`
	Branch            string `json:"branch,omitempty"`
	Head              string `json:"head,omitempty"`
	RelatedTask       string `json:"related_task,omitempty"`
	LastOwner         string `json:"last_owner,omitempty"`
	RecommendedAction string `json:"recommended_action"`
	Reason            string `json:"reason"`
}

type OrphanReport struct {
	Count int          `json:"count"`
	Risks []OrphanRisk `json:"risks"`
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid git scan request", false)
}
func missing() error {
	return domain.NewError(domain.CodeNotFound, "git scan actor lineage not found", false)
}
func unavailable() error {
	return domain.NewError(domain.CodeUnavailable, "git observation unavailable", true)
}
func id() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return "git-observation-" + hex.EncodeToString(b[:]), nil
}

// ReconcileCurrent overlays canonical lineage and Git adoption facts onto an
// immutable observed snapshot. It performs no scan and persists no changes.
// Read-only projections, including boards, share this function with Current.
func ReconcileCurrent(ctx context.Context, r ports.Repositories, snapshot gitobs.Snapshot) (gitobs.Snapshot, error) {
	sessions, err := r.Coordination().ListSessions(ctx, lineage.ID(snapshot.ProjectID))
	if err != nil {
		return snapshot, err
	}
	candidates := make([]gitobs.OwnershipCandidate, 0, len(sessions))
	for _, session := range sessions {
		runs, err := r.Coordination().ListRunsForSession(ctx, session.ProjectID, session.ID)
		if err != nil {
			return snapshot, err
		}
		var selected lineage.TaskRun
		foundRun := false
		for _, run := range runs {
			if run.SessionID != session.ID {
				continue
			}
			if !foundRun || run.StartedAt.After(selected.StartedAt) || (run.StartedAt.Equal(selected.StartedAt) && run.ID > selected.ID) {
				selected, foundRun = run, true
			}
		}
		if !foundRun || selected.TaskID == "" {
			continue
		}
		task, ok, err := r.Coordination().GetTask(ctx, selected.TaskID)
		if err != nil {
			return snapshot, err
		}
		if !ok || task.ProjectID != session.ProjectID {
			continue
		}
		candidateSession := session
		candidateSession.TaskID = selected.TaskID
		candidates = append(candidates, gitobs.OwnershipCandidate{Session: candidateSession, Task: task, Run: selected})
	}
	adoptions := make([]coord.Adoption, 0, len(snapshot.Observation.Assets))
	for _, asset := range snapshot.Observation.Assets {
		adoption, ok, err := r.Coordination().LatestGitAdoption(ctx, string(snapshot.ProjectID), asset.StableFingerprint())
		if err != nil {
			return snapshot, err
		}
		if ok {
			adoptions = append(adoptions, adoption)
		}
	}
	observation, assets := gitobs.ReconcileOwnership(snapshot.Observation, candidates, adoptions)
	previous := make(map[string]gitobs.AssetRecord, len(snapshot.Assets))
	for _, record := range snapshot.Assets {
		previous[record.Fingerprint] = record
	}
	for i := range assets {
		if record, ok := previous[assets[i].Fingerprint]; ok {
			assets[i].FirstSeenAt, assets[i].LastSeenAt = record.FirstSeenAt, record.LastSeenAt
		}
	}
	snapshot.Observation, snapshot.Assets = observation, assets
	return snapshot, nil
}

// Scan runs Git before the transaction, then optionally validates complete
// actor lineage and commits one immutable observation with a safe receipt summary.
func (s *Service) Scan(ctx context.Context, key domain.IdempotencyKey, q ScanRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil {
		return domain.Result{}, invalid()
	}
	hasAttribution := q.SessionID != "" || q.TaskID != "" || q.RunID != ""
	if s == nil || s.store == nil || s.scanner == nil || key == "" || q.ProjectID == "" || q.Directory == "" || (hasAttribution && (q.SessionID == "" || q.TaskID == "" || q.RunID == "")) {
		return domain.Result{}, invalid()
	}
	observed, e := s.scanner.Scan(ctx, q.Directory)
	if e != nil {
		return domain.Result{}, unavailable()
	}
	if safety.RejectPrefixed(key, q, observed) != nil {
		return domain.Result{}, invalid()
	}
	if observed.Revision != gitobs.ObservationRevision {
		return domain.Result{}, unavailable()
	}
	oid, e := id()
	if e != nil {
		return domain.Result{}, unavailable()
	}
	at := s.now().UTC()
	_, out, e := s.store.Write(ctx, key, "git.inventory", func(r ports.Repositories) (domain.Result, error) {
		if hasAttribution {
			c := r.Coordination()
			session, ok, e := c.GetSession(ctx, q.SessionID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || session.ProjectID != lineage.ID(q.ProjectID) {
				return domain.Result{}, missing()
			}
			task, ok, e := c.GetTask(ctx, q.TaskID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || task.ProjectID != lineage.ID(q.ProjectID) {
				return domain.Result{}, missing()
			}
			run, ok, e := c.GetRun(ctx, q.RunID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || run.TaskID != q.TaskID || run.SessionID != q.SessionID {
				return domain.Result{}, missing()
			}
		}
		pending, e := ReconcileCurrent(ctx, r, gitobs.Snapshot{ID: oid, ProjectID: q.ProjectID, IdempotencyKey: key, ActorSessionID: q.SessionID, TaskID: q.TaskID, RunID: q.RunID, Trigger: "scan", ObservedAt: at, Observation: observed})
		if e != nil {
			return domain.Result{}, e
		}
		snapshot, e := r.Git().CreateSnapshot(ctx, pending)
		if e != nil {
			return domain.Result{}, e
		}
		summary := ScanSummary{ObservationID: snapshot.ID, Hash: snapshot.Observation.Hash, RepositoryState: snapshot.Observation.Repository, Confidence: snapshot.Observation.Confidence, AssetCount: len(snapshot.Assets)}
		return domain.Result{ID: domain.ResultID(snapshot.ID), Outcome: domain.OutcomeOK, Data: summary}, nil
	})
	if e != nil {
		return domain.Result{}, mapErr(e)
	}
	return out, nil
}
func mapErr(e error) error {
	if e == nil {
		return nil
	}
	var d domain.DomainError
	if errors.As(e, &d) {
		return d
	}
	return unavailable()
}

func (s *Service) Get(ctx context.Context, project domain.ProjectID, id string) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" || id == "" {
		return gitobs.Snapshot{}, invalid()
	}
	var out gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var err error
		out, ok, err = r.Git().GetSnapshot(ctx, project, id)
		if err != nil {
			return err
		}
		if !ok {
			return missing()
		}
		return nil
	})
	return out, mapErr(err)
}
func (s *Service) Latest(ctx context.Context, project domain.ProjectID) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" {
		return gitobs.Snapshot{}, invalid()
	}
	var out gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var err error
		out, ok, err = r.Git().LatestSnapshot(ctx, project)
		if err != nil {
			return err
		}
		if !ok {
			return missing()
		}
		return nil
	})
	return out, mapErr(err)
}

// RecordedCurrent overlays the latest durable observation with current OMG
// ownership facts. It does not inspect or mutate Git and exists for historical
// evidence views; callers that need repository truth must use Inspect.
func (s *Service) RecordedCurrent(ctx context.Context, project domain.ProjectID) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" {
		return gitobs.Snapshot{}, invalid()
	}
	var out gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		snapshot, ok, err := r.Git().LatestSnapshot(ctx, project)
		if err != nil {
			return err
		}
		if !ok {
			return missing()
		}
		out, err = ReconcileCurrent(ctx, r, snapshot)
		return err
	})
	return out, mapErr(err)
}

// Inspect reads the selected repository directly from Git, overlays OMG's
// coordination-only ownership facts, and returns the result without creating
// a Git observation, audit event, or command receipt. Git remains authoritative
// for repository state; Scan is only for explicit point-in-time evidence.
func (s *Service) Inspect(ctx context.Context, project domain.ProjectID, directory string) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || s.scanner == nil || project == "" || directory == "" {
		return gitobs.Snapshot{}, invalid()
	}
	observed, err := s.scanner.Scan(ctx, directory)
	if err != nil || observed.Revision != gitobs.ObservationRevision {
		return gitobs.Snapshot{}, unavailable()
	}
	at := s.now().UTC()
	// Keep live identity tied only to Git facts. OMG ownership overlays may change
	// independently and must not masquerade as a different repository state.
	digest := sha256.Sum256([]byte(observed.Hash))
	snapshot := gitobs.Snapshot{
		ID:          "git-live-" + hex.EncodeToString(digest[:16]),
		ProjectID:   project,
		Trigger:     "live",
		ObservedAt:  at,
		Observation: observed,
	}
	err = s.store.Read(ctx, func(r ports.Repositories) error {
		var reconcileErr error
		snapshot, reconcileErr = ReconcileCurrent(ctx, r, snapshot)
		return reconcileErr
	})
	if err != nil {
		return gitobs.Snapshot{}, mapErr(err)
	}
	for index := range snapshot.Assets {
		snapshot.Assets[index].FirstSeenAt = at
		snapshot.Assets[index].LastSeenAt = at
	}
	return snapshot, nil
}

// CleanupPlan derives advisory candidates from a fresh, non-persisted Git
// inspection. It contains no cleanup application capability.
func (s *Service) CleanupPlan(ctx context.Context, project domain.ProjectID, directory, fingerprint string) (gitobs.CleanupPlan, error) {
	current, err := s.Inspect(ctx, project, directory)
	if err != nil {
		return gitobs.CleanupPlan{}, err
	}
	facts := make([]gitobs.AssetFacts, 0, len(current.Assets))
	if fingerprint != "" {
		found := false
		for _, asset := range current.Assets {
			if asset.Fingerprint == fingerprint {
				facts = append(facts, asset.Facts)
				found = true
				break
			}
		}
		if !found {
			return gitobs.CleanupPlan{}, missing()
		}
	} else {
		for _, asset := range current.Assets {
			facts = append(facts, asset.Facts)
		}
	}
	return gitobs.Plan(facts), nil
}
func (s *Service) History(ctx context.Context, project domain.ProjectID) ([]gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" {
		return nil, invalid()
	}
	var out []gitobs.Snapshot
	err := s.store.Read(ctx, func(r ports.Repositories) error { var err error; out, err = r.Git().History(ctx, project); return err })
	return out, mapErr(err)
}
func (s *Service) Diff(ctx context.Context, project domain.ProjectID, before, after string) (gitobs.Diff, error) {
	left, err := s.Get(ctx, project, before)
	if err != nil {
		return gitobs.Diff{}, err
	}
	right, err := s.Get(ctx, project, after)
	if err != nil {
		return gitobs.Diff{}, err
	}
	return gitobs.ExactDiff(left.Observation, right.Observation), nil
}

// Reconcile verifies canonical handoff evidence against the actual selected
// repository. It is read-only and treats failures as per-handoff blockers so
// one stale branch cannot hide the rest of the queue.
func (s *Service) Reconcile(ctx context.Context, project domain.ProjectID, directory, integrationRef string) (ReconcileReport, error) {
	if s == nil || s.store == nil || s.verifier == nil || project == "" || directory == "" || integrationRef == "" {
		return ReconcileReport{}, invalid()
	}
	head, err := s.verifier.ResolveRevision(ctx, directory, integrationRef)
	if err != nil {
		return ReconcileReport{}, unavailable()
	}
	type record struct {
		handoff  coord.Handoff
		events   []coord.HandoffLifecycleEvent
		decision *coord.HandoffDecision
	}
	records := []record{}
	err = s.store.Read(ctx, func(r ports.Repositories) error {
		tasks, readErr := r.Coordination().ListTasks(ctx, project)
		if readErr != nil {
			return readErr
		}
		for _, task := range tasks {
			handoffs, readErr := r.Coordination().ListHandoffs(ctx, string(task.ID))
			if readErr != nil {
				return readErr
			}
			for _, handoff := range handoffs {
				events, readErr := r.Coordination().ListHandoffLifecycleEvents(ctx, handoff.ID)
				if readErr != nil {
					return readErr
				}
				decision, ok, readErr := r.Coordination().GetHandoffDecision(ctx, handoff.ID)
				if readErr != nil {
					return readErr
				}
				var decisionPtr *coord.HandoffDecision
				if ok {
					copy := decision
					decisionPtr = &copy
				}
				records = append(records, record{handoff: handoff, events: events, decision: decisionPtr})
			}
		}
		return nil
	})
	if err != nil {
		return ReconcileReport{}, mapErr(err)
	}
	report := ReconcileReport{IntegrationRef: integrationRef, IntegrationHead: head, Items: make([]HandoffReconcileItem, 0, len(records))}
	for _, record := range records {
		handoff := record.handoff
		item := HandoffReconcileItem{HandoffID: handoff.ID, TaskID: handoff.TaskID, SourceCommit: handoff.SourceCommit, SourceTree: handoff.SourceTree, LifecycleState: coord.CurrentIntegrationState(record.events, record.decision), Blockers: []string{}}
		for _, event := range record.events {
			if event.IntegrationCommit != "" {
				item.IntegrationCommit = event.IntegrationCommit
			}
		}
		if handoff.SourceCommit == "" || handoff.SourceTree == "" {
			item.Blockers = append(item.Blockers, "missing_source_commit_or_tree")
		}
		if item.IntegrationCommit == "" {
			item.Blockers = append(item.Blockers, "missing_integration_commit")
		}
		if len(item.Blockers) == 0 {
			evidence, verifyErr := s.verifier.Reconcile(ctx, directory, handoff.SourceCommit, handoff.SourceTree, item.IntegrationCommit, integrationRef)
			if verifyErr != nil {
				item.Blockers = append(item.Blockers, "git_object_or_relationship_unavailable")
			} else {
				item.Evidence = &evidence
				if !evidence.SourceTreeMatches {
					item.Blockers = append(item.Blockers, "source_tree_mismatch")
				}
				if !evidence.Reflected {
					item.Blockers = append(item.Blockers, "source_not_reflected")
				}
				if !evidence.IntegrationRetained {
					item.Blockers = append(item.Blockers, "integration_commit_not_in_current_head")
				}
			}
		}
		item.Ready = len(item.Blockers) == 0
		if item.Ready {
			report.ReadyCount++
		} else {
			report.BlockedCount++
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

// OrphanScan combines a fresh read-only Git scan with reconciliation results.
// It never deletes a branch or worktree.
func (s *Service) OrphanScan(ctx context.Context, project domain.ProjectID, directory, integrationRef string) (OrphanReport, error) {
	if s == nil || s.scanner == nil {
		return OrphanReport{}, unavailable()
	}
	observation, err := s.scanner.Scan(ctx, directory)
	if err != nil {
		return OrphanReport{}, unavailable()
	}
	reconcile, err := s.Reconcile(ctx, project, directory, integrationRef)
	if err != nil {
		return OrphanReport{}, err
	}
	risks := []OrphanRisk{}
	owners := map[string]gitobs.AssetRecord{}
	if snapshot, snapshotErr := s.reconcileObserved(ctx, project, observation); snapshotErr == nil {
		for _, record := range snapshot.Assets {
			owners[record.Fingerprint] = record
		}
	}
	knownSources := map[string]bool{}
	for _, item := range reconcile.Items {
		if item.SourceCommit != "" {
			knownSources[item.SourceCommit] = true
		}
		if !item.Ready && item.SourceCommit != "" {
			risks = append(risks, OrphanRisk{Code: "handoff_not_reconciled", Severity: "high", HandoffID: item.HandoffID, Head: item.SourceCommit, RelatedTask: item.TaskID, RecommendedAction: "inspect", Reason: strings.Join(item.Blockers, ",")})
		}
	}
	for _, asset := range observation.Assets {
		fingerprint := asset.StableFingerprint()
		owner := owners[fingerprint]
		branch, head := asset.Facts.Branch, asset.Facts.Status.Head
		if head == "" {
			head = asset.Worktree.Head
		}
		if asset.Facts.Status.TrackedDirty+asset.Facts.Status.Untracked > 0 && asset.Facts.Owner.State == gitobs.OwnerUnknown {
			risks = append(risks, OrphanRisk{Code: "dirty_unowned", Severity: "high", Fingerprint: fingerprint, Worktree: asset.Facts.WorktreePath, Branch: branch, Head: head, RelatedTask: string(owner.OwnerTaskID), LastOwner: string(owner.OwnerSessionID), RecommendedAction: "inspect", Reason: "dirty worktree has no canonical owner"})
		}
		if !asset.Facts.DefaultBranch && asset.Facts.Fingerprint.DefaultCountsKnown && asset.Facts.DefaultAhead > 0 && !knownSources[head] {
			risks = append(risks, OrphanRisk{Code: "untracked_completed_branch", Severity: "medium", Fingerprint: fingerprint, Worktree: asset.Facts.WorktreePath, Branch: branch, Head: head, RelatedTask: string(owner.OwnerTaskID), LastOwner: string(owner.OwnerSessionID), RecommendedAction: "reconcile", Reason: "branch is ahead of default without matching handoff source evidence"})
		}
	}
	return OrphanReport{Count: len(risks), Risks: risks}, nil
}

func (s *Service) reconcileObserved(ctx context.Context, project domain.ProjectID, observation gitobs.Observation) (gitobs.Snapshot, error) {
	if s == nil || s.store == nil || project == "" || observation.Revision != gitobs.ObservationRevision {
		return gitobs.Snapshot{}, invalid()
	}
	snapshot := gitobs.Snapshot{ProjectID: project, Trigger: "live", ObservedAt: s.now().UTC(), Observation: observation}
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var reconcileErr error
		snapshot, reconcileErr = ReconcileCurrent(ctx, r, snapshot)
		return reconcileErr
	})
	if err != nil {
		return gitobs.Snapshot{}, mapErr(err)
	}
	return snapshot, nil
}
