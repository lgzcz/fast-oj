// Package totp is a small RFC 6238 implementation: 30-second steps, six digits,
// HMAC-SHA1, which is what every mainstream authenticator app speaks.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	Step   = 30 * time.Second
	Digits = 6
	// Skew accepts one step before and after "now": phone clocks drift, and a
	// person typing a code at the boundary should not be locked out.
	Skew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func GenerateSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return b32.EncodeToString(raw), nil
}

func normalizeSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	return b32.DecodeString(s)
}

func Code(secret string, at time.Time) (string, error) {
	key, err := normalizeSecret(secret)
	if err != nil {
		return "", fmt.Errorf("bad totp secret: %w", err)
	}
	return codeAt(key, at.Unix()/int64(Step/time.Second)), nil
}

func codeAt(key []byte, counter int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}

// Validate accepts the current step and ±Skew steps. The comparison is constant
// time: a six-digit code is small enough that timing a digit-by-digit compare
// would matter.
func Validate(secret, code string, at time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	key, err := normalizeSecret(secret)
	if err != nil {
		return false
	}
	base := at.Unix() / int64(Step/time.Second)
	for delta := int64(-Skew); delta <= Skew; delta++ {
		if hmac.Equal([]byte(codeAt(key, base+delta)), []byte(code)) {
			return true
		}
	}
	return false
}

// URL is the otpauth:// URI an authenticator app understands.
func URL(issuer, account, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("period", "30")
	q.Set("digits", "6")
	return "otpauth://totp/" + url.PathEscape(account) + "?" + q.Encode()
}
