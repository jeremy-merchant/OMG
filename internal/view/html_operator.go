package view

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"strings"

	"example.invalid/coordledger/internal/app/query"
)

const operatorHTMLStyles = `:root{color-scheme:dark light;--canvas:#151820;--surface:#0f1216;--surface-raised:#1b2028;--surface-soft:#202630;--text-primary:#e8ecf4;--text-secondary:#bdc4cf;--text-muted:#858e9d;--accent:#00b4ff;--accent-soft:#06364c;--success:#00e982;--success-soft:#073526;--warning:#f0ad45;--warning-soft:#3d2d16;--danger:#ff5a67;--danger-soft:#421c22;--info:#55c8ff;--pending:#d4c090;--blocked:#d989ff;--blocked-soft:#392146;--border-strong:#39424e;--border-subtle:#252c35;--radius-sm:6px;--radius-md:10px;--radius-lg:16px;--space-compact-1:.25rem;--space-compact-2:.5rem;--space-compact-3:.75rem;--space-comfort-1:1rem;--space-comfort-2:1.5rem;--space-comfort-3:2.25rem;--font-ui:Inter,ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--font-mono:"SFMono-Regular",Consolas,"Liberation Mono",Menlo,monospace;--type-title:clamp(1.8rem,4vw,3.4rem);--type-section:1rem;--type-body:.875rem;--type-meta:.72rem;--shadow:0 20px 70px rgba(0,0,0,.32);--focus-ring:0 0 0 3px rgba(0,180,255,.38)}*{box-sizing:border-box}html{scroll-behavior:smooth;overflow-x:hidden}body{margin:0;min-height:100dvh;overflow-x:hidden;background:var(--canvas);color:var(--text-primary);font-family:var(--font-ui);font-size:var(--type-body);line-height:1.5;text-rendering:optimizeLegibility}body:before{content:"";position:fixed;inset:0;pointer-events:none;background:radial-gradient(circle at 80% -10%,rgba(0,180,255,.12),transparent 29rem),linear-gradient(rgba(255,255,255,.018) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.018) 1px,transparent 1px);background-size:auto,32px 32px,32px 32px;mask-image:linear-gradient(to bottom,#000 0,transparent 80%)}a{color:inherit}.skip-link{position:fixed;z-index:10;top:.75rem;left:.75rem;transform:translateY(-180%);padding:.65rem .85rem;border-radius:var(--radius-sm);background:var(--text-primary);color:var(--canvas);font-weight:700;text-decoration:none}.skip-link:focus{transform:none}:focus-visible{outline:0;box-shadow:var(--focus-ring);border-radius:var(--radius-sm)}.shell{position:relative;display:grid;grid-template-columns:17rem minmax(0,1fr);width:min(100%,112rem);min-height:100dvh;margin:auto}.rail{position:sticky;top:0;align-self:start;height:100dvh;padding:var(--space-comfort-2);border-right:1px solid var(--border-subtle);background:color-mix(in srgb,var(--surface) 92%,transparent);backdrop-filter:blur(18px)}.brand{display:flex;align-items:center;gap:.75rem;margin-bottom:2rem}.brand-mark{display:grid;place-items:center;width:2.25rem;height:2.25rem;border:1px solid var(--border-strong);border-radius:var(--radius-md);background:var(--surface-raised);color:var(--accent);font:800 .72rem/1 var(--font-mono);letter-spacing:-.08em;box-shadow:inset 0 0 0 1px rgba(255,255,255,.025)}.brand-copy strong{display:block;font-size:.82rem;letter-spacing:.08em}.brand-copy span{color:var(--text-muted);font:500 .66rem/1.3 var(--font-mono);text-transform:uppercase}.rail-health{margin-bottom:1.5rem;padding:.9rem;border:1px solid var(--border-subtle);border-radius:var(--radius-md);background:var(--surface-raised)}.rail-health p{margin:.45rem 0 0;color:var(--text-secondary);font-size:.76rem}.rail-context{display:grid;gap:.5rem;margin:0 0 1.6rem}.rail-context div{min-width:0}.rail-context dt{color:var(--text-muted);font:650 .62rem/1.2 var(--font-mono);letter-spacing:.08em;text-transform:uppercase}.rail-context dd{margin:.15rem 0 0;overflow-wrap:anywhere;color:var(--text-secondary);font:550 .72rem/1.35 var(--font-mono)}.section-index{display:grid;gap:.18rem}.section-index a{display:flex;align-items:center;gap:.65rem;padding:.46rem .55rem;border-radius:var(--radius-sm);color:var(--text-muted);font:600 .72rem/1.2 var(--font-mono);text-decoration:none}.section-index a:before{content:"";width:.35rem;height:.35rem;border-radius:50%;background:var(--border-strong)}.section-index a:hover{background:var(--surface-raised);color:var(--text-primary)}.section-index a:hover:before{background:var(--accent)}.rail-footer{position:absolute;right:1.5rem;bottom:1.3rem;left:1.5rem;color:var(--text-muted);font:500 .64rem/1.45 var(--font-mono)}.console{min-width:0;padding:clamp(1rem,3vw,3rem)}.masthead{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:2rem;align-items:end;padding:clamp(1.4rem,3vw,2.8rem) 0 1.5rem;border-bottom:1px solid var(--border-strong)}.kicker{margin:0 0 .6rem;color:var(--accent);font:700 .7rem/1.2 var(--font-mono);letter-spacing:.16em;text-transform:uppercase}.masthead h1{max-width:18ch;margin:0;font-size:var(--type-title);line-height:.95;letter-spacing:-.055em}.masthead p{max-width:64ch;margin:.8rem 0 0;color:var(--text-secondary)}.mast-meta{display:grid;grid-template-columns:auto auto;gap:.45rem 1.1rem;margin:0;font-family:var(--font-mono);font-size:var(--type-meta)}.mast-meta dt{color:var(--text-muted)}.mast-meta dd{max-width:24rem;margin:0;overflow-wrap:anywhere;color:var(--text-secondary);text-align:right}.status-badge{display:inline-flex;align-items:center;gap:.38rem;width:max-content;padding:.22rem .48rem;border:1px solid currentColor;border-radius:999px;font:700 .64rem/1.2 var(--font-mono);letter-spacing:.025em;text-transform:uppercase}.status-success{color:var(--success);background:var(--success-soft)}.status-working,.status-info{color:var(--info);background:var(--accent-soft)}.status-pending,.status-warning{color:var(--warning);background:var(--warning-soft)}.status-blocked{color:var(--blocked);background:var(--blocked-soft)}.status-danger{color:var(--danger);background:var(--danger-soft)}.status-muted{color:var(--text-muted);background:var(--surface-soft)}.section{scroll-margin-top:1rem;padding:2rem 0;border-bottom:1px solid var(--border-subtle)}.section:last-child{border-bottom:0}.section-heading{display:flex;align-items:baseline;justify-content:space-between;gap:1rem;margin-bottom:1rem}.section-heading h2{margin:0;font-size:var(--type-section);letter-spacing:-.01em}.section-heading p{margin:0;color:var(--text-muted);font:500 var(--type-meta)/1.4 var(--font-mono)}.empty-state{display:flex;align-items:center;gap:.6rem;margin:0;padding:.75rem 0;color:var(--text-muted);font-family:var(--font-mono)}.empty-state:before{content:"·";color:var(--border-strong);font-size:1.4rem}.now-grid{display:grid;grid-template-columns:minmax(0,1.55fr) minmax(16rem,.75fr);gap:1rem}.now-primary{position:relative;overflow:hidden;padding:1.1rem 1.2rem;border:1px solid var(--border-strong);border-radius:var(--radius-lg);background:linear-gradient(135deg,var(--surface-raised),var(--surface));box-shadow:var(--shadow)}.now-primary:before{content:"";position:absolute;inset:0 auto 0 0;width:3px;background:var(--accent)}.now-primary h3{margin:0 0 .35rem;font-size:1.1rem}.now-primary>p{margin:0;color:var(--text-secondary)}.activity-strip{display:flex;flex-wrap:wrap;gap:.6rem;margin-top:1rem}.activity-token{display:inline-flex;align-items:center;gap:.45rem;padding:.45rem .58rem;border:1px solid var(--border-subtle);border-radius:var(--radius-sm);background:var(--surface);color:var(--text-secondary);font:600 .7rem/1 var(--font-mono)}.activity-token strong{color:var(--text-primary)}.blocker-stack{display:grid;gap:.55rem}.blocker{padding:.75rem .8rem;border-left:2px solid var(--blocked);border-radius:0 var(--radius-sm) var(--radius-sm) 0;background:var(--surface-raised)}.blocker strong{display:block;margin-bottom:.2rem;font-size:.8rem}.blocker span{color:var(--text-muted);font:500 .68rem/1.4 var(--font-mono)}.tree,.tree ol{margin:0;padding:0;list-style:none}.tree ol{position:relative;margin-left:.78rem;padding-left:1.1rem}.tree ol:before{content:"";position:absolute;top:0;bottom:.9rem;left:.18rem;border-left:1px solid var(--border-strong)}.tree-node{position:relative;padding:.34rem 0}.tree ol>.tree-node:before{content:"";position:absolute;top:1.05rem;left:-.92rem;width:.7rem;border-top:1px solid var(--border-strong)}.node-main{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:.7rem;align-items:center}.node-copy{min-width:0}.node-title{display:block;overflow-wrap:anywhere;font-weight:650}.node-subtitle{display:block;margin-top:.12rem;color:var(--text-muted);font:500 var(--type-meta)/1.4 var(--font-mono);overflow-wrap:anywhere}.node-id{color:var(--text-muted);font:500 var(--type-meta)/1.3 var(--font-mono)}.meta-disclosure{margin:.42rem 0 0 1.8rem}.meta-disclosure summary{width:max-content;cursor:pointer;color:var(--text-muted);font:550 .66rem/1.3 var(--font-mono)}.meta-disclosure[open] summary{color:var(--accent)}.fact-list{display:flex;flex-wrap:wrap;gap:.28rem .5rem;margin:.45rem 0 0;padding:.55rem .65rem;border:1px solid var(--border-subtle);border-radius:var(--radius-sm);background:var(--surface)}.fact-list code{overflow-wrap:anywhere;color:var(--text-muted);font:500 .64rem/1.5 var(--font-mono)}.fact-list code+code:before{content:"·";margin-right:.5rem;color:var(--border-strong)}.timeline{display:grid;gap:.75rem}.task-row{position:relative;padding:1rem 1rem 1rem 1.2rem;border-left:2px solid var(--accent);border-radius:0 var(--radius-md) var(--radius-md) 0;background:var(--surface-raised)}.task-row:before{content:"";position:absolute;top:1.2rem;left:-.38rem;width:.64rem;height:.64rem;border:2px solid var(--canvas);border-radius:50%;background:var(--accent)}.task-header{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:.7rem;align-items:start}.task-title{margin:0;font-size:.94rem}.task-title small{margin-right:.45rem;color:var(--text-muted);font-family:var(--font-mono)}.run-list{display:grid;gap:.4rem;margin:.7rem 0 0 1.2rem;padding-left:.9rem;border-left:1px solid var(--border-strong)}.run-row{display:grid;grid-template-columns:auto minmax(0,1fr);gap:.6rem;align-items:start}.run-copy strong{font-size:.78rem}.run-copy span{display:block;color:var(--text-muted);font:500 var(--type-meta)/1.4 var(--font-mono)}.progress-lanes{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:.6rem;margin-top:.7rem}.progress-lane{min-width:0;padding:.6rem;border:1px solid var(--border-subtle);border-radius:var(--radius-sm);background:var(--surface)}.progress-lane h4{margin:0 0 .35rem;color:var(--text-muted);font:700 .62rem/1.2 var(--font-mono);letter-spacing:.08em;text-transform:uppercase}.progress-lane ul{margin:0;padding-left:1rem}.progress-lane li{overflow-wrap:anywhere;color:var(--text-secondary);font-size:.72rem}.dependency-map{display:grid;gap:.5rem}.dependency-link{display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr) auto;gap:.6rem;align-items:center;padding:.72rem .8rem;border:1px solid var(--border-subtle);border-radius:var(--radius-md);background:var(--surface-raised)}.dependency-link code{overflow-wrap:anywhere;color:var(--text-secondary);font:600 .72rem/1.4 var(--font-mono)}.dependency-arrow{color:var(--text-muted);font-family:var(--font-mono)}.message-stack{display:grid;gap:.5rem}.message{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:.8rem;align-items:start;padding:.8rem 0;border-bottom:1px solid var(--border-subtle)}.message:last-child{border-bottom:0}.message-glyph{color:var(--pending);font:700 1rem/1 var(--font-mono)}.message h3{margin:0;font-size:.82rem}.message p{margin:.2rem 0 0;color:var(--text-muted);font:500 var(--type-meta)/1.45 var(--font-mono)}.handoff-flow{display:grid;gap:.7rem}.handoff{display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);gap:.7rem;align-items:stretch}.handoff-party,.handoff-summary{min-width:0;padding:.75rem;border:1px solid var(--border-subtle);border-radius:var(--radius-md);background:var(--surface-raised)}.handoff-party strong,.handoff-summary strong{display:block;overflow-wrap:anywhere;font-size:.78rem}.handoff-party span,.handoff-summary span{display:block;margin-top:.18rem;overflow-wrap:anywhere;color:var(--text-muted);font:500 .68rem/1.4 var(--font-mono)}.handoff-arrow{display:grid;place-items:center;color:var(--accent);font:700 1.1rem/1 var(--font-mono)}.reservation-list{display:grid;gap:.5rem}.reservation{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:.7rem;align-items:center;padding:.7rem .75rem;border:1px solid var(--border-subtle);border-radius:var(--radius-md);background:var(--surface-raised)}.path-mark{color:var(--accent);font:700 .8rem/1 var(--font-mono)}.reservation code{overflow-wrap:anywhere;color:var(--text-primary);font:600 .72rem/1.4 var(--font-mono)}.reservation p{margin:.18rem 0 0;color:var(--text-muted);font:500 .67rem/1.4 var(--font-mono)}.git-console{display:grid;grid-template-columns:minmax(0,1.4fr) minmax(14rem,.6fr);gap:1rem}.git-assets{display:grid;gap:.5rem}.git-asset{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:.75rem;align-items:start;padding:.75rem;border-left:2px solid var(--border-strong);background:var(--surface-raised)}.git-asset strong{display:block;overflow-wrap:anywhere;font-size:.8rem}.git-asset p{margin:.2rem 0 0;color:var(--text-muted);font:500 .67rem/1.45 var(--font-mono)}.warning-stack{display:grid;align-content:start;gap:.45rem}.warning-item{padding:.65rem .7rem;border-left:2px solid var(--warning);background:var(--warning-soft);color:var(--text-secondary);font-size:.72rem;overflow-wrap:anywhere}.command-palette{display:grid;gap:.45rem}.command{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:.7rem;align-items:center;padding:.72rem .8rem;border:1px solid var(--border-subtle);border-radius:var(--radius-md);background:var(--surface-raised)}.command:hover{border-color:var(--border-strong);background:var(--surface-soft)}.command-prompt{color:var(--accent);font:800 .9rem/1 var(--font-mono)}.command code{overflow-wrap:anywhere;color:var(--text-primary);font:600 .76rem/1.4 var(--font-mono)}.command span{color:var(--text-muted);font:500 .66rem/1.4 var(--font-mono)}.snapshot-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.5rem}.snapshot-item{min-width:0;padding:.65rem .75rem;border:1px solid var(--border-subtle);border-radius:var(--radius-sm);background:var(--surface)}.snapshot-item dt{color:var(--text-muted);font:650 .61rem/1.2 var(--font-mono);letter-spacing:.06em;text-transform:uppercase}.snapshot-item dd{margin:.18rem 0 0;overflow-wrap:anywhere;color:var(--text-secondary);font:500 .68rem/1.4 var(--font-mono)}.page-footer{display:flex;justify-content:space-between;gap:1rem;padding:1.2rem 0;color:var(--text-muted);font:500 .65rem/1.4 var(--font-mono)}@media(max-width:72rem){.shell{grid-template-columns:13rem minmax(0,1fr)}.rail{padding:1.1rem}.rail-footer{display:none}.now-grid,.git-console{grid-template-columns:1fr}}@media(max-width:52rem){.shell{display:block}.rail{position:relative;height:auto;border-right:0;border-bottom:1px solid var(--border-subtle)}.brand,.rail-context{margin-bottom:1rem}.section-index{display:flex;overflow-x:auto;padding-bottom:.25rem}.section-index a{white-space:nowrap}.console{padding:1rem}.masthead{grid-template-columns:1fr}.mast-meta dd{text-align:left}.progress-lanes{grid-template-columns:1fr}.dependency-link{grid-template-columns:minmax(0,1fr) auto minmax(0,1fr)}.dependency-link .status-badge{grid-column:1/-1}.handoff{grid-template-columns:1fr}.handoff-arrow{transform:rotate(90deg)}.snapshot-grid{grid-template-columns:1fr}}@media(max-width:34rem){.masthead h1{font-size:2rem}.mast-meta{grid-template-columns:1fr}.task-header,.node-main,.message,.reservation,.git-asset,.command{grid-template-columns:auto minmax(0,1fr)}.task-header>.status-badge,.node-main>.node-id,.message>.status-badge,.reservation>.status-badge,.git-asset>.status-badge,.command>span{grid-column:2}.dependency-link{grid-template-columns:1fr}.dependency-arrow{transform:rotate(90deg);text-align:center}.activity-strip{display:grid}.page-footer{display:block}.page-footer span{display:block;margin-top:.35rem}}@media(prefers-color-scheme:light){:root{--canvas:#f3f5f7;--surface:#fff;--surface-raised:#f8fafc;--surface-soft:#eef3f7;--text-primary:#111820;--text-secondary:#34404c;--text-muted:#647180;--accent:#006f9d;--accent-soft:#dceff7;--success:#087443;--success-soft:#dff4e8;--warning:#8a5700;--warning-soft:#f8ebcf;--danger:#b42336;--danger-soft:#fae5e8;--info:#006f9d;--pending:#755f20;--blocked:#7d2f95;--blocked-soft:#f0e3f5;--border-strong:#aeb8c2;--border-subtle:#d7dee5;--shadow:0 18px 50px rgba(28,39,49,.1);--focus-ring:0 0 0 3px rgba(0,111,157,.28)}body:before{background:radial-gradient(circle at 80% -10%,rgba(0,111,157,.08),transparent 29rem),linear-gradient(rgba(17,24,32,.022) 1px,transparent 1px),linear-gradient(90deg,rgba(17,24,32,.022) 1px,transparent 1px)}}@media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}.skip-link{transition:none}}@media print{:root{color-scheme:light;--canvas:#fff;--surface:#fff;--surface-raised:#fff;--surface-soft:#fff;--text-primary:#000;--text-secondary:#222;--text-muted:#444;--accent:#000;--accent-soft:#fff;--success:#000;--success-soft:#fff;--warning:#000;--warning-soft:#fff;--danger:#000;--danger-soft:#fff;--info:#000;--pending:#000;--blocked:#000;--blocked-soft:#fff;--border-strong:#555;--border-subtle:#aaa;--shadow:none;--focus-ring:none}body:before,.rail,.skip-link{display:none}.shell{display:block;width:100%}.console{padding:0}.section,.now-primary,.task-row,.reservation,.git-asset,.command{break-inside:avoid;box-shadow:none}.masthead{padding-top:0}.meta-disclosure{display:block}.meta-disclosure>summary{list-style:none}.meta-disclosure:not([open])>:not(summary){display:block}.page-footer{border-top:1px solid #000}a{text-decoration:none}}`

