// Package git defines pure, argv-only read-only Git observation plans,
// porcelain parsers, and conservative asset classifications. It never runs a
// process, reads the filesystem, or authorizes cleanup.
package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CommandPlan is an executable and its exact argv, deliberately not a shell string.
type CommandPlan struct {
	Executable string
	Args       []string
}

func plan(args ...string) CommandPlan { return CommandPlan{Executable: "git", Args: args} }

// CommonDirPlan observes the common Git directory for linked worktrees.
func CommonDirPlan() CommandPlan {
	return plan("rev-parse", "--path-format=absolute", "--git-common-dir")
}

// IsInsideWorkTreePlan distinguishes a non-Git directory from a repository.
func IsInsideWorkTreePlan() CommandPlan { return plan("rev-parse", "--is-inside-work-tree") }

// IsBarePlan observes whether the repository is bare.
func IsBarePlan() CommandPlan { return plan("rev-parse", "--is-bare-repository") }

// TopLevelPlan observes a worktree root when one exists.
func TopLevelPlan() CommandPlan {
	return plan("rev-parse", "--path-format=absolute", "--show-toplevel")
}

// WorktreeListPlan requests NUL-delimited porcelain. Callers may fall back to
// ParseWorktreePorcelain for old Git versions which reject -z.
func WorktreeListPlan() CommandPlan { return plan("worktree", "list", "--porcelain", "-z") }

// WorktreeListFallbackPlan is read-only newline-delimited porcelain for older Git.
func WorktreeListFallbackPlan() CommandPlan { return plan("worktree", "list", "--porcelain") }

// StatusPlan disables optional index locks and fsmonitor hooks before using
// porcelain, so status remains an observation rather than a repository write.
func StatusPlan() CommandPlan {
	return plan("--no-optional-locks", "-c", "core.fsmonitor=false", "status", "--porcelain=v2", "-z", "--branch")
}

// RefPlan lists local heads with NUL-delimited fields; Git's record newlines
// are handled by ParseLocalRefs.
func RefPlan() CommandPlan {
	return plan("for-each-ref", "--format=%(refname)%00%(objectname)%00", "refs/heads")
}

// AheadBehindPlan counts commits on each side of two already-observed refs.
// Empty or option-like refs are rejected rather than becoming argv.
func AheadBehindPlan(upstream, head string) (CommandPlan, error) {
	if err := safeRef(upstream); err != nil {
		return CommandPlan{}, err
	}
	if err := safeRef(head); err != nil {
		return CommandPlan{}, err
	}
	return plan("rev-list", "--left-right", "--count", upstream+"..."+head), nil
}

// MergeBasePlan observes the merge base of two already-observed refs.
func MergeBasePlan(base, head string) (CommandPlan, error) {
	if err := safeRef(base); err != nil {
		return CommandPlan{}, err
	}
	if err := safeRef(head); err != nil {
		return CommandPlan{}, err
	}
	return plan("merge-base", base, head), nil
}

func safeRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.IndexByte(ref, 0) >= 0 {
		return errors.New("invalid git ref")
	}
	return nil
}

// AllReadOnlyPlans supplies representative exported plans for allowlist verification.
func AllReadOnlyPlans() []CommandPlan {
	ahead, _ := AheadBehindPlan("refs/remotes/origin/trunk", "refs/heads/topic")
	merge, _ := MergeBasePlan("refs/heads/trunk", "refs/heads/topic")
	return []CommandPlan{CommonDirPlan(), IsInsideWorkTreePlan(), IsBarePlan(), TopLevelPlan(), WorktreeListPlan(), WorktreeListFallbackPlan(), StatusPlan(), RefPlan(), ahead, merge}
}

type Confidence string

const (
	ConfidenceObserved   Confidence = "observed"
	ConfidenceIncomplete Confidence = "incomplete"
	ConfidenceUnknown    Confidence = "unknown"
)

type RepoState string

const (
	RepoUnknown  RepoState = "unknown"
	RepoNonGit   RepoState = "non_git"
	RepoBare     RepoState = "bare"
	RepoWorktree RepoState = "worktree"
)

// ParseRepoState combines normalized rev-parse outputs without interpreting errors.
func ParseRepoState(insideWorkTree, bare string) RepoState {
	inside := strings.TrimSpace(insideWorkTree)
	isBare := strings.TrimSpace(bare)
	switch {
	case inside == "false":
		return RepoNonGit
	case inside == "true" && isBare == "true":
		return RepoBare
	case inside == "true" && isBare == "false":
		return RepoWorktree
	default:
		return RepoUnknown
	}
}

// Worktree is a Git worktree record as reported by porcelain.
type Worktree struct {
	Path        string
	Head        string
	Branch      string
	Detached    bool
	Bare        bool
	Locked      bool
	Prunable    bool
	PruneReason string
}

