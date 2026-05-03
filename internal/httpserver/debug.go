package httpserver

import (
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"brrgate/internal/state"
)

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>brrgate debug</title>
	<style>
		body { font-family: system-ui, sans-serif; margin: 0; background: #0b0d12; color: #e6e9ef; }
		header { padding: 1rem 1.5rem; border-bottom: 1px solid #1f2430; }
		main { padding: 1.5rem; max-width: 1100px; margin: 0 auto; }
		.card { border: 1px solid #1f2430; border-radius: 10px; padding: 1rem 1.25rem; margin-bottom: 1rem; background: #11141c; }
		.muted { color: #8b93a7; font-size: 0.9rem; }
		table { width: 100%; border-collapse: collapse; margin-top: 0.75rem; font-size: 0.9rem; }
		th, td { border-bottom: 1px solid #1f2430; padding: 0.35rem 0.5rem; text-align: left; vertical-align: top; }
		pre { background: #0d1017; padding: 0.75rem; border-radius: 8px; overflow: auto; max-height: 320px; }
		button { cursor: pointer; border: 0; border-radius: 6px; padding: 0.45rem 0.9rem; font-weight: 600; }
		.approve { background: #2ea043; color: #041109; margin-right: 0.5rem; }
		.deny { background: #f85149; color: #1b0504; }
		.pill { display: inline-block; padding: 0.1rem 0.45rem; border-radius: 999px; font-size: 0.75rem; border: 1px solid #2b3242; }
	</style>
</head>
<body>
	<header>
		<h1 style="margin:0;font-size:1.4rem;">brrgate · manual approvals</h1>
		<p class="muted" style="margin:0.35rem 0 0;">Updated {{ .Generated }} · bind this UI to loopback in production</p>
	</header>
	<main>
		{{ if .Items }}
			{{ range .Items }}
		<div class="card">
			<div style="display:flex;justify-content:space-between;gap:1rem;flex-wrap:wrap;">
				<div>
					<div><span class="pill">decision</span> <code>{{ .ID }}</code></div>
					<div class="muted">request_id {{ .RequestID }} · {{ .CreatedAt.Format "2006-01-02 15:04:05 UTC" }}</div>
				</div>
				<div>
					<form method="post" action="/decision/{{ .ID }}/approve" style="display:inline;">
						<button class="approve" type="submit">Approve (200)</button>
					</form>
					<form method="post" action="/decision/{{ .ID }}/deny" style="display:inline;">
						<button class="deny" type="submit">Deny (403)</button>
					</form>
				</div>
			</div>
			<p><strong>Haystack</strong> (field match): <code>{{ .Haystack }}</code>
				{{ if .MissingField }}
					<span class="pill" style="border-color:#f85149;color:#f85149;">missing {{ .MissingFieldKey }}</span>
				{{ end }}
			</p>
			<p class="muted">Pattern engine alone would {{ if .PatternMatched }}<strong style="color:#56d364;">accept</strong> via <code>{{ .AcceptedRule }}</code>{{ else }}<strong style="color:#f85149;">reject</strong> (no rule matched){{ end }} — debug mode still waits for you.</p>
			{{ if .EvalRows }}
			<table>
				<thead>
					<tr>
						<th>Kind</th>
						<th>Name</th>
						<th>Pattern</th>
						<th>Flags</th>
						<th>Hit</th>
						<th>Error</th>
					</tr>
				</thead>
				<tbody>
					{{ range .EvalRows }}
					<tr>
						{{ range . }}
						<td>{{ . }}</td>
						{{ end }}
					</tr>
					{{ end }}
				</tbody>
			</table>
			{{ end }}
			<details style="margin-top:0.75rem;">
				<summary class="muted">Payload</summary>
				<pre>{{ .RawPretty }}</pre>
			</details>
		</div>
			{{ end }}
		{{ else }}
			<p class="muted">No pending releases. When autobrr posts with debug mode enabled, decisions appear here.</p>
		{{ end }}
	</main>
</body>
</html>`

var dashboard = template.Must(template.New("dashboard").Parse(dashboardHTML))

type dashboardModel struct {
	Generated time.Time
	Items     []state.Snapshot
}

type debugServer struct {
	reg *state.Registry
}

// NewDebugHandler serves the blocking manual approval UI (bind to loopback).
func NewDebugHandler(reg *state.Registry) http.Handler {
	return &debugServer{reg: reg}
}

func (d *debugServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/" && r.Method == http.MethodGet:
		d.renderHome(w, r)
	case strings.HasPrefix(path, "/decision/") && r.Method == http.MethodPost:
		d.handleDecision(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (d *debugServer) renderHome(w http.ResponseWriter, r *http.Request) {
	items := d.reg.List()
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = dashboard.Execute(w, dashboardModel{
		Generated: time.Now().UTC().Round(time.Second),
		Items:     items,
	})
}

func (d *debugServer) handleDecision(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 || parts[0] != "decision" {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSpace(parts[1])
	action := strings.TrimSpace(strings.ToLower(parts[2]))
	if id == "" || (action != "approve" && action != "deny") {
		http.NotFound(w, r)
		return
	}
	pending, ok := d.reg.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pending.Waiter.Resolve(action == "approve")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
