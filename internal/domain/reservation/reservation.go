// Package reservation defines advisory, transport-neutral path reservations.
// It deliberately has no authority to perform filesystem, Git, or write actions.
package reservation

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

type PatternKind string

const (
	Exact           PatternKind = "exact"
	DirectoryPrefix PatternKind = "directory_prefix"
	Glob            PatternKind = "glob"
)

type CaseSensitivity string

const (
	CaseSensitive   CaseSensitivity = "sensitive"
	CaseInsensitive CaseSensitivity = "insensitive"
)

type Overlap string

const (
	OverlapNone     Overlap = "none"
	OverlapPossible Overlap = "possible"
	OverlapCertain  Overlap = "certain"
)

type Mode string

const (
	Shared    Mode = "shared"
	Exclusive Mode = "exclusive"
)

type Lifecycle string

const (
	Active     Lifecycle = "active"
	Released   Lifecycle = "released"
	Expired    Lifecycle = "expired"
	Overridden Lifecycle = "overridden"
)

// Pattern is a normalized project-relative path pattern. Glob accepts only *,
// ?, and a whole-component **; it never invokes shell expansion.
type Pattern struct {
	Kind            PatternKind
	Value           string
	CaseSensitivity CaseSensitivity
}

// NewPattern validates and normalizes a project-relative pattern. Both slash
// forms are accepted; output always uses slash separators.
func NewPattern(kind PatternKind, raw string, sensitivity CaseSensitivity) (Pattern, error) {
	if kind != Exact && kind != DirectoryPrefix && kind != Glob {
		return Pattern{}, errors.New("invalid pattern kind")
	}
	if sensitivity != CaseSensitive && sensitivity != CaseInsensitive {
		return Pattern{}, errors.New("invalid case sensitivity")
	}
	value, err := normalize(raw, kind == Glob)
	if err != nil {
		return Pattern{}, err
	}
	return Pattern{Kind: kind, Value: value, CaseSensitivity: sensitivity}, nil
}

