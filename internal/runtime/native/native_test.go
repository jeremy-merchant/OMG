package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.invalid/coordledger/internal/domain/lineage"
)

type fakeAdapter struct {
	name    RuntimeName
	lookup  func(context.Context, Locator) (Metadata, error)
	called  bool
	locator Locator
}

func (a *fakeAdapter) Runtime() RuntimeName { return a.name }

func (a *fakeAdapter) Resolve(ctx context.Context, locator Locator) (Metadata, error) {
	a.called = true
	a.locator = locator
	return a.lookup(ctx, locator)
}

func TestResolverAvailableReturnsVerifiedMinimalMetadata(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	session := nativeSession("runtime-a", "native-1", "opaque-ref", &started, lineage.NativeAccessAvailable)
	adapter := &fakeAdapter{name: "runtime-a", lookup: func(_ context.Context, got Locator) (Metadata, error) {
		if got.RuntimeHome() != "/private/runtime" || got.NativeSessionID() != "native-1" || got.NativeSessionRef() != "opaque-ref" {
			t.Fatalf("adapter locator = %#v", got)
		}
		return Metadata{NativeSessionID: "native-1", StartedAt: &started, NativeParentSessionID: "native-parent", AccessState: lineage.NativeAccessAvailable}, nil
	}}
	registry, err := NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}

	got, err := registry.Resolve(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.called || got.NativeSessionID != "native-1" || got.NativeParentSessionID != "native-parent" || got.StartedAt == nil || !got.StartedAt.Equal(started) || got.AccessState != lineage.NativeAccessAvailable {
		t.Fatalf("resolution = %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"/private/runtime", "opaque-ref", session.NativeSessionFingerprint} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("serialized resolution leaked %q: %s", private, encoded)
		}
	}
}

func TestResolverMapsMissingUnreadableAndUnsupported(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		session lineage.AgentSession
		adapter *fakeAdapter
		want    lineage.NativeAccessState
	}{
		{
			name:    "stored unsupported",
			session: nativeSession("runtime-a", "", "", nil, lineage.NativeAccessUnsupported),
			adapter: &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				t.Fatal("unsupported session invoked adapter")
				return Metadata{}, nil
			}},
			want: lineage.NativeAccessUnsupported,
		},
		{
			name:    "unregistered runtime",
			session: nativeSession("runtime-b", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable),
			adapter: &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				t.Fatal("mismatched runtime invoked adapter")
				return Metadata{}, nil
			}},
			want: lineage.NativeAccessUnsupported,
		},
		{
			name:    "absent stored locator",
			session: lineage.AgentSession{Runtime: "runtime-a", NativeAccessState: lineage.NativeAccessAvailable},
			adapter: &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				t.Fatal("absent locator invoked adapter")
				return Metadata{}, nil
			}},
			want: lineage.NativeAccessMissing,
		},
		{
			name:    "adapter unsupported",
			session: nativeSession("runtime-a", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable),
			adapter: &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				return Metadata{AccessState: lineage.NativeAccessUnsupported}, nil
			}},
			want: lineage.NativeAccessUnsupported,
		},
		{
			name:    "source missing",
			session: nativeSession("runtime-a", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable),
			adapter: &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				return Metadata{AccessState: lineage.NativeAccessMissing}, nil
			}},
			want: lineage.NativeAccessMissing,
		},
		{
			name:    "source unreadable error",
			session: nativeSession("runtime-a", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable),
			adapter: &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				return Metadata{}, errors.New("permission denied: /private/runtime")
			}},
			want: lineage.NativeAccessUnreadable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry, err := NewRegistry(tc.adapter)
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Resolve(context.Background(), tc.session)
			if err != nil {
				t.Fatal(err)
			}
			if got.AccessState != tc.want || got.NativeSessionID != "" || got.NativeParentSessionID != "" || got.StartedAt != nil {
				t.Fatalf("resolution = %#v, want safe %s result", got, tc.want)
			}
		})
	}
}

