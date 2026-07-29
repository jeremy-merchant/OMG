package reservation

import (
	"testing"
	"time"
)

func mustPattern(t *testing.T, kind PatternKind, raw string, cs CaseSensitivity) Pattern {
	t.Helper()
	p, err := NewPattern(kind, raw, cs)
	if err != nil {
		t.Fatalf("NewPattern(%q): %v", raw, err)
	}
	return p
}

func TestPatternNormalizationAndRejection(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		kind            PatternKind
		wantErr         bool
	}{
		{"spaces CJK", `src\\设计 /a/../含 空格.go`, "src/设计 /含 空格.go", Exact, false},
		{"glob normalized", `src\\**\\*.go`, "src/**/*.go", Glob, false},
		{"prefix boundary", "src/a", "src/a", DirectoryPrefix, false},
		{"empty", "", "", Exact, true},
		{"nul", "src/\x00x", "", Exact, true},
		{"absolute unix", "/etc/passwd", "", Exact, true},
		{"volume", `C:\\src\\x`, "", Exact, true},
		{"traversal", "../secret", "", Exact, true},
		{"device", "src/CON/file", "", Exact, true},
		{"unsupported glob", "src/[ab].go", "", Glob, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewPattern(tc.kind, tc.raw, CaseSensitive)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Value != tc.want {
				t.Fatalf("Value = %q, want %q", got.Value, tc.want)
			}
		})
	}
}

