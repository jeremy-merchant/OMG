package view

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOperatorTTYKeepsSemanticHierarchyWithAndWithoutANSI(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"프로젝트-1","snapshot_cursor":"cursor-1","scope":{"project_id":"프로젝트-1","mode":"all"},"identity":{"id":"session-abcdefghijklmnopqrstuvwxyz-0123456789","kind":"agent","role":"검증자","runtime":"native","liveness":"alive","native_access_state":"available"},"sessions":[],"tasks":[{"id":"task-1","display_number":1,"title":"한글 작업 상태 확인","state":"active"}],"runs":[],"progress":[],"dependencies":[{"id":"dep-1","dependent_task_id":"task-1","blocker_task_id":"task-2","type":"blocks","unblock_on":"verified_done","satisfied":false}],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}

	plain := renderTTY(board, false)
	colored := renderTTY(board, true)
	for _, output := range []string{plain, colored} {
		for _, want := range []string{"OMG", "OPERATOR LEDGER", "NOW", "SESSIONS", "WORK GRAPH", "⦸ BLOCKED", "프로젝트-1", "한글 작업 상태 확인"} {
			if !strings.Contains(output, want) {
				t.Errorf("TTY output missing %q:\n%s", want, output)
			}
		}
		if !strings.Contains(output, shortID("session-abcdefghijklmnopqrstuvwxyz-0123456789")) {
			t.Errorf("TTY primary view does not abbreviate the long session ID: %s", output)
		}
		if !strings.Contains(output, "session-abcdefghijklmnopqrstuvwxyz-0123456789") {
			t.Errorf("TTY secondary metadata lost the canonical session ID: %s", output)
		}
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain TTY fallback contains ANSI controls: %q", plain)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("color-enabled TTY output does not contain ANSI styling: %q", colored)
	}

	var nonTTY bytes.Buffer
	if err := Render(FormatTTY, model, &nonTTY); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nonTTY.String(), "\x1b[") {
		t.Fatalf("non-TTY writer received ANSI controls: %q", nonTTY.String())
	}
}

func TestOperatorHTMLUsesDomainComponentsWithoutGenericTables(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`class="now-grid"`,
		`class="tree"`,
		`class="timeline"`,
		`class="dependency-map"`,
		`class="message-stack"`,
		`class="handoff-flow"`,
		`class="reservation-list"`,
		`class="git-console"`,
		`class="command-palette"`,
		`<details class="meta-disclosure">`,
		`prefers-color-scheme:light`,
		`@media print`,
		`:focus-visible`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML operator board missing %q", want)
		}
	}
	for _, forbidden := range []string{"<table", "<script", "<link", "http://", "https://"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Errorf("HTML operator board contains forbidden generic or external markup %q", forbidden)
		}
	}
}

func TestOperatorHTMLPreservesCJKAndCanonicalLongIDs(t *testing.T) {
	longID := "session-abcdefghijklmnopqrstuvwxyz-0123456789"
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"me","project_id":"프로젝트-1","snapshot_cursor":"cursor-1","scope":{"project_id":"프로젝트-1","mode":"me"},"identity":{"id":"`+longID+`","kind":"agent","role":"운영자","runtime":"native","liveness":"alive","native_access_state":"available"},"sessions":[],"tasks":[{"id":"task-1","display_number":1,"title":"현재 누가 무엇을 하는지 확인","state":"active"}],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"프로젝트-1", "운영자", "현재 누가 무엇을 하는지 확인", shortID(longID), longID} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML output lost CJK or canonical identifier %q", want)
		}
	}
}

func TestOperatorNowUsesLivenessBeforeNativeAccess(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor-1","scope":{"project_id":"project-1","mode":"all"},"identity":{"id":"session-stale","kind":"agent","runtime":"native","liveness":"stale","native_access_state":"available"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}

	if got := renderTTY(board, false); !strings.Contains(got, "0 active session(s)") {
		t.Fatalf("TTY Now summary counted a stale native session as active:\n%s", got)
	}
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "<strong>0</strong> active sessions") {
		t.Fatalf("HTML Now summary counted a stale native session as active:\n%s", got)
	}
}

func TestShortIDPreservesUnicodeRuneBoundaries(t *testing.T) {
	short := strings.Repeat("界", 9)
	if got := shortID(short); got != short {
		t.Fatalf("short Unicode ID = %q, want unchanged %q", got, short)
	}

	long := strings.Repeat("界", 30)
	got := shortID(long)
	if !utf8.ValidString(got) {
		t.Fatalf("shortID produced invalid UTF-8: %q", got)
	}
	want := strings.Repeat("界", 11) + "…" + strings.Repeat("界", 8)
	if got != want {
		t.Fatalf("shortID = %q, want %q", got, want)
	}
}

