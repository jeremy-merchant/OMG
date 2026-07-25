package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"sort"
	"time"
)

//go:embed migrations/0004_git_assets.sql
var gitInventoryFS embed.FS
var gitInventorySQL = mustGitInventorySQL()

//go:embed migrations/0005_scoped_receipts_audit.sql
var scopedAuditMigrationFS embed.FS
var scopedAuditSQL = mustScopedAuditSQL()

//go:embed migrations/0006_scoped_humans.sql
var scopedHumansMigrationFS embed.FS
var scopedHumansSQL = mustScopedHumansSQL()

//go:embed migrations/0007_receipt_operation.sql
var receiptOperationMigrationFS embed.FS
var receiptOperationSQL = mustReceiptOperationSQL()

//go:embed migrations/0008_legacy_human_associations.sql
var legacyHumanAssociationsMigrationFS embed.FS
var legacyHumanAssociationsSQL = mustLegacyHumanAssociationsSQL()

func mustLegacyHumanAssociationsSQL() string {
	b, e := legacyHumanAssociationsMigrationFS.ReadFile("migrations/0008_legacy_human_associations.sql")
	if e != nil {
		panic(e)
	}
	return string(b)
}

func mustReceiptOperationSQL() string {
	b, e := receiptOperationMigrationFS.ReadFile("migrations/0007_receipt_operation.sql")
	if e != nil {
		panic(e)
	}
	return string(b)
}

func mustScopedHumansSQL() string {
	b, e := scopedHumansMigrationFS.ReadFile("migrations/0006_scoped_humans.sql")
	if e != nil {
		panic(e)
	}
	return string(b)
}

func mustScopedAuditSQL() string {
	b, e := scopedAuditMigrationFS.ReadFile("migrations/0005_scoped_receipts_audit.sql")
	if e != nil {
		panic(e)
	}
	return string(b)
}

func mustGitInventorySQL() string {
	b, e := gitInventoryFS.ReadFile("migrations/0004_git_assets.sql")
	if e != nil {
		panic(e)
	}
	return string(b)
}

type gitRepository struct {
	tx      *sql.Tx
	project domain.ProjectID
}

func (r repositories) Git() ports.GitRepository { return gitRepository{tx: r.tx, project: r.project} }

func (r gitRepository) acceptsProject(project domain.ProjectID) bool {
	return r.project == "legacy" || project == r.project
}

