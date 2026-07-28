package view

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/app"
	"github.com/jeremy-merchant/OMG/internal/app/query"
)

func TestRenderFormatsExposeEquivalentBoardFacts(t *testing.T) {
	model := representativeViewModel(t)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			if err := Render(format, model, &output); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			for _, want := range []string{"task-7", "run-4", "inbox-2", "handoff-3", "reservation-6", "active", "satisfied=false", "safe.status"} {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
			for _, heading := range []string{"identity", "task", "progress", "dependencies", "inbox", "handoffs", "reservations", "warnings", "action", "snapshot"} {
				if !strings.Contains(strings.ToLower(got), heading) {
					t.Errorf("output missing section %q", heading)
				}
			}
		})
	}
}

func TestHumanRenderersExposeOperatorHierarchy(t *testing.T) {
	model := representativeViewModel(t)

	t.Run("tty", func(t *testing.T) {
		var output bytes.Buffer
		if err := Render(FormatTTY, model, &output); err != nil {
			t.Fatal(err)
		}
		got := output.String()
		for _, want := range []string{
			"OMG  OPERATOR LEDGER / BOARD",
			"NOW  who is working and what is blocked",
			"SESSIONS  1 identity record(s)",
			"WORK GRAPH  1 task(s) · 1 run(s) · 1 progress update(s)",
			"└─ ✔ AVAILABLE",
			"⦸ BLOCKED",
			"COMMAND PALETTE",
			"snapshot_cursor=cursor-9",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("TTY output missing operator hierarchy %q:\n%s", want, got)
			}
		}
	})

	t.Run("html", func(t *testing.T) {
		var output bytes.Buffer
		if err := Render(FormatHTML, model, &output); err != nil {
			t.Fatal(err)
		}
		got := output.String()
		for _, want := range []string{
			`<title>OMG · project-1 · BLOCKED</title>`,
			`class="brand-mark"`,
			`aria-label="Board sections"`,
			`aria-label="Snapshot metadata"`,
			`class="now-grid"`,
			`class="tree"`,
			`class="timeline"`,
			`class="dependency-map"`,
			`class="message-stack"`,
			`class="handoff-flow"`,
			`class="reservation-list"`,
			`class="git-console"`,
			`class="command-palette"`,
			`class="snapshot-grid"`,
			`<details class="meta-disclosure">`,
			`<code>`,
			`--accent:`,
			`prefers-reduced-motion`,
			`@media(max-width:`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("HTML output missing operator hierarchy %q", want)
			}
		}
	})
}

func TestRenderJSONPreservesCanonicalEnvelope(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatJSON, model, &output); err != nil {
		t.Fatal(err)
	}
	want, err := model.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want)+"\n" {
		t.Fatalf("JSON output is not the canonical envelope:\n got %s\nwant %s", output.String(), want)
	}
	var envelope struct {
		ViewVersion    int             `json:"view_version"`
		Kind           string          `json:"kind"`
		SnapshotCursor string          `json:"snapshot_cursor"`
		Data           json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ViewVersion != query.ViewVersion || envelope.Kind != "board" || envelope.SnapshotCursor != "cursor-9" || !bytes.Equal(envelope.Data, model.Data()) {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
}

func TestAllRenderersExposeADR0003MetadataAndGitAdvisory(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"task","project_id":"project-1","snapshot_cursor":"audit:42","generated_at":"2026-07-23T12:00:00Z","scope":{"project_id":"project-1","workspace_id":"workspace-1","mode":"task","selector":"task-7"},"redaction":{"policy_name":"board_safe_text","policy_version":1,"content_omitted":true,"content_redacted":true},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":["git_observation_advisory_non_authorizing"],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatJSON, FormatMarkdown, FormatHTML} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			if err := Render(format, model, &output); err != nil {
				t.Fatal(err)
			}
			normalized := strings.ReplaceAll(output.String(), `\_`, "_")
			for _, want := range []string{"version", "audit:42", "2026-07-23T12:00:00Z", "workspace-1", "task-7", "board_safe_text", "git_observation_advisory_non_authorizing"} {
				if !strings.Contains(normalized, want) {
					t.Errorf("%s output missing ADR 0003 metadata %q: %s", format, want, output.String())
				}
			}
		})
	}
}