func TestResolverPropagatesAdapterCancellationUnchanged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
	}{
		{name: "canceled", err: context.Canceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
		{name: "wrapped canceled", err: fmt.Errorf("adapter failed: %w", context.Canceled)},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("adapter failed: %w", context.DeadlineExceeded)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := nativeSession("runtime-a", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable)
			adapter := &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
				return Metadata{}, tc.err
			}}
			registry, err := NewRegistry(adapter)
			if err != nil {
				t.Fatal(err)
			}

			got, err := registry.Resolve(context.Background(), session)
			if err != tc.err {
				t.Fatalf("error = %v, want exact %v", err, tc.err)
			}
			if got != (Resolution{}) {
				t.Fatalf("cancellation result = %#v, want no resolution", got)
			}
		})
	}
}

func TestResolverRejectsFingerprintMismatchWithoutLocatorLeak(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	session := nativeSession("runtime-a", "native-1", "opaque-ref", &started, lineage.NativeAccessAvailable)
	adapter := &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
		return Metadata{NativeSessionID: "substituted-native", StartedAt: &started, AccessState: lineage.NativeAccessAvailable}, nil
	}}
	registry, err := NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}

	got, err := registry.Resolve(context.Background(), session)
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("error = %v, want fingerprint mismatch", err)
	}
	if got != (Resolution{}) {
		t.Fatalf("mismatch result leaked metadata: %#v", got)
	}
	for _, private := range []string{"/private/runtime", "opaque-ref", session.NativeSessionFingerprint} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("error leaked %q: %v", private, err)
		}
	}
}

func TestResolverSupportsNilNativeStartTime(t *testing.T) {
	t.Parallel()
	session := nativeSession("runtime-a", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable)
	adapter := &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
		return Metadata{NativeSessionID: "native-1", AccessState: lineage.NativeAccessAvailable}, nil
	}}
	registry, err := NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.Resolve(context.Background(), session)
	if err != nil || got.StartedAt != nil || got.AccessState != lineage.NativeAccessAvailable {
		t.Fatalf("resolution = %#v, err = %v", got, err)
	}
}

func TestRegistryRejectsDuplicateOrEmptyRuntime(t *testing.T) {
	t.Parallel()
	valid := func(name RuntimeName) *fakeAdapter {
		return &fakeAdapter{name: name, lookup: func(context.Context, Locator) (Metadata, error) { return Metadata{}, nil }}
	}
	if _, err := NewRegistry(valid("runtime-a"), valid("runtime-a")); !errors.Is(err, ErrDuplicateRuntime) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := NewRegistry(valid("")); !errors.Is(err, ErrInvalidRuntime) {
		t.Fatalf("empty runtime error = %v", err)
	}
	if _, err := NewRegistry(valid(RuntimeName(strings.Repeat("r", 65)))); !errors.Is(err, ErrInvalidRuntime) {
		t.Fatalf("oversized runtime error = %v", err)
	}
}

func TestRegistryRejectsNonCanonicalRuntimeNames(t *testing.T) {
	t.Parallel()
	valid := func(name RuntimeName) *fakeAdapter {
		return &fakeAdapter{name: name, lookup: func(context.Context, Locator) (Metadata, error) { return Metadata{}, nil }}
	}
	for _, name := range []RuntimeName{" runtime", "runtime ", "runtime name", "runtime\nname", "runtime\x00name"} {
		if _, err := NewRegistry(valid(name)); !errors.Is(err, ErrInvalidRuntime) {
			t.Fatalf("runtime %q error = %v", name, err)
		}
	}
	if _, err := NewRegistry(valid("runtime.v1_2-alpha")); err != nil {
		t.Fatalf("canonical runtime rejected: %v", err)
	}
}

