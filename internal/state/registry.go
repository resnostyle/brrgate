package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"brrgate/internal/hub"
	"brrgate/internal/patterns"
)

// PendingRelease tracks an in-flight debug decision.
type PendingRelease struct {
	ID         string
	CreatedAt  time.Time
	RequestID  string
	Haystack   string
	Missing    bool // match field absent in JSON payload
	MissingKey string
	Outcome    patterns.MatchOutcome
	Waiter     *hub.Waiter
	RawPretty  string // JSON indented for dashboard
}

// Registry stores active debug sessions keyed by opaque decision IDs.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]*PendingRelease
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*PendingRelease)}
}

// Add registers a pending entry.
func (r *Registry) Add(p *PendingRelease) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[p.ID] = p
}

// Remove drops a pending entry after resolution.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
}

// Get returns pending metadata if still queued.
func (r *Registry) Get(id string) (*PendingRelease, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	return p, ok
}

// Snapshot shows dashboard-visible fields.
type Snapshot struct {
	ID              string     `json:"id"`
	RequestID       string     `json:"request_id"`
	CreatedAt       time.Time  `json:"created_at"`
	Haystack        string     `json:"haystack"`
	MissingField    bool       `json:"missing_field"`
	MissingFieldKey string     `json:"missing_key,omitempty"`
	RawPretty       string     `json:"payload"`
	PatternMatched  bool       `json:"pattern_matched_any"`
	AcceptedRule    string     `json:"pattern_rule,omitempty"`
	EvalRows        [][]string `json:"-"`
}

// List clones current pendings for dashboards.
func (r *Registry) List() []Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Snapshot, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, SnapshotFromPending(p))
	}
	return out
}

// SnapshotFromPending builds dashboard structs.
func SnapshotFromPending(p *PendingRelease) Snapshot {
	snap := Snapshot{
		ID:              p.ID,
		RequestID:       p.RequestID,
		CreatedAt:       p.CreatedAt,
		Haystack:        p.Haystack,
		MissingField:    p.Missing,
		MissingFieldKey: p.MissingKey,
		RawPretty:       p.RawPretty,
		PatternMatched:  p.Outcome.Accepted,
		EvalRows:        evalRows(p.Outcome.Evals),
	}
	if p.Outcome.Accepted {
		snap.AcceptedRule = p.Outcome.RuleName
	}
	return snap
}

func evalRows(rows []patterns.EvalResult) [][]string {
	var out [][]string
	for _, e := range rows {
		pass := "no"
		if e.Matched {
			pass = "yes"
		}
		flags := "-"
		if e.IgnoreCase {
			flags = "ic"
		}
		err := strings.TrimSpace(e.ErrMsg)

		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = "(unnamed)"
		}
		out = append(out, []string{
			string(e.Kind),
			name,
			e.Pattern,
			flags,
			pass,
			err,
		})
	}
	return out
}

// PayloadPretty marshals decoded JSON indented (size clamped).
func PayloadPretty(v any) ([]byte, error) {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard payload: %w", err)
	}
	return clamp(bytes, maxPayloadBytes), nil
}

const maxPayloadBytes = 1 << 15 // 32 KiB trim cap

func clamp(bytes []byte, limit int) []byte {
	if len(bytes) <= limit {
		return bytes
	}
	head := append([]byte(nil), bytes[:limit]...)
	return append(head, []byte("\n…trimmed…")...)
}