func TestRenderDeterministicAndEmptyReadable(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"empty","sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		var first, second bytes.Buffer
		if err := Render(format, model, &first); err != nil {
			t.Fatal(err)
		}
		if err := Render(format, model, &second); err != nil {
			t.Fatal(err)
		}
		if first.String() != second.String() {
			t.Errorf("%s output was not deterministic", format)
		}
		emptyMarker := map[Format]string{FormatTTY: "No session identity recorded", FormatMarkdown: "None", FormatHTML: `class="empty-state"`}[format]
		if !strings.Contains(first.String(), emptyMarker) {
			t.Errorf("%s output does not make empty sections explicit: %s", format, first.String())
		}
	}
}

func TestRenderHTMLEscapesAndHasAccessibleStaticDocument(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","sessions":[],"tasks":[{"id":"task-<script>alert(1)</script>","display_number":1,"title":"<img src=x onerror=alert(1)>","state":"active","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":["<script>alert(2)</script>"],"suggested_actions":[]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, forbidden := range []string{"<script", "</script", "<img ", "http://", "https://", "<link"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Errorf("HTML contains forbidden %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "Content-Security-Policy", "default-src 'none'", "<main", "<nav", "Skip to board content", "<details", "<h2", "@media print", "prefers-color-scheme", ":focus-visible"} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestRenderHTMLHashesInlineStylesAndPreservesAccessibleThemes(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()

	styleStart := strings.Index(got, "<style>")
	styleEnd := strings.Index(got, "</style>")
	if styleStart < 0 || styleEnd <= styleStart {
		t.Fatalf("HTML stylesheet boundaries missing")
	}
	style := got[styleStart+len("<style>") : styleEnd]
	sum := sha256.Sum256([]byte(style))
	hashSource := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !strings.Contains(got, "style-src "+hashSource) {
		t.Errorf("HTML CSP missing hashed style source %q", hashSource)
	}
	if strings.Contains(got, "'unsafe-inline'") {
		t.Error("HTML CSP permits unhashed inline content")
	}
	if strings.Contains(got, "frame-ancestors") {
		t.Error("HTML meta CSP claims unsupported frame-ancestor enforcement")
	}

	accent := cssHexColor(t, style, "--accent")
	for _, background := range []string{"--canvas", "--surface"} {
		if ratio := contrastRatio(accent, cssHexColor(t, style, background)); ratio < 4.5 {
			t.Errorf("%s against %s contrast ratio %.2f is below 4.5:1", accent, background, ratio)
		}
	}
	if ratio := contrastRatio(cssHexColor(t, style, "--text-primary"), cssHexColor(t, style, "--canvas")); ratio < 7 {
		t.Errorf("primary text contrast ratio %.2f is below 7:1", ratio)
	}

	light := strings.Index(style, "@media(prefers-color-scheme:light)")
	print := strings.Index(style, "@media print")
	if light < 0 || print <= light {
		t.Fatal("print theme must override the light theme")
	}
	lightStyles := style[light:print]
	for themeName, themeStyles := range map[string]string{"dark": style[:light], "light": lightStyles} {
		for _, foreground := range []string{"--text-muted", "--text-secondary"} {
			for _, background := range []string{"--canvas", "--surface", "--surface-raised"} {
				ratio := contrastRatio(cssHexColor(t, themeStyles, foreground), cssHexColor(t, themeStyles, background))
				if ratio < 4.5 {
					t.Errorf("%s %s against %s contrast ratio %.2f is below 4.5:1", themeName, foreground, background, ratio)
				}
			}
		}
		for _, pair := range [][2]string{
			{"--success", "--success-soft"},
			{"--warning", "--warning-soft"},
			{"--danger", "--danger-soft"},
			{"--info", "--accent-soft"},
			{"--blocked", "--blocked-soft"},
		} {
			ratio := contrastRatio(cssHexColor(t, themeStyles, pair[0]), cssHexColor(t, themeStyles, pair[1]))
			if ratio < 4.5 {
				t.Errorf("%s %s against %s contrast ratio %.2f is below 4.5:1", themeName, pair[0], pair[1], ratio)
			}
		}
	}
	printStyles := style[print:]
	for _, want := range []string{"color-scheme:light", "--canvas:#fff", "--surface:#fff", "--text-primary:#000", "--text-muted:#444", "--accent:#000", ".rail,.skip-link{display:none}"} {
		if !strings.Contains(printStyles, want) {
			t.Errorf("print theme missing %q", want)
		}
	}
}