// ParseWorktreePorcelainZ parses `git worktree list --porcelain -z` output.
func ParseWorktreePorcelainZ(input []byte) ([]Worktree, error) {
	return parseWorktreeRecords(strings.Split(string(input), "\x00"))
}

// ParseWorktreePorcelain parses newline porcelain used by old Git versions.
func ParseWorktreePorcelain(input []byte) ([]Worktree, error) {
	return parseWorktreeRecords(strings.Split(string(input), "\n"))
}

func parseWorktreeRecords(records []string) ([]Worktree, error) {
	worktrees := make([]Worktree, 0)
	var current *Worktree
	finish := func() error {
		if current == nil {
			return nil
		}
		if current.Path == "" {
			return errors.New("malformed worktree porcelain: missing path")
		}
		worktrees = append(worktrees, *current)
		current = nil
		return nil
	}
	for _, record := range records {
		if record == "" {
			if err := finish(); err != nil {
				return nil, err
			}
			continue
		}
		key, value, found := strings.Cut(record, " ")
		if key == "worktree" {
			if err := finish(); err != nil {
				return nil, err
			}
			if !found || value == "" {
				return nil, errors.New("malformed worktree porcelain: missing worktree path")
			}
			current = &Worktree{Path: value}
			continue
		}
		if current == nil {
			return nil, errors.New("malformed worktree porcelain: record before worktree")
		}
		switch key {
		case "HEAD":
			if !found || value == "" {
				return nil, errors.New("malformed worktree porcelain: missing HEAD")
			}
			current.Head = value
		case "branch":
			if !found || !strings.HasPrefix(value, "refs/heads/") {
				return nil, errors.New("malformed worktree porcelain: invalid branch")
			}
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			if found {
				return nil, errors.New("malformed worktree porcelain: detached has value")
			}
			current.Detached = true
		case "bare":
			if found {
				return nil, errors.New("malformed worktree porcelain: bare has value")
			}
			current.Bare = true
		case "locked":
			current.Locked = true
		case "prunable":
			current.Prunable = true
			current.PruneReason = value
		default:
			return nil, fmt.Errorf("malformed worktree porcelain: unknown record %q", key)
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	if len(worktrees) == 0 {
		return nil, errors.New("malformed worktree porcelain: no worktrees")
	}
	return worktrees, nil
}

// Status is a conservative summary of porcelain v2 branch and dirtiness facts.
type Status struct {
	Branch       string
	Head         string
	Detached     bool
	Upstream     string
	Ahead        int
	Behind       int
	TrackedDirty int
	Untracked    int
	Ignored      int
	Confidence   Confidence
}

// ParseStatusPorcelainV2 parses NUL-delimited `git status --porcelain=v2 -z --branch`.
func ParseStatusPorcelainV2(input []byte) (Status, error) {
	status := Status{Confidence: ConfidenceUnknown}
	records := strings.Split(string(input), "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			if err := parseStatusHeader(&status, strings.TrimPrefix(record, "# ")); err != nil {
				return Status{Confidence: ConfidenceUnknown}, err
			}
			continue
		}
		switch record[0] {
		case '1':
			if len(strings.Fields(record)) < 9 {
				return Status{Confidence: ConfidenceUnknown}, errors.New("malformed status porcelain: ordinary entry")
			}
			status.TrackedDirty++
		case '2':
			if len(strings.Fields(record)) < 10 || index+1 >= len(records) || records[index+1] == "" {
				return Status{Confidence: ConfidenceUnknown}, errors.New("malformed status porcelain: rename entry")
			}
			status.TrackedDirty++
			index++
		case 'u':
			if len(strings.Fields(record)) < 10 {
				return Status{Confidence: ConfidenceUnknown}, errors.New("malformed status porcelain: unmerged entry")
			}
			status.TrackedDirty++
		case '?':
			if len(record) < 3 {
				return Status{Confidence: ConfidenceUnknown}, errors.New("malformed status porcelain: untracked entry")
			}
			status.Untracked++
		case '!':
			if len(record) < 3 {
				return Status{Confidence: ConfidenceUnknown}, errors.New("malformed status porcelain: ignored entry")
			}
			status.Ignored++
		default:
			return Status{Confidence: ConfidenceUnknown}, fmt.Errorf("malformed status porcelain: unknown record %q", record[:1])
		}
	}
	status.Confidence = ConfidenceObserved
	return status, nil
}

