package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

var _ ports.Scanner = (*GitScanner)(nil)

func TestGitScannerFakeRunnerUsesOnlyFixedReadOnlyArgv(t *testing.T) {
	var calls []gitobs.CommandPlan
	scanner := NewGitScanner(GitScannerDependencies{Runner: func(_ context.Context, _ string, plan gitobs.CommandPlan) ([]byte, error) {
		calls = append(calls, plan)
		switch strings.Join(plan.Args, " ") {
		case "rev-parse --is-inside-work-tree":
			return []byte("true\n"), nil
		case "rev-parse --is-bare-repository":
			return []byte("false\n"), nil
		case "rev-parse --path-format=absolute --git-common-dir":
			return []byte("/private/repo/.git\n"), nil
		case "rev-parse --path-format=absolute --show-toplevel":
			return []byte("/private/repo\n"), nil
		case "worktree list --porcelain -z":
			return []byte("worktree /private/repo\x00HEAD abc\x00branch refs/heads/trunk\x00\x00"), nil
		case "for-each-ref --format=%(refname)%00%(objectname)%00 refs/heads":
			return []byte("refs/heads/trunk\x00abc\x00"), nil
		case "symbolic-ref --quiet refs/remotes/origin/HEAD":
			return []byte("refs/remotes/origin/trunk\n"), nil
		case "--no-optional-locks -c core.fsmonitor=false status --porcelain=v2 -z --branch":
			return []byte("# branch.oid abc\x00# branch.head trunk\x00"), nil
		case "merge-base refs/heads/trunk refs/heads/trunk":
			return []byte("abc\n"), nil
		case "rev-list --left-right --count refs/heads/trunk...refs/heads/trunk":
			return []byte("3\t2\n"), nil
		default:
			return nil, errors.New("unexpected argv")
		}
	}})
	observation, err := scanner.Scan(context.Background(), "/private/repo")
	if err != nil {
		t.Fatal(err)
	}
	if observation.Repository != gitobs.RepoWorktree || observation.DefaultBranch != "trunk" || len(observation.Assets) != 1 {
		t.Fatalf("observation = %#v", observation)
	}
	if observation.Assets[0].Facts.Fingerprint.MergeBaseEqualsHead != true || observation.Assets[0].Facts.DefaultAhead != 2 || observation.Assets[0].Facts.DefaultBehind != 3 {
		t.Fatalf("asset = %#v", observation.Assets[0])
	}
	for _, call := range calls {
		if err := allowedReadOnlyPlan(call); err != nil {
			t.Fatalf("unsafe argv %#v: %v", call, err)
		}
	}
}