func cssHexColor(t *testing.T, stylesheet, property string) string {
	t.Helper()
	_, remainder, found := strings.Cut(stylesheet, property+":")
	if !found {
		t.Fatalf("stylesheet missing %s", property)
	}
	value, _, found := strings.Cut(remainder, ";")
	if !found || value == "" || value[0] != '#' {
		t.Fatalf("stylesheet %s is not a hex color: %q", property, value)
	}
	switch len(value) {
	case 4:
		return "#" + strings.Repeat(string(value[1]), 2) + strings.Repeat(string(value[2]), 2) + strings.Repeat(string(value[3]), 2)
	case 7:
		return value
	default:
		t.Fatalf("stylesheet %s is not a three- or six-digit hex color: %q", property, value)
		return ""
	}
}

func contrastRatio(first, second string) float64 {
	firstLuminance := relativeLuminance(first)
	secondLuminance := relativeLuminance(second)
	if firstLuminance < secondLuminance {
		firstLuminance, secondLuminance = secondLuminance, firstLuminance
	}
	return (firstLuminance + 0.05) / (secondLuminance + 0.05)
}

func relativeLuminance(color string) float64 {
	component := func(offset int) float64 {
		value, err := strconv.ParseUint(color[offset:offset+2], 16, 8)
		if err != nil {
			panic(err)
		}
		channel := float64(value) / 255
		if channel <= 0.04045 {
			return channel / 12.92
		}
		return math.Pow((channel+0.055)/1.055, 2.4)
	}
	return 0.2126*component(1) + 0.7152*component(3) + 0.0722*component(5)
}

func TestHumanRenderersExposeCanonicalCoordinationMetadata(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","identity":{"id":"session-1","parent_session_id":"parent-secret","root_session_id":"root-1","native_access_state":"available","started_at":"2026-01-02T03:04:05Z"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[{"id":"reservation-1","session_id":"session-1","task_id":"task-1","run_id":"run-1","pattern_kind":"path","pattern_fingerprint":"fingerprint-secret","case_sensitivity":"sensitive","mode":"exclusive","intent":"modify","lifecycle":"active","expires_at":"2026-01-02T04:04:05Z","conflict_ids":[]}],"git":{"observation_id":"git-1","observed_at":"2026-01-02T03:04:05Z","repository":"/absolute/worktree/path","confidence":"high","assets":[{"fingerprint":"asset-fingerprint-secret","type":"worktree","classification":[],"confidence":"high"}]},"warnings":[],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		var output bytes.Buffer
		if err := Render(format, model, &output); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"parent-secret", "fingerprint-secret"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("%s omitted canonical field %q", format, want)
			}
		}
	}
}

func TestHumanRenderersIncludeIdentityTaskMetadata(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"me","project_id":"project-1","snapshot_cursor":"cursor","identity":{"id":"session-1","liveness":"stale","instruction_source":"project","provenance_confidence":"high","human_id":"human-1","parent_session_id":"parent-1","root_session_id":"root-1","root_human_id":"root-human-1","continuation_of_session_id":"continued-1","task_id":"task-current","previous_task_id":"task-previous","worktree_bound":true,"native_access_state":"available","started_at":"2026-01-02T03:04:05Z","heartbeat_at":"2026-01-02T03:05:05Z","ended_at":"2026-01-02T03:06:05Z","interrupted_at":"2026-01-02T03:07:05Z"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		var output bytes.Buffer
		if err := Render(format, model, &output); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"liveness=stale", "instruction_source=project", "provenance_confidence=high", "human-1", "parent-1", "root-1", "root-human-1", "continued-1", "task-current", "task-previous", "worktree_bound=true", "started=2026-01-02T03:04:05Z", "heartbeat=2026-01-02T03:05:05Z", "ended=2026-01-02T03:06:05Z", "interrupted=2026-01-02T03:07:05Z"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("%s output missing identity task metadata %q", format, want)
			}
		}
	}
}

