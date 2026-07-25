package safety

import (
	"strings"
	"testing"
)

var sampleToken = "omgdt_v1_" + strings.Repeat("a", 43)

type nestedInput struct {
	Title string
	Items []map[string]any
	Bytes []byte
}

func TestRejectDelegationTokenDetectsFixedLengthCredentialSubstrings(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "exact token", value: sampleToken},
		{name: "prefixed token", value: "Bearer " + sampleToken},
		{name: "one URL-safe suffix character", value: sampleToken + "b"},
		{name: "many URL-safe suffix characters", value: sampleToken + "-_more-token-data"},
		{name: "punctuation boundaries", value: "(" + sampleToken + ")."},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := Reject(nestedInput{Items: []map[string]any{{"message": test.value}}}); err == nil {
				t.Fatal("delegation token accepted")
			}
		})
	}
}

func TestRejectDelegationTokenDetectsOrderedCompositeStringLeaves(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "two string leaves", value: []string{sampleToken[:17], sampleToken[17:]}},
		{name: "three string leaves", value: []any{sampleToken[:9], []string{sampleToken[9:31], sampleToken[31:]}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := Reject(test.value); err == nil {
				t.Fatal("split delegation token accepted")
			}
		})
	}
}

func TestRejectPrefixedDetectsLaterNestedPayloadLeaf(t *testing.T) {
	prefix := "omgdt_"
	payload := nestedInput{Title: "benign title", Items: []map[string]any{{"branch": "v1_" + strings.Repeat("a", 43)}}}
	if err := RejectPrefixed(prefix, payload); err == nil {
		t.Fatal("delegation token split across prefix and nested payload leaf accepted")
	}
}

func TestRejectPrefixedDetectsCompositeTokenAcrossEveryPayloadLeaf(t *testing.T) {
	payload := nestedInput{
		Title: "v1_" + strings.Repeat("a", 20),
		Items: []map[string]any{{"": []any{[]byte(strings.Repeat("a", 23))}}},
	}
	if err := RejectPrefixed("omgdt_", payload); err == nil {
		t.Fatal("delegation token split across nested payload leaves accepted")
	}
}

func TestRejectPrefixedPermitsBenignAdjacentPayloadLeaf(t *testing.T) {
	if err := RejectPrefixed("omgdt_", nestedInput{Title: "v1_" + strings.Repeat("a", 42)}); err != nil {
		t.Fatalf("benign prefix/payload adjacency rejected: %v", err)
	}
}

func TestRejectDelegationTokenRestartsAfterIncompleteCompositeCandidate(t *testing.T) {
	firstLeaf := delegationTokenPrefix + strings.Repeat("a", delegationTokenLength-1)
	if err := Reject([]string{firstLeaf, sampleToken}); err == nil {
		t.Fatal("delegation token after incomplete composite candidate accepted")
	}
}

func TestRejectDelegationTokenPermitsBenignAdjacentStrings(t *testing.T) {
	if err := Reject([]string{"omgdt_v1_" + strings.Repeat("a", 42), ". ordinary adjacent text"}); err != nil {
		t.Fatalf("benign adjacent strings rejected: %v", err)
	}
}

func TestRejectDelegationTokenPermitsInvalidLookalikes(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "ordinary prose", value: "arbitrary 43-character data abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"},
		{name: "short lookalike", value: "omgdt_v1_" + strings.Repeat("a", 42)},
		{name: "long interrupted lookalike", value: "omgdt_v1_" + strings.Repeat("a", 42) + "." + strings.Repeat("a", 42)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := Reject(test.value); err != nil {
				t.Fatalf("non-token text rejected: %v", err)
			}
		})
	}
}

func TestRedactDelegationTokenReplacesEachFixedLengthCredentialSubstring(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "exact token", value: sampleToken, want: redactedToken},
		{name: "prefixed token", value: "Bearer " + sampleToken, want: "Bearer " + redactedToken},
		{name: "one URL-safe suffix character", value: sampleToken + "b", want: redactedToken + "b"},
		{name: "many URL-safe suffix characters", value: sampleToken + "-_more-token-data", want: redactedToken + "-_more-token-data"},
		{name: "punctuation boundaries", value: "(" + sampleToken + ").", want: "(" + redactedToken + ")."},
		{name: "short lookalike", value: "omgdt_v1_" + strings.Repeat("a", 42), want: "omgdt_v1_" + strings.Repeat("a", 42)},
		{name: "long interrupted lookalike", value: "omgdt_v1_" + strings.Repeat("a", 42) + "." + strings.Repeat("a", 42), want: "omgdt_v1_" + strings.Repeat("a", 42) + "." + strings.Repeat("a", 42)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Redact(test.value); got != test.want {
				t.Fatalf("redaction = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSafeTextAppliesEveryPrivacyRuleToMixedInput(t *testing.T) {
	mixed := sampleToken + " /Users/alice/private-work"
	got := SafeText(mixed)
	if strings.Contains(got, sampleToken) || strings.Contains(got, "/Users/") {
		t.Fatalf("mixed sensitive input leaked: %q", got)
	}
	if !strings.HasPrefix(got, "[REDACTED:") {
		t.Fatalf("mixed sensitive input was not redacted: %q", got)
	}
}

func TestSafeTextPreservesSpecificTokenRedactionForOtherwiseSafeInput(t *testing.T) {
	got := SafeText("prefix " + sampleToken + " suffix")
	want := "prefix " + redactedToken + " suffix"
	if got != want {
		t.Fatalf("redaction = %q, want %q", got, want)
	}
}

func TestSafeTextRedactsStableMetadataSensitiveVariants(t *testing.T) {
	for _, value := range []string{
		"api_key=release",
		"api-key=release",
		"apikey=release",
		"private_key=release",
		`C:\Users\alice\private`,
		`C:\private\credential`,
	} {
		got := SafeText(value)
		if !strings.HasPrefix(got, "[REDACTED:") || strings.Contains(got, value) {
			t.Errorf("SafeText(%q) = %q, want redaction", value, got)
		}
	}
	if got := SafeText("release-42"); got != "release-42" {
		t.Fatalf("SafeText benign metadata = %q", got)
	}
}