var operatorHTMLStyleCSPSource = func() string {
	sum := sha256.Sum256([]byte(operatorHTMLStyles))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

func renderHTML(board query.BoardSnapshot) string {
	health := boardHealth(board)
	var out strings.Builder
	out.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	out.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	out.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src " + operatorHTMLStyleCSPSource + "; img-src 'none'; font-src 'none'; script-src 'none'; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'\">")
	out.WriteString("<meta name=\"color-scheme\" content=\"dark light\"><title>OMG operator board</title><style>")
	out.WriteString(operatorHTMLStyles)
	out.WriteString("</style></head><body><a class=\"skip-link\" href=\"#board-content\">Skip to board content</a><div class=\"shell\">")
	writeHTMLRail(&out, board, health)
	out.WriteString("<main class=\"console\" id=\"board-content\" tabindex=\"-1\">")
	writeHTMLMasthead(&out, board, health)
	writeHTMLNow(&out, board, health)
	writeHTMLSessions(&out, board)
	writeHTMLWork(&out, board)
	writeHTMLDependencies(&out, board)
	writeHTMLInbox(&out, board)
	writeHTMLHandoffs(&out, board)
	writeHTMLReservations(&out, board)
	writeHTMLGit(&out, board)
	writeHTMLActions(&out, board)
	writeHTMLSnapshot(&out, board)
	out.WriteString("<footer class=\"page-footer\"><strong>OMG operator ledger</strong><span>Self-contained · no scripts · canonical redacted snapshot</span></footer></main></div></body></html>\n")
	return out.String()
}

func writeHTMLRail(out *strings.Builder, board query.BoardSnapshot, health boardHealthView) {
	out.WriteString("<aside class=\"rail\"><div class=\"brand\"><span class=\"brand-mark\" aria-hidden=\"true\">OMG</span><span class=\"brand-copy\"><strong>OPERATOR LEDGER</strong><span>coordination console</span></span></div>")
	out.WriteString("<div class=\"rail-health\">" + htmlStatusBadge(health.Status) + "<p>" + escapeHTML(health.Headline) + "</p></div>")
	out.WriteString("<dl class=\"rail-context\"><div><dt>Project</dt><dd>" + escapeHTML(primaryProject(board)) + "</dd></div><div><dt>Scope</dt><dd>" + escapeHTML(string(board.Scope.Mode)) + "</dd></div>")
	if board.Scope.Selector != "" {
		out.WriteString("<div><dt>Selector</dt><dd title=\"" + escapeAttr(board.Scope.Selector) + "\">" + escapeHTML(shortID(board.Scope.Selector)) + "</dd></div>")
	}
	out.WriteString("</dl><nav class=\"section-index\" aria-label=\"Board sections\">")
	for _, section := range []struct{ ID, Label string }{{"now", "Now"}, {"sessions", "Sessions"}, {"work", "Work graph"}, {"dependencies", "Dependencies"}, {"inbox", "Inbox"}, {"handoffs", "Handoffs"}, {"reservations", "Reservations"}, {"git", "Git + warnings"}, {"actions", "Actions"}, {"snapshot", "Snapshot"}} {
		out.WriteString("<a href=\"#" + section.ID + "\">" + section.Label + "</a>")
	}
	out.WriteString("</nav><p class=\"rail-footer\">Generated " + escapeHTML(stamp(board.GeneratedAt)) + "<br>Cursor " + escapeHTML(shortID(board.SnapshotCursor)) + "</p></aside>")
}

func writeHTMLMasthead(out *strings.Builder, board query.BoardSnapshot, health boardHealthView) {
	out.WriteString("<header class=\"masthead\"><div><p class=\"kicker\">OMG / BOARD / " + escapeHTML(strings.ToUpper(string(board.Mode))) + "</p><h1>Coordination, without the log dump.</h1><p>See active ownership, blocked work, handoffs, reservations, and safe next commands in one canonical operator view.</p></div>")
	out.WriteString("<dl class=\"mast-meta\" aria-label=\"Snapshot metadata\"><dt>Health</dt><dd>" + htmlStatusBadge(health.Status) + "</dd><dt>Project</dt><dd>" + escapeHTML(primaryProject(board)) + "</dd><dt>Generated</dt><dd>" + escapeHTML(stamp(board.GeneratedAt)) + "</dd><dt>Cursor</dt><dd title=\"" + escapeAttr(board.SnapshotCursor) + "\">" + escapeHTML(shortID(board.SnapshotCursor)) + "</dd></dl></header>")
}

func writeHTMLSectionStart(out *strings.Builder, id, title, detail string) {
	out.WriteString("<section class=\"section\" id=\"" + id + "\"><header class=\"section-heading\"><h2>" + title + "</h2><p>" + escapeHTML(detail) + "</p></header>")
}

func writeHTMLEmpty(out *strings.Builder, message string) {
	out.WriteString("<p class=\"empty-state\">" + escapeHTML(message) + "</p>")
}

func writeHTMLNow(out *strings.Builder, board query.BoardSnapshot, health boardHealthView) {
	writeHTMLSectionStart(out, "now", "Now", "current ownership and blockers")
	activeSessions, openTasks, blocked := 0, 0, 0
	for _, identity := range allIdentities(board) {
		if activeIdentity(identity) {
			activeSessions++
		}
	}
	for _, task := range board.Tasks {
		state := presentStatus(task.State).Semantic
		if state == stateWorking || state == statePending || state == stateBlocked {
			openTasks++
		}
	}
	for _, dependency := range board.Dependencies {
		if !dependency.Satisfied {
			blocked++
		}
	}
	out.WriteString("<div class=\"now-grid\"><article class=\"now-primary\"><h3>" + escapeHTML(health.Headline) + "</h3><p>" + escapeHTML(health.Detail) + "</p><div class=\"activity-strip\">")
	for _, token := range []struct {
		Value int
		Label string
	}{{activeSessions, "active sessions"}, {openTasks, "open tasks"}, {blocked, "blockers"}, {len(board.Warnings), "warnings"}} {
		out.WriteString("<span class=\"activity-token\"><strong>" + fmt.Sprint(token.Value) + "</strong> " + token.Label + "</span>")
	}
	out.WriteString("</div></article><div class=\"blocker-stack\">")
	if blocked == 0 && len(board.Warnings) == 0 {
		out.WriteString("<div class=\"blocker\"><strong>No blocking signal</strong><span>Dependencies and advisory warnings are clear.</span></div>")
	} else {
		for _, dependency := range board.Dependencies {
			if dependency.Satisfied {
				continue
			}
			out.WriteString("<div class=\"blocker\"><strong>" + escapeHTML(shortID(dependency.DependentTaskID)) + " waits for " + escapeHTML(shortID(dependency.BlockerTaskID)) + "</strong><span>" + escapeHTML(dependency.Type) + " · unblock on " + escapeHTML(dependency.UnblockOn) + "</span></div>")
		}
		for _, warning := range board.Warnings {
			out.WriteString("<div class=\"blocker\"><strong>Advisory warning</strong><span>" + escapeHTML(warning) + "</span></div>")
		}
	}
	out.WriteString("</div></div></section>")
}

func writeHTMLSessions(out *strings.Builder, board query.BoardSnapshot) {
	identities := allIdentities(board)
	writeHTMLSectionStart(out, "sessions", "Sessions", fmt.Sprintf("%d identity record(s)", len(identities)))
	if len(identities) == 0 {
		writeHTMLEmpty(out, "No session identity recorded")
		out.WriteString("</section>")
		return
	}
	children := make(map[string][]query.IdentityView)
	roots := make([]query.IdentityView, 0, len(identities))
	known := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		known[identity.ID] = struct{}{}
	}
	for _, identity := range identities {
		if identity.ParentSessionID == "" {
			roots = append(roots, identity)
		} else if _, exists := known[identity.ParentSessionID]; exists {
			children[identity.ParentSessionID] = append(children[identity.ParentSessionID], identity)
		} else {
			roots = append(roots, identity)
		}
	}
	if len(roots) == 0 {
		roots = append(roots, identities...)
	}
	out.WriteString("<ol class=\"tree\" aria-label=\"Session lineage\">")
	visited := make(map[string]bool, len(identities))
	for _, root := range roots {
		writeHTMLSessionNode(out, root, children, visited)
	}
	out.WriteString("</ol></section>")
}

