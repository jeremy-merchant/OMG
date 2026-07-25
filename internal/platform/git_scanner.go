package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

const GitObservationRevision = gitobs.ObservationRevision
const maxGitOutput = 1 << 20

// GitCommandRunner is the narrow argv-only boundary used by GitScanner.
type GitCommandRunner func(context.Context, string, gitobs.CommandPlan) ([]byte, error)

// GitScannerDependencies makes process execution injectable without exposing a shell.
type GitScannerDependencies struct{ Runner GitCommandRunner }

// GitScanner performs read-only repository observations.
type GitScanner struct{ runner GitCommandRunner }

var _ ports.Scanner = (*GitScanner)(nil)

// ScanError deliberately contains no path, ref, command output, or stderr details.
type ScanError struct{ Code string }

func (e *ScanError) Error() string { return "git scan " + e.Code }

// GitRunnerError describes a process failure without carrying command output.
type GitRunnerError struct{ Kind string }

func (e *GitRunnerError) Error() string { return "git runner " + e.Kind }

// GitAsset and GitObservation retain the scanner's established public API.
type GitAsset = gitobs.Asset
type GitObservation = gitobs.Observation

func NewGitScanner(dependencies GitScannerDependencies) *GitScanner {
	if dependencies.Runner == nil {
		dependencies.Runner = runGitPlan
	}
	return &GitScanner{runner: dependencies.Runner}
}

