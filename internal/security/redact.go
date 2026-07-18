package security

import "regexp"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)\s*[:=]\s*(["']?)[^\s,"']+`),
	regexp.MustCompile(`\b(sk-ant-[A-Za-z0-9_-]{16,}|sk-[A-Za-z0-9_-]{20,})\b`),
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