func writeHTMLSessionNode(out *strings.Builder, identity query.IdentityView, children map[string][]query.IdentityView, visited map[string]bool) {
	if visited[identity.ID] {
		return
	}
	visited[identity.ID] = true
	status := presentStatus(string(identity.Liveness))
	if identity.Liveness == "" {
		status = presentStatus(identity.NativeAccessState)
	}
	out.WriteString("<li class=\"tree-node\"><div class=\"node-main\">" + htmlStatusBadge(status) + "<div class=\"node-copy\"><span class=\"node-title\">" + escapeHTML(firstNonEmpty(identity.Role, identity.Kind, "session")) + "</span><span class=\"node-subtitle\">" + escapeHTML(strings.Join(nonEmpty(identity.Runtime, identity.NativeAccessState, string(identity.Liveness)), " · ")) + "</span></div><span class=\"node-id\" title=\"" + escapeAttr(identity.ID) + "\">" + escapeHTML(shortID(identity.ID)) + "</span></div>")
	writeHTMLFacts(out, "Session metadata", identityRows(query.BoardSnapshot{Identity: &identity}, text))
	if len(children[identity.ID]) > 0 {
		out.WriteString("<ol>")
		for _, child := range children[identity.ID] {
			writeHTMLSessionNode(out, child, children, visited)
		}
		out.WriteString("</ol>")
	}
	out.WriteString("</li>")
}

