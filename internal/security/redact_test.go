package security

import (
	"strings"
	"testing"
)

func TestRedactKnownSecrets(t *testing.T) {
	input := "api_key=abc123 token: xyz987 Authorization: Bearer very-secret sk-ant-abcdefghijklmnopqrstuvwxyz nvapi-abcdefghijklmnopqrstuvwxyz_123456"
	got := Redact(input)
	for _, secret := range []string{"abc123", "xyz987", "very-secret", "sk-ant-abcdefghijklmnopqrstuvwxyz", "nvapi-abcdefghijklmnopqrstuvwxyz_123456"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret remains: %s", got)
		}
	}
}

func TestRedactCategories(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		secret  string
		keepers []string
	}{
		{"openrouter_key", "using key sk-or-v1-abcdefghijklmnopqrstuvwxyz1234567890 to call the API", "sk-or-v1-abcdefghijklmnopqrstuvwxyz1234567890", []string{"using key", "to call the API"}},
		{"nvidia_key", "NVIDIA_API_KEY=nvapi-abcdefghijklmnopqrstuvwxyz_123456 loaded", "nvapi-abcdefghijklmnopqrstuvwxyz_123456", []string{"loaded"}},
		{"authorization_header", `Authorization: Bearer sekret-token-value-0000`, "sekret-token-value-0000", nil},
		{"authorization_header_json", `{"Authorization": "Bearer sekret-token-value-0000"}`, "sekret-token-value-0000", nil},
		{"cookie_header", "Cookie: sessionid=abcdef1234567890; other=1", "abcdef1234567890", nil},
		{"bootstrap_query_param", "GET /#bootstrap=q1w2e3r4t5y6u7i8o9p0asdfghjklzxcvbnmQW1 200", "q1w2e3r4t5y6u7i8o9p0asdfghjklzxcvbnmQW1", []string{"GET /#bootstrap=", "200"}},
		{"api_key_query_param", "request GET /x?api_key=abcSECRET123&foo=bar succeeded", "abcSECRET123", []string{"foo=bar", "succeeded"}},
		{"session_query_param", "?session=abcSECRET1234567890abc&other=1", "abcSECRET1234567890abc", []string{"other=1"}},
		{"generic_token_field", `token: "xyz987longenoughvalue1234"`, "xyz987longenoughvalue1234", nil},
		{"password_field", "password=hunter2value", "hunter2value", nil},
		{"private_key_block", "-----BEGIN PRIVATE KEY-----\nMIIBogIBAAsecretmaterial\n-----END PRIVATE KEY-----", "MIIBogIBAAsecretmaterial", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Redact(c.input)
			if strings.Contains(got, c.secret) {
				t.Fatalf("secret %q remains in output: %q", c.secret, got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("expected placeholder [REDACTED] in output: %q", got)
			}
			for _, keep := range c.keepers {
				if !strings.Contains(got, keep) {
					t.Fatalf("expected non-secret context %q preserved, got: %q", keep, got)
				}
			}
		})
	}
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{"api_key", "apiKey", "access_token", "auth_secret", "password", "githubToken"}
	for _, key := range sensitive {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", key)
		}
	}
	notSensitive := []string{"message", "text", "status"}
	for _, key := range notSensitive {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", key)
		}
	}
}