func (r gitRepository) LatestSequence(ctx context.Context, projectID domain.ProjectID) (int64, error) {
	if !r.acceptsProject(projectID) {
		return 0, errors.New("project mismatch")
	}
	var sequence int64
	if err := r.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_no), 0) FROM git_observations WHERE project_id=?`, projectID).Scan(&sequence); err != nil {
		return 0, err
	}
	return sequence, nil
}
func fixedUTC(t time.Time) (string, error) {
	if t.IsZero() {
		return "", errors.New("zero time")
	}
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z"), nil
}
func parseUTC(v string) (time.Time, error) {
	if len(v) != 30 {
		return time.Time{}, errors.New("invalid stored timestamp")
	}
	t, e := time.Parse("2006-01-02T15:04:05.000000000Z", v)
	if e != nil {
		return time.Time{}, errors.New("invalid stored timestamp")
	}
	return t, nil
}
func b(v bool) int {
	if v {
		return 1
	}
	return 0
}
func storedBool(v int) (bool, error) {
	if v != 0 && v != 1 {
		return false, errors.New("invalid stored boolean")
	}
	return v == 1, nil
}
func (r gitRepository) validActor(c context.Context, s git.Snapshot) error {
	hasAttribution := s.ActorSessionID != "" || s.TaskID != "" || s.RunID != ""
	if !hasAttribution {
		return nil
	}
	if s.ActorSessionID == "" || s.TaskID == "" || s.RunID == "" {
		return errors.New("incomplete stored git actor lineage")
	}
	var ok int
	e := r.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM agent_sessions a JOIN tasks t ON t.id=? JOIN task_runs r ON r.id=? WHERE a.id=? AND a.project_id=? AND t.project_id=? AND r.task_id=t.id AND r.session_id=a.id)`, s.TaskID, s.RunID, s.ActorSessionID, s.ProjectID, s.ProjectID).Scan(&ok)
	if e != nil {
		return e
	}
	if ok != 1 {
		return errors.New("invalid stored git actor lineage")
	}
	return nil
}
func (r gitRepository) validOwner(c context.Context, observationID string, registered bool, state git.OwnerState, session, task, run sql.NullString) error {
	if !registered {
		if state != git.OwnerUnknown || session.Valid || task.Valid || run.Valid {
			return errors.New("invalid stored git owner")
		}
		return nil
	}
	if !session.Valid || !task.Valid || !run.Valid {
		return errors.New("registered git asset owner requires complete lineage")
	}
	var ok int
	e := r.tx.QueryRowContext(c, `SELECT EXISTS(SELECT 1 FROM git_observations o WHERE o.id=? AND (? IS NULL OR EXISTS(SELECT 1 FROM agent_sessions s WHERE s.id=? AND s.project_id=o.project_id)) AND (? IS NULL OR EXISTS(SELECT 1 FROM tasks t WHERE t.id=? AND t.project_id=o.project_id)) AND (? IS NULL OR EXISTS(SELECT 1 FROM task_runs r JOIN tasks rt ON rt.id=r.task_id JOIN agent_sessions rs ON rs.id=r.session_id WHERE r.id=? AND rt.project_id=o.project_id AND rs.project_id=o.project_id AND (? IS NULL OR r.task_id=?) AND (? IS NULL OR r.session_id=?))))`, observationID, nullable(session), session.String, nullable(task), task.String, nullable(run), run.String, nullable(task), task.String, nullable(session), session.String).Scan(&ok)
	if e != nil {
		return e
	}
	if ok != 1 {
		return errors.New("invalid stored git owner lineage")
	}
	return nil
}
func nullable(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
func nullableID(v lineage.ID) any {
	if v == "" {
		return nil
	}
	return string(v)
}
func sameStoredAsset(left, right git.Asset) bool {
	if left.Facts != right.Facts || left.Worktree != right.Worktree || left.Classification.Confidence != right.Classification.Confidence || len(left.Classification.Labels) != len(right.Classification.Labels) {
		return false
	}
	for i := range left.Classification.Labels {
		if left.Classification.Labels[i] != right.Classification.Labels[i] {
			return false
		}
	}
	return true
}

func (r gitRepository) CreateSnapshot(c context.Context, s git.Snapshot) (git.Snapshot, error) {
	if !r.acceptsProject(s.ProjectID) {
		return git.Snapshot{}, errors.New("project mismatch")
	}
	hasAttribution := s.ActorSessionID != "" || s.TaskID != "" || s.RunID != ""
	if s.ID == "" || s.ProjectID == "" || s.IdempotencyKey == "" || (hasAttribution && (s.ActorSessionID == "" || s.TaskID == "" || s.RunID == "")) || s.Trigger != "scan" || !validObservation(s.Observation) || s.Observation.Hash != git.HashObservation(s.Observation) || len(s.Observation.Assets) > 10000 {
		return git.Snapshot{}, errors.New("invalid git observation")
	}
	if old, ok, err := r.byKey(c, s.ProjectID, s.IdempotencyKey); err != nil {
		return git.Snapshot{}, err
	} else if ok {
		return old, nil
	}
	at, err := fixedUTC(s.ObservedAt)
	if err != nil {
		return git.Snapshot{}, err
	}
	if err = r.tx.QueryRowContext(c, `SELECT COALESCE(MAX(sequence_no),0)+1 FROM git_observations WHERE project_id=?`, s.ProjectID).Scan(&s.SequenceNo); err != nil {
		return git.Snapshot{}, err
	}
	if _, err = r.tx.ExecContext(c, `INSERT INTO git_observations VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.ID, s.ProjectID, s.IdempotencyKey, nullableID(s.ActorSessionID), nullableID(s.TaskID), nullableID(s.RunID), s.Trigger, at, s.Observation.Revision, s.Observation.Hash, s.Observation.Repository, s.Observation.Confidence, s.Observation.CommonDir, s.Observation.TopLevel, s.Observation.DefaultBranch, s.SequenceNo); err != nil {
		return git.Snapshot{}, err
	}
	owners := make(map[string]git.AssetRecord, len(s.Assets))
	for _, record := range s.Assets {
		if record.Fingerprint == "" || record.Fingerprint != record.Asset.StableFingerprint() {
			return git.Snapshot{}, errors.New("invalid git asset owner")
		}
		if _, exists := owners[record.Fingerprint]; exists {
			return git.Snapshot{}, errors.New("asset fingerprint collision")
		}
		owners[record.Fingerprint] = record
	}
	seen := map[string]struct{}{}
	for i, asset := range s.Observation.Assets {
		fingerprint := asset.StableFingerprint()
		if _, ok := seen[fingerprint]; ok {
			return git.Snapshot{}, errors.New("asset fingerprint collision")
		}
		seen[fingerprint] = struct{}{}
		record, hasOwner := owners[fingerprint]
		if len(owners) != 0 && !hasOwner {
			return git.Snapshot{}, errors.New("missing git asset owner")
		}
		if hasOwner && !sameStoredAsset(record.Asset, asset) {
			return git.Snapshot{}, errors.New("git asset owner mismatch")
		}
		first := at
		var previous string
		if err := r.tx.QueryRowContext(c, `SELECT a.first_seen_at FROM git_observation_assets a JOIN git_observations o ON o.id=a.observation_id WHERE o.project_id=? AND a.fingerprint=? ORDER BY o.sequence_no LIMIT 1`, s.ProjectID, fingerprint).Scan(&previous); err == nil {
			first = previous
		} else if !errors.Is(err, sql.ErrNoRows) {
			return git.Snapshot{}, err
		}
		labels, err := json.Marshal(asset.Classification.Labels)
		if err != nil {
			return git.Snapshot{}, err
		}
		f, status, worktree := asset.Facts, asset.Facts.Status, asset.Worktree
		if err := r.validOwner(c, s.ID, f.Owner.Registered, f.Owner.State, sql.NullString{String: string(record.OwnerSessionID), Valid: record.OwnerSessionID != ""}, sql.NullString{String: string(record.OwnerTaskID), Valid: record.OwnerTaskID != ""}, sql.NullString{String: string(record.OwnerRunID), Valid: record.OwnerRunID != ""}); err != nil {
			return git.Snapshot{}, err
		}
		_, err = r.tx.ExecContext(c, `INSERT INTO git_observation_assets VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("%s-%03d", s.ID, i), s.ID, fingerprint, git.DeriveAssetType(s.Observation, asset), first, at, f.Confidence, f.WorktreePath, f.Branch, b(f.Detached), b(f.BranchOnly), b(f.DefaultBranch), b(f.WorktreePrunable), status.Branch, status.Head, b(status.Detached), status.Upstream, status.Ahead, status.Behind, status.TrackedDirty, status.Untracked, status.Ignored, status.Confidence, b(f.Fingerprint.MergeBaseKnown), b(f.Fingerprint.MergeBaseEqualsHead), b(f.Fingerprint.DefaultCountsKnown), f.DefaultAhead, f.DefaultBehind, b(f.Owner.Registered), f.Owner.State, worktree.Path, worktree.Head, worktree.Branch, b(worktree.Detached), b(worktree.Bare), b(worktree.Locked), b(worktree.Prunable), worktree.PruneReason, labels, asset.Classification.Confidence, nullableID(record.OwnerSessionID), nullableID(record.OwnerTaskID), nullableID(record.OwnerRunID))
		if err != nil {
			return git.Snapshot{}, err
		}
	}
	if len(owners) != 0 && len(owners) != len(seen) {
		return git.Snapshot{}, errors.New("extra git asset owner")
	}
	return r.get(c, s.ProjectID, s.ID)
}
func validObservation(o git.Observation) bool {
	if o.Revision != git.ObservationRevision || !repo(o.Repository) || !conf(o.Confidence) || len(o.CommonDir) > 4096 || len(o.TopLevel) > 4096 || len(o.DefaultBranch) > 1024 {
		return false
	}
	for _, a := range o.Assets {
		f, st, w := a.Facts, a.Facts.Status, a.Worktree
		if !assetIdentity(a) || !uniqueLabels(a.Classification.Labels) || !conf(f.Confidence) || !conf(st.Confidence) || !conf(a.Classification.Confidence) || !owner(f.Owner.State) || anyNegative(st.Ahead, st.Behind, st.TrackedDirty, st.Untracked, st.Ignored, f.DefaultAhead, f.DefaultBehind) || len(f.WorktreePath) > 4096 || len(w.Path) > 4096 || len(w.PruneReason) > 4096 {
			return false
		}
		for _, x := range a.Classification.Labels {
			if !class(x) {
				return false
			}
		}
	}
	return true
}
func assetIdentity(a git.Asset) bool {
	if a.Facts.BranchOnly || a.Worktree.Bare || a.Worktree.Path == "" {
		return a.Facts.Branch != "" || a.Worktree.Branch != ""
	}
	return a.Facts.WorktreePath != "" || a.Worktree.Path != ""
}
func uniqueLabels(v []git.Classification) bool {
	if len(v) == 0 {
		return false
	}
	seen := map[git.Classification]bool{}
	for _, x := range v {
		if seen[x] || !class(x) {
			return false
		}
		seen[x] = true
	}
	return true
}
func anyNegative(v ...int) bool {
	for _, x := range v {
		if x < 0 || x > 1000000000 {
			return true
		}
	}
	return false
}
func repo(v git.RepoState) bool {
	return v == git.RepoUnknown || v == git.RepoNonGit || v == git.RepoBare || v == git.RepoWorktree
}
func conf(v git.Confidence) bool {
	return v == git.ConfidenceObserved || v == git.ConfidenceIncomplete || v == git.ConfidenceUnknown
}
func owner(v git.OwnerState) bool {
	return v == git.OwnerUnknown || v == git.OwnerActive || v == git.OwnerWaiting || v == git.OwnerReady || v == git.OwnerStale
}
func class(v git.Classification) bool {
	for _, x := range []git.Classification{git.ClassActiveOwned, git.ClassWaitingOwned, git.ClassReadyOwned, git.ClassStaleOwned, git.ClassOrphanedWorktree, git.ClassBranchOnly, git.ClassDirtyUnowned, git.ClassUnpushed, git.ClassDiverged, git.ClassDetachedUnowned, git.ClassMergedClean, git.ClassSafeToRemoveCandidate, git.ClassPossiblyIntegrated, git.ClassUnknown} {
		if v == x {
			return true
		}
	}
	return false
}
func (r gitRepository) GetSnapshot(c context.Context, p domain.ProjectID, id string) (git.Snapshot, bool, error) {
	if !r.acceptsProject(p) {
		return git.Snapshot{}, false, errors.New("project mismatch")
	}
	s, e := r.get(c, p, id)
	if errors.Is(e, sql.ErrNoRows) {
		return git.Snapshot{}, false, nil
	}
	return s, e == nil, e
}
func (r gitRepository) LatestSnapshot(c context.Context, p domain.ProjectID) (git.Snapshot, bool, error) {
	if !r.acceptsProject(p) {
		return git.Snapshot{}, false, errors.New("project mismatch")
	}
	var id string
	e := r.tx.QueryRowContext(c, `SELECT id FROM git_observations WHERE project_id=? ORDER BY sequence_no DESC LIMIT 1`, p).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return git.Snapshot{}, false, nil
	}
	if e != nil {
		return git.Snapshot{}, false, e
	}
	return r.GetSnapshot(c, p, id)
}
func (r gitRepository) History(c context.Context, p domain.ProjectID) ([]git.Snapshot, error) {
	if !r.acceptsProject(p) {
		return nil, errors.New("project mismatch")
	}
	rows, e := r.tx.QueryContext(c, `SELECT id FROM git_observations WHERE project_id=? ORDER BY sequence_no`, p)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []git.Snapshot{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		s, e := r.get(c, p, id)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r gitRepository) byKey(c context.Context, p domain.ProjectID, k domain.IdempotencyKey) (git.Snapshot, bool, error) {
	if !r.acceptsProject(p) {
		return git.Snapshot{}, false, errors.New("project mismatch")
	}
	var id string
	e := r.tx.QueryRowContext(c, `SELECT id FROM git_observations WHERE project_id=? AND idempotency_key=?`, p, k).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return git.Snapshot{}, false, nil
	}
	if e != nil {
		return git.Snapshot{}, false, e
	}
	s, e := r.get(c, p, id)
	return s, e == nil, e
}
func (r gitRepository) get(c context.Context, p domain.ProjectID, id string) (git.Snapshot, error) {
	if !r.acceptsProject(p) {
		return git.Snapshot{}, errors.New("project mismatch")
	}
	var s git.Snapshot
	var at string
	var actorSessionID, taskID, runID sql.NullString
	e := r.tx.QueryRowContext(c, `SELECT id,project_id,idempotency_key,actor_session_id,task_id,run_id,trigger_kind,observed_at,revision,observation_hash,repository_state,confidence,common_dir,top_level,default_branch,sequence_no FROM git_observations WHERE project_id=? AND id=?`, p, id).Scan(&s.ID, &s.ProjectID, &s.IdempotencyKey, &actorSessionID, &taskID, &runID, &s.Trigger, &at, &s.Observation.Revision, &s.Observation.Hash, &s.Observation.Repository, &s.Observation.Confidence, &s.Observation.CommonDir, &s.Observation.TopLevel, &s.Observation.DefaultBranch, &s.SequenceNo)
	if e != nil {
		return s, e
	}
	s.ActorSessionID, s.TaskID, s.RunID = lineage.ID(actorSessionID.String), lineage.ID(taskID.String), lineage.ID(runID.String)
	if s.ObservedAt, e = parseUTC(at); e != nil {
		return s, e
	}
	if e = r.validActor(c, s); e != nil {
		return s, e
	}
	rows, e := r.tx.QueryContext(c, `SELECT fingerprint,asset_type,first_seen_at,last_seen_at,facts_confidence,worktree_path,branch,detached,branch_only,default_branch,worktree_prunable,status_branch,status_head,status_detached,status_upstream,status_ahead,status_behind,status_tracked_dirty,status_untracked,status_ignored,status_confidence,merge_base_known,merge_base_equals_head,default_counts_known,default_ahead,default_behind,owner_registered,owner_state,wt_path,wt_head,wt_branch,wt_detached,wt_bare,wt_locked,wt_prunable,wt_prune_reason,classification_json,classification_confidence,owner_session_id,owner_task_id,owner_run_id FROM git_observation_assets WHERE observation_id=? ORDER BY fingerprint`, id)
	if e != nil {
		return s, e
	}
	defer rows.Close()
	for rows.Next() {
		var a git.AssetRecord
		var typ, first, last string
		var labels []byte
		var d, bo, db, wp, sd, mb, me, dc, oreg, wd, wb, wl, wpr int
		var os, ot, or sql.NullString
		e = rows.Scan(&a.Fingerprint, &typ, &first, &last, &a.Facts.Confidence, &a.Facts.WorktreePath, &a.Facts.Branch, &d, &bo, &db, &wp, &a.Facts.Status.Branch, &a.Facts.Status.Head, &sd, &a.Facts.Status.Upstream, &a.Facts.Status.Ahead, &a.Facts.Status.Behind, &a.Facts.Status.TrackedDirty, &a.Facts.Status.Untracked, &a.Facts.Status.Ignored, &a.Facts.Status.Confidence, &mb, &me, &dc, &a.Facts.DefaultAhead, &a.Facts.DefaultBehind, &oreg, &a.Facts.Owner.State, &a.Worktree.Path, &a.Worktree.Head, &a.Worktree.Branch, &wd, &wb, &wl, &wpr, &a.Worktree.PruneReason, &labels, &a.Classification.Confidence, &os, &ot, &or)
		if e != nil {
			return s, e
		}
		for _, v := range []int{d, bo, db, wp, sd, mb, me, dc, oreg, wd, wb, wl, wpr} {
			if _, e = storedBool(v); e != nil {
				return s, e
			}
		}
		a.Facts.Detached = d == 1
		a.Facts.BranchOnly = bo == 1
		a.Facts.DefaultBranch = db == 1
		a.Facts.WorktreePrunable = wp == 1
		a.Facts.Status.Detached = sd == 1
		a.Facts.Fingerprint.MergeBaseKnown = mb == 1
		a.Facts.Fingerprint.MergeBaseEqualsHead = me == 1
		a.Facts.Fingerprint.DefaultCountsKnown = dc == 1
		a.Facts.Owner.Registered = oreg == 1
		a.Worktree.Detached = wd == 1
		a.Worktree.Bare = wb == 1
		a.Worktree.Locked = wl == 1
		a.Worktree.Prunable = wpr == 1
		if e = r.validOwner(c, id, a.Facts.Owner.Registered, a.Facts.Owner.State, os, ot, or); e != nil {
			return s, e
		}
		if e = json.Unmarshal(labels, &a.Classification.Labels); e != nil {
			return s, e
		}
		if a.FirstSeenAt, e = parseUTC(first); e != nil {
			return s, e
		}
		if a.LastSeenAt, e = parseUTC(last); e != nil || a.FirstSeenAt.After(a.LastSeenAt) {
			return s, errors.New("invalid stored timestamp")
		}
		a.OwnerSessionID = lineage.ID(os.String)
		a.OwnerTaskID = lineage.ID(ot.String)
		a.OwnerRunID = lineage.ID(or.String)
		if a.Fingerprint != a.Asset.StableFingerprint() || typ != string(git.DeriveAssetType(s.Observation, a.Asset)) || !validObservation(git.Observation{Revision: s.Observation.Revision, Hash: s.Observation.Hash, Repository: s.Observation.Repository, Confidence: s.Observation.Confidence, CommonDir: s.Observation.CommonDir, TopLevel: s.Observation.TopLevel, DefaultBranch: s.Observation.DefaultBranch, Assets: []git.Asset{a.Asset}}) {
			return s, errors.New("invalid stored git asset")
		}
		s.Assets = append(s.Assets, a)
		s.Observation.Assets = append(s.Observation.Assets, a.Asset)
	}
	if e = rows.Err(); e != nil {
		return s, e
	}
	if s.Observation.Hash != git.HashObservation(s.Observation) {
		return s, errors.New("invalid stored git hash")
	}
	sort.Slice(s.Assets, func(i, j int) bool { return s.Assets[i].Fingerprint < s.Assets[j].Fingerprint })
	return s, nil
}