func writeHTMLWork(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "work", "Work graph", fmt.Sprintf("%d tasks · %d runs · %d progress updates", len(board.Tasks), len(board.Runs), len(board.Progress)))
	if len(board.Tasks) == 0 && len(board.Runs) == 0 && len(board.Progress) == 0 {
		writeHTMLEmpty(out, "No task or run recorded")
		out.WriteString("</section>")
		return
	}
	runs := make(map[string][]query.RunView)
	progress := make(map[string][]query.ProgressView)
	for _, run := range board.Runs {
		runs[run.TaskID] = append(runs[run.TaskID], run)
	}
	for _, item := range board.Progress {
		progress[item.TaskID] = append(progress[item.TaskID], item)
	}
	out.WriteString("<div class=\"timeline\">")
	for _, task := range board.Tasks {
		out.WriteString("<article class=\"task-row\"><div class=\"task-header\">" + htmlStatusBadge(presentStatus(task.State)) + "<div><h3 class=\"task-title\"><small>#" + fmt.Sprint(task.DisplayNumber) + "</small>" + escapeHTML(task.Title) + "</h3><span class=\"node-subtitle\" title=\"" + escapeAttr(task.ID) + "\">" + escapeHTML(shortID(task.ID)) + " · " + escapeHTML(task.State) + "</span></div><span class=\"node-id\">" + escapeHTML(shortID(task.ClaimedBySessionID)) + "</span></div>")
		writeHTMLFacts(out, "Task metadata", tasksRunsRows(query.BoardSnapshot{Tasks: []query.TaskView{task}}, text))
		if len(runs[task.ID]) > 0 {
			out.WriteString("<div class=\"run-list\">")
			for _, run := range runs[task.ID] {
				out.WriteString("<div class=\"run-row\">" + htmlStatusBadge(presentStatus(run.State)) + "<div class=\"run-copy\"><strong>Run " + escapeHTML(shortID(run.ID)) + "</strong><span>session " + escapeHTML(shortID(run.SessionID)) + " · state=" + escapeHTML(run.State) + "</span></div></div>")
			}
			out.WriteString("</div>")
		}
		for _, item := range progress[task.ID] {
			writeHTMLProgress(out, item)
		}
		out.WriteString("</article>")
	}
	out.WriteString("</div></section>")
}