func TestRenderersExposeCanonicalSessionLiveness(t *testing.T) {
	for _, liveness := range []string{"alive", "stale", "no_signal"} {
		t.Run(liveness, func(t *testing.T) {
			model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","identity":{"id":"session-1","liveness":"`+liveness+`","root_session_id":"session-1","native_access_state":"available","started_at":"2026-01-02T03:04:05Z"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
			for _, format := range []Format{FormatJSON, FormatMarkdown, FormatHTML} {
				var output bytes.Buffer
				if err := Render(format, model, &output); err != nil {
					t.Fatal(err)
				}
				want := `liveness=` + liveness
				if format == FormatJSON {
					want = `"liveness":"` + liveness + `"`
				}
				got := output.String()
				if format == FormatMarkdown {
					got = strings.ReplaceAll(got, `\_`, `_`)
				}
				if !strings.Contains(got, want) {
					t.Errorf("%s output missing liveness %q: %s", format, liveness, output.String())
				}
			}
		})
	}
}

func TestHumanRenderersIncludeHandoffReservationAndGitDetails(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[{"id":"dep-1","dependent_task_id":"task-1","blocker_task_id":"task-2","type":"blocks","unblock_on":"complete","satisfied":true}],"inbox":[],"handoffs":[{"id":"handoff-1","task_id":"task-1","run_id":"run-1","source_session_id":"session-1","target_session_id":"session-2","target_task_id":"task-2","summary":"safe summary","final_output_policy":"hash_only","final_output_hash":"hash-1","changed_file_count":1,"verification_item_count":2,"status":"accepted","integration_state":"INTEGRATED","created_at":"2026-01-02T03:04:05Z"}],"reservations":[{"id":"reservation-1","session_id":"session-1","task_id":"task-1","run_id":"run-1","pattern_kind":"path","pattern_fingerprint":"fingerprint-1","case_sensitivity":"sensitive","mode":"exclusive","intent":"modify","lifecycle":"active","expires_at":"2026-01-02T04:04:05Z","conflict_ids":["reservation-2"]}],"git":{"observation_id":"git-1","observed_at":"2026-01-02T03:04:05Z","repository":"repo","confidence":"high","assets":[{"type":"worktree","head":"head-1","upstream":"upstream-1","ahead_default":2,"behind_default":3,"ahead_upstream":4,"behind_upstream":5,"tracked_dirty":6,"untracked_dirty":7,"classification":["owned"],"confidence":"high"}]},"warnings":["git warning"],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		var output bytes.Buffer
		if err := Render(format, model, &output); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"satisfied=true", "session-2", "safe summary", "hash-1", "integration_state=INTEGRATED", "fingerprint-1", "reservation-2", "head-1", "upstream-1", "ahead_default=2", "behind_upstream=5", "git warning"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("%s output missing canonical detail %q", format, want)
			}
		}
	}
}

func TestHumanRenderersExposeSubmittedHandoffDecision(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","sessions":[],"tasks":[],"runs":[{"id":"run-1","task_id":"task-1","session_id":"session-1","state":"work_complete","started_at":"2026-01-02T03:04:05Z"}],"progress":[],"dependencies":[],"inbox":[],"handoffs":[{"id":"handoff-1","task_id":"task-1","run_id":"run-1","run_state":"work_complete","source_session_id":"session-1","target_session_id":"session-2","target_task_id":"task-2","summary":"handoff ready for review","final_output_policy":"hash_only","final_output_hash":"hash-1","changed_file_count":1,"verification_item_count":2,"status":"submitted","decision":{"id":"decision-1","decision":"rejected","actor_session_id":"session-2","created_at":"2026-01-02T03:05:05Z"},"created_at":"2026-01-02T03:04:05Z"}],"reservations":[],"warnings":[],"suggested_actions":[]}`)

	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			if err := Render(format, model, &output); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"status=submitted", "decision=rejected"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("%s output missing handoff detail %q: %s", format, want, output.String())
				}
			}
		})
	}
}

func TestHumanRenderersExposeHandoffRunState(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[{"id":"handoff-work","task_id":"task-1","run_id":"run-work","run_state":"work_complete","status":"submitted"},{"id":"handoff-verified","task_id":"task-2","run_id":"run-verified","run_state":"verified_done","status":"accepted"}],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			if err := Render(format, model, &output); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			if format == FormatMarkdown {
				got = strings.ReplaceAll(got, `\_`, `_`)
			}
			for _, want := range []string{"run_state=work_complete", "run_state=verified_done"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s output missing handoff state %q: %s", format, want, got)
				}
			}
		})
	}
}

