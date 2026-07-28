package git

// RevisionEvidence is an exact, read-only Git observation. RefFingerprint is
// a hash of ref movement history when a reflog is available.
type RevisionEvidence struct {
	Commit         string `json:"commit"`
	Tree           string `json:"tree"`
	RefFingerprint string `json:"ref_fingerprint,omitempty"`
}

type IntegrationMethod string

const (
	IntegrationMethodMerge           IntegrationMethod = "merge"
	IntegrationMethodPatchEquivalent IntegrationMethod = "cherry_pick_or_squash"
	IntegrationMethodExactTree       IntegrationMethod = "exact_tree"
	IntegrationMethodUnknown         IntegrationMethod = "unknown"
)

// ReconcileEvidence is conservative: Reflected is true only when Git itself
// proves ancestry, patch equivalence, or exact tree equality.
type ReconcileEvidence struct {
	Source              RevisionEvidence  `json:"source"`
	Integration         RevisionEvidence  `json:"integration"`
	CurrentIntegration  RevisionEvidence  `json:"current_integration"`
	SourceTreeMatches   bool              `json:"source_tree_matches"`
	IntegrationRetained bool              `json:"integration_retained"`
	Reflected           bool              `json:"reflected"`
	Method              IntegrationMethod `json:"method"`
}