func writeHTMLProgress(out *strings.Builder, progress query.ProgressView) {
	out.WriteString("<div class=\"progress-lanes\">")
	for _, lane := range []struct {
		Title  string
		Values []string
	}{{"Done", progress.Done}, {"Doing", progress.Doing}, {"Next", progress.Next}} {
		out.WriteString("<div class=\"progress-lane\"><h4>" + lane.Title + "</h4>")
		if len(lane.Values) == 0 {
			out.WriteString("<span class=\"node-subtitle\">None</span>")
		} else {
			out.WriteString("<ul>")
			for _, value := range lane.Values {
				out.WriteString("<li>" + escapeHTML(value) + "</li>")
			}
			out.WriteString("</ul>")
		}
		out.WriteString("</div>")
	}
	out.WriteString("</div>")
	writeHTMLFacts(out, "Progress metadata", progressRows(query.BoardSnapshot{Progress: []query.ProgressView{progress}}, text))
}

func writeHTMLDependencies(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "dependencies", "Dependencies", fmt.Sprintf("%d directed edge(s)", len(board.Dependencies)))
	if len(board.Dependencies) == 0 {
		writeHTMLEmpty(out, "No dependency edge recorded")
		out.WriteString("</section>")
		return
	}
	out.WriteString("<div class=\"dependency-map\">")
	for _, dependency := range board.Dependencies {
		out.WriteString("<article class=\"dependency-link\"><code title=\"" + escapeAttr(dependency.DependentTaskID) + "\">" + escapeHTML(shortID(dependency.DependentTaskID)) + "</code><span class=\"dependency-arrow\">waits for ←</span><code title=\"" + escapeAttr(dependency.BlockerTaskID) + "\">" + escapeHTML(shortID(dependency.BlockerTaskID)) + "</code>" + htmlStatusBadge(dependencyStatus(dependency)))
		writeHTMLFacts(out, "Dependency metadata", dependenciesRows(query.BoardSnapshot{Dependencies: []query.DependencyView{dependency}}, text))
		out.WriteString("</article>")
	}
	out.WriteString("</div></section>")
}

