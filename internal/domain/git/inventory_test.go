package git

import (
	"testing"
	"time"

	coord "example.invalid/coordledger/internal/domain/coordination"
	"example.invalid/coordledger/internal/domain/lineage"
)

func TestExactDiffDistinguishesFactsAndClassifications(t *testing.T) {
	base := Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, Branch: "topic", Status: Status{Confidence: ConfidenceObserved}}, Worktree: Worktree{Branch: "topic", Head: "abc"}, Classification: AssetClassification{Labels: []Classification{ClassBranchOnly}, Confidence: ConfidenceObserved}}
	changed := base
	changed.Facts.Status.TrackedDirty = 1
	changed.Classification = AssetClassification{Labels: []Classification{ClassDirtyUnowned}, Confidence: ConfidenceObserved}
	added := Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, Branch: "new", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}}, Worktree: Worktree{Branch: "new", Head: "def"}, Classification: AssetClassification{Labels: []Classification{ClassBranchOnly}, Confidence: ConfidenceObserved}}
	d := ExactDiff(Observation{Assets: []Asset{base, Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, Branch: "gone", BranchOnly: true}, Worktree: Worktree{Branch: "gone", Head: "old"}}}}, Observation{Assets: []Asset{changed, added}})
	if len(d.Changed) != 1 || len(d.New) != 1 || len(d.Missing) != 1 {
		t.Fatalf("diff = %+v", d)
	}
	if base.StableFingerprint() != changed.StableFingerprint() {
		t.Fatal("fact changes must not change stable fingerprint")
	}
}

func TestHashObservationIsPermutationStable(t *testing.T) {
	a := Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, Branch: "a", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}}, Worktree: Worktree{Branch: "a"}, Classification: AssetClassification{Labels: []Classification{ClassBranchOnly}, Confidence: ConfidenceObserved}}
	b := Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, Branch: "b", BranchOnly: true, Status: Status{Confidence: ConfidenceObserved}}, Worktree: Worktree{Branch: "b"}, Classification: AssetClassification{Labels: []Classification{ClassBranchOnly}, Confidence: ConfidenceObserved}}
	left := Observation{Revision: ObservationRevision, Repository: RepoBare, Confidence: ConfidenceObserved, Assets: []Asset{a, b}}
	right := left
	right.Assets = []Asset{b, a}
	if HashObservation(left) != HashObservation(right) {
		t.Fatal("hash changed with asset ordering")
	}
}

func TestReconcileOwnershipUsesOnlyUniqueExactCanonicalWorktreeMatches(t *testing.T) {
	asset := Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, WorktreePath: "/repo/work", Status: Status{Confidence: ConfidenceObserved}}, Worktree: Worktree{Path: "/repo/work"}}
	one := ownershipCandidate("session-1", "/repo/work", lineage.TaskInProgress, lineage.RunRunning)
	observed, records := ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{asset}}, []OwnershipCandidate{one}, nil)
	if !observed.Assets[0].Facts.Owner.Registered || observed.Assets[0].Facts.Owner.State != OwnerActive || records[0].OwnerSessionID != one.Session.ID {
		t.Fatalf("unique owner = %+v %+v", observed.Assets[0].Facts.Owner, records[0])
	}
	two := ownershipCandidate("session-2", "/repo/work", lineage.TaskInProgress, lineage.RunRunning)
	observed, records = ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{asset}}, []OwnershipCandidate{one, two}, nil)
	if observed.Assets[0].Facts.Owner.Registered || records[0].OwnerSessionID != "" {
		t.Fatal("ambiguous worktree was adopted")
	}
	prefix := ownershipCandidate("session-3", "/repo/work-extra", lineage.TaskInProgress, lineage.RunRunning)
	observed, _ = ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{asset}}, []OwnershipCandidate{prefix}, nil)
	if observed.Assets[0].Facts.Owner.Registered {
		t.Fatal("prefix worktree match was accepted")
	}
	noncanonical := ownershipCandidate("session-4", "/repo/../repo/work", lineage.TaskInProgress, lineage.RunRunning)
	observed, _ = ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{asset}}, []OwnershipCandidate{noncanonical}, nil)
	if observed.Assets[0].Facts.Owner.Registered {
		t.Fatal("noncanonical automatic worktree was accepted")
	}
}

func TestReconcileOwnershipExplicitAdoptionWinsAndStateFailsClosed(t *testing.T) {
	asset := Asset{Facts: AssetFacts{Confidence: ConfidenceObserved, WorktreePath: "/repo/work", Status: Status{Confidence: ConfidenceObserved}}, Worktree: Worktree{Path: "/repo/work"}}
	auto := ownershipCandidate("session-auto", "/repo/work", lineage.TaskInProgress, lineage.RunRunning)
	adopted := ownershipCandidate("session-adopted", "/elsewhere", lineage.TaskWaiting, lineage.RunWaiting)
	adopted.Session.WorktreeRef = ""
	adoption := coord.Adoption{ID: "adoption-1", ProjectID: "project", GitAssetID: asset.StableFingerprint(), NewOwnerSessionID: string(adopted.Session.ID), Reason: "owner", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	observed, records := ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{asset}}, []OwnershipCandidate{auto, adopted}, []coord.Adoption{adoption})
	if records[0].OwnerSessionID != adopted.Session.ID || observed.Assets[0].Facts.Owner.State != OwnerWaiting {
		t.Fatalf("explicit adoption did not win: %+v %+v", records[0], observed.Assets[0].Facts.Owner)
	}
	corrupt := adopted
	corrupt.Run.ID = ""
	observed, _ = ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{asset}}, []OwnershipCandidate{auto, corrupt}, []coord.Adoption{adoption})
	if observed.Assets[0].Facts.Owner.Registered {
		t.Fatal("invalid explicit owner was accepted")
	}
}

func ownershipCandidate(id, path string, taskState lineage.TaskState, runState lineage.RunState) OwnershipCandidate {
	return OwnershipCandidate{
		Session: lineage.AgentSession{ID: lineage.ID(id), ProjectID: "project", TaskID: lineage.ID("task-" + id), WorktreeRef: path},
		Task:    lineage.Task{ID: lineage.ID("task-" + id), ProjectID: "project", State: taskState},
		Run:     lineage.TaskRun{ID: lineage.ID("run-" + id), TaskID: lineage.ID("task-" + id), SessionID: lineage.ID(id), State: runState},
	}
}

func TestOwnerStateMappingIsConservative(t *testing.T) {
	candidate := ownershipCandidate("state", "/repo/state", lineage.TaskWorkComplete, lineage.RunWorkComplete)
	if got := ownerState(candidate); got != OwnerReady {
		t.Fatalf("ready state = %q", got)
	}
	candidate.Task.State = lineage.TaskFailed
	if got := ownerState(candidate); got != OwnerStale {
		t.Fatalf("terminal state = %q", got)
	}
	candidate.Task.State = lineage.TaskReady
	candidate.Run.State = lineage.RunRunning
	if got := ownerState(candidate); got != OwnerActive {
		t.Fatalf("run active state = %q", got)
	}
	candidate.Run.State = lineage.RunState("unexpected")
	if got := ownerState(candidate); got != OwnerUnknown {
		t.Fatalf("unknown state = %q", got)
	}
}
