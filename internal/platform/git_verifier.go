package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	gitobs "github.com/jeremy-merchant/oh-my-group/internal/domain/git"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

// GitVerifier reuses the scanner's constrained runner. Every accepted argv is
// independently allowlisted in allowedReadOnlyPlan.
type GitVerifier struct{ runner GitCommandRunner }

var _ ports.GitVerifier = (*GitVerifier)(nil)

func NewGitVerifier(dependencies GitScannerDependencies) *GitVerifier {
	runner := dependencies.Runner
	if runner == nil {
		runner = runGitPlan
	}
	return &GitVerifier{runner: runner}
}

func (v *GitVerifier) ResolveRevision(ctx context.Context, directory, ref string) (gitobs.RevisionEvidence, error) {
	commitPlan, err := gitobs.ResolveCommitPlan(ref)
	if err != nil {
		return gitobs.RevisionEvidence{}, err
	}
	commitBytes, err := v.runner(ctx, directory, commitPlan)
	if err != nil {
		return gitobs.RevisionEvidence{}, err
	}
	commit := strings.TrimSpace(string(commitBytes))
	treePlan, err := gitobs.ResolveTreePlan(commit)
	if err != nil {
		return gitobs.RevisionEvidence{}, err
	}
	treeBytes, err := v.runner(ctx, directory, treePlan)
	if err != nil {
		return gitobs.RevisionEvidence{}, err
	}
	revision := gitobs.RevisionEvidence{Commit: commit, Tree: strings.TrimSpace(string(treeBytes))}
	if reflogPlan, planErr := gitobs.ReflogPlan(ref); planErr == nil {
		if reflog, runErr := v.runner(ctx, directory, reflogPlan); runErr == nil {
			sum := sha256.Sum256(reflog)
			revision.RefFingerprint = "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	return revision, nil
}

// VerifyLocalIntegration proves the local-only rolling gate without granting
// any Git mutation authority. A missing merge base is reported as an
// unreachable candidate; failure to observe worktree status is an error
// because cleanliness must be positively established.
func (v *GitVerifier) VerifyLocalIntegration(ctx context.Context, directory, candidateSHA, rollingRef string) (gitobs.LocalIntegrationEvidence, error) {
	candidate, err := v.ResolveRevision(ctx, directory, candidateSHA)
	if err != nil {
		return gitobs.LocalIntegrationEvidence{}, err
	}
	rolling, err := v.ResolveRevision(ctx, directory, rollingRef)
	if err != nil {
		return gitobs.LocalIntegrationEvidence{}, err
	}
	evidence := gitobs.LocalIntegrationEvidence{Candidate: candidate, Rolling: rolling}
	if basePlan, planErr := gitobs.MergeBasePlan(candidate.Commit, rolling.Commit); planErr == nil {
		if output, runErr := v.runner(ctx, directory, basePlan); runErr == nil {
			evidence.CandidateReachable = strings.TrimSpace(string(output)) == candidate.Commit
		}
	}
	statusBytes, err := v.runner(ctx, directory, gitobs.StatusPlan())
	if err != nil {
		return gitobs.LocalIntegrationEvidence{}, err
	}
	status, err := gitobs.ParseStatusPorcelainV2(statusBytes)
	if err != nil {
		return gitobs.LocalIntegrationEvidence{}, err
	}
	evidence.WorktreeClean = status.TrackedDirty == 0 && status.Untracked == 0
	return evidence, nil
}

func (v *GitVerifier) Reconcile(ctx context.Context, directory, sourceCommit, declaredSourceTree, integrationCommit, currentIntegrationRef string) (gitobs.ReconcileEvidence, error) {
	source, err := v.ResolveRevision(ctx, directory, sourceCommit)
	if err != nil {
		return gitobs.ReconcileEvidence{}, err
	}
	integration, err := v.ResolveRevision(ctx, directory, integrationCommit)
	if err != nil {
		return gitobs.ReconcileEvidence{}, err
	}
	current, err := v.ResolveRevision(ctx, directory, currentIntegrationRef)
	if err != nil {
		return gitobs.ReconcileEvidence{}, err
	}
	evidence := gitobs.ReconcileEvidence{
		Source: source, Integration: integration, CurrentIntegration: current,
		SourceTreeMatches: source.Tree == declaredSourceTree,
		Method:            gitobs.IntegrationMethodUnknown,
	}
	if base, planErr := gitobs.MergeBasePlan(integration.Commit, current.Commit); planErr == nil {
		if output, runErr := v.runner(ctx, directory, base); runErr == nil {
			evidence.IntegrationRetained = strings.TrimSpace(string(output)) == integration.Commit
		}
	}
	if base, planErr := gitobs.MergeBasePlan(source.Commit, integration.Commit); planErr == nil {
		if output, runErr := v.runner(ctx, directory, base); runErr == nil && strings.TrimSpace(string(output)) == source.Commit {
			evidence.Method, evidence.Reflected = gitobs.IntegrationMethodMerge, evidence.SourceTreeMatches
		}
	}
	if !evidence.Reflected && evidence.SourceTreeMatches && source.Tree == integration.Tree {
		evidence.Method, evidence.Reflected = gitobs.IntegrationMethodExactTree, true
	}
	if !evidence.Reflected && evidence.SourceTreeMatches {
		if cherry, planErr := gitobs.CherryPlan(integration.Commit, source.Commit); planErr == nil {
			if output, runErr := v.runner(ctx, directory, cherry); runErr == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
					if strings.HasPrefix(line, "- ") && strings.TrimSpace(strings.TrimPrefix(line, "- ")) == source.Commit {
						evidence.Method, evidence.Reflected = gitobs.IntegrationMethodPatchEquivalent, true
						break
					}
				}
			}
		}
	}
	return evidence, nil
}