func writeHTMLInbox(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "inbox", "Inbox", fmt.Sprintf("%d message(s)", len(board.Inbox)))
	if len(board.Inbox) == 0 {
		writeHTMLEmpty(out, "Inbox is clear")
		out.WriteString("</section>")
		return
	}
	out.WriteString("<div class=\"message-stack\">")
	for _, message := range board.Inbox {
		state := presentStatus(message.Acknowledgement)
		out.WriteString("<article class=\"message\"><span class=\"message-glyph\" aria-hidden=\"true\">◆</span><div><h3>" + escapeHTML(firstNonEmpty(message.Subject, message.Type+" message")) + "</h3><p>from " + escapeHTML(shortID(message.SenderSessionID)) + " · message=" + escapeHTML(message.MessageID) + " · task=" + escapeHTML(message.RelatedTaskID) + "</p>")
		writeHTMLFacts(out, "Message metadata", inboxRows(query.BoardSnapshot{Inbox: []query.InboxItemView{message}}, text))
		out.WriteString("</div>" + htmlStatusBadge(state) + "</article>")
	}
	out.WriteString("</div></section>")
}

func writeHTMLHandoffs(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "handoffs", "Handoffs", fmt.Sprintf("%d transfer(s)", len(board.Handoffs)))
	if len(board.Handoffs) == 0 {
		writeHTMLEmpty(out, "No handoff waiting")
		out.WriteString("</section>")
		return
	}
	out.WriteString("<div class=\"handoff-flow\">")
	for _, handoff := range board.Handoffs {
		stateValue := handoff.Status
		if handoff.Decision != nil && handoff.Decision.Decision != "" {
			stateValue = handoff.Decision.Decision
		}
		out.WriteString("<article><div class=\"handoff\"><div class=\"handoff-party\"><strong>" + escapeHTML(shortID(handoff.SourceSessionID)) + "</strong><span>source · task " + escapeHTML(shortID(handoff.TaskID)) + "</span></div><span class=\"handoff-arrow\" aria-hidden=\"true\">→</span><div class=\"handoff-summary\"><strong>" + escapeHTML(firstNonEmpty(handoff.TargetSessionID, handoff.TargetTaskID, "unassigned")) + "</strong><span>" + escapeHTML(handoff.Summary) + "</span>" + htmlStatusBadge(presentStatus(stateValue)) + "</div></div>")
		writeHTMLFacts(out, "Handoff metadata", handoffRows(query.BoardSnapshot{Handoffs: []query.HandoffView{handoff}}, text))
		out.WriteString("</article>")
	}
	out.WriteString("</div></section>")
}

