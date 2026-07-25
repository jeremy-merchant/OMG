package domain

import "testing"

func TestDomainErrorIsSafeAndClassifiable(t *testing.T) {
	err := NewError(CodeUninitialized, "coordledger is not initialized", false)
	if err.Code != CodeUninitialized || err.Retryable {
		t.Fatalf("unexpected error metadata: %#v", err)
	}
	if got := err.Error(); got != "coordledger is not initialized" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestActorContextCopiesCapabilities(t *testing.T) {
	capabilities := []Capability{CapabilityRead}
	actor := NewActorContext(ScopeID("scope-1"), ProjectID("project-1"), WorkspaceID("workspace-1"), InvocationCLI, capabilities)
	capabilities[0] = CapabilityWrite

	if !actor.Has(CapabilityRead) || actor.Has(CapabilityWrite) {
		t.Fatalf("actor capabilities were mutable: %#v", actor)
	}
}

func TestSecretFreeStableMetadata(t *testing.T) {
	for _, value := range []struct {
		value string
		want  bool
	}{
		{value: "progress-42", want: true},
		{value: "run_1", want: true},
		{value: "  progress-42", want: false},
		{value: "progress-42  ", want: false},
		{value: "api_key=release", want: false},
		{value: "api-key=release", want: false},
		{value: "apikey=release", want: false},
		{value: "private_key=release", want: false},
		{value: `C:\Users\alice\private`, want: false},
		{value: `C:\private\credential`, want: false},
		{value: "password=release-secret", want: false},
		{value: "/Users/alice/private", want: false},
		{value: "~/.ssh/id_ed25519", want: false},
		{value: "line\nbreak", want: false},
	} {
		if got := IsSecretFreeStableMetadata(value.value); got != value.want {
			t.Errorf("IsSecretFreeStableMetadata(%q) = %t, want %t", value.value, got, value.want)
		}
	}
}
