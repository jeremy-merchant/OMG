package git

import (
	"os"
	"reflect"
	"testing"
)

func TestCommandPlansAreReadOnly(t *testing.T) {
	allowed := map[string]bool{
		"rev-parse": true, "worktree": true, "status": true,
		"for-each-ref": true, "rev-list": true, "merge-base": true,
		"--no-optional-locks": true,
	}
	for _, plan := range AllReadOnlyPlans() {
		if plan.Executable != "git" || len(plan.Args) == 0 || !allowed[plan.Args[0]] {
			t.Fatalf("unsafe command plan: %#v", plan)
		}
	}
	if got := WorktreeListPlan().Args; !reflect.DeepEqual(got, []string{"worktree", "list", "--porcelain", "-z"}) {
		t.Fatalf("worktree args = %#v", got)
	}
	if got := StatusPlan().Args; !reflect.DeepEqual(got, []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "status", "--porcelain=v2", "-z", "--branch"}) {
		t.Fatalf("status args = %#v", got)
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	input := "worktree /tmp/主 项目\x00HEAD aabbcc\x00branch refs/heads/feature/空 格\x00\x00" +
		"worktree /tmp/deleted\x00HEAD deadbeef\x00branch refs/heads/old\x00prunable gitdir file points to non-existent location\x00"
	got, err := ParseWorktreePorcelainZ([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "/tmp/主 项目" || got[0].Branch != "feature/空 格" || !got[1].Prunable {
		t.Fatalf("worktrees = %#v", got)
	}
}

func TestParseWorktreePorcelainNewline(t *testing.T) {
	input := "worktree /tmp/space dir\nHEAD abc\nbranch refs/heads/trunk\n\nworktree /tmp/bare\nbare\n\n"
	got, err := ParseWorktreePorcelain([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "/tmp/space dir" || got[0].Branch != "trunk" || !got[1].Bare {
		t.Fatalf("worktrees = %#v", got)
	}
}

func TestMalformedWorktreePorcelainReturnsStableError(t *testing.T) {
	if _, err := ParseWorktreePorcelainZ([]byte("branch refs/heads/topic\x00")); err == nil {
		t.Fatal("malformed worktree porcelain was accepted")
	}
}

func TestParseStatusPorcelainV2(t *testing.T) {
	input := "# branch.oid abcdef\x00# branch.head feature/空 格\x00# branch.upstream origin/feature/空 格\x00# branch.ab +2 -1\x00" +
		"1 M. N... 100644 100644 100644 a b src/主 file.go\x00? new file.txt\x00"
	got, err := ParseStatusPorcelainV2([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "feature/空 格" || got.Ahead != 2 || got.Behind != 1 || got.TrackedDirty != 1 || got.Untracked != 1 || got.Confidence != ConfidenceObserved {
		t.Fatalf("status = %#v", got)
	}
}

func TestParseStatusPorcelainV2DetachedAndMalformed(t *testing.T) {
	detached, err := ParseStatusPorcelainV2([]byte("# branch.oid abc\x00# branch.head (detached)\x00"))
	if err != nil || !detached.Detached || detached.Confidence != ConfidenceObserved {
		t.Fatalf("detached = %#v, err = %v", detached, err)
	}
	malformed, err := ParseStatusPorcelainV2([]byte("1 broken\x00"))
	if err == nil || malformed.Confidence != ConfidenceUnknown {
		t.Fatalf("malformed = %#v, err = %v", malformed, err)
	}
}

func TestPorcelainFixtures(t *testing.T) {
	worktrees, err := os.ReadFile("../../git/testdata/worktrees-z.fixture")
	if err != nil {
		t.Fatal(err)
	}
	gotWorktrees, err := ParseWorktreePorcelainZ(worktrees)
	if err != nil || len(gotWorktrees) != 2 || gotWorktrees[0].Path != "/tmp/主 项目" || !gotWorktrees[1].Prunable {
		t.Fatalf("worktree fixture = %#v, err = %v", gotWorktrees, err)
	}
	status, err := os.ReadFile("../../git/testdata/status-branch-z.fixture")
	if err != nil {
		t.Fatal(err)
	}
	gotStatus, err := ParseStatusPorcelainV2(status)
	if err != nil || gotStatus.Branch != "feature/空 格" || gotStatus.Ahead != 2 || gotStatus.Behind != 1 {
		t.Fatalf("status fixture = %#v, err = %v", gotStatus, err)
	}
}

func TestParseRefAndReachabilityObservations(t *testing.T) {
	refs, err := ParseLocalRefs([]byte("refs/heads/トピック\x00abc123\x00refs/heads/trunk\x00def456\x00"))
	if err != nil || len(refs) != 2 || refs[0].Name != "トピック" {
		t.Fatalf("refs = %#v, err = %v", refs, err)
	}
	ahead, behind, err := ParseAheadBehind([]byte("3\t2\n"))
	if err != nil || ahead != 2 || behind != 3 {
		t.Fatalf("ahead-behind = %d, %d, %v", ahead, behind, err)
	}
	base, err := ParseMergeBase([]byte("abc123\n"))
	if err != nil || base != "abc123" {
		t.Fatalf("merge base = %q, %v", base, err)
	}
	for _, malformed := range [][]byte{[]byte("refs/heads/a\x00"), []byte("-1 0"), []byte("abc def")} {
		if string(malformed) == "-1 0" {
			_, _, err = ParseAheadBehind(malformed)
		} else if string(malformed) == "abc def" {
			_, err = ParseMergeBase(malformed)
		} else {
			_, err = ParseLocalRefs(malformed)
		}
		if err == nil {
			t.Fatalf("malformed observation accepted: %q", malformed)
		}
	}
}

func TestRenameStatusConsumesOriginalPath(t *testing.T) {
	input := "2 R. N... 100644 100644 100644 a b c R100 renamed name\x00old name\x00"
	status, err := ParseStatusPorcelainV2([]byte(input))
	if err != nil || status.TrackedDirty != 1 {
		t.Fatalf("rename status = %#v, err = %v", status, err)
	}
}

func TestClassifyAssets(t *testing.T) {
	tests := []struct {
		name  string
		facts AssetFacts
		want  Classification
	}{
		{"active owner", AssetFacts{Confidence: ConfidenceObserved, Branch: "trunk", Owner: OwnerFacts{Registered: true, State: OwnerActive}}, ClassActiveOwned},
		{"waiting owner", AssetFacts{Confidence: ConfidenceObserved, Branch: "trunk", Owner: OwnerFacts{Registered: true, State: OwnerWaiting}}, ClassWaitingOwned},
		{"ready owner", AssetFacts{Confidence: ConfidenceObserved, Branch: "trunk", Owner: OwnerFacts{Registered: true, State: OwnerReady}}, ClassReadyOwned},
		{"stale owner", AssetFacts{Confidence: ConfidenceObserved, Branch: "trunk", Owner: OwnerFacts{Registered: true, State: OwnerStale}}, ClassStaleOwned},
		{"orphaned worktree", AssetFacts{Confidence: ConfidenceObserved, WorktreePath: "/gone", WorktreePrunable: true}, ClassOrphanedWorktree},
		{"branch only", AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", BranchOnly: true}, ClassBranchOnly},
		{"dirty unowned", AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", Status: Status{TrackedDirty: 1, Confidence: ConfidenceObserved}}, ClassDirtyUnowned},
		{"unpushed", AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", Status: Status{Ahead: 1, Upstream: "origin/topic", Confidence: ConfidenceObserved}}, ClassUnpushed},
		{"diverged", AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", Status: Status{Ahead: 1, Behind: 1, Upstream: "origin/topic", Confidence: ConfidenceObserved}}, ClassDiverged},
		{"detached", AssetFacts{Confidence: ConfidenceObserved, Detached: true}, ClassDetachedUnowned},
		{"merged clean", AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}, Fingerprint: FingerprintFacts{MergeBaseKnown: true, MergeBaseEqualsHead: true}}, ClassMergedClean},
		{"possibly integrated", AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}, Fingerprint: FingerprintFacts{MergeBaseKnown: false}}, ClassPossiblyIntegrated},
		{"unknown", AssetFacts{Confidence: ConfidenceIncomplete, Branch: "topic"}, ClassUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.facts)
			if !got.Has(tt.want) {
				t.Fatalf("classification = %#v; want %q", got, tt.want)
			}
		})
	}
}

func TestCleanupPlanIsCandidateOnlyAndBlocksUncertainAssets(t *testing.T) {
	plan := BuildCleanupPlan([]AssetFacts{
		{Confidence: ConfidenceObserved, Branch: "topic", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}, Fingerprint: FingerprintFacts{MergeBaseKnown: true, MergeBaseEqualsHead: true}},
		{Confidence: ConfidenceIncomplete, Branch: "unknown"},
		{Confidence: ConfidenceObserved, Branch: "status-unknown", BranchOnly: true},
		{Confidence: ConfidenceObserved, Branch: "main", BranchOnly: true, DefaultBranch: true, Status: Status{Confidence: ConfidenceObserved}, Fingerprint: FingerprintFacts{MergeBaseKnown: true, MergeBaseEqualsHead: true}},
		{Confidence: ConfidenceObserved, Branch: "dirty", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved, TrackedDirty: 1}, Fingerprint: FingerprintFacts{MergeBaseKnown: true, MergeBaseEqualsHead: true}},
		{Confidence: ConfidenceObserved, Branch: "owned", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}, Fingerprint: FingerprintFacts{MergeBaseKnown: true, MergeBaseEqualsHead: true}, Owner: OwnerFacts{Registered: true, State: OwnerActive}},
	})
	if plan.Mutating || len(plan.Candidates) != 1 || plan.Candidates[0].Classification != ClassSafeToRemoveCandidate || len(plan.Blocked) != 5 {
		t.Fatalf("cleanup plan = %#v", plan)
	}
}

func TestIncompleteStatusCannotProduceConfidentCleanupClassification(t *testing.T) {
	classification := Classify(AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", BranchOnly: true})
	if classification.Confidence != ConfidenceIncomplete || !classification.Has(ClassUnknown) || classification.Has(ClassSafeToRemoveCandidate) {
		t.Fatalf("classification = %#v", classification)
	}
}

func TestParseRepoState(t *testing.T) {
	if got := ParseRepoState("false\n", ""); got != RepoNonGit {
		t.Fatalf("non-git = %q", got)
	}
	if got := ParseRepoState("true\n", "true\n"); got != RepoBare {
		t.Fatalf("bare = %q", got)
	}
	if got := ParseRepoState("true\n", "false\n"); got != RepoWorktree {
		t.Fatalf("worktree = %q", got)
	}
	if got := ParseRepoState("maybe", "false"); got != RepoUnknown {
		t.Fatalf("unknown = %q", got)
	}
}
