package git

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"example.invalid/coordledger/internal/domain"
	coord "example.invalid/coordledger/internal/domain/coordination"
	"example.invalid/coordledger/internal/domain/lineage"
)

// ObservationRevision identifies the durable scanner wire format.
const ObservationRevision = "git-observation/v1"

// AssetType distinguishes independently observed worktrees from local branches
type AssetType string

const (
	AssetMainWorktree   AssetType = "main_worktree"
	AssetLinkedWorktree AssetType = "linked_worktree"
	AssetLocalBranch    AssetType = "local_branch"
	AssetDetachedHead   AssetType = "detached_head"
)

// Asset is one scanner observation and its conservative classification.
type Asset struct {
	Facts          AssetFacts
	Classification AssetClassification
	Worktree       Worktree
}

// Type is derived only from observed facts.
// DeriveAssetType uses the enclosing observation to preserve the distinction
// between a main worktree, linked worktree, branch-only asset, and detached head.
func DeriveAssetType(o Observation, a Asset) AssetType {
	if a.Facts.Detached || a.Worktree.Detached {
		return AssetDetachedHead
	}
	if a.Facts.BranchOnly || a.Worktree.Bare || o.Repository == RepoBare {
		return AssetLocalBranch
	}
	if filesystemPathIdentity(a.Facts.WorktreePath) == filesystemPathIdentity(o.TopLevel) || filesystemPathIdentity(a.Worktree.Path) == filesystemPathIdentity(o.TopLevel) {
		return AssetMainWorktree
	}
	return AssetLinkedWorktree
}