func TestGitScannerMarksStaleWorktreeIncomplete(t *testing.T) {
	scanner := NewGitScanner(GitScannerDependencies{Runner: func(_ context.Context, directory string, plan gitobs.CommandPlan) ([]byte, error) {
		switch strings.Join(plan.Args, " ") {
		case "rev-parse --is-inside-work-tree":
			return []byte("true\n"), nil
		case "rev-parse --is-bare-repository":
			return []byte("false\n"), nil
		case "worktree list --porcelain -z":
			return []byte("worktree /private/deleted\x00HEAD deadbeef\x00branch refs/heads/topic\x00prunable missing\x00\x00"), nil
		case "for-each-ref --format=%(refname)%00%(objectname)%00 refs/heads":
			return []byte("refs/heads/topic\x00deadbeef\x00"), nil
		default:
			return nil, errors.New("unavailable")
		}
	}})
	observation, err := scanner.Scan(context.Background(), "/private/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 1 || observation.Assets[0].Facts.Confidence != gitobs.ConfidenceIncomplete || !observation.Assets[0].Classification.Has(gitobs.ClassUnknown) {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestGitScannerNonGitAndSafeFailure(t *testing.T) {
	nonGit := NewGitScanner(GitScannerDependencies{Runner: func(_ context.Context, _ string, _ gitobs.CommandPlan) ([]byte, error) {
		return nil, &GitRunnerError{Kind: "not_repository"}
	}})
	observation, err := nonGit.Scan(context.Background(), "/private/non-git")
	if err != nil || observation.Repository != gitobs.RepoNonGit || observation.Hash == "" {
		t.Fatalf("observation=%#v err=%v", observation, err)
	}
	if err := allowedReadOnlyPlan(gitobs.CommandPlan{Executable: "git", Args: []string{"checkout", "topic"}}); err == nil {
		t.Fatal("mutating plan accepted")
	}
	if err := allowedReadOnlyPlan(gitobs.CommandPlan{Executable: "git", Args: []string{"merge-base", "-bad", "refs/heads/topic"}}); err == nil {
		t.Fatal("option-like ref accepted")
	}
	if !strings.Contains((&ScanError{Code: "failed"}).Error(), "failed") || strings.Contains((&ScanError{Code: "failed"}).Error(), "/private") {
		t.Fatal("unsafe scan error")
	}
}

func TestGitScannerFixtureClassifiesAllRecoveryStatesWithoutMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo with 한글")
	runFixtureGit(t, "", "init", "-b", "trunk", root)
	runFixtureGit(t, root, "config", "user.email", "fixture@example.invalid")
	runFixtureGit(t, root, "config", "user.name", "Fixture")
	remote := filepath.Join(t.TempDir(), "origin.git")
	runFixtureGit(t, "", "init", "--bare", remote)
	runFixtureGit(t, root, "remote", "add", "origin", remote)
	writeFixtureFile(t, filepath.Join(root, "tracked.txt"), "base\n")
	runFixtureGit(t, root, "add", "tracked.txt")
	runFixtureGit(t, root, "commit", "-m", "initial")

	linked := filepath.Join(t.TempDir(), "linked space 한글")
	runFixtureGit(t, root, "worktree", "add", "-b", "topic", linked)
	writeFixtureFile(t, filepath.Join(linked, "topic.txt"), "one\n")
	runFixtureGit(t, linked, "add", "topic.txt")
	runFixtureGit(t, linked, "commit", "-m", "topic one")
	writeFixtureFile(t, filepath.Join(linked, "topic.txt"), "two\n")
	runFixtureGit(t, linked, "commit", "-am", "topic two")

	writeFixtureFile(t, filepath.Join(root, "trunk.txt"), "trunk\n")
	runFixtureGit(t, root, "add", "trunk.txt")
	runFixtureGit(t, root, "commit", "-m", "trunk one")
	trunkHead := strings.TrimSpace(runFixtureGit(t, root, "rev-parse", "refs/heads/trunk"))
	runFixtureGit(t, root, "update-ref", "refs/remotes/origin/trunk", trunkHead)
	runFixtureGit(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")

	unpushed := filepath.Join(t.TempDir(), "unpushed")
	runFixtureGit(t, root, "worktree", "add", "-b", "unpushed", unpushed)
	runFixtureGit(t, unpushed, "branch", "--set-upstream-to=origin/trunk", "unpushed")
	writeFixtureFile(t, filepath.Join(unpushed, "unpushed.txt"), "local only\n")
	runFixtureGit(t, unpushed, "add", "unpushed.txt")
	runFixtureGit(t, unpushed, "commit", "-m", "unpushed")

	writeFixtureFile(t, filepath.Join(linked, "tracked.txt"), "dirty\n")
	writeFixtureFile(t, filepath.Join(linked, "untracked.txt"), "untracked\n")
	detached := filepath.Join(t.TempDir(), "detached")
	runFixtureGit(t, root, "worktree", "add", "--detach", detached, "HEAD")

	branchOnlyWorktree := filepath.Join(t.TempDir(), "branch-only")
	runFixtureGit(t, root, "worktree", "add", "-b", "branch-only", branchOnlyWorktree)
	writeFixtureFile(t, filepath.Join(branchOnlyWorktree, "branch-only.txt"), "branch only\n")
	runFixtureGit(t, branchOnlyWorktree, "add", "branch-only.txt")
	runFixtureGit(t, branchOnlyWorktree, "commit", "-m", "branch-only")
	runFixtureGit(t, root, "worktree", "remove", "--force", branchOnlyWorktree)
	runFixtureGit(t, root, "branch", "merged-clean", "trunk")

	stale := filepath.Join(t.TempDir(), "stale")
	runFixtureGit(t, root, "worktree", "add", "-b", "stale-topic", stale)
	if err := os.RemoveAll(stale); err != nil {
		t.Fatal(err)
	}

	before := fixtureScanEvidence(t, root, map[string]string{
		"root":     root,
		"topic":    linked,
		"unpushed": unpushed,
		"detached": detached,
	}, map[string]string{
		"root-tracked":     filepath.Join(root, "tracked.txt"),
		"root-trunk":       filepath.Join(root, "trunk.txt"),
		"topic-tracked":    filepath.Join(linked, "tracked.txt"),
		"topic-commit":     filepath.Join(linked, "topic.txt"),
		"topic-untracked":  filepath.Join(linked, "untracked.txt"),
		"unpushed-commit":  filepath.Join(unpushed, "unpushed.txt"),
		"detached-tracked": filepath.Join(detached, "tracked.txt"),
	})

	observation, err := NewGitScanner(GitScannerDependencies{}).Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if observation.DefaultBranch != "trunk" {
		t.Fatalf("default = %q", observation.DefaultBranch)
	}
	after := fixtureScanEvidence(t, root, map[string]string{
		"root":     root,
		"topic":    linked,
		"unpushed": unpushed,
		"detached": detached,
	}, map[string]string{
		"root-tracked":     filepath.Join(root, "tracked.txt"),
		"root-trunk":       filepath.Join(root, "trunk.txt"),
		"topic-tracked":    filepath.Join(linked, "tracked.txt"),
		"topic-commit":     filepath.Join(linked, "topic.txt"),
		"topic-untracked":  filepath.Join(linked, "untracked.txt"),
		"unpushed-commit":  filepath.Join(unpushed, "unpushed.txt"),
		"detached-tracked": filepath.Join(detached, "tracked.txt"),
	})
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("scan changed immutable fixture evidence\nbefore=%#v\nafter=%#v", before, after)
	}

	assertFixtureLabels(t, observation.Assets, "stale-topic", stale, gitobs.ClassUnknown, gitobs.ClassOrphanedWorktree)
	staleIncomplete := false
	for _, asset := range observation.Assets {
		if asset.Facts.Branch == "stale-topic" && sameFixturePath(asset.Facts.WorktreePath, stale) {
			staleIncomplete = asset.Classification.Confidence == gitobs.ConfidenceIncomplete
		}
	}
	if !staleIncomplete {
		t.Fatalf("stale worktree was not observed as incomplete: %#v", observation.Assets)
	}
	assertFixtureLabels(t, observation.Assets, "branch-only", "", gitobs.ClassBranchOnly)
	assertFixtureLabels(t, observation.Assets, "topic", linked, gitobs.ClassDirtyUnowned, gitobs.ClassDiverged)
	assertFixtureLabels(t, observation.Assets, "unpushed", unpushed, gitobs.ClassUnpushed)
	assertFixtureLabels(t, observation.Assets, "", detached, gitobs.ClassDetachedUnowned)
	assertFixtureLabels(t, observation.Assets, "merged-clean", "", gitobs.ClassMergedClean, gitobs.ClassSafeToRemoveCandidate)

	facts := make([]gitobs.AssetFacts, 0, len(observation.Assets))
	for _, asset := range observation.Assets {
		facts = append(facts, asset.Facts)
	}
	cleanup := gitobs.BuildCleanupPlan(facts)
	if cleanup.Mutating {
		t.Fatalf("cleanup plan must be inert: %#v", cleanup)
	}
	if len(cleanup.Candidates) != 1 || cleanup.Candidates[0].Branch != "merged-clean" || cleanup.Candidates[0].Classification != gitobs.ClassSafeToRemoveCandidate {
		t.Fatalf("cleanup candidates = %#v", cleanup.Candidates)
	}

	bare := filepath.Join(t.TempDir(), "fixture.git")
	runFixtureGit(t, "", "clone", "--bare", root, bare)
	bareObservation, err := NewGitScanner(GitScannerDependencies{}).Scan(context.Background(), bare)
	if err != nil || bareObservation.Repository != gitobs.RepoBare || bareObservation.DefaultBranch != "trunk" || len(bareObservation.Assets) == 0 {
		t.Fatalf("bare = %#v, err = %v", bareObservation, err)
	}
	nonGitObservation, err := NewGitScanner(GitScannerDependencies{}).Scan(context.Background(), t.TempDir())
	if err != nil || nonGitObservation.Repository != gitobs.RepoNonGit {
		t.Fatalf("non-git = %#v, err = %v", nonGitObservation, err)
	}
	gitDirObservation, err := NewGitScanner(GitScannerDependencies{}).Scan(context.Background(), filepath.Join(root, ".git"))
	if err != nil || gitDirObservation.Repository != gitobs.RepoUnknown {
		t.Fatalf("git directory = %#v, err = %v", gitDirObservation, err)
	}
}