// Scan observes only fixed read-only Git plans. Per-worktree failures become incomplete
// assets so stale metadata is preserved rather than silently discarded.
func (s *GitScanner) Scan(ctx context.Context, directory string) (GitObservation, error) {
	if s == nil || s.runner == nil {
		return GitObservation{}, &ScanError{Code: "unavailable"}
	}
	inside, insideErr := s.run(ctx, directory, gitobs.IsInsideWorkTreePlan())
	bare, bareErr := s.run(ctx, directory, gitobs.IsBarePlan())
	state := repoState(inside, bare, insideErr, bareErr)
	result := GitObservation{Revision: GitObservationRevision, Repository: state, Confidence: gitobs.ConfidenceObserved, Assets: make([]GitAsset, 0)}
	if state == gitobs.RepoNonGit {
		result.Hash = observationHash(result)
		return result, nil
	}
	if state == gitobs.RepoUnknown {
		result.Confidence = gitobs.ConfidenceUnknown
		result.Hash = observationHash(result)
		return result, nil
	}

	if output, err := s.run(ctx, directory, gitobs.CommonDirPlan()); err == nil {
		result.CommonDir = strings.TrimSpace(string(output))
	} else {
		result.Confidence = gitobs.ConfidenceIncomplete
	}
	if state == gitobs.RepoBare {
		refs, observed := s.refs(ctx, directory)
		if !observed {
			result.Confidence = gitobs.ConfidenceIncomplete
		}
		result.DefaultBranch = s.defaultBranch(ctx, directory, refs, true)
		for _, ref := range refs {
			asset := GitAsset{Worktree: gitobs.Worktree{Branch: ref.Name, Head: ref.Head, Bare: true}, Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, Branch: ref.Name, BranchOnly: true, DefaultBranch: ref.Name == result.DefaultBranch}}
			if !s.addReachability(ctx, directory, result.DefaultBranch, &asset) {
				asset.Facts.Confidence = gitobs.ConfidenceIncomplete
				result.Confidence = gitobs.ConfidenceIncomplete
			}
			asset.Classification = gitobs.Classify(asset.Facts)
			result.Assets = append(result.Assets, asset)
		}
		sortAssets(result.Assets)
		result.Hash = observationHash(result)
		return result, nil
	}

	if output, err := s.run(ctx, directory, gitobs.TopLevelPlan()); err == nil {
		result.TopLevel = strings.TrimSpace(string(output))
	} else {
		result.Confidence = gitobs.ConfidenceIncomplete
	}
	worktrees, listed := s.worktrees(ctx, directory)
	if !listed {
		result.Confidence = gitobs.ConfidenceIncomplete
	}
	refs, refsObserved := s.refs(ctx, directory)
	if !refsObserved {
		result.Confidence = gitobs.ConfidenceIncomplete
	}
	result.DefaultBranch = s.defaultBranch(ctx, directory, refs, false)

	attached := make(map[string]bool, len(worktrees))
	for _, worktree := range worktrees {
		asset := GitAsset{Worktree: worktree, Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, WorktreePath: worktree.Path, Branch: worktree.Branch, Detached: worktree.Detached, DefaultBranch: worktree.Branch == result.DefaultBranch, WorktreePrunable: worktree.Prunable}}
		if worktree.Branch != "" {
			attached[worktree.Branch] = true
		}
		if !worktree.Prunable {
			status, err := s.status(ctx, worktree.Path)
			if err != nil {
				asset.Facts.Status = gitobs.Status{Confidence: gitobs.ConfidenceUnknown}
				asset.Facts.Confidence = gitobs.ConfidenceIncomplete
				result.Confidence = gitobs.ConfidenceIncomplete
			} else {
				asset.Facts.Status = status
				if status.Branch != "" {
					asset.Facts.Branch = status.Branch
				}
				asset.Facts.Detached = status.Detached
			}
		}
		reachabilityDirectory := worktree.Path
		if worktree.Prunable {
			reachabilityDirectory = directory
		}
		if asset.Facts.Branch != "" && !s.addReachability(ctx, reachabilityDirectory, result.DefaultBranch, &asset) {
			asset.Facts.Confidence = gitobs.ConfidenceIncomplete
			result.Confidence = gitobs.ConfidenceIncomplete
		}
		asset.Classification = gitobs.Classify(asset.Facts)
		result.Assets = append(result.Assets, asset)
	}
	for _, ref := range refs {
		if attached[ref.Name] {
			continue
		}
		confidence := gitobs.ConfidenceObserved
		branchOnly := listed
		if !listed {
			confidence = gitobs.ConfidenceIncomplete
		}
		asset := GitAsset{Worktree: gitobs.Worktree{Branch: ref.Name, Head: ref.Head}, Facts: gitobs.AssetFacts{Confidence: confidence, Branch: ref.Name, BranchOnly: branchOnly, DefaultBranch: ref.Name == result.DefaultBranch, Status: gitobs.Status{Confidence: gitobs.ConfidenceObserved}}}
		if !s.addReachability(ctx, directory, result.DefaultBranch, &asset) {
			asset.Facts.Confidence = gitobs.ConfidenceIncomplete
			result.Confidence = gitobs.ConfidenceIncomplete
		}
		asset.Classification = gitobs.Classify(asset.Facts)
		result.Assets = append(result.Assets, asset)
	}
	sortAssets(result.Assets)
	result.Hash = observationHash(result)
	return result, nil
}
func repoState(inside, bare []byte, insideErr, bareErr error) gitobs.RepoState {
	if bareErr == nil && strings.TrimSpace(string(bare)) == "true" {
		return gitobs.RepoBare
	}
	if isNotRepository(insideErr) && isNotRepository(bareErr) {
		return gitobs.RepoNonGit
	}
	if insideErr == nil && bareErr == nil && !(strings.TrimSpace(string(inside)) == "false" && strings.TrimSpace(string(bare)) == "false") {
		return gitobs.ParseRepoState(string(inside), string(bare))
	}
	return gitobs.RepoUnknown
}

func isNotRepository(err error) bool {
	var runnerError *GitRunnerError
	return errors.As(err, &runnerError) && runnerError.Kind == "not_repository"
}

func (s *GitScanner) run(ctx context.Context, directory string, plan gitobs.CommandPlan) ([]byte, error) {
	return s.runner(ctx, directory, plan)
}

