package security

import "regexp"

const sensitiveKeyAlternation = `api[_-]?key|token|secret|password|bootstrap|cookie`

var sensitiveKeyPattern = regexp.MustCompile(`(?i)` + sensitiveKeyAlternation)

type secretRule struct {
	pattern     *regexp.Regexp
	replacement string
}

var secretRules = []secretRule{
	{regexp.MustCompile(`(?i)(cookie["']?\s*:\s*)[^\r\n]+`), "$1[REDACTED]"},
	{regexp.MustCompile(`(?i)(` + sensitiveKeyAlternation + `)["']?\s*[:=]\s*["']?[^\s,"'&#]+`), "$1=[REDACTED]"},
	{regexp.MustCompile(`\b(sk-ant-[A-Za-z0-9_-]{16,}|sk-[A-Za-z0-9_-]{20,})\b`), "[REDACTED]"},
	{regexp.MustCompile(`\b(nvapi-[A-Za-z0-9_-]{20,})\b`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)authorization["']?\s*:\s*["']?(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)([?&#](?:api[_-]?key|key|token|access_token|bootstrap|session)=)[^&\s"'#]+`), "$1[REDACTED]"},
}

func Redact(input string) string {
	out := input
	for _, rule := range secretRules {
		out = rule.pattern.ReplaceAllString(out, rule.replacement)
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
