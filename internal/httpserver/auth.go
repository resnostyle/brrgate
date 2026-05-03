package httpserver

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const maxIncomingRequestID = 96

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func sanitizedRequestID(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || len(candidate) > maxIncomingRequestID {
		return ""
	}
	for _, r := range candidate {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return ""
		}
	}
	return candidate
}

func secretEquals(expected, supplied string) bool {
	a := strings.TrimSpace(expected)
	b := strings.TrimSpace(supplied)
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return len(b) == 0
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func webhookSecretFromRequest(r *http.Request, expected string) (ok bool, reason string) {
	fromBearer := bearerToken(r.Header.Get("Authorization"))
	fromHeader := strings.TrimSpace(r.Header.Get("X-Webhook-Secret"))

	guess := fromBearer
	if guess == "" {
		guess = fromHeader
	}
	if guess == "" {
		return false, "missing_header"
	}
	if !secretEquals(expected, guess) {
		return false, "bad_token"
	}
	return true, ""
}