func TestHTMLBoardPrioritizesSignalsActionsAndAccessibleModernFallbacks(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`class="mast-actions"`,
		`class="signal-deck"`,
		`class="signal-card signal-`,
		`class="section-count`,
		`Priority signal`,
		`Attention queue`,
		`SAFE NEXT`,
		`class="section-kicker"`,
		`section:target`,
		`@container operator`,
		`prefers-contrast:more`,
		`prefers-reduced-transparency:reduce`,
		`forced-colors:active`,
		`outline:3px solid Highlight`,
		`--target-size:2.75rem`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML operator board missing current interaction pattern %q", want)
		}
	}
	if count := strings.Count(got, `class="signal-card `); count != 6 {
		t.Fatalf("signal card count = %d, want 6", count)
	}
	for _, target := range []string{`href="#now"`, `href="#sessions"`, `href="#work"`, `href="#inbox"`, `href="#handoffs"`, `href="#reservations"`, `href="#actions"`} {
		if !strings.Contains(got, target) {
			t.Errorf("HTML signal/action navigation missing %s", target)
		}
	}
	if strings.Contains(got, `backdrop-filter:blur(20px) saturate(135%)}.signal-card`) {
		t.Fatal("translucent material leaked from the navigation layer into signal content cards")
	}
}

func TestHTMLHealthyBoardUsesClearStateDrivenCopy(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"project-clear","snapshot_cursor":"cursor-clear","scope":{"project_id":"project-clear","mode":"all"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`The coordination graph is clear.`,
		`Review current state`,
		`<span class="signal-label">Attention</span><strong class="signal-value">0</strong>`,
		`class="blocker blocker-clear"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("healthy HTML board missing %q:\n%s", want, got)
		}
	}
}

func TestHTMLMobileRailIsCompactAndAnchorLayoutIsStable(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`position:sticky;top:0;z-index:20`,
		`.rail-health,.rail-context,.rail-footer{display:none}`,
		`grid-column:1/-1;overflow-x:auto`,
		`scroll-padding-top:8rem`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mobile rail contract missing %q", want)
		}
	}
	if strings.Contains(got, `content-visibility:auto`) {
		t.Fatal("anchor-sensitive board sections use content-visibility and may scroll to stale intrinsic positions")
	}
}

func TestHTMLMobileMastheadKeepsProjectWhilePrioritizingSignals(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`class="mast-project"`,
		`</span></p><h1 id="board-title">`,
		`@container operator (max-width:40rem)`,
		`.mast-meta{display:none}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mobile masthead contract missing %q", want)
		}
	}
	if strings.Index(got, `class="mast-copy"`) > strings.Index(got, `class="signal-deck"`) {
		t.Fatal("signal deck appears before the mobile masthead context")
	}
}