// StableFingerprint identifies an asset across snapshots without using its
// classification. It is opaque and intentionally deterministic.
func (a Asset) StableFingerprint() string {
	h := sha256.New()
	write := func(v string) { _, _ = h.Write([]byte(v)); _, _ = h.Write([]byte{0}) }
	// Asset identity intentionally excludes changing facts such as HEAD, counts,
	// status and classifications so ExactDiff can report them as changes.
	if a.Facts.BranchOnly || a.Worktree.Bare || a.Worktree.Path == "" {
		write("branch")
		branch := a.Facts.Branch
		if branch == "" {
			branch = a.Worktree.Branch
		}
		write(branch)
	} else {
		write("worktree")
		path := a.Facts.WorktreePath
		if path == "" {
			path = a.Worktree.Path
		}
		write(filesystemPathIdentity(path))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Observation is the versioned, read-only scanner result.
type Observation struct {
	Revision      string
	Hash          string
	Repository    RepoState
	Confidence    Confidence
	CommonDir     string
	TopLevel      string
	DefaultBranch string
	Assets        []Asset
}

// CanonicalAssets returns a sorted copy suitable for deterministic storage.
func (o Observation) CanonicalAssets() []Asset {
	out := append([]Asset(nil), o.Assets...)
	sort.Slice(out, func(i, j int) bool { return out[i].StableFingerprint() < out[j].StableFingerprint() })
	return out
}

// Diff is an exact snapshot comparison. Changed means matching fingerprints
// with any stored fact or classification changed.
type Diff struct{ New, Missing, Changed []Asset }

func ExactDiff(previous, current Observation) Diff {
	before := make(map[string]Asset, len(previous.Assets))
	after := make(map[string]Asset, len(current.Assets))
	for _, a := range previous.Assets {
		before[a.StableFingerprint()] = a
	}
	for _, a := range current.Assets {
		after[a.StableFingerprint()] = a
	}
	out := Diff{}
	for k, a := range after {
		if b, ok := before[k]; !ok {
			out.New = append(out.New, a)
		} else if !sameAsset(b, a) {
			out.Changed = append(out.Changed, a)
		}
	}
	for k, a := range before {
		if _, ok := after[k]; !ok {
			out.Missing = append(out.Missing, a)
		}
	}
	sort.Slice(out.New, func(i, j int) bool { return out.New[i].StableFingerprint() < out.New[j].StableFingerprint() })
	sort.Slice(out.Missing, func(i, j int) bool { return out.Missing[i].StableFingerprint() < out.Missing[j].StableFingerprint() })
	sort.Slice(out.Changed, func(i, j int) bool { return out.Changed[i].StableFingerprint() < out.Changed[j].StableFingerprint() })
	return out
}

func sameAsset(a, b Asset) bool {
	return a.Facts == b.Facts && a.Worktree == b.Worktree && a.Classification.Confidence == b.Classification.Confidence && equalLabels(a.Classification.Labels, b.Classification.Labels)
}
func equalLabels(a, b []Classification) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HashObservation is the scanner's canonical opaque observation hash. It is
// deliberately versioned and covers every observed fact and classification.
func HashObservation(o Observation) string {
	h := sha256.New()
	add := func(v ...string) {
		for _, x := range v {
			_, _ = h.Write([]byte(x))
			_, _ = h.Write([]byte{0})
		}
	}
	add(o.Revision, string(o.Repository), string(o.Confidence), o.CommonDir, o.TopLevel, o.DefaultBranch)
	for _, a := range o.CanonicalAssets() {
		f, s, w := a.Facts, a.Facts.Status, a.Worktree
		labels := make([]string, len(a.Classification.Labels))
		for i, v := range a.Classification.Labels {
			labels[i] = string(v)
		}
		add(f.WorktreePath, f.Branch, string(f.Confidence), string(f.Owner.State), strconv.FormatBool(f.Owner.Registered), strconv.FormatBool(f.Detached), strconv.FormatBool(f.BranchOnly), strconv.FormatBool(f.DefaultBranch), strconv.FormatBool(f.WorktreePrunable), strconv.FormatBool(f.Fingerprint.MergeBaseKnown), strconv.FormatBool(f.Fingerprint.MergeBaseEqualsHead), strconv.FormatBool(f.Fingerprint.DefaultCountsKnown), strconv.Itoa(f.DefaultAhead), strconv.Itoa(f.DefaultBehind), s.Branch, s.Head, strconv.FormatBool(s.Detached), s.Upstream, strconv.Itoa(s.Ahead), strconv.Itoa(s.Behind), strconv.Itoa(s.TrackedDirty), strconv.Itoa(s.Untracked), strconv.Itoa(s.Ignored), string(s.Confidence), w.Path, w.Head, w.Branch, strconv.FormatBool(w.Detached), strconv.FormatBool(w.Bare), strconv.FormatBool(w.Locked), strconv.FormatBool(w.Prunable), w.PruneReason, string(a.Classification.Confidence), strings.Join(labels, ","))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Snapshot is an immutable persisted observation. Asset records preserve every
// scanner fact plus optional future ownership lineage without authorizing it.
type Snapshot struct {
	ID             string
	ProjectID      domain.ProjectID
	IdempotencyKey domain.IdempotencyKey
	ActorSessionID lineage.ID
	TaskID         lineage.ID
	SequenceNo     int64
	RunID          lineage.ID
	Trigger        string
	ObservedAt     time.Time
	Observation    Observation
	Assets         []AssetRecord
}

type AssetRecord struct {
	Asset
	Fingerprint    string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	OwnerSessionID lineage.ID
	OwnerTaskID    lineage.ID
	OwnerRunID     lineage.ID
}

// OwnershipCandidate is canonical lineage supplied by the application. A
// missing task or run is intentionally not inferred from scanner output.
type OwnershipCandidate struct {
	Session lineage.AgentSession
	Task    lineage.Task
	Run     lineage.TaskRun
}

// ReconcileOwnership overlays conservative ownership onto an observation. It
// never changes scanner facts other than derived owner/classification fields.
func ReconcileOwnership(observation Observation, candidates []OwnershipCandidate, adoptions []coord.Adoption) (Observation, []AssetRecord) {
	byPath := make(map[string][]OwnershipCandidate)
	bySession := make(map[lineage.ID]OwnershipCandidate)
	for _, candidate := range candidates {
		if !validCandidate(candidate) {
			continue
		}
		bySession[candidate.Session.ID] = candidate
		if path, ok := canonicalCandidateWorktreePath(candidate); ok {
			byPath[path] = append(byPath[path], candidate)
		}
	}
	latest := make(map[string]coord.Adoption)
	for _, adoption := range adoptions {
		if adoption.GitAssetID == "" || adoption.Validate() != nil {
			continue
		}
		old, ok := latest[adoption.GitAssetID]
		if !ok || adoption.CreatedAt.After(old.CreatedAt) || (adoption.CreatedAt.Equal(old.CreatedAt) && adoption.ID > old.ID) {
			latest[adoption.GitAssetID] = adoption
		}
	}
	out := observation
	out.Assets = make([]Asset, len(observation.Assets))
	records := make([]AssetRecord, len(observation.Assets))
	for i, raw := range observation.Assets {
		asset := raw
		fingerprint := asset.StableFingerprint()
		var owner OwnershipCandidate
		owned := false
		if adoption, ok := latest[fingerprint]; ok {
			owner, owned = bySession[lineage.ID(adoption.NewOwnerSessionID)]
		} else if path, ok := canonicalWorktreePath(asset); ok {
			matches := byPath[path]
			if len(matches) == 1 {
				owner, owned = matches[0], true
			}
		}
		if owned {
			asset.Facts.Owner = OwnerFacts{Registered: true, State: ownerState(owner)}
		} else {
			asset.Facts.Owner = OwnerFacts{Registered: false, State: OwnerUnknown}
		}
		asset.Classification = Classify(asset.Facts)
		out.Assets[i] = asset
		records[i] = AssetRecord{Asset: asset, Fingerprint: fingerprint}
		if owned {
			records[i].OwnerSessionID = owner.Session.ID
			records[i].OwnerTaskID = owner.Task.ID
			records[i].OwnerRunID = owner.Run.ID
		}
	}
	out.Hash = HashObservation(out)
	return out, records
}

func canonicalWorktreePath(asset Asset) (string, bool) {
	path := asset.Facts.WorktreePath
	if path == "" {
		path = asset.Worktree.Path
	}
	if path == "" || !filepath.IsAbs(path) || (runtime.GOOS != "windows" && filepath.Clean(path) != path) {
		return "", false
	}
	return filesystemPathIdentity(path), true
}

func validCandidate(candidate OwnershipCandidate) bool {
	session, task, run := candidate.Session, candidate.Task, candidate.Run
	return session.ID != "" && session.ProjectID != "" &&
		task.ID != "" && task.ProjectID == session.ProjectID && session.TaskID == task.ID &&
		run.ID != "" && run.TaskID == task.ID && run.SessionID == session.ID
}

func canonicalCandidateWorktreePath(candidate OwnershipCandidate) (string, bool) {
	path := candidate.Session.WorktreeRef
	if path == "" || !filepath.IsAbs(path) || (runtime.GOOS != "windows" && filepath.Clean(path) != path) {
		return "", false
	}
	return filesystemPathIdentity(path), true
}

func filesystemPathIdentity(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Clean(path))
	}
	return path
}

func ownerState(candidate OwnershipCandidate) OwnerState {
	session, task, run := candidate.Session, candidate.Task, candidate.Run
	if session.EndedAt != nil || session.InterruptedAt != nil || terminalTask(task.State) || terminalRun(run.State) {
		return OwnerStale
	}
	if task.State == lineage.TaskWaiting || task.State == lineage.TaskBlocked || run.State == lineage.RunWaiting || run.State == lineage.RunBlocked {
		return OwnerWaiting
	}
	if task.State == lineage.TaskWorkComplete || task.State == lineage.TaskVerifiedDone || run.State == lineage.RunWorkComplete || run.State == lineage.RunVerifiedDone {
		return OwnerReady
	}
	if task.State == lineage.TaskClaimed || task.State == lineage.TaskInProgress || task.State == lineage.TaskRework || run.State == lineage.RunRunning || run.State == lineage.RunRework {
		return OwnerActive
	}
	return OwnerUnknown
}

func terminalTask(state lineage.TaskState) bool {
	return state == lineage.TaskFailed || state == lineage.TaskAbandoned || state == lineage.TaskInterrupted || state == lineage.TaskStale || state == lineage.TaskCancelled
}
func terminalRun(state lineage.RunState) bool {
	return state == lineage.RunFailed || state == lineage.RunAbandoned || state == lineage.RunInterrupted || state == lineage.RunStale || state == lineage.RunCancelled
}