func parseStatusHeader(status *Status, header string) error {
	key, value, found := strings.Cut(header, " ")
	if !found || value == "" {
		return errors.New("malformed status porcelain: header")
	}
	switch key {
	case "branch.oid":
		status.Head = value
	case "branch.head":
		if value == "(detached)" {
			status.Detached = true
		} else if value == "(unknown)" {
			return errors.New("incomplete status porcelain: unknown branch")
		} else {
			status.Branch = value
		}
	case "branch.upstream":
		status.Upstream = value
	case "branch.ab":
		fields := strings.Fields(value)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "+") || !strings.HasPrefix(fields[1], "-") {
			return errors.New("malformed status porcelain: branch.ab")
		}
		ahead, aheadErr := strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
		behind, behindErr := strconv.Atoi(strings.TrimPrefix(fields[1], "-"))
		if aheadErr != nil || behindErr != nil || ahead < 0 || behind < 0 {
			return errors.New("malformed status porcelain: branch.ab")
		}
		status.Ahead, status.Behind = ahead, behind
	case "branch.upstreamGone":
		return errors.New("incomplete status porcelain: upstream gone")
	default:
		return fmt.Errorf("malformed status porcelain: unknown header %q", key)
	}
	return nil
}

// Ref is a local branch ref reported by RefPlan.
type Ref struct {
	Name string
	Head string
}

// ParseLocalRefs parses RefPlan output without splitting branch names on spaces.
func ParseLocalRefs(input []byte) ([]Ref, error) {
	parts := strings.Split(string(input), "\x00")
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 || len(parts)%2 != 0 {
		return nil, errors.New("malformed ref observation")
	}
	refs := make([]Ref, 0, len(parts)/2)
	for index := 0; index < len(parts); index += 2 {
		name := strings.TrimPrefix(parts[index], "\n")
		head := strings.TrimSuffix(parts[index+1], "\n")
		if !strings.HasPrefix(name, "refs/heads/") || head == "" || strings.ContainsAny(head, " \t\r\n") {
			return nil, errors.New("malformed ref observation")
		}
		refs = append(refs, Ref{Name: strings.TrimPrefix(name, "refs/heads/"), Head: head})
	}
	return refs, nil
}

// ParseAheadBehind converts `rev-list --left-right --count base...head` output
// to semantic head-ahead, head-behind counters. Git prints base-only first.
func ParseAheadBehind(input []byte) (ahead, behind int, err error) {
	fields := strings.Fields(string(input))
	if len(fields) != 2 {
		return 0, 0, errors.New("malformed ahead-behind observation")
	}
	behind, behindErr := strconv.Atoi(fields[0])
	ahead, aheadErr := strconv.Atoi(fields[1])
	if aheadErr != nil || behindErr != nil || ahead < 0 || behind < 0 {
		return 0, 0, errors.New("malformed ahead-behind observation")
	}
	return ahead, behind, nil
}

// ParseMergeBase validates one object ID from `git merge-base`.
func ParseMergeBase(input []byte) (string, error) {
	mergeBase := strings.TrimSpace(string(input))
	if mergeBase == "" || strings.ContainsAny(mergeBase, " \t\r\n\x00") {
		return "", errors.New("malformed merge-base observation")
	}
	return mergeBase, nil
}

type OwnerState string

const (
	OwnerUnknown OwnerState = "unknown"
	OwnerActive  OwnerState = "active"
	OwnerWaiting OwnerState = "waiting"
	OwnerReady   OwnerState = "ready"
	OwnerStale   OwnerState = "stale"
)

type OwnerFacts struct {
	Registered bool
	State      OwnerState
}
type FingerprintFacts struct {
	MergeBaseKnown      bool
	MergeBaseEqualsHead bool
	DefaultCountsKnown  bool
}

// AssetFacts combines only independently observed facts. Zero values are uncertain.
type AssetFacts struct {
	Confidence       Confidence
	WorktreePath     string
	Branch           string
	Detached         bool
	BranchOnly       bool
	DefaultBranch    bool
	WorktreePrunable bool
	Status           Status
	Fingerprint      FingerprintFacts
	Owner            OwnerFacts
	DefaultAhead     int
	DefaultBehind    int
}

type Classification string

const (
	ClassActiveOwned           Classification = "active_owned"
	ClassWaitingOwned          Classification = "waiting_owned"
	ClassReadyOwned            Classification = "ready_owned"
	ClassStaleOwned            Classification = "stale_owned"
	ClassOrphanedWorktree      Classification = "orphaned_worktree"
	ClassBranchOnly            Classification = "branch_only"
	ClassDirtyUnowned          Classification = "dirty_unowned"
	ClassUnpushed              Classification = "unpushed"
	ClassDiverged              Classification = "diverged"
	ClassDetachedUnowned       Classification = "detached_unowned"
	ClassMergedClean           Classification = "merged_clean"
	ClassSafeToRemoveCandidate Classification = "safe_to_remove_candidate"
	ClassPossiblyIntegrated    Classification = "possibly_integrated"
	ClassUnknown               Classification = "unknown"
)