func TestHTMLBoardNamesLandmarksAndSupportsMixedDirectionContent(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`<meta name="description" content="Canonical local coordination snapshot:`,
		`<main class="console" id="board-content" tabindex="-1" aria-labelledby="board-title" aria-describedby="board-summary">`,
		`<header class="masthead" aria-labelledby="board-title" aria-describedby="board-summary">`,
		`<h1 id="board-title">`,
		`<p id="board-summary">`,
		`<nav class="signal-deck" aria-label="Coordination signals">`,
		`<section class="section" id="work" aria-labelledby="work-heading" aria-describedby="work-detail">`,
		`<h2 id="work-heading">Work graph</h2>`,
		`<p id="work-detail">`,
		`class="blocker-stack" aria-label="Attention queue"`,
		`class="task-title" dir="auto"`,
		`class="mast-project" dir="auto"`,
		`class="warning-item" dir="auto"`,
		`<code dir="auto">`,
		`.skip-link{z-index:100}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("semantic HTML contract missing %q", want)
		}
	}
	for _, id := range []string{"now", "sessions", "work", "dependencies", "inbox", "handoffs", "reservations", "git", "actions", "snapshot"} {
		section := `<section class="section" id="` + id + `" aria-labelledby="` + id + `-heading" aria-describedby="` + id + `-detail">`
		if !strings.Contains(got, section) {
			t.Errorf("section %s lacks labelled landmark semantics", id)
		}
	}
	if strings.Contains(got, `aria-live=`) || strings.Contains(got, `role="alert"`) {
		t.Fatal("static snapshot uses a live announcement region and would announce content unnecessarily")
	}
}

func TestHTMLHostileBidirectionalTextRemainsEscapedInsideAutoDirectionContainers(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"مشروع-협업","snapshot_cursor":"cursor-rtl","scope":{"project_id":"مشروع-협업","mode":"all"},"sessions":[],"tasks":[{"id":"task-rtl","display_number":1,"title":"مرحبا </h3><script>globalThis.rtlPwned=1</script> 협업","state":"active"}],"runs":[],"progress":[],"dependencies":[],"inbox":[{"message_id":"message-rtl","type":"notice","subject":"הודעה <img src=x onerror=alert(1)>","sender_session_id":"session-rtl","acknowledgement":"pending"}],"handoffs":[{"id":"handoff-rtl","task_id":"task-rtl","source_session_id":"source","target_session_id":"target","summary":"نقل العمل <svg onload=alert(2)>","status":"submitted"}],"reservations":[],"warnings":["אזהרה <script>alert(3)</script>"],"suggested_actions":[{"code":"inspect","command":"omg board task --task task-rtl"}]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`<span class="mast-project" dir="auto">مشروع-협업</span>`,
		`<h3 class="task-title" dir="auto"><small>#1</small>مرحبا &lt;/h3&gt;&lt;script&gt;globalThis.rtlPwned=1&lt;/script&gt; 협업</h3>`,
		`<h3 dir="auto">הודעה &lt;img src=x onerror=alert(1)&gt;</h3>`,
		`<span dir="auto">نقل العمل &lt;svg onload=alert(2)&gt;</span>`,
		`<div class="warning-item" dir="auto">⚠ אזהרה &lt;script&gt;alert(3)&lt;/script&gt;</div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mixed-direction hostile text contract missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{`<script>globalThis.rtlPwned`, `<img src=x`, `<svg onload`} {
		if strings.Contains(got, forbidden) {
			t.Errorf("hostile mixed-direction markup became executable: %s", forbidden)
		}
	}
}

func TestHTMLStatusBadgesNeverRelyOnColorAlone(t *testing.T) {
	tests := []struct {
		value string
		glyph string
		label string
		class string
	}{
		{"verified", "✔", "verified", "status-success"},
		{"working", "⟳", "working", "status-working"},
		{"warning", "⚠", "warning", "status-warning"},
		{"blocked", "⦸", "blocked", "status-blocked"},
		{"error", "✘", "error", "status-danger"},
		{"pending", "○", "pending", "status-pending"},
		{"", "·", "inactive", "status-muted"},
	}
	seenGlyphs := map[string]bool{}
	for _, test := range tests {
		badge := htmlStatusBadge(presentStatus(test.value))
		for _, want := range []string{test.glyph, test.label, test.class, `aria-hidden="true"`} {
			if !strings.Contains(badge, want) {
				t.Errorf("status %q badge missing %q: %s", test.value, want, badge)
			}
		}
		if seenGlyphs[test.glyph] && test.glyph != "·" {
			t.Errorf("status glyph %q is reused and weakens non-color differentiation", test.glyph)
		}
		seenGlyphs[test.glyph] = true
	}
}

func TestHTMLBrowserChromeIdentifiesPrivateProjectAndHealth(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`<meta name="referrer" content="no-referrer">`,
		`<meta name="robots" content="noindex,nofollow,noarchive">`,
		`<meta name="format-detection" content="telephone=no">`,
		`<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#151820">`,
		`<meta name="theme-color" media="(prefers-color-scheme: light)" content="#f3f5f7">`,
		`<title>OMG · project-1 · BLOCKED</title>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("browser chrome contract missing %q", want)
		}
	}
}

func TestHTMLBrowserTitleEscapesHostileProjectIdentity(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"</title><script>globalThis.titlePwned=1</script>","snapshot_cursor":"cursor","scope":{"project_id":"</title><script>globalThis.titlePwned=1</script>","mode":"all"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, `<title>OMG · &lt;/title&gt;&lt;script&gt;globalThis.titlePwned=1&lt;/script&gt; · CLEAR</title>`) {
		t.Fatalf("hostile project identity was not safely represented in title:\n%s", got)
	}
	if strings.Contains(got, `<script>globalThis.titlePwned`) {
		t.Fatal("hostile project identity escaped the title element")
	}
}

func TestHTMLIDsAndARIAReferencesAreUniqueAndResolvable(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	idPattern := regexp.MustCompile(`\sid="([^"]+)"`)
	ids := make(map[string]bool)
	for _, match := range idPattern.FindAllStringSubmatch(got, -1) {
		id := match[1]
		if ids[id] {
			t.Errorf("HTML contains duplicate id %q", id)
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		t.Fatal("HTML contains no IDs")
	}
	for _, attribute := range []string{"aria-labelledby", "aria-describedby"} {
		pattern := regexp.MustCompile(`\s` + attribute + `="([^"]+)"`)
		for _, match := range pattern.FindAllStringSubmatch(got, -1) {
			for _, id := range strings.Fields(match[1]) {
				if !ids[id] {
					t.Errorf("%s references missing id %q", attribute, id)
				}
			}
		}
	}
	for _, match := range regexp.MustCompile(`href="#([^"]+)"`).FindAllStringSubmatch(got, -1) {
		if !ids[match[1]] {
			t.Errorf("fragment link references missing id %q", match[1])
		}
	}
}

