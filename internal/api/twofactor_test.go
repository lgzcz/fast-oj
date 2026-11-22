package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
	"github.com/liangbinchen2013/fast-oj/internal/totp"
)

var recoveryCodeRE = regexp.MustCompile(`[A-Z2-9]{4}-[A-Z2-9]{4}`)

func totpSecretOf(t *testing.T, h *harness, handle string) string {
	t.Helper()
	var secret string
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT secret FROM user_totp WHERE user_id=(SELECT id FROM users WHERE handle=?)`, handle).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestTwoFactorSetupAndChallenge(t *testing.T) {
	h := newHarness(t)
	user := h.signIn("secure")

	if code, _ := h.getAs(user, "/settings/security"); code != http.StatusOK {
		t.Fatalf("security page: %d", code)
	}
	if code, body := h.postAs(user, "/settings/2fa/setup", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "密钥") || !strings.Contains(body, "启用两步验证") {
		t.Fatalf("setup: %d %s", code, body)
	}
	secret := totpSecretOf(t, h, "secure")
	code, body := h.postAs(user, "/settings/2fa/enable", url.Values{
		"code": {mustTOTP(t, secret)},
	})
	if code != http.StatusOK || !strings.Contains(body, "恢复码（只显示这一次）") {
		t.Fatalf("enable: %d %s", code, body)
	}
	codes := recoveryCodeRE.FindAllString(body, -1)
	if len(codes) != recoveryCodeCount {
		t.Fatalf("recovery codes = %d, want %d", len(codes), recoveryCodeCount)
	}

	// 密码通过之后不应直接给会话，而是给一张挑战票。
	u, err := h.db.GetUserByHandle(ctx(), "secure")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(""))
	h.app.afterPassword(rec, req, u, "/settings")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/2fa" {
		t.Fatalf("after password: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	set := rec.Header().Values("Set-Cookie")
	if cookieValue(set, twoFactorCookie) == "" {
		t.Fatalf("no challenge cookie in %q", set)
	}

	// 六位码正确才建会话。
	ch, err := h.db.CreateLoginChallenge(ctx(), u.ID, "/settings")
	if err != nil {
		t.Fatal(err)
	}
	code, page, hdr := h.postForm("/login/2fa", url.Values{"code": {mustTOTP(t, secret)}},
		twoFactorCookie+"="+ch.ID, "")
	if code != http.StatusSeeOther || hdr.Get("Location") != "/settings" {
		t.Fatalf("2fa login: %d %q %s", code, hdr.Get("Location"), page)
	}
	if cookieValue(hdr.Values("Set-Cookie"), "fastoj_session") == "" {
		t.Fatalf("2fa login did not mint a session: %q", hdr.Values("Set-Cookie"))
	}
	if _, err := h.db.GetLoginChallenge(ctx(), ch.ID); err == nil {
		t.Fatal("challenge should be single use")
	}

	// 恢复码同样能登录，而且只能用一次。
	ch, _ = h.db.CreateLoginChallenge(ctx(), u.ID, "/")
	code, _, hdr = h.postForm("/login/2fa", url.Values{"code": {codes[0]}},
		twoFactorCookie+"="+ch.ID, "")
	if code != http.StatusSeeOther || hdr.Get("Location") != "/" {
		t.Fatalf("recovery login: %d %q", code, hdr.Get("Location"))
	}
	ch, _ = h.db.CreateLoginChallenge(ctx(), u.ID, "/")
	if code, _, hdr = h.postForm("/login/2fa", url.Values{"code": {codes[0]}},
		twoFactorCookie+"="+ch.ID, ""); code != http.StatusSeeOther ||
		!strings.HasPrefix(hdr.Get("Location"), "/login/2fa?msg=") {
		t.Fatalf("reused recovery should fail, got %d %q", code, hdr.Get("Location"))
	}

	// 连错 5 次挑战作废。
	ch, _ = h.db.CreateLoginChallenge(ctx(), u.ID, "/")
	for i := 0; i < twoFactorMaxAttempts; i++ {
		h.postForm("/login/2fa", url.Values{"code": {"000000"}},
			twoFactorCookie+"="+ch.ID, "")
	}
	if _, err := h.db.GetLoginChallenge(ctx(), ch.ID); err == nil {
		t.Fatal("challenge should be burned after too many attempts")
	}

	// 关闭要密码。
	if code, body := h.postAs(user, "/settings/2fa/disable", url.Values{"password": {"wrong"}}); code != http.StatusOK ||
		!strings.Contains(body, "当前密码不正确") {
		t.Fatalf("wrong password disable: %d %s", code, body)
	}
	if code, body := h.postAs(user, "/settings/2fa/disable", url.Values{"password": {testPassword}}); code != http.StatusOK ||
		!strings.Contains(body, "两步验证已关闭") {
		t.Fatalf("disable: %d %s", code, body)
	}
	if _, err := h.db.GetTOTP(ctx(), u.ID); err != store.ErrTOTPNotSet {
		t.Fatalf("totp row should be gone, got %v", err)
	}
}

func TestLoginDeviceListAndRevoke(t *testing.T) {
	h := newHarness(t)
	user := h.signIn("devices")
	u, err := h.db.GetUserByHandle(ctx(), "devices")
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.db.CreateSession(ctx(), u.ID, "203.0.113.9", "OtherBrowser/1.0", store.SessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	code, body := h.getAs(user, "/settings/security")
	if code != http.StatusOK || !strings.Contains(body, "OtherBrowser/1.0") ||
		!strings.Contains(body, "203.0.113.9") || !strings.Contains(body, "当前") {
		t.Fatalf("device list: %d %s", code, body)
	}
	if code, body := h.postAs(user, "/settings/sessions/"+other.ID+"/revoke", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "这台设备已退出") {
		t.Fatalf("revoke: %d %s", code, body)
	}
	if _, _, err := h.db.SessionUser(ctx(), other.ID); err == nil {
		t.Fatal("revoked session should be gone")
	}
	// 其他设备一键全退，当前设备还活着。
	again, _ := h.db.CreateSession(ctx(), u.ID, "203.0.113.10", "Third/1.0", store.SessionTTL)
	if code, _ := h.postAs(user, "/settings/sessions/revoke-others", url.Values{}); code != http.StatusOK {
		t.Fatalf("revoke others: %d", code)
	}
	if _, _, err := h.db.SessionUser(ctx(), again.ID); err == nil {
		t.Fatal("other session should be gone")
	}
	if _, body := h.getAs(user, "/settings"); !strings.Contains(body, "设置") {
		t.Fatal("current session should survive")
	}
}

func mustTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}