func TestGitScannerHashCoversObservedFactsAndUnavailableDirectoryIsUnknown(t *testing.T) {
	base := GitObservation{Revision: GitObservationRevision, Repository: gitobs.RepoWorktree, Confidence: gitobs.ConfidenceObserved, CommonDir: "common", TopLevel: "top", DefaultBranch: "trunk", Assets: []GitAsset{{Facts: gitobs.AssetFacts{Confidence: gitobs.ConfidenceObserved, Branch: "topic", Status: gitobs.Status{Confidence: gitobs.ConfidenceObserved, Head: "one", TrackedDirty: 1}, DefaultAhead: 2}, Worktree: gitobs.Worktree{Path: "worktree", Head: "one"}}}}
	first := observationHash(base)
	base.Assets[0].Facts.Status.TrackedDirty++
	if first == observationHash(base) {
		t.Fatal("status change did not change hash")
	}
	base.Assets[0].Facts.Status.TrackedDirty--
	base.Assets[0].Worktree.Head = "two"
	if first == observationHash(base) {
		t.Fatal("head change did not change hash")
	}
	base.Assets[0].Worktree.Head = "one"
	base.Assets[0].Facts.DefaultAhead++
	if first == observationHash(base) {
		t.Fatal("count change did not change hash")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	observation, err := NewGitScanner(GitScannerDependencies{}).Scan(context.Background(), missing)
	if err != nil || observation.Repository != gitobs.RepoUnknown || observation.Confidence != gitobs.ConfidenceUnknown {
		t.Fatalf("missing = %#v, err = %v", observation, err)
	}
}

func TestGitScannerEnvironmentAndOutputCapAreSafe(t *testing.T) {
	t.Setenv("GIT_DIR", "/private/redirected")
	t.Setenv("GIT_TRACE", "/private/trace")
	t.Setenv("git_dir", "/private/lowercase-redirected")
	environment := strings.Join(scannerEnvironment(), "\x00")
	if strings.Contains(strings.ToUpper(environment), "GIT_DIR=/PRIVATE/REDIRECTED") || strings.Contains(strings.ToUpper(environment), "GIT_TRACE=/PRIVATE/TRACE") || strings.Contains(strings.ToUpper(environment), "GIT_DIR=/PRIVATE/LOWERCASE-REDIRECTED") || !strings.Contains(environment, "GIT_NO_LAZY_FETCH=1") || !strings.Contains(environment, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("unsafe scanner environment: %q", environment)
	}
	var buffer cappedBuffer
	if _, err := buffer.Write(make([]byte, maxGitOutput+1)); err != nil || !buffer.overflow || len(buffer.bytes) != maxGitOutput {
		t.Fatalf("cap = %#v, err = %v", buffer, err)
	}
}

type fixtureEvidence struct {
	Refs      string
	Worktrees string
	Status    map[string]string
	Index     map[string]string
	Files     map[string][]byte
}

func fixtureScanEvidence(t *testing.T, root string, worktrees, files map[string]string) fixtureEvidence {
	t.Helper()
	evidence := fixtureEvidence{
		Refs:      runFixtureGit(t, root, "for-each-ref", "--format=%(refname)%00%(objectname)%00"),
		Worktrees: runFixtureGit(t, root, "worktree", "list", "--porcelain", "-z"),
		Status:    make(map[string]string, len(worktrees)),
		Index:     make(map[string]string, len(worktrees)),
		Files:     make(map[string][]byte, len(files)),
	}
	for name, directory := range worktrees {
		evidence.Status[name] = runFixtureGit(t, directory, "--no-optional-locks", "status", "--porcelain=v2", "-z", "--branch")
		evidence.Index[name] = runFixtureGit(t, directory, "ls-files", "--stage", "-z")
	}
	for name, filename := range files {
		content, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("read fixture file %q: %v", filename, err)
		}
		evidence.Files[name] = content
	}
	return evidence
}

func writeFixtureFile(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFixtureLabels(t *testing.T, assets []GitAsset, branch, worktreePath string, labels ...gitobs.Classification) {
	t.Helper()
	for _, asset := range assets {
		if asset.Facts.Branch != branch || (worktreePath != "" && !sameFixturePath(asset.Facts.WorktreePath, worktreePath)) {
			continue
		}
		for _, label := range labels {
			if !asset.Classification.Has(label) {
				t.Fatalf("asset branch=%q path=%q labels=%#v, missing %q", branch, worktreePath, asset.Classification.Labels, label)
			}
		}
		return
	}
	t.Fatalf("asset branch=%q path=%q not found in %#v", branch, worktreePath, assets)
}

func sameFixturePath(left, right string) bool {
	return strings.TrimPrefix(left, "/private") == strings.TrimPrefix(right, "/private")
}

func runFixtureGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("fixture git %q: %v", args, err)
	}
	return string(output)
}