func TestHTMLCommandPaletteGroupsSafeActionsByOperatorIntent(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"project-actions","snapshot_cursor":"cursor-actions","scope":{"project_id":"project-actions","mode":"all"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[{"code":"show_task","command":"omg board task --task task-1"},{"code":"reservation_history","command":"omg reserve history --payload '{\"reservation_id\":\"reservation-1\"}'"},{"code":"git_cleanup_plan","command":"omg git cleanup-plan"},{"code":"handoff_create","command":"omg handoff create --payload-file handoff.json"},{"code":"future_action","command":"omg future action"}]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`class="command-groups"`,
		`class="command-group command-group-inspect" aria-labelledby="action-group-inspect"`,
		`class="command-group command-group-recover" aria-labelledby="action-group-recover"`,
		`class="command-group command-group-coordinate" aria-labelledby="action-group-coordinate"`,
		`class="command-group command-group-other" aria-labelledby="action-group-other"`,
		`id="action-group-inspect">Inspect</h3>`,
		`id="action-group-recover">Recover</h3>`,
		`id="action-group-coordinate">Coordinate</h3>`,
		`id="action-group-other">Other</h3>`,
		`Inspect the selected task and its adjacent coordination facts.`,
		`Review reservation ownership, renewal, conflict, and release history.`,
		`Generate a non-destructive cleanup plan for the observed repository risk.`,
		`Review this bounded command and its selected scope.`,
		`<span class="command-code">future_action</span>`,
		`class="command-review-note">Select and copy a command, replace '&lt;PROJECT_PATH&gt;' with the intended absolute checkout path, review its scope, then run it in your shell. Commands are inert in this export.</p>`,
		`<pre class="command-value" tabindex="0" aria-label="Copyable safe command"><code dir="auto">omg board task --task task-1</code></pre>`,
		`.command-value{display:flex;min-height:var(--target-size)`,
		`cursor:text;white-space:pre-wrap;word-break:break-word;user-select:all`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("grouped command palette missing %q", want)
		}
	}
	if strings.Contains(got, `<button`) {
		t.Fatal("static command palette implies an unavailable scripted action button")
	}
}

func TestHTMLNavigationProgressivelyHighlightsTargetsAndAdaptsDensity(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`@supports selector(body:has(.section:target))`,
		`body:not(:has(.section:target)) .section-index a[href="#now"]`,
		`body:has(#actions:target) .section-index a[href="#actions"]`,
		`@media(min-width:52.01rem) and (max-height:46rem)`,
		`.section-index a{min-height:var(--target-size);padding:.34rem .55rem}`,
		`.skip-link{display:inline-flex;min-height:var(--target-size);align-items:center}`,
		`.meta-disclosure summary{min-height:var(--target-size);padding:.4rem .5rem}`,
		`@media(pointer:coarse)`,
		`.section-index a:hover,.signal-card:hover,.mast-action:hover,.command:hover{transform:none}`,
		`.command-groups{grid-template-columns:1fr}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("adaptive navigation contract missing %q", want)
		}
	}
}

func TestTTYCommandPaletteIsReviewFirstAndGrouped(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"project-actions","snapshot_cursor":"cursor-actions","scope":{"project_id":"project-actions","mode":"all"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[{"code":"show_task","command":"omg board task --task task-1"},{"code":"reservation_history","command":"omg reserve history --payload '{\"reservation_id\":\"reservation-1\"}'"},{"code":"git_cleanup_plan","command":"omg git cleanup-plan"}]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}
	got := renderTTYWidth(board, false, 80)
	canonical := strings.Join(strings.Fields(got), " ")
	for _, want := range []string{
		"COMMAND PALETTE 3 safe action(s)",
		"Copy a command, replace '<PROJECT_PATH>' with the intended absolute checkout path, review its scope, then run it in your shell. Nothing is executed by this view.",
		"INSPECT 2 action(s)",
		"RECOVER 1 action(s)",
		"show_task Inspect the selected task and its adjacent coordination facts.",
		"reservation_history Review reservation ownership, renewal, conflict, and release history.",
		"git_cleanup_plan Generate a non-destructive cleanup plan for the observed repository risk.",
		"❯ omg board task --task task-1",
		"❯ omg git cleanup-plan",
	} {
		if !strings.Contains(canonical, want) {
			t.Errorf("review-first TTY command palette missing %q:\n%s", want, got)
		}
	}
	if strings.Index(canonical, "INSPECT 2 action(s)") > strings.Index(canonical, "RECOVER 1 action(s)") {
		t.Fatal("TTY action groups are not ordered from inspection to recovery")
	}
	for _, line := range strings.Split(got, "\n") {
		if ttyDisplayWidth(line) > 80 {
			t.Errorf("TTY action line width %d exceeds 80: %q", ttyDisplayWidth(line), line)
		}
	}
}
