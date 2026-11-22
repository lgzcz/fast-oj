package totp

import (
	"strings"
	"testing"
	"time"
)

func TestRFC6238Vector(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // "12345678901234567890"
	got, err := Code(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != "287082" {
		t.Fatalf("code = %s, want 287082", got)
	}
	if !Validate(secret, "287082", time.Unix(59, 0)) {
		t.Fatal("the RFC vector should validate")
	}
	if !Validate(secret, "287082", time.Unix(59+30, 0)) {
		t.Fatal("one step of skew should be accepted")
	}
	if Validate(secret, "287082", time.Unix(59+150, 0)) {
		t.Fatal("five steps away must not validate")
	}
	if Validate(secret, "abcdef", time.Unix(59, 0)) {
		t.Fatal("non-numeric codes must not validate")
	}
}

func TestSecretAndURL(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < 20 || strings.ContainsAny(secret, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("unexpected secret %q", secret)
	}
	u := URL("Fast OJ", "alice@fast", secret)
	for _, want := range []string{"otpauth://totp/alice@fast?", "secret=" + secret, "issuer=Fast+OJ"} {
		if !strings.Contains(u, want) {
			t.Fatalf("url %q missing %q", u, want)
		}
	}
}