func TestOverlapSupportedPatterns(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b Pattern
		want Overlap
	}{
		{"exact exact", mustPattern(t, Exact, "src/a.go", CaseSensitive), mustPattern(t, Exact, "src/a.go", CaseSensitive), OverlapCertain},
		{"prefix component boundary", mustPattern(t, DirectoryPrefix, "src/a", CaseSensitive), mustPattern(t, Exact, "src/ab/x.go", CaseSensitive), OverlapNone},
		{"prefix descendant", mustPattern(t, DirectoryPrefix, "src/a", CaseSensitive), mustPattern(t, Exact, "src/a/x.go", CaseSensitive), OverlapCertain},
		{"exact glob", mustPattern(t, Exact, "src/a.go", CaseSensitive), mustPattern(t, Glob, "src/*.go", CaseSensitive), OverlapCertain},
		{"recursive glob", mustPattern(t, Exact, "src/a/b.go", CaseSensitive), mustPattern(t, Glob, "src/**/*.go", CaseSensitive), OverlapCertain},
		{"glob no false negative", mustPattern(t, Glob, "src/**/*.go", CaseSensitive), mustPattern(t, Glob, "src/*/*.go", CaseSensitive), OverlapPossible},
		{"case insensitive", mustPattern(t, Exact, "SRC/A.go", CaseInsensitive), mustPattern(t, Exact, "src/a.GO", CaseInsensitive), OverlapCertain},
		{"case mismatch is conservative", mustPattern(t, Exact, "SRC/A.go", CaseSensitive), mustPattern(t, Exact, "src/a.go", CaseInsensitive), OverlapPossible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyOverlap(tc.a, tc.b); got != tc.want {
				t.Fatalf("ClassifyOverlap() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReservationLifecycleAndConflict(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	owner := Owner{HumanID: "human-1", SessionID: "session-1", TaskID: "task-1", RunID: "run-1"}
	base, err := New(ReservationInput{ID: "r1", Pattern: mustPattern(t, Exact, "src/a.go", CaseSensitive), Mode: Exclusive, Owner: owner, Intent: "edit parser", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if base.LifecycleAt(now) != Active || base.LifecycleAt(now.Add(time.Minute)) != Expired {
		t.Fatal("TTL boundary wrong")
	}
	shared, err := New(ReservationInput{ID: "r2", Pattern: mustPattern(t, Exact, "src/a.go", CaseSensitive), Mode: Shared, Owner: Owner{HumanID: "h2", SessionID: "s2", TaskID: "t2", RunID: "run2"}, Intent: "review", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if decision := Decide(base, shared, now); !decision.Conflict || decision.Overlap != OverlapCertain {
		t.Fatalf("exclusive conflict = %#v", decision)
	}
	self, err := New(ReservationInput{ID: "r-self", Pattern: mustPattern(t, Glob, "src/**", CaseSensitive), Mode: Exclusive, Owner: owner, Intent: "same run follow-up", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if decision := Decide(base, self, now); decision.Conflict || decision.Overlap != OverlapCertain {
		t.Fatalf("same-lineage reservation conflict = %#v", decision)
	}
	if decision := Decide(shared, shared, now); decision.Conflict {
		t.Fatalf("shared conflict = %#v", decision)
	}
	renewed, fact, err := base.Renew(now, now.Add(2*time.Hour))
	if err != nil || fact.At != now || renewed.ExpiresAt != now.Add(2*time.Hour) {
		t.Fatalf("renew = %#v %#v %v", renewed, fact, err)
	}
	released, release, err := renewed.Release(now, "work complete")
	if err != nil || release.At != now || released.LifecycleAt(now) != Released {
		t.Fatalf("release = %#v %#v %v", released, release, err)
	}
	if decision := Decide(released, shared, now); decision.Conflict {
		t.Fatalf("released conflict = %#v", decision)
	}
}

func TestOverrideRequiresHumanReason(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	r, err := New(ReservationInput{ID: "r", Pattern: mustPattern(t, Exact, "src/a.go", CaseSensitive), Mode: Exclusive, Owner: Owner{HumanID: "h", SessionID: "s", TaskID: "t", RunID: "r"}, Intent: "x", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Override(now, OverrideRecord{}); err == nil {
		t.Fatal("empty override accepted")
	}
	o, fact, err := r.Override(now, OverrideRecord{HumanID: "human", Reason: "coordinated exception"})
	if err != nil || o.LifecycleAt(now) != Overridden || fact.Record.Reason == "" {
		t.Fatalf("override = %#v %#v %v", o, fact, err)
	}
	if got := o.LifecycleAt(now.Add(time.Hour)); got != Expired {
		t.Fatalf("expired override lifecycle = %q, want expired", got)
	}
	if decision := Decide(o, o, now.Add(time.Hour)); decision.Conflict {
		t.Fatalf("expired override conflict = %#v", decision)
	}
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestLifecycleUsesInjectedClock(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.FixedZone("non-UTC", 2*60*60))
	r, err := New(ReservationInput{ID: "r", Pattern: mustPattern(t, Exact, "src/a.go", CaseSensitive), Mode: Shared, Owner: Owner{HumanID: "h", SessionID: "s", TaskID: "t", RunID: "run"}, Intent: "inspect", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Lifecycle(fixedClock{now: now.UTC()}); got != Active {
		t.Fatalf("Lifecycle() = %q, want active", got)
	}
	if got := r.Lifecycle(fixedClock{now: now.UTC().Add(time.Minute)}); got != Expired {
		t.Fatalf("Lifecycle() = %q, want expired", got)
	}
}

func TestSupportedExactGlobNeverReturnsFalseNegative(t *testing.T) {
	globs := []string{"src/*.go", "src/?/x.go", "src/**/*.go", "设计/**/含 空格?.go"}
	paths := []string{"src/a.go", "src/甲/x.go", "src/a/b.go", "设计/a/含 空格1.go"}
	for _, rawGlob := range globs {
		glob := mustPattern(t, Glob, rawGlob, CaseSensitive)
		for _, rawPath := range paths {
			exact := mustPattern(t, Exact, rawPath, CaseSensitive)
			if globMatches(glob.Value, exact.Value, CaseSensitive) && ClassifyOverlap(exact, glob) == OverlapNone {
				t.Fatalf("false negative: exact %q matches glob %q", rawPath, rawGlob)
			}
		}
	}
}
