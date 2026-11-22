package api

// 忘记密码：申请人只填邮箱，站点把一条 30 分钟、一次性的重置链接寄过去。
// 页面对"邮箱不存在"和"信已经寄出"给同一句话，避免把这里变成账号枚举接口。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

const (
	resetRequestWindow = 15 * time.Minute
	maxResetRequestsIP = 5
	resetAddressWindow = time.Hour
	maxResetMailsAddr  = 3
	// resetRequestedNotice 是无论账号存不存在都显示的那句话。
	resetRequestedNotice = "如果这个邮箱对应一个可以登录的账号，重置邮件会在几分钟内到达。" +
		"没收到就看看垃圾箱，或者过一会儿再试。"
)

// passwordMailReady 判断"现在发得出信吗"。没有 SMTP 时不该让申请人对着一个假表单
// 等一封永远不来的邮件；这不是秘密，直接写在页面上。
func (s *Server) passwordMailReady(ctx context.Context) bool {
	c := smtpFromEnv(s.site(ctx).SMTP)
	return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != "" && c.Port > 0
}

type forgotData struct {
	Disabled string
	Sent     bool
	Message  string
	Error    string
	Email    string
	Human    *humanGate
}

func (s *Server) handleForgotPage(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	if !s.passwordMailReady(r.Context()) {
		s.page(w, r, http.StatusOK, "找回密码", "password_forgot", a.viewer(), "", forgotData{
			Disabled: "站点还没有配置发信（SMTP），自助找回暂时用不了。请联系管理员重置密码。",
		})
		return
	}
	gate, err := s.humanGateFor(r.Context(), store.VerifyLogin, clientIP(r), "")
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.page(w, r, http.StatusOK, "找回密码", "password_forgot", a.viewer(), "", forgotData{Human: gate})
}

// renderForgotForm 重画表单时换一张新的人机验证：上一张已经被这次提交核销。
func (s *Server) renderForgotForm(w http.ResponseWriter, r *http.Request, status int, data forgotData) {
	gate, err := s.humanGateFor(r.Context(), store.VerifyLogin, clientIP(r), "")
	if err == nil {
		data.Human = gate
	}
	a, _ := s.authenticate(r)
	s.page(w, r, status, "找回密码", "password_forgot", a.viewer(), "", data)
}

func (s *Server) handleForgotPost(w http.ResponseWriter, r *http.Request) {
	if !s.passwordMailReady(r.Context()) {
		s.renderForgotForm(w, r, http.StatusServiceUnavailable, forgotData{
			Disabled: "站点还没有配置发信（SMTP），自助找回暂时用不了。请联系管理员重置密码。",
		})
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	ip := clientIP(r)
	ipKey := "pwreset:ip:" + ip
	allowed, err := s.db.LoginAllowed(r.Context(), ipKey, resetRequestWindow, maxResetRequestsIP)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !allowed {
		s.renderForgotForm(w, r, http.StatusTooManyRequests, forgotData{
			Email: email,
			Error: "这台机器申请得太频繁了，等 15 分钟再试。",
		})
		return
	}
	if !validEmail(email) {
		s.renderForgotForm(w, r, http.StatusBadRequest, forgotData{
			Email: email, Error: "邮箱格式不正确",
		})
		return
	}
	humanOK, err := s.humanAllowed(r, store.VerifyLogin)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !humanOK {
		s.renderForgotForm(w, r, http.StatusForbidden, forgotData{
			Email: email,
			Error: "请先完成页面里的验证：等它转完，再点那一下「我是人类，不是机器」，然后回来点发送。",
		})
		return
	}

	// 这个计数器记的是"申请过"，不是"成功了"：不存在的地址也占一格，
	// 否则一个字都不存在的邮箱可以无限试探。
	count := func(key string, window time.Duration) {
		if _, err := s.db.RegisterLoginFailure(r.Context(), key, window); err != nil {
			s.log.Error("password reset counter", "err", err)
		}
	}
	count(ipKey, resetRequestWindow)

	if u, err := s.db.GetUserByEmail(r.Context(), email); err == nil && u.Status == "active" && u.Email != "" {
		// 同一个收件地址每小时最多三封，避免这里被拿来给别人塞信。
		addrKey := "pwreset:mail:" + strings.ToLower(email)
		mailOK, err := s.db.LoginAllowed(r.Context(), addrKey, resetAddressWindow, maxResetMailsAddr)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		if mailOK {
			token, err := s.db.CreatePasswordReset(r.Context(), u.ID, store.PasswordResetTTL)
			if err != nil {
				s.log.Warn("password reset token failed", "user", u.Handle, "err", err)
			} else if err := s.sendPasswordResetMail(r.Context(), s.siteFresh(r.Context()),
				s.baseURL(r), u.Email, u.Handle, token); err != nil {
				// SMTP 的报错可能带服务器问候，不带信内容；收件地址只进数据库与邮件，
				// 不进日志行。
				s.log.Warn("password reset mail failed", "user", u.Handle, "err", err)
			} else {
				count(addrKey, resetAddressWindow)
				s.log.Info("password reset mail sent", "user", u.Handle, "ip", ip)
			}
		}
	}
	s.sentForgot(w, r, email)
}

// sentForgot 对"账号存在"和"账号不存在"渲染同一页：没有逐字相同的回答，
// 前面所有保密努力都会被一次对照请求拆穿。
func (s *Server) sentForgot(w http.ResponseWriter, r *http.Request, email string) {
	a, _ := s.authenticate(r)
	s.page(w, r, http.StatusOK, "找回密码", "password_forgot", a.viewer(), "", forgotData{
		Sent: true, Message: resetRequestedNotice, Email: email,
	})
}

type resetData struct {
	// State 是 form / invalid / done。invalid 时 Message 是给持链接的人看的理由。
	State   string
	Message string
	Error   string
	Token   string
	Handle  string
}

func (s *Server) handleResetPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	a, _ := s.authenticate(r)
	if token == "" {
		s.page(w, r, http.StatusBadRequest, "重置密码", "password_reset", nil, "", resetData{
			State:   "invalid",
			Message: "这个页面要从邮件里的链接进来。没有收到邮件，可以在「找回密码」页再申请一次。",
		})
		return
	}
	userID, err := s.db.PasswordResetUser(r.Context(), token)
	if err != nil {
		s.page(w, r, http.StatusGone, "重置密码", "password_reset", nil, "", resetData{
			State: "invalid", Message: passwordResetReason(err),
		})
		return
	}
	u, err := s.db.GetUserByID(r.Context(), userID)
	if err != nil {
		s.page(w, r, http.StatusGone, "重置密码", "password_reset", nil, "", resetData{
			State: "invalid", Message: passwordResetReason(store.ErrPasswordResetInvalid),
		})
		return
	}
	s.page(w, r, http.StatusOK, "重置密码", "password_reset", a.viewer(), "", resetData{
		State: "form", Token: token, Handle: u.Handle,
	})
}

