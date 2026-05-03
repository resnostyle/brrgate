package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"brrgate/internal/hub"
	"brrgate/internal/logx"
	"brrgate/internal/patterns"
	"brrgate/internal/state"
)

const maxWebhookPayload = 1 << 17 // 128 KiB
const maxLoggedBody = 1 << 13     // 8 KiB

// Webhook serves the synchronous autobrr decision endpoint plus health checks.
type Webhook struct {
	Logger       *logx.Logger
	Secret       string
	Matcher      patterns.Matcher
	MatchField   string
	Debug        bool
	DebugTimeout time.Duration
	Registry     *state.Registry
}

// RegisterRoutes wires HTTP handlers onto mux (Go 1.18 compatible).
func (s *Webhook) RegisterRoutes(mx *http.ServeMux) {
	mx.HandleFunc("/decide", enforceMethod(http.MethodPost, s.handleDecide))
	mx.HandleFunc("/healthz", enforceMethod(http.MethodGet, healthzHandler))
}

func enforceMethod(method string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		fn(w, r)
	}
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Webhook) handleDecide(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	reqID := sanitizedRequestID(r.Header.Get("X-Request-ID"))
	if reqID == "" {
		reqID = generateRequestID()
	}
	w.Header().Set("X-Request-ID", reqID)

	s.Logger.Info("event", "event", "webhook_in", "request_id", reqID, "remote", strings.TrimSpace(r.RemoteAddr))

	if ok, reason := webhookSecretFromRequest(r, s.Secret); !ok {
		s.Logger.Warn("event", "event", "auth_fail", "request_id", reqID, "reason", reason)
		writeJSONMessage(w, http.StatusUnauthorized, "unauthorized")
		s.logOutcome(reqID, start, http.StatusUnauthorized, "normal", false, "")
		return
	}

	payload, rawBody, ok := readAndParseBody(s.Logger, r, reqID)
	if !ok {
		writeJSONMessage(w, http.StatusBadRequest, "invalid_json")
		s.logOutcome(reqID, start, http.StatusBadRequest, "normal", false, "invalid_json")
		return
	}

	haystack, fieldOK := extractHaystack(payload, s.MatchField)
	if fieldOK {
		if s.Logger.Allows(logx.LevelDebug) {
			s.Logger.Debug("event", "event", "match_eval", "request_id", reqID, "haystack", haystack)
		} else {
			s.Logger.Info("event", "event", "match_eval", "request_id", reqID, "haystack_len", len(haystack))
		}
	} else {
		s.Logger.Warn("event", "event", "match_eval", "request_id", reqID, "match_field", s.MatchField, "reason", "missing_match_field")
	}

	outcome := s.Matcher.Evaluate(haystack)
	if s.Logger.Allows(logx.LevelDebug) {
		for i, ev := range outcome.Evals {
			s.Logger.Debug("event", "event", "match_rule_detail", "request_id", reqID, "index", i, "name", ev.Name, "kind", string(ev.Kind), "pattern", ev.Pattern, "ignore_case", ev.IgnoreCase, "matched", ev.Matched, "err", ev.ErrMsg)
		}
	}

	matchReason := outcome.RuleName
	if matchReason == "" {
		matchReason = "none"
	}
	ruleIdx := outcome.RuleIndex
	if !outcome.Accepted {
		ruleIdx = -1
	}
	s.Logger.Info("event", "event", "match_result", "request_id", reqID, "accepted", outcome.Accepted, "rules", outcome.RulesCount, "rule", matchReason, "rule_index", ruleIdx)

	humanHaystack := haystack
	if !fieldOK {
		humanHaystack = "<missing field>"
	}
	s.Logger.Info("webhook_summary",
		"request_id", reqID,
		"msg", fmt.Sprintf("field=%s haystack=%q pattern_auto_accept=%t", s.MatchField, humanHaystack, outcome.Accepted))

	if !s.Debug {
		status := http.StatusForbidden
		if outcome.Accepted {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		s.logOutcome(reqID, start, status, "normal", outcome.Accepted, "")
		return
	}

	decisionHex, err := randomHexToken(10)
	if err != nil {
		s.Logger.Error("decision token", "error", err)
		writeJSONMessage(w, http.StatusInternalServerError, "entropy_error")
		s.logOutcome(reqID, start, http.StatusInternalServerError, "debug", false, "entropy")
		return
	}

	builder, errPretty := buildPrettyPayload(payload, rawBody)
	if errPretty != nil {
		s.Logger.Warn("payload pretty", "request_id", reqID, "error", errPretty)
	}

	waiter := hub.NewWaiter()
	pending := &state.PendingRelease{
		ID:         decisionHex,
		CreatedAt:  time.Now().UTC(),
		RequestID:  reqID,
		Haystack:   haystack,
		Missing:    !fieldOK,
		MissingKey: s.MatchField,
		Outcome:    outcome,
		Waiter:     waiter,
		RawPretty:  builder,
	}
	s.Registry.Add(pending)
	defer s.Registry.Remove(decisionHex)

	s.Logger.Info("event", "event", "debug_pending", "request_id", reqID, "decision_id", decisionHex, "timeout", s.DebugTimeout.String())

	ctxWait, cancel := context.WithTimeout(r.Context(), s.DebugTimeout)
	defer cancel()

	manual := waiter.Wait(ctxWait)

	mode := "debug"
	switch {
	case errors.Is(ctxWait.Err(), context.DeadlineExceeded):
		mode = "debug_timeout"
	case ctxWait.Err() != nil:
		mode = "debug_cancel"
	}

	status := http.StatusForbidden
	if manual {
		status = http.StatusOK
	}
	w.WriteHeader(status)

	s.Logger.Info("event", "event", "debug_resolved", "request_id", reqID, "decision_id", decisionHex, "manual_accept", manual, "mode", mode)

	s.logOutcome(reqID, start, status, mode, manual, "")
}

func (s *Webhook) logOutcome(reqID string, start time.Time, status int, mode string, accepted bool, detail string) {
	if detail == "" {
		s.Logger.Info("event", "event", "response", "request_id", reqID, "status", status, "duration_ms", int(time.Since(start).Milliseconds()), "mode", mode, "accepted", accepted)
		return
	}
	s.Logger.Info("event", "event", "response", "request_id", reqID, "status", status, "duration_ms", int(time.Since(start).Milliseconds()), "mode", mode, "accepted", accepted, "detail", detail)
}

func readAndParseBody(logger *logx.Logger, r *http.Request, reqID string) (map[string]any, []byte, bool) {
	defer r.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookPayload+1))
	if err != nil {
		logger.Warn("read body", "request_id", reqID, "error", err)
		return nil, nil, false
	}
	if len(raw) > maxWebhookPayload {
		logger.Warn("payload too large", "request_id", reqID, "bytes", len(raw))
		return nil, nil, false
	}

	if logger.Allows(logx.LevelDebug) {
		chunk := raw
		truncated := 0
		if len(chunk) > maxLoggedBody {
			truncated = len(chunk) - maxLoggedBody
			chunk = chunk[:maxLoggedBody]
		}
		logger.Debug("event", "event", "request_body", "request_id", reqID, "body", string(chunk), "truncated_bytes", truncated)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.Warn("json decode", "request_id", reqID, "error", err)
		return nil, raw, false
	}
	return payload, raw, true
}

func extractHaystack(body map[string]any, key string) (string, bool) {
	if body == nil {
		return "", false
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return "", false
	}
	val, ok := body[k]
	if !ok {
		return "", false
	}
	switch typed := val.(type) {
	case string:
		return strings.TrimSpace(typed), true
	default:
		return strings.TrimSpace(fmt.Sprint(typed)), true
	}
}

func buildPrettyPayload(payload map[string]any, raw []byte) (string, error) {
	if payload == nil {
		return string(raw), nil
	}
	out, err := state.PayloadPretty(payload)
	if err != nil {
		return string(raw), err
	}
	return string(out), nil
}

func writeJSONMessage(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func generateRequestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func randomHexToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
