package output

import (
	"net/url"
	"regexp"
	"strings"
)

// Redact scrubs secrets from a string before it is logged in --verbose mode.
// It removes Authorization header values, token/code fields, and the query
// strings of presigned/storage URLs (anything with a signature-style param).
func Redact(s string) string {
	s = redactHeaders(s)
	s = redactJSONFields(s)
	s = redactURLs(s)
	return s
}

var (
	reAuthHeader = regexp.MustCompile(`(?i)(authorization:\s*)(bearer\s+)?\S+`)
	// token/code/access_token/verifier/challenge in JSON or query form.
	reSecretField = regexp.MustCompile(`(?i)("?\b(?:token|access_token|refresh_token|code|verifier|challenge|secret|password)\b"?\s*[:=]\s*"?)([^"&\s,}]+)`)
	reURL         = regexp.MustCompile(`https?://[^\s"'<>]+`)
)

func redactHeaders(s string) string {
	return reAuthHeader.ReplaceAllString(s, "${1}<redacted>")
}

func redactJSONFields(s string) string {
	return reSecretField.ReplaceAllString(s, "${1}<redacted>")
}

// redactURLs replaces the query string of any URL that looks presigned (carries
// a signature-style parameter) with "<redacted>".
func redactURLs(s string) string {
	return reURL.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.RawQuery == "" {
			return raw
		}
		if isPresignedQuery(u.Query()) {
			return u.Scheme + "://" + u.Host + u.Path + "?<redacted>"
		}
		return raw
	})
}

func isPresignedQuery(q url.Values) bool {
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") || strings.HasPrefix(lk, "x-goog-") ||
			lk == "signature" || lk == "sig" || lk == "token" || strings.Contains(lk, "credential") {
			return true
		}
	}
	return false
}

// RedactURL renders a URL safe for logging regardless of params (host+path only).
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<url>"
	}
	if u.RawQuery == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host + u.Path + "?<redacted>"
}