func (s *Server) handleResetPost(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.FormValue("token"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	// 先记下这次请求进门前是谁：密码一改，这个账号的会话就被删了，事后再问
	// 已经问不出原来的身份。
	before, _ := s.authenticate(r)
	userID, err := s.db.PasswordResetUser(r.Context(), token)
	if err != nil {
		s.page(w, r, http.StatusGone, "重置密码", "password_reset", nil, "", resetData{
			State: "invalid", Message: passwordResetReason(err),
		})
		return
	}
	u, err := s.db.GetUserByID(r.Context(), userID)
	if err != nil {
		s.page(w, r, http.StatusGone, "重置密码", "password_reset", nil, "", resetData{
			State: "invalid", Message: passwordResetReason(store.ErrPasswordResetInvalid),
		})
		return
	}
	if problem := passwordProblem(password); problem != "" {
		s.page(w, r, http.StatusBadRequest, "重置密码", "password_reset", nil, "", resetData{
			State: "form", Token: token, Handle: u.Handle, Error: problem,
		})
		return
	}
	if password != confirm {
		s.page(w, r, http.StatusBadRequest, "重置密码", "password_reset", nil, "", resetData{
			State: "form", Token: token, Handle: u.Handle, Error: "两次输入的密码不一样",
		})
		return
	}
	if err := s.db.ConsumePasswordReset(r.Context(), token, password, s.cfg.BCryptCost); err != nil {
		s.page(w, r, http.StatusGone, "重置密码", "password_reset", nil, "", resetData{
			State: "invalid", Message: passwordResetReason(err),
		})
		return
	}
	// 这个账号的所有会话都已经在事务里删掉了；浏览器上如果还拿着那枚 cookie，
	// 现在再清一次，别让下个页面看起来还登录着。
	if before.Cookie && before.User.ID == userID {
		s.clearSession(w)
	}
	s.log.Info("password reset", "user", u.Handle, "ip", clientIP(r))
	s.page(w, r, http.StatusOK, "密码已重置", "password_reset", nil, "", resetData{
		State: "done", Handle: u.Handle,
	})
}

func passwordResetReason(err error) string {
	if errors.Is(err, store.ErrPasswordResetExpired) {
		return "重置链接已经过期。回到「找回密码」页再申请一条就行。"
	}
	return "这个重置链接无效、已经用过，或者密码在这之后改过。可以重新申请一条。"
}
