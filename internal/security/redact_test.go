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
