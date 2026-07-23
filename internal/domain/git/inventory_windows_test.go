//go:build windows

package git

import (
	"testing"

	"example.invalid/coordledger/internal/domain/lineage"
)

func TestWindowsWorktreeIdentityNormalizesSeparatorsAndCase(t *testing.T) {
	slashed := Asset{
		Facts:    AssetFacts{Confidence: ConfidenceObserved, WorktreePath: `C:/Repo/Work`, Status: Status{Confidence: ConfidenceObserved}},
		Worktree: Worktree{Path: `C:/Repo/Work`},
	}
	backslashed := slashed
	backslashed.Facts.WorktreePath = `c:\repo\work`
	backslashed.Worktree.Path = `c:\repo\work`
	if slashed.StableFingerprint() != backslashed.StableFingerprint() {
		t.Fatal("separator- and case-equivalent Windows paths produced different fingerprints")
	}
	candidate := ownershipCandidate("windows-owner", `c:\repo\work`, lineage.TaskInProgress, lineage.RunRunning)
	observed, records := ReconcileOwnership(Observation{Revision: ObservationRevision, Assets: []Asset{slashed}}, []OwnershipCandidate{candidate}, nil)
	if !observed.Assets[0].Facts.Owner.Registered || records[0].OwnerSessionID != candidate.Session.ID {
		t.Fatalf("separator- and case-equivalent Windows worktree was not owned: %+v %+v", observed.Assets[0].Facts.Owner, records[0])
	}
}
