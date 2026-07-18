package security

import (
	"strings"
	"testing"
)

func TestRedactKnownSecrets(t *testing.T) {
	input := "api_key=abc123 token: xyz987 Authorization: Bearer very-secret sk-ant-abcdefghijklmnopqrstuvwxyz"
	got := Redact(input)
	for _, secret := range []string{"abc123", "xyz987", "very-secret", "sk-ant-abcdefghijklmnopqrstuvwxyz"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret remains: %s", got)
		}
	}
}
