package httpserver

import (
	"net/http"
	"strings"
	"time"

	"brrgate/internal/logx"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// WithAccessLog wraps an http.Handler and emits one structured line per HTTP request.
// serverName identifies which listener produced the line (for example "webhook" or "debug").
func WithAccessLog(logger *logx.Logger, serverName string, next http.Handler) http.Handler {
	name := strings.TrimSpace(serverName)
	if name == "" {
		name = "http"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := newStatusRecorder(w)

		next.ServeHTTP(rec, r)

		fields := []any{
			"event", "http_access",
			"server", name,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", int(time.Since(start).Milliseconds()),
			"remote", strings.TrimSpace(r.RemoteAddr),
		}

		if rq := r.URL.RawQuery; rq != "" {
			fields = append(fields, "query", rq)
		}
		if ua := r.UserAgent(); ua != "" {
			fields = append(fields, "user_agent", ua)
		}
		if reqID := sanitizedRequestID(r.Header.Get("X-Request-ID")); reqID != "" {
			fields = append(fields, "request_id_in", reqID)
		}

		logger.Info("request", fields...)
	})
}
