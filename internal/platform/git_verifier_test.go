package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"

	gitobs "github.com/jeremy-merchant/OMG/internal/domain/git"
)

func TestGitVerifierProvesMergeAndPinsCurrentRefHistory(t *testing.T) {
	runner := func(_ context.Context, directory string, plan gitobs.CommandPlan) ([]byte, error) {
		if directory != "/selected-project" {
			return nil, fmt.Errorf("unexpected directory")
		}
		key := strings.Join(plan.Args, " ")
		outputs := map[string]string{
			"rev-parse --verify source^{commit}":           "source\n",
			"rev-parse --verify source^{tree}":             "source-tree\n",
			"reflog show --format=%H%x00%gs -- source":     "source commit\n",
			"rev-parse --verify integrated^{commit}":       "integrated\n",
			"rev-parse --verify integrated^{tree}":         "integrated-tree\n",
			"reflog show --format=%H%x00%gs -- integrated": "integrated commit\n",
			"rev-parse --verify main^{commit}":             "integrated\n",
			"reflog show --format=%H%x00%gs -- main":       "integrated commit\n",
			"merge-base integrated integrated":             "integrated\n",
			"merge-base source integrated":                 "source\n",
		}
		output, ok := outputs[key]
		if !ok {
			return nil, fmt.Errorf("unexpected plan %q", key)
		}
		return []byte(output), nil
	}
	verifier := NewGitVerifier(GitScannerDependencies{Runner: runner})
	evidence, err := verifier.Reconcile(context.Background(), "/selected-project", "source", "source-tree", "integrated", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.SourceTreeMatches || !evidence.IntegrationRetained || !evidence.Reflected || evidence.Method != gitobs.IntegrationMethodMerge || evidence.CurrentIntegration.RefFingerprint == "" {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestGitVerifierRejectsUnsafeRevisionArgumentsBeforeRunner(t *testing.T) {
	called := false
	verifier := NewGitVerifier(GitScannerDependencies{Runner: func(context.Context, string, gitobs.CommandPlan) ([]byte, error) {
		called = true
		return nil, nil
	}})
	if _, err := verifier.ResolveRevision(context.Background(), "/selected-project", "--upload-pack=evil"); err == nil {
		t.Fatal("unsafe ref accepted")
	}
	if called {
		t.Fatal("runner was called for unsafe ref")
	}
}
