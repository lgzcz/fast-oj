package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

func TestPasswordResetWebFlow(t *testing.T) {
	h := newHarness(t)
	cookie, csrf := h.login("resettarget", "reset@example.com", testPassword)
	u, err := h.db.GetUserByEmail(ctx(), "reset@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token, err := h.db.CreatePasswordReset(ctx(), u.ID, store.PasswordResetTTL)
	if err != nil {
		t.Fatal(err)
	}

	code, page := h.htmlWithCookie(http.MethodGet, "/reset?token="+url.QueryEscape(token), cookie)
	if code != http.StatusOK || !strings.Contains(page, "重置 resettarget 的密码") {
		t.Fatalf("重置页 = %d：\n%s", code, short(page))
	}
	if csrf = extractCSRF(page); csrf == "" {
		t.Fatal("重置页没有 csrf")
	}

	// 两次输入不一致、密码太短都不花掉令牌。
	if code, body := h.postForm2("/reset", url.Values{
		"token": {token}, "password": {"a-brand-new-password"}, "confirm": {"different-password"},
	}, cookie, csrf); code != http.StatusBadRequest || !strings.Contains(body, "两次输入的密码不一样") {
		t.Fatalf("不一致 = %d：\n%s", code, short(body))
	}
	if code, body := h.postForm2("/reset", url.Values{
		"token": {token}, "password": {"short1"}, "confirm": {"short1"},
	}, cookie, csrf); code != http.StatusBadRequest || !strings.Contains(body, "密码至少") {
		t.Fatalf("弱密码 = %d：\n%s", code, short(body))
	}

	code, body := h.postForm2("/reset", url.Values{
		"token": {token}, "password": {"a-brand-new-password"}, "confirm": {"a-brand-new-password"},
	}, cookie, csrf)
	if code != http.StatusOK || !strings.Contains(body, "密码已重置") {
		t.Fatalf("重置 = %d：\n%s", code, short(body))
	}

	// 令牌一次性，旧会话已经下线，旧密码不能再用，新密码可以登录。
	if code, _ := h.htmlGet("/reset?token="+url.QueryEscape(token), ""); code != http.StatusGone {
		t.Errorf("已用令牌再打开 = %d, want 410", code)
	}
	if _, _, err := h.db.SessionUser(ctx(), cookieValue([]string{cookie}, "fastoj_session")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("旧会话还在: %v", err)
	}
	if code, _, _ := h.postNoRedirect("/login", url.Values{
		"identifier": {"resettarget"}, "password": {testPassword},
	}, ""); code != http.StatusUnauthorized {
		t.Errorf("旧密码登录 = %d, want 401", code)
	}
	if code, body, _ := h.postNoRedirect("/login", url.Values{
		"identifier": {"resettarget"}, "password": {"a-brand-new-password"},
	}, ""); code != http.StatusSeeOther {
		t.Errorf("新密码登录 = %d：\n%s", code, short(body))
	}
}

// 没有 SMTP 时不能假装邮件已经寄出：页面上直接说清楚，POST 也不发。
func TestForgotWithoutMailer(t *testing.T) {
	h := newHarness(t)
	if code, page := h.htmlGet("/forgot", ""); code != http.StatusOK || !strings.Contains(page, "还没有配置发信") {
		t.Fatalf("没有 SMTP 的找回页 = %d：\n%s", code, short(page))
	}
	if code, page, _ := h.postNoRedirect("/forgot", url.Values{"email": {"nobody@example.com"}}, ""); code != http.StatusServiceUnavailable ||
		!strings.Contains(page, "还没有配置发信") {
		t.Fatalf("没有 SMTP 的申请 = %d：\n%s", code, short(page))
	}
}

// 有 SMTP 但寄不出去时，页面对"账号存在"和"账号不存在"仍然是同一句话：
// 找回密码不能变成账号枚举接口。
func TestForgotDoesNotRevealAccounts(t *testing.T) {
	h := newHarness(t)
	// 先建一个真实账号，再把 SMTP 配到一个必然连不上的地址（端口 1）。
	h.login("knownuser", "known@example.com", testPassword)
	if code, body := h.postBearer("/admin/site", smtpForm(""), h.adminToken()); code != http.StatusOK {
		t.Fatalf("保存发信配置 = %d：\n%s", code, short(body))
	}

	code, known, _ := h.postNoRedirect("/forgot", url.Values{"email": {"known@example.com"}}, "")
	if code != http.StatusOK || !strings.Contains(known, resetRequestedNotice) {
		t.Fatalf("已知账号 = %d：\n%s", code, short(known))
	}
	code, ghost, _ := h.postNoRedirect("/forgot", url.Values{"email": {"ghost@example.com"}}, "")
	if code != http.StatusOK || !strings.Contains(ghost, resetRequestedNotice) {
		t.Fatalf("未知账号 = %d：\n%s", code, short(ghost))
	}

	if _, err := h.db.PasswordResetUser(ctx(), "no-such-token"); !errors.Is(err, store.ErrPasswordResetInvalid) {
		t.Errorf("乱猜令牌 = %v", err)
	}
	// 一次申请真的只产生一条未使用的令牌，而且只能从邮件里的原文用。
	var unused int
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT COUNT(*) FROM password_resets WHERE used_at IS NULL`).Scan(&unused); err != nil {
		t.Fatal(err)
	}
	if unused != 1 {
		t.Errorf("未使用令牌 = %d, want 1", unused)
	}
}
