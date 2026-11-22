package api

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
	"github.com/liangbinchen2013/fast-oj/internal/totp"
)

// 第二步验证：密码对之后先领一张 10 分钟的挑战票，票在，会话不在。
// 六位码与恢复码都从同一个输入框进来，六位纯数字先按 TOTP 验。

const (
	twoFactorCookie     = "fastoj_2fa"
	twoFactorMaxAttempts = 5
	recoveryCodeCount   = 10
)

func (s *Server) setTwoFactorCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name: twoFactorCookie, Value: id, Path: "/login/2fa",
		HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(store.LoginChallengeTTL.Seconds()),
	})
}

func (s *Server) clearTwoFactorCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: twoFactorCookie, Value: "", Path: "/login/2fa",
		HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// afterPassword is where a successful password check lands: straight to a session
// when the account has no second factor, otherwise to the challenge page.
func (s *Server) afterPassword(w http.ResponseWriter, r *http.Request, u store.User, next string) {
	if state, err := s.db.GetTOTP(r.Context(), u.ID); err == nil && state.Enabled {
		ch, err := s.db.CreateLoginChallenge(r.Context(), u.ID, next)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		s.setTwoFactorCookie(w, ch.ID)
		s.log.Info("login awaiting 2fa", "user", u.Handle, "ip", clientIP(r))
		http.Redirect(w, r, "/login/2fa", http.StatusSeeOther)
		return
	}
	sess, err := s.db.CreateSession(r.Context(), u.ID, clientIP(r), r.UserAgent(), store.SessionTTL)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.db.ClearLoginFailures(r.Context(), "login:"+clientIP(r)); err != nil {
		s.log.Error("throttle reset", "err", err)
	}
	if err := s.db.TouchLogin(r.Context(), u.ID); err != nil {
		s.log.Error("touch login", "err", err)
	}
	s.setSession(w, sess.ID, store.SessionTTL)
	s.log.Info("login", "user", u.Handle, "ip", clientIP(r))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) challengeFromCookie(r *http.Request) (store.LoginChallenge, bool) {
	c, err := r.Cookie(twoFactorCookie)
	if err != nil || c.Value == "" {
		return store.LoginChallenge{}, false
	}
	ch, err := s.db.GetLoginChallenge(r.Context(), c.Value)
	if err != nil {
		return store.LoginChallenge{}, false
	}
	return ch, true
}

func (s *Server) handleLogin2FAPage(w http.ResponseWriter, r *http.Request) {
	if a, err := s.authenticate(r); err == nil && a.User.ID != 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if _, ok := s.challengeFromCookie(r); !ok {
		s.clearTwoFactorCookie(w)
		http.Redirect(w, r, "/login?msg="+url.QueryEscape("验证已过期，请重新登录"), http.StatusSeeOther)
		return
	}
	s.page(w, r, http.StatusOK, "两步验证", "login_2fa", nil, msgOf(r), map[string]any{})
}