func TestResolverRejectsNilContextAndInvalidAdapterMetadata(t *testing.T) {
	started := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	session := nativeSession("runtime-a", "native-1", "opaque-ref", &started, lineage.NativeAccessAvailable)
	adapter := &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
		return Metadata{NativeSessionID: "native-1", StartedAt: &started, AccessState: lineage.NativeAccessAvailable}, nil
	}}
	registry, err := NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := registry.Resolve(nil, session); !errors.Is(err, ErrInvalidContext) || got.AccessState != lineage.NativeAccessUnreadable || adapter.called {
		t.Fatalf("nil context result = %#v, error = %v, called = %t", got, err, adapter.called)
	}

	cases := []Metadata{
		{NativeSessionID: "native\x00id", StartedAt: &started, AccessState: lineage.NativeAccessAvailable},
		{NativeSessionID: "native-1", NativeParentSessionID: "parent\nid", StartedAt: &started, AccessState: lineage.NativeAccessAvailable},
		{NativeSessionID: "native-1", StartedAt: new(time.Date(2026, 7, 23, 12, 0, 0, 0, time.FixedZone("offset", 3600))), AccessState: lineage.NativeAccessAvailable},
	}
	for _, metadata := range cases {
		adapter.lookup = func(context.Context, Locator) (Metadata, error) { return metadata, nil }
		got, err := registry.Resolve(context.Background(), session)
		if !errors.Is(err, ErrInvalidMetadata) || got.AccessState != lineage.NativeAccessUnreadable || got.NativeSessionID != "" || got.NativeParentSessionID != "" || got.StartedAt != nil {
			t.Fatalf("invalid metadata result = %#v, error = %v", got, err)
		}
	}
}

func TestResolverInvalidStoredRuntimeDoesNotInvokeAdapter(t *testing.T) {
	t.Parallel()
	adapter := &fakeAdapter{name: "runtime-a", lookup: func(context.Context, Locator) (Metadata, error) {
		t.Fatal("invalid stored runtime invoked adapter")
		return Metadata{}, nil
	}}
	registry, err := NewRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.Resolve(context.Background(), nativeSession(" runtime-a", "native-1", "opaque-ref", nil, lineage.NativeAccessAvailable))
	if err != nil || got.AccessState != lineage.NativeAccessUnsupported {
		t.Fatalf("resolution = %#v, error = %v", got, err)
	}
}

func TestAdapterContractExcludesTranscriptAndPrivateLocatorFields(t *testing.T) {
	t.Parallel()
	var _ Adapter = (*fakeAdapter)(nil)

	metadata := reflect.TypeFor[Metadata]()
	if metadata.NumField() != 4 {
		t.Fatalf("metadata fields = %d, want only native identity, start, parent, and access state", metadata.NumField())
	}
	for _, field := range []string{"RuntimeHome", "NativeSessionRef", "NativeSessionFingerprint", "Transcript", "Messages", "Content"} {
		if _, exists := metadata.FieldByName(field); exists {
			t.Fatalf("adapter result exposes forbidden field %q", field)
		}
	}
	locator := reflect.TypeFor[Locator]()
	for index := 0; index < locator.NumField(); index++ {
		if locator.Field(index).IsExported() {
			t.Fatalf("locator field %q is serializable outside this package", locator.Field(index).Name)
		}
	}
}

func nativeSession(runtime, nativeID, nativeRef string, started *time.Time, state lineage.NativeAccessState) lineage.AgentSession {
	session := lineage.AgentSession{
		Runtime:                runtime,
		RuntimeHome:            "/private/runtime",
		NativeSessionID:        nativeID,
		NativeSessionRef:       nativeRef,
		NativeSessionStartedAt: started,
		NativeAccessState:      state,
	}
	if state == lineage.NativeAccessUnsupported {
		session.RuntimeHome = ""
	} else {
		session.NativeSessionFingerprint = lineage.NativeSessionFingerprint(runtime, nativeID, nativeRef, started)
	}
	return session
}