func writeHTMLReservations(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "reservations", "Reservations", fmt.Sprintf("%d path claim(s)", len(board.Reservations)))
	if len(board.Reservations) == 0 {
		writeHTMLEmpty(out, "No active path reservation")
		out.WriteString("</section>")
		return
	}
	out.WriteString("<div class=\"reservation-list\">")
	for _, reservation := range board.Reservations {
		primary := firstNonEmpty(reservation.Pattern, reservation.PatternFingerprint, reservation.PatternKind)
		out.WriteString("<article class=\"reservation\"><span class=\"path-mark\" aria-hidden=\"true\">⌁</span><div><code>" + escapeHTML(primary) + "</code><p>" + escapeHTML(reservation.Mode) + " · " + escapeHTML(reservation.Intent) + " · session " + escapeHTML(shortID(reservation.SessionID)) + "</p>")
		writeHTMLFacts(out, "Reservation metadata", reservationRows(query.BoardSnapshot{Reservations: []query.ReservationView{reservation}}, text))
		out.WriteString("</div>" + htmlStatusBadge(reservationStatus(reservation)) + "</article>")
	}
	out.WriteString("</div></section>")
}

func writeHTMLGit(out *strings.Builder, board query.BoardSnapshot) {
	assetCount := 0
	if board.Git != nil {
		assetCount = len(board.Git.Assets)
	}
	writeHTMLSectionStart(out, "git", "Git + warnings", fmt.Sprintf("%d asset(s) · %d warning(s)", assetCount, len(board.Warnings)))
	if assetCount == 0 && len(board.Warnings) == 0 {
		writeHTMLEmpty(out, "No Git observation or warning recorded")
		out.WriteString("</section>")
		return
	}
	out.WriteString("<div class=\"git-console\"><div class=\"git-assets\">")
	if board.Git != nil {
		for _, asset := range board.Git.Assets {
			out.WriteString("<article class=\"git-asset\"><span class=\"path-mark\" aria-hidden=\"true\">git</span><div><strong>" + escapeHTML(firstNonEmpty(asset.Branch, asset.Type, shortID(asset.Head))) + "</strong><p>head " + escapeHTML(shortID(asset.Head)) + " · ↑" + fmt.Sprint(asset.AheadDefault) + " ↓" + fmt.Sprint(asset.BehindDefault) + " · tracked_dirty=" + fmt.Sprint(asset.TrackedDirty) + " · untracked_dirty=" + fmt.Sprint(asset.UntrackedDirty) + "</p>")
			writeHTMLFacts(out, "Git asset metadata", gitRows(query.BoardSnapshot{Git: &query.GitView{Assets: []query.GitAssetView{asset}}}, text))
			out.WriteString("</div>" + htmlStatusBadge(gitAssetStatus(asset)) + "</article>")
		}
	}
	out.WriteString("</div><div class=\"warning-stack\">")
	for _, warning := range board.Warnings {
		out.WriteString("<div class=\"warning-item\">⚠ " + escapeHTML(warning) + "</div>")
	}
	if len(board.Warnings) == 0 {
		writeHTMLEmpty(out, "No advisory warning")
	}
	out.WriteString("</div></div></section>")
}