func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	if a, err := s.authenticate(r); err == nil && a.User.ID != 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ch, ok := s.challengeFromCookie(r)
	if !ok {
		s.clearTwoFactorCookie(w)
		http.Redirect(w, r, "/login?msg="+url.QueryEscape("验证已过期，请重新登录"), http.StatusSeeOther)
		return
	}
	fail := func(msg string) {
		http.Redirect(w, r, "/login/2fa?msg="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if ch.Attempts >= twoFactorMaxAttempts {
		_ = s.db.DeleteLoginChallenge(r.Context(), ch.ID)
		s.clearTwoFactorCookie(w)
		http.Redirect(w, r, "/login?msg="+url.QueryEscape("验证尝试过多，请重新登录"), http.StatusSeeOther)
		return
	}
	state, err := s.db.GetTOTP(r.Context(), ch.UserID)
	if err != nil || !state.Enabled {
		_ = s.db.DeleteLoginChallenge(r.Context(), ch.ID)
		s.clearTwoFactorCookie(w)
		http.Redirect(w, r, "/login?msg="+url.QueryEscape("两步验证状态已变化，请重新登录"), http.StatusSeeOther)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	good := false
	if len(code) == totp.Digits {
		good = totp.Validate(state.Secret, code, time.Now())
	}
	if !good && code != "" {
		good, err = s.db.ConsumeRecoveryCode(r.Context(), ch.UserID, code)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
	}
	if !good {
		n, _ := s.db.BumpLoginChallenge(r.Context(), ch.ID)
		if n >= twoFactorMaxAttempts {
			_ = s.db.DeleteLoginChallenge(r.Context(), ch.ID)
			s.clearTwoFactorCookie(w)
			http.Redirect(w, r, "/login?msg="+url.QueryEscape("验证尝试过多，请重新登录"), http.StatusSeeOther)
			return
		}
		s.log.Warn("2fa failed", "challenge", clip(ch.ID, 8), "ip", clientIP(r))
		fail("验证码不对。六位数字或一枚尚未用过的恢复码。")
		return
	}
	u, err := s.db.GetUserByID(r.Context(), ch.UserID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.db.DeleteLoginChallenge(r.Context(), ch.ID); err != nil {
		s.log.Error("drop 2fa challenge", "err", err)
	}
	s.clearTwoFactorCookie(w)
	sess, err := s.db.CreateSession(r.Context(), u.ID, clientIP(r), r.UserAgent(), store.SessionTTL)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.db.ClearLoginFailures(r.Context(), "login:"+clientIP(r)); err != nil {
		s.log.Error("throttle reset", "err", err)
	}
	if err := s.db.TouchLogin(r.Context(), u.ID); err != nil {
		s.log.Error("touch login", "err", err)
	}
	if err := s.db.TouchTOTP(r.Context(), u.ID); err != nil {
		s.log.Error("touch totp", "err", err)
	}
	s.setSession(w, sess.ID, store.SessionTTL)
	s.log.Info("login with 2fa", "user", u.Handle, "ip", clientIP(r))
	http.Redirect(w, r, safeNext(ch.Next), http.StatusSeeOther)
}

// --- settings: two-factor and login devices ---

type securitySession struct {
	ID       string
	Short    string
	Current  bool
	IP       string
	Agent    string
	Created  string
	LastSeen string
}

type securityData struct {
	HasPassword   bool
	TOTPEnabled   bool
	PendingSecret string
	OTPURL        string
	RecoveryLeft  int
	RecoveryCodes []string
	Sessions      []securitySession
	Errors        []string
	Disabled      bool
}

func (s *Server) handleSecurityPage(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	data, err := s.securityData(r.Context(), a)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.page(w, r, http.StatusOK, "安全", "security", a.viewer(), msgOf(r), data)
}

func (s *Server) securityData(ctx context.Context, a actor) (securityData, error) {
	d := securityData{HasPassword: a.User.HasPassword}
	state, err := s.db.GetTOTP(ctx, a.User.ID)
	switch {
	case errors.Is(err, store.ErrTOTPNotSet):
	case err != nil:
		return d, err
	default:
		d.TOTPEnabled = state.Enabled
		if !state.Enabled && state.Secret != "" {
			d.PendingSecret = state.Secret
			d.OTPURL = totp.URL(s.site(ctx).Title, a.User.Handle+"@"+s.site(ctx).Title, state.Secret)
		}
	}
	if d.TOTPEnabled {
		n, err := s.db.CountRecoveryCodes(ctx, a.User.ID)
		if err != nil {
			return d, err
		}
		d.RecoveryLeft = n
	}
	sessions, err := s.db.ListSessions(ctx, a.User.ID)
	if err != nil {
		return d, err
	}
	for _, sess := range sessions {
		short := sess.ID
		if len(short) > 8 {
			short = short[:8]
		}
		d.Sessions = append(d.Sessions, securitySession{
			ID: sess.ID, Short: short, Current: sess.ID == a.Session.ID,
			IP: sess.IP, Agent: clip(sess.UserAgent, 90),
			Created: agoMS(sess.CreatedAt), LastSeen: agoMS(sess.LastSeenAt),
		})
	}
	return d, nil
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	if state, err := s.db.GetTOTP(r.Context(), a.User.ID); err == nil && state.Enabled {
		http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("两步验证已经开着"), http.StatusSeeOther)
		return
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.db.SetTOTPSecret(r.Context(), a.User.ID, secret); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("把密钥加进验证器，再输入它显示的六位码启用"), http.StatusSeeOther)
}

func newRecoveryCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	out := make([]byte, 8)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out[:4]) + "-" + string(out[4:]), nil
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	state, err := s.db.GetTOTP(r.Context(), a.User.ID)
	if err != nil || state.Enabled {
		http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("先点「开始设置」"), http.StatusSeeOther)
		return
	}
	if !totp.Validate(state.Secret, r.FormValue("code"), time.Now()) {
		d, err := s.securityData(r.Context(), a)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		d.Errors = []string{"验证器显示的六位码不对；请确认手机时间准确，再试一次。"}
		s.page(w, r, http.StatusBadRequest, "安全", "security", a.viewer(), "", d)
		return
	}
	if err := s.db.EnableTOTP(r.Context(), a.User.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	codes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		code, err := newRecoveryCode()
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		codes = append(codes, code)
	}
	if err := s.db.ReplaceRecoveryCodes(r.Context(), a.User.ID, codes); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if _, err := s.db.RevokeOtherSessions(r.Context(), a.User.ID, a.Session.ID); err != nil {
		s.log.Error("revoke sessions after 2fa", "err", err)
	}
	d, err := s.securityData(r.Context(), a)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d.RecoveryCodes = codes
	s.log.Info("2fa enabled", "user", a.User.Handle)
	s.page(w, r, http.StatusOK, "安全", "security", a.viewer(), "两步验证已启用；恢复码只显示这一次。", d)
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	if a.User.HasPassword {
		current, err := s.db.UserPasswordHash(r.Context(), a.User.ID)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		if !store.CheckPassword(current, r.FormValue("password")) {
			http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("当前密码不正确"), http.StatusSeeOther)
			return
		}
	}
	if err := s.db.DisableTOTP(r.Context(), a.User.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("2fa disabled", "user", a.User.Handle)
	http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("两步验证已关闭"), http.StatusSeeOther)
}

func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	id := r.PathValue("id")
	if id == a.Session.ID {
		// 踢掉当前设备等于退出登录，走那个按钮更清楚。
		http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("当前设备请用「退出登录」"), http.StatusSeeOther)
		return
	}
	removed, err := s.db.RevokeSession(r.Context(), a.User.ID, id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	msg := "这台设备已退出"
	if !removed {
		msg = "这台设备已经不在线"
	}
	http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) handleSessionRevokeOthers(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	n, err := s.db.RevokeOtherSessions(r.Context(), a.User.ID, a.Session.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("sessions revoked", "user", a.User.Handle, "count", n)
	http.Redirect(w, r, "/settings/security?msg="+url.QueryEscape("已退出 "+strconv.Itoa(n)+" 台其他设备"), http.StatusSeeOther)
}