func TestTTYAndMarkdownNeutralizeHostileText(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor","sessions":[],"tasks":[{"id":"task-1","display_number":1,"title":"<script>alert(1)</script> [click](https://bad.example) \u001b]8;;https://bad.example\u0007link\u001b]8;;\u0007 *heading* # tag","state":"active","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	var tty, markdownOutput bytes.Buffer
	if err := Render(FormatTTY, model, &tty); err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"\x1b", "\x07"} {
		if strings.Contains(tty.String(), control) {
			t.Fatalf("TTY contains control sequence %q", control)
		}
	}
	if err := Render(FormatMarkdown, model, &markdownOutput); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"&lt;script&gt;", "\\[click\\]\\(https://bad.example\\)", "\\*heading\\*", "\\# tag"} {
		if !strings.Contains(markdownOutput.String(), want) {
			t.Errorf("Markdown did not neutralize %q: %s", want, markdownOutput.String())
		}
	}
}

func TestRenderRejectsInvalidFormatAndModel(t *testing.T) {
	model := representativeViewModel(t)
	if err := Render(Format("xml"), model, &bytes.Buffer{}); err == nil || err.Error() != `view: unsupported format "xml"` {
		t.Fatalf("unexpected format error: %v", err)
	}
	wrongKind := viewModel(t, `{"schema_version":1}`)
	wrongKind, _ = query.NewViewModel("other", "cursor", wrongKind.Data())
	if err := Render(FormatTTY, wrongKind, &bytes.Buffer{}); err == nil || err.Error() != `view: unsupported model kind "other"` {
		t.Fatalf("unexpected kind error: %v", err)
	}
	badSchema := viewModel(t, `{"schema_version":2}`)
	if err := Render(FormatTTY, badSchema, &bytes.Buffer{}); err == nil || err.Error() != `view: unsupported board schema 2` {
		t.Fatalf("unexpected schema error: %v", err)
	}
}

func TestHumanRenderersExposeBoardOwnershipAndSafeAssetFacts(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"mode":"task","project_id":"project-1","snapshot_cursor":"cursor","identity":{"id":"session-1","branch":"feature/safe","worktree_fingerprint":"worktree-fingerprint","native_access_state":"available","started_at":"2026-01-02T03:04:05Z"},"sessions":[],"tasks":[{"id":"task-1","display_number":1,"title":"safe","state":"claimed","created_by_session_id":"creator-1","claimed_by_session_id":"claimer-1","parent_task_id":"parent-1","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[{"id":"reservation-1","pattern":"src/safe.go","pattern_kind":"exact","pattern_fingerprint":"reservation-fingerprint","case_sensitivity":"sensitive","mode":"exclusive","intent":"edit","lifecycle":"active","expires_at":"2026-01-02T04:04:05Z","conflict_ids":[]}],"git":{"assets":[{"fingerprint":"asset-fingerprint","type":"worktree","owner_state":"claimed","owner_session_id":"owner-session-1","owner_task_id":"owner-task-1"}]},"warnings":[],"suggested_actions":[]}`)
	for _, format := range []Format{FormatTTY, FormatMarkdown, FormatHTML} {
		t.Run(string(format), func(t *testing.T) {
			var output bytes.Buffer
			if err := Render(format, model, &output); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			for _, want := range []string{
				"branch=feature/safe", "worktree_fingerprint=worktree-fingerprint",
				"created_by_session=creator-1", "claimed_by_session=claimer-1", "parent_task=parent-1",
				"pattern=src/safe.go", "fingerprint=reservation-fingerprint",
				"fingerprint=asset-fingerprint", "owner_state=claimed", "owner_session=owner-session-1", "owner_task=owner-task-1",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%s output missing board fact %q: %s", format, want, got)
				}
			}
			if strings.Contains(got, "/private/") {
				t.Errorf("%s output leaked an absolute private path: %s", format, got)
			}
		})
	}
}

func TestRenderPreflightTTYShowsSafeOperatorSections(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	rendered := RenderPreflightTTY(app.PreflightView{
		Healthy:           true,
		PendingMigrations: 0,
		Details: &app.PreflightDetails{
			Identity:     &query.IdentityView{ID: "session-current", Kind: "agent", Role: "owner", Runtime: "native", StartedAt: now},
			Sessions:     []query.IdentityView{{ID: "session-peer", Kind: "agent", Role: "worker", Runtime: "native", StartedAt: now}},
			Tasks:        []query.TaskView{{ID: "task-7", DisplayNumber: 7, Title: "Safe \x1b[31mtitle", State: "active", CreatedAt: now, UpdatedAt: now}},
			Inbox:        []query.InboxItemView{{MessageID: "inbox-2", Type: "NOTICE", Subject: "Read \x07 safely", SenderSessionID: "session-peer"}},
			Dependencies: []query.DependencyView{{ID: "dependency-3", DependentTaskID: "task-7", BlockerTaskID: "task-6", Type: "blocks"}},
			Reservations: []query.ReservationView{{ID: "reservation-6", SessionID: "session-current", TaskID: "task-7", PatternKind: "exact", Pattern: "internal/view/render.go", PatternFingerprint: "fingerprint", Mode: "exclusive", Lifecycle: "active"}},
			Git:          &query.GitView{Assets: []query.GitAssetView{{Fingerprint: "asset-8", Type: "worktree", Branch: "main", OwnerState: "claimed"}}},
		},
	})

	for _, want := range []string{
		"OMG  OPERATOR LEDGER / PREFLIGHT", "STATE", "Healthy: true",
		"IDENTITY", "SESSIONS + TASKS", "INBOX", "DEPENDENCIES", "RESERVATIONS", "GIT",
		"session-current", "session-peer", "task-7", "inbox-2", "dependency-3", "reservation-6", "asset-8",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("preflight output missing %q:\n%s", want, rendered)
		}
	}
	for _, control := range []string{"\x1b", "\x07"} {
		if strings.Contains(rendered, control) {
			t.Fatalf("preflight output contains control sequence %q: %q", control, rendered)
		}
	}
	if strings.Contains(rendered, "{Initialized:") {
		t.Fatalf("preflight output is a Go struct dump: %s", rendered)
	}
}