func (s *GitScanner) worktrees(ctx context.Context, directory string) ([]gitobs.Worktree, bool) {
	output, err := s.run(ctx, directory, gitobs.WorktreeListPlan())
	if err == nil {
		if worktrees, parseErr := gitobs.ParseWorktreePorcelainZ(output); parseErr == nil {
			return worktrees, true
		}
	}
	output, err = s.run(ctx, directory, gitobs.WorktreeListFallbackPlan())
	if err != nil {
		return nil, false
	}
	worktrees, err := gitobs.ParseWorktreePorcelain(output)
	return worktrees, err == nil
}
func (s *GitScanner) refs(ctx context.Context, directory string) ([]gitobs.Ref, bool) {
	output, err := s.run(ctx, directory, gitobs.RefPlan())
	if err != nil {
		return nil, false
	}
	refs, err := gitobs.ParseLocalRefs(output)
	return refs, err == nil
}
func (s *GitScanner) status(ctx context.Context, directory string) (gitobs.Status, error) {
	output, err := s.run(ctx, directory, gitobs.StatusPlan())
	if err != nil {
		return gitobs.Status{Confidence: gitobs.ConfidenceUnknown}, err
	}
	return gitobs.ParseStatusPorcelainV2(output)
}
func (s *GitScanner) defaultBranch(ctx context.Context, directory string, refs []gitobs.Ref, bare bool) string {
	plan := gitobs.CommandPlan{Executable: "git", Args: []string{"symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"}}
	output, err := s.run(ctx, directory, plan)
	if err == nil {
		if candidate := observedBranch(strings.TrimSpace(string(output)), "refs/remotes/origin/", refs); candidate != "" {
			return candidate
		}
	}
	if !bare {
		return ""
	}
	plan = gitobs.CommandPlan{Executable: "git", Args: []string{"symbolic-ref", "--quiet", "HEAD"}}
	output, err = s.run(ctx, directory, plan)
	if err != nil {
		return ""
	}
	return observedBranch(strings.TrimSpace(string(output)), "refs/heads/", refs)
}

func observedBranch(reference, prefix string, refs []gitobs.Ref) string {
	if !strings.HasPrefix(reference, prefix) {
		return ""
	}
	candidate := strings.TrimPrefix(reference, prefix)
	for _, ref := range refs {
		if ref.Name == candidate {
			return candidate
		}
	}
	return ""
}

func (s *GitScanner) addReachability(ctx context.Context, directory, base string, asset *GitAsset) bool {
	if base == "" || asset.Facts.Branch == "" || asset.Facts.Detached {
		return false
	}
	baseRef, headRef := "refs/heads/"+base, "refs/heads/"+asset.Facts.Branch
	mergePlan, err := gitobs.MergeBasePlan(baseRef, headRef)
	if err != nil {
		return false
	}
	output, err := s.run(ctx, directory, mergePlan)
	if err != nil {
		return false
	}
	mergeBase, err := gitobs.ParseMergeBase(output)
	if err != nil {
		return false
	}
	asset.Facts.Fingerprint.MergeBaseKnown = true
	if asset.Worktree.Head != "" {
		asset.Facts.Fingerprint.MergeBaseEqualsHead = mergeBase == asset.Worktree.Head
	}
	aheadPlan, err := gitobs.AheadBehindPlan(baseRef, headRef)
	if err != nil {
		return false
	}
	output, err = s.run(ctx, directory, aheadPlan)
	if err != nil {
		return false
	}
	asset.Facts.DefaultAhead, asset.Facts.DefaultBehind, err = gitobs.ParseAheadBehind(output)
	if err != nil {
		asset.Facts.DefaultAhead, asset.Facts.DefaultBehind = 0, 0
		return false
	}
	asset.Facts.Fingerprint.DefaultCountsKnown = true
	return true
}

func sortAssets(assets []GitAsset) {
	sort.Slice(assets, func(i, j int) bool {
		left, right := assets[i], assets[j]
		if left.Facts.Branch != right.Facts.Branch {
			return left.Facts.Branch < right.Facts.Branch
		}
		if left.Facts.WorktreePath != right.Facts.WorktreePath {
			return left.Facts.WorktreePath < right.Facts.WorktreePath
		}
		return left.Worktree.Head < right.Worktree.Head
	})
}