type AssetClassification struct {
	Labels     []Classification
	Confidence Confidence
}

func (c AssetClassification) Has(label Classification) bool {
	for _, got := range c.Labels {
		if got == label {
			return true
		}
	}
	return false
}
func Classify(f AssetFacts) AssetClassification {
	if f.Confidence != ConfidenceObserved {
		labels := []Classification{ClassUnknown}
		if f.Confidence == ConfidenceIncomplete && !f.Owner.Registered {
			if f.WorktreePrunable {
				labels = append(labels, ClassOrphanedWorktree)
			}
			if f.Detached {
				labels = append(labels, ClassDetachedUnowned)
			}
		}
		return AssetClassification{Labels: labels, Confidence: ConfidenceIncomplete}
	}
	labels := make([]Classification, 0, 5)
	statusObserved := f.Status.Confidence == ConfidenceObserved
	classificationConfidence := ConfidenceObserved
	if !f.Owner.Registered && !statusObserved {
		labels = append(labels, ClassUnknown)
		classificationConfidence = ConfidenceIncomplete
	}
	if f.Owner.Registered {
		switch f.Owner.State {
		case OwnerActive:
			labels = append(labels, ClassActiveOwned)
		case OwnerWaiting:
			labels = append(labels, ClassWaitingOwned)
		case OwnerReady:
			labels = append(labels, ClassReadyOwned)
		case OwnerStale:
			labels = append(labels, ClassStaleOwned)
		default:
			labels = append(labels, ClassUnknown)
		}
	} else {
		if f.WorktreePrunable {
			labels = append(labels, ClassOrphanedWorktree)
		}
		if f.Detached {
			labels = append(labels, ClassDetachedUnowned)
		}
		if f.BranchOnly {
			labels = append(labels, ClassBranchOnly)
		}
		if statusObserved && (f.Status.TrackedDirty > 0 || f.Status.Untracked > 0) {
			labels = append(labels, ClassDirtyUnowned)
		}
	}
	if (statusObserved && f.Status.Ahead > 0 && f.Status.Behind > 0) || (f.Fingerprint.DefaultCountsKnown && f.DefaultAhead > 0 && f.DefaultBehind > 0) {
		labels = append(labels, ClassDiverged)
	} else if statusObserved && f.Status.Ahead > 0 {
		labels = append(labels, ClassUnpushed)
	}
	clean := statusObserved && f.Status.TrackedDirty == 0 && f.Status.Untracked == 0
	if !f.Owner.Registered && !f.DefaultBranch && f.BranchOnly && clean {
		if f.Fingerprint.MergeBaseKnown && f.Fingerprint.MergeBaseEqualsHead {
			labels = append(labels, ClassMergedClean, ClassSafeToRemoveCandidate)
		} else if !f.Fingerprint.MergeBaseKnown {
			labels = append(labels, ClassPossiblyIntegrated)
		}
	}
	if len(labels) == 0 {
		labels = append(labels, ClassUnknown)
	}
	return AssetClassification{Labels: labels, Confidence: classificationConfidence}
}

// CleanupPlan is an inert report. It contains no command and cannot apply cleanup.
type CleanupPlan struct {
	Mutating   bool
	Candidates []CleanupCandidate
	Blocked    []CleanupBlock
}
type CleanupCandidate struct {
	Branch         string
	WorktreePath   string
	Classification Classification
}
type CleanupBlock struct {
	Branch       string
	WorktreePath string
	Reason       string
}

// BuildCleanupPlan includes only fully observed, clean, unowned candidates and explains every block.
func BuildCleanupPlan(facts []AssetFacts) CleanupPlan {
	result := CleanupPlan{Candidates: make([]CleanupCandidate, 0), Blocked: make([]CleanupBlock, 0)}
	for _, fact := range facts {
		classification := Classify(fact)
		if classification.Has(ClassSafeToRemoveCandidate) {
			result.Candidates = append(result.Candidates, CleanupCandidate{Branch: fact.Branch, WorktreePath: fact.WorktreePath, Classification: ClassSafeToRemoveCandidate})
			continue
		}
		reason := "not a fully observed clean unowned merged branch-only asset"
		if classification.Has(ClassUnknown) {
			reason = "observation incomplete or unknown"
		}
		result.Blocked = append(result.Blocked, CleanupBlock{Branch: fact.Branch, WorktreePath: fact.WorktreePath, Reason: reason})
	}
	return result
}

// Plan is an advisory-only cleanup report; it never produces a Git command.
func Plan(facts []AssetFacts) CleanupPlan { return BuildCleanupPlan(facts) }