func normalize(raw string, glob bool) (string, error) {
	if raw == "" || strings.IndexByte(raw, 0) >= 0 || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return "", errors.New("path must be nonempty and project-relative")
	}
	raw = strings.ReplaceAll(raw, `\`, "/")
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(out) == 0 {
				return "", errors.New("path escapes project")
			}
			out = out[:len(out)-1]
			continue
		}
		if strings.Contains(part, ":") || isDeviceName(part) {
			return "", errors.New("path contains prohibited component")
		}
		if err := validateComponent(part, glob); err != nil {
			return "", err
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return "", errors.New("path must name a project component")
	}
	return strings.Join(out, "/"), nil
}

func isDeviceName(component string) bool {
	base := strings.ToUpper(component)
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func validateComponent(component string, glob bool) error {
	if !glob && strings.ContainsAny(component, "*?[]{}") {
		return errors.New("wildcards require glob pattern")
	}
	for _, r := range component {
		if unicode.IsControl(r) || (!glob && (r == '*' || r == '?')) || (glob && strings.ContainsRune("[]{}", r)) {
			return errors.New("path contains unsupported pattern syntax")
		}
	}
	if glob && strings.Contains(component, "**") && component != "**" {
		return errors.New("recursive wildcard must be a complete component")
	}
	return nil
}

// Owner identifies the human and execution lineage responsible for a reservation.
type Owner struct {
	HumanID   string
	SessionID string
	TaskID    string
	RunID     string
}

type ReservationInput struct {
	ID        string
	Pattern   Pattern
	Mode      Mode
	Owner     Owner
	Intent    string
	ExpiresAt time.Time
}

// Reservation is the current projection of immutable reservation facts.
// ExpiresAt and lifecycle are derived by the repository; they are never mutable
// columns in the canonical reservation row.
type Reservation struct {
	ID        string
	Pattern   Pattern
	Mode      Mode
	Owner     Owner
	Intent    string
	ExpiresAt time.Time
	state     Lifecycle
}

// ReservationHistory exposes local canonical facts for a deliberate history
// query. It is not a receipt or audit rendering surface.
type ReservationHistory struct {
	Base     Reservation
	Current  Reservation
	Renewals []RenewalFact
	Release  *ReleaseFact
	Override *OverrideFact
}

// Clock permits deterministic lifecycle evaluation without consulting wall time.
type Clock interface{ Now() time.Time }

func New(input ReservationInput) (Reservation, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Intent) == "" || !validOwner(input.Owner) {
		return Reservation{}, errors.New("reservation identity and intent are required")
	}
	if input.Mode != Shared && input.Mode != Exclusive {
		return Reservation{}, errors.New("invalid reservation mode")
	}
	canonical, err := NewPattern(input.Pattern.Kind, input.Pattern.Value, input.Pattern.CaseSensitivity)
	if err != nil {
		return Reservation{}, errors.New("invalid reservation pattern")
	}
	if input.ExpiresAt.IsZero() {
		return Reservation{}, errors.New("expiry is required")
	}
	input.ExpiresAt = input.ExpiresAt.UTC()
	return Reservation{ID: input.ID, Pattern: canonical, Mode: input.Mode, Owner: input.Owner, Intent: input.Intent, ExpiresAt: input.ExpiresAt, state: Active}, nil
}

func validOwner(owner Owner) bool {
	return strings.TrimSpace(owner.HumanID) != "" && strings.TrimSpace(owner.SessionID) != "" && strings.TrimSpace(owner.TaskID) != "" && strings.TrimSpace(owner.RunID) != ""
}

// LifecycleAt evaluates expiry at a supplied UTC-normalized instant. At the
// exact expiry instant the reservation is expired.
func (r Reservation) LifecycleAt(at time.Time) Lifecycle {
	if r.state == Released {
		return Released
	}
	if !at.UTC().Before(r.ExpiresAt) {
		return Expired
	}
	if r.state == Overridden {
		return Overridden
	}
	return Active
}

func (r Reservation) Lifecycle(clock Clock) Lifecycle { return r.LifecycleAt(clock.Now()) }

type RenewalFact struct {
	At           time.Time
	Previous     time.Time
	ExpiresAt    time.Time
	CheckpointID string
}

func (r Reservation) Renew(at, expiresAt time.Time) (Reservation, RenewalFact, error) {
	at, expiresAt = at.UTC(), expiresAt.UTC()
	if r.LifecycleAt(at) != Active || !expiresAt.After(at) || !expiresAt.After(r.ExpiresAt) {
		return Reservation{}, RenewalFact{}, errors.New("renewal requires an active reservation and extended future expiry")
	}
	next := r
	next.ExpiresAt = expiresAt
	return next, RenewalFact{At: at, Previous: r.ExpiresAt, ExpiresAt: expiresAt}, nil
}

type ReleaseFact struct {
	At     time.Time
	Reason string
}

func (r Reservation) Release(at time.Time, reason string) (Reservation, ReleaseFact, error) {
	if strings.TrimSpace(reason) == "" {
		return Reservation{}, ReleaseFact{}, errors.New("release reason is required")
	}
	at = at.UTC()
	if r.LifecycleAt(at) != Active {
		return Reservation{}, ReleaseFact{}, errors.New("only active reservations can be released")
	}
	next := r
	next.state = Released
	return next, ReleaseFact{At: at, Reason: reason}, nil
}

type OverrideRecord struct {
	HumanID string
	Reason  string
}

type OverrideFact struct {
	At     time.Time
	Record OverrideRecord
}

// Override records a human-attributed advisory exception. It does not grant
// write authority and callers must still surface any conflict decision.
func (r Reservation) Override(at time.Time, record OverrideRecord) (Reservation, OverrideFact, error) {
	if strings.TrimSpace(record.HumanID) == "" || strings.TrimSpace(record.Reason) == "" {
		return Reservation{}, OverrideFact{}, errors.New("override requires human attribution and reason")
	}
	at = at.UTC()
	if r.LifecycleAt(at) != Active {
		return Reservation{}, OverrideFact{}, errors.New("only active reservations can be overridden")
	}
	next := r
	next.state = Overridden
	return next, OverrideFact{At: at, Record: record}, nil
}

type Decision struct {
	Overlap  Overlap
	Conflict bool
	Advisory bool
}

// Decide classifies an advisory reservation conflict. Released and expired
// reservations do not block; overridden reservations remain conflict-visible.
func Decide(a, b Reservation, at time.Time) Decision {
	if a.LifecycleAt(at) == Released || a.LifecycleAt(at) == Expired || b.LifecycleAt(at) == Released || b.LifecycleAt(at) == Expired {
		return Decision{Overlap: OverlapNone, Advisory: true}
	}
	overlap := ClassifyOverlap(a.Pattern, b.Pattern)
	if a.Owner == b.Owner {
		return Decision{Overlap: overlap, Advisory: true}
	}
	return Decision{Overlap: overlap, Conflict: overlap != OverlapNone && (a.Mode == Exclusive || b.Mode == Exclusive), Advisory: true}
}

// ClassifyOverlap returns certain only for proven intersections, none only for
// proven disjoint supported patterns, and possible otherwise. This fail-closed
// treatment of glob-to-glob and prefix-to-glob avoids false negatives.
func ClassifyOverlap(a, b Pattern) Overlap {
	if a.CaseSensitivity != b.CaseSensitivity {
		return OverlapPossible
	}
	if a.Kind == Glob && b.Kind == Glob {
		return OverlapPossible
	}
	if a.Kind == Glob {
		return classifyGlob(b, a)
	}
	if b.Kind == Glob {
		return classifyGlob(a, b)
	}
	if a.Kind == Exact && b.Kind == Exact {
		if equalPath(a, b) {
			return OverlapCertain
		}
		return OverlapNone
	}
	if a.Kind == DirectoryPrefix && b.Kind == DirectoryPrefix {
		if componentPrefix(a.Value, b.Value, a.CaseSensitivity) || componentPrefix(b.Value, a.Value, a.CaseSensitivity) {
			return OverlapCertain
		}
		return OverlapNone
	}
	if a.Kind == DirectoryPrefix {
		if componentPrefix(a.Value, b.Value, a.CaseSensitivity) {
			return OverlapCertain
		}
		return OverlapNone
	}
	if componentPrefix(b.Value, a.Value, b.CaseSensitivity) {
		return OverlapCertain
	}
	return OverlapNone
}

func classifyGlob(nonGlob, glob Pattern) Overlap {
	if nonGlob.Kind == Exact {
		if globMatches(glob.Value, nonGlob.Value, glob.CaseSensitivity) {
			return OverlapCertain
		}
		return OverlapNone
	}
	return OverlapPossible
}

func equalPath(a, b Pattern) bool {
	return normalizeCase(a.Value, a.CaseSensitivity) == normalizeCase(b.Value, b.CaseSensitivity)
}
func normalizeCase(value string, sensitivity CaseSensitivity) string {
	if sensitivity == CaseInsensitive {
		return strings.ToLower(value)
	}
	return value
}
func componentPrefix(prefix, value string, sensitivity CaseSensitivity) bool {
	p, v := normalizeCase(prefix, sensitivity), normalizeCase(value, sensitivity)
	return p == v || strings.HasPrefix(v, p+"/")
}

func globMatches(pattern, value string, sensitivity CaseSensitivity) bool {
	p, v := strings.Split(normalizeCase(pattern, sensitivity), "/"), strings.Split(normalizeCase(value, sensitivity), "/")
	var match func(int, int) bool
	match = func(pi, vi int) bool {
		if pi == len(p) {
			return vi == len(v)
		}
		if p[pi] == "**" {
			for next := vi; next <= len(v); next++ {
				if match(pi+1, next) {
					return true
				}
			}
			return false
		}
		return vi < len(v) && matchComponent(p[pi], v[vi]) && match(pi+1, vi+1)
	}
	return match(0, 0)
}

func matchComponent(pattern, value string) bool {
	p, v := []rune(pattern), []rune(value)
	memo := make(map[[2]int]bool)
	seen := make(map[[2]int]bool)
	var match func(int, int) bool
	match = func(pi, vi int) bool {
		key := [2]int{pi, vi}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		var result bool
		switch {
		case pi == len(p):
			result = vi == len(v)
		case p[pi] == '*':
			result = match(pi+1, vi) || (vi < len(v) && match(pi, vi+1))
		case p[pi] == '?':
			result = vi < len(v) && match(pi+1, vi+1)
		default:
			result = vi < len(v) && p[pi] == v[vi] && match(pi+1, vi+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}
