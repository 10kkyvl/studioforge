package security

import "regexp"

const sensitiveKeyAlternation = `api[_-]?key|token|secret|password`

var sensitiveKeyPattern = regexp.MustCompile(`(?i)` + sensitiveKeyAlternation)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(` + sensitiveKeyAlternation + `)\s*[:=]\s*(["']?)[^\s,"']+`),
	regexp.MustCompile(`\b(sk-ant-[A-Za-z0-9_-]{16,}|sk-[A-Za-z0-9_-]{20,})\b`),
	regexp.MustCompile(`\b(nvapi-[A-Za-z0-9_-]{20,})\b`),
	regexp.MustCompile(`(?i)authorization:\s*(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
}

func Redact(input string) string {
	out := input
	out = secretPatterns[0].ReplaceAllString(out, "$1=[REDACTED]")
	for _, pattern := range secretPatterns[1:] {
		out = pattern.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

// IsSensitiveKey reports whether a JSON object key's name looks like it holds
// a secret (api_key, token, secret, password, and compound forms such as
// access_token), so a decoded-tree redaction pass can catch a secret whose
// value alone carries no recognizable shape.
func IsSensitiveKey(key string) bool {
	return sensitiveKeyPattern.MatchString(key)
}