func observationHash(observation GitObservation) string { return gitobs.HashObservation(observation) }

func runGitPlan(ctx context.Context, directory string, plan gitobs.CommandPlan) ([]byte, error) {
	if err := allowedReadOnlyPlan(plan); err != nil {
		return nil, &GitRunnerError{Kind: "rejected"}
	}
	command := exec.CommandContext(ctx, plan.Executable, plan.Args...)
	command.Dir = directory
	command.WaitDelay = time.Second
	command.Env = scannerEnvironment()
	var stderr, stdoutBuffer cappedBuffer
	command.Stderr = &stderr
	terminate := configureGitProcess(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, &GitRunnerError{Kind: "unavailable"}
	}
	command.Cancel = func() error { terminate(); return stdout.Close() }
	if err := command.Start(); err != nil {
		if ctx.Err() != nil {
			return nil, &GitRunnerError{Kind: "cancelled"}
		}
		return nil, &GitRunnerError{Kind: "unavailable"}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			terminate()
			_ = stdout.Close()
		case <-done:
		}
	}()
	_, readErr := io.Copy(&stdoutBuffer, stdout)
	waitErr := command.Wait()
	close(done)
	if readErr != nil || stdoutBuffer.overflow {
		return nil, &GitRunnerError{Kind: "output_unavailable"}
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, &GitRunnerError{Kind: "cancelled"}
		}
		if strings.Contains(strings.ToLower(stderr.String()), "not a git repository") {
			return nil, &GitRunnerError{Kind: "not_repository"}
		}
		return nil, &GitRunnerError{Kind: "failed"}
	}
	return stdoutBuffer.bytes, nil
}

type cappedBuffer struct {
	bytes    []byte
	overflow bool
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	remaining := maxGitOutput - len(b.bytes)
	if remaining < len(value) {
		b.overflow = true
	}
	if remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		b.bytes = append(b.bytes, value[:remaining]...)
	}
	return len(value), nil
}
func (b *cappedBuffer) String() string { return string(b.bytes) }

func scannerEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		environment = append(environment, value)
	}
	return append(environment, "LC_ALL=C", "LANG=C", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
}
func allowedReadOnlyPlan(plan gitobs.CommandPlan) error {
	if plan.Executable != "git" || len(plan.Args) == 0 {
		return errors.New("git plan rejected")
	}
	joined := strings.Join(plan.Args, "\x00")
	fixed := map[string]bool{
		"rev-parse\x00--is-inside-work-tree": true, "rev-parse\x00--is-bare-repository": true,
		"rev-parse\x00--path-format=absolute\x00--git-common-dir": true, "rev-parse\x00--path-format=absolute\x00--show-toplevel": true,
		"worktree\x00list\x00--porcelain\x00-z": true, "worktree\x00list\x00--porcelain": true,
		"--no-optional-locks\x00-c\x00core.fsmonitor=false\x00status\x00--porcelain=v2\x00-z\x00--branch": true, "for-each-ref\x00--format=%(refname)%00%(objectname)%00\x00refs/heads": true,
		"symbolic-ref\x00--quiet\x00refs/remotes/origin/HEAD": true, "symbolic-ref\x00--quiet\x00HEAD": true,
	}
	if fixed[joined] {
		return nil
	}
	if len(plan.Args) == 3 && plan.Args[0] == "merge-base" && validObservedRef(plan.Args[1]) && validObservedRef(plan.Args[2]) {
		return nil
	}
	if len(plan.Args) == 4 && plan.Args[0] == "rev-list" && plan.Args[1] == "--left-right" && plan.Args[2] == "--count" {
		left, right, found := strings.Cut(plan.Args[3], "...")
		if found && validObservedRef(left) && validObservedRef(right) {
			return nil
		}
	}
	return errors.New("git plan rejected")
}
func validObservedRef(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "-") && !strings.ContainsAny(ref, "\x00\r\n")
}