func representativeViewModel(t *testing.T) query.ViewModel {
	t.Helper()
	return viewModel(t, `{"schema_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor-9","identity":{"id":"session-1","kind":"agent","runtime":"native","role":"worker","instruction_source":"project","provenance_confidence":"high","human_id":"worker-a","root_session_id":"session-1","root_human_id":"worker-a","native_access_state":"available","started_at":"2026-01-02T03:04:05Z"},"sessions":[],"tasks":[{"id":"task-7","display_number":7,"title":"Render board","state":"active","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}],"runs":[{"id":"run-4","task_id":"task-7","session_id":"session-1","state":"active","started_at":"2026-01-02T03:04:05Z"}],"progress":[{"id":"progress-1","task_id":"task-7","run_id":"run-4","session_id":"session-1","phase":"implementation","done":["test"],"doing":["render"],"next":["verify"],"created_at":"2026-01-02T03:04:05Z"}],"dependencies":[{"id":"dependency-1","dependent_task_id":"task-7","blocker_task_id":"task-8","type":"blocks","unblock_on":"complete"}],"inbox":[{"message_id":"inbox-2","type":"notice","subject":"Ready","sender_session_id":"session-1","acknowledgement":"pending","created_at":"2026-01-02T03:04:05Z"}],"handoffs":[{"id":"handoff-3","task_id":"task-7","run_id":"run-4","source_session_id":"session-1","summary":"Board ready","final_output_policy":"hash_only","changed_file_count":2,"verification_item_count":1,"status":"accepted","created_at":"2026-01-02T03:04:05Z"}],"reservations":[{"id":"reservation-6","session_id":"session-1","task_id":"task-7","run_id":"run-4","pattern_kind":"path","pattern_fingerprint":"redacted","case_sensitivity":"sensitive","mode":"exclusive","intent":"modify","lifecycle":"active","expires_at":"2026-01-02T04:04:05Z","conflict_ids":[]}],"git":{"observation_id":"git-1","observed_at":"2026-01-02T03:04:05Z","repository":"repo","confidence":"high","default_branch":"main","assets":[{"fingerprint":"secret-fingerprint","type":"worktree","branch":"feature/render","ahead_default":1,"behind_default":0,"ahead_upstream":1,"behind_upstream":0,"tracked_dirty":2,"untracked_dirty":1,"classification":["owned"],"confidence":"high","owner_session_id":"session-1","owner_task_id":"task-7"}]},"warnings":["dirty worktree"],"suggested_actions":[{"code":"safe.status","command":"coord status"}]}`)
}

func viewModel(t *testing.T, data string) query.ViewModel {
	t.Helper()
	model, err := query.NewViewModel("board", "cursor-9", json.RawMessage(data))
	if err != nil {
		t.Fatal(err)
	}
	return model
}