func writeHTMLActions(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "actions", "Command palette", fmt.Sprintf("%d safe action(s)", len(board.SuggestedActions)))
	if len(board.SuggestedActions) == 0 {
		writeHTMLEmpty(out, "No suggested action for this scope")
		out.WriteString("</section>")
		return
	}
	out.WriteString("<div class=\"command-palette\">")
	for _, action := range board.SuggestedActions {
		out.WriteString("<div class=\"command\"><span class=\"command-prompt\" aria-hidden=\"true\">❯</span><code>" + escapeHTML(action.Command) + "</code><span>" + escapeHTML(action.Code) + "</span></div>")
	}
	out.WriteString("</div></section>")
}

func writeHTMLSnapshot(out *strings.Builder, board query.BoardSnapshot) {
	writeHTMLSectionStart(out, "snapshot", "Snapshot", "canonical metadata and redaction state")
	out.WriteString("<dl class=\"snapshot-grid\">")
	fields := []struct{ Name, Value string }{
		{"Project", primaryProject(board)}, {"Workspace", board.Scope.WorkspaceID}, {"Mode", string(board.Scope.Mode)}, {"Selector", board.Scope.Selector}, {"Generated", stamp(board.GeneratedAt)}, {"Snapshot cursor", board.SnapshotCursor}, {"Schema version", fmt.Sprint(board.SchemaVersion)}, {"View version", fmt.Sprint(board.ViewVersion)}, {"Redaction policy", board.Redaction.PolicyName}, {"Redaction policy version", fmt.Sprint(board.Redaction.PolicyVersion)}, {"Content omitted", fmt.Sprint(board.Redaction.ContentOmitted)}, {"Content redacted", fmt.Sprint(board.Redaction.ContentRedacted)},
	}
	for _, field := range fields {
		if field.Value == "" {
			continue
		}
		out.WriteString("<div class=\"snapshot-item\"><dt>" + field.Name + "</dt><dd>" + escapeHTML(field.Value) + "</dd></div>")
	}
	out.WriteString("</dl></section>")
}

func writeHTMLFacts(out *strings.Builder, summary string, rows []string) {
	if len(rows) == 0 {
		return
	}
	out.WriteString("<details class=\"meta-disclosure\"><summary>" + escapeHTML(summary) + "</summary><div class=\"fact-list\">")
	for _, row := range rows {
		out.WriteString("<code>" + escapeHTML(row) + "</code>")
	}
	out.WriteString("</div></details>")
}

func htmlStatusBadge(status statusPresentation) string {
	return "<span class=\"status-badge status-" + string(status.Semantic) + "\"><span aria-hidden=\"true\">" + status.Glyph + "</span>" + escapeHTML(status.Label) + "</span>"
}

func escapeHTML(value string) string {
	return html.EscapeString(text(value))
}

func escapeAttr(value string) string {
	return html.EscapeString(text(value))
}
