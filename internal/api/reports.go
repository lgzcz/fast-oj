package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 举报与拉黑：举报进队列由 topic.moderate 处理；拉黑是单向的，
// 只影响私信与关注这两条直接通道，不隐藏对方已有的公开内容。

const (
	reportWindow  = time.Hour
	maxReportsHour = 20
)

func reportTargetURL(kind string, id int64, extra string) string {
	switch kind {
	case store.ReportTopic:
		return "/topics/" + strconv.FormatInt(id, 10)
	case store.ReportComment:
		return "/topics/" + extra + "#c" + strconv.FormatInt(id, 10)
	case store.ReportUser:
		return "/u/" + url.PathEscape(extra)
	case store.ReportArticle:
		if extra == store.ArticleSolution {
			return "/solutions/" + strconv.FormatInt(id, 10)
		}
		return "/columns/" + strconv.FormatInt(id, 10)
	}
	return "/"
}

// resolveReportTarget checks that the thing exists and snapshots a label plus a
// stable link, so the queue does not depend on the target still being reachable.
func (s *Server) resolveReportTarget(w http.ResponseWriter, r *http.Request, kind string, id int64) (label, targetURL, back string, ok bool) {
	switch kind {
	case store.ReportTopic:
		t, err := s.db.GetTopic(r.Context(), id)
		if err != nil {
			s.writeStoreError(w, r, err)
			return "", "", "/topics", false
		}
		return "帖子《" + t.Title + "》", reportTargetURL(kind, id, ""), "/topics/" + strconv.FormatInt(id, 10), true
	case store.ReportComment:
		c, err := s.db.GetTopicComment(r.Context(), id)
		if err != nil {
			s.writeStoreError(w, r, err)
			return "", "", "/topics", false
		}
		body := []rune(strings.TrimSpace(c.Body))
		if len(body) > 30 {
			body = append(body[:30], '…')
		}
		back := "/topics/" + strconv.FormatInt(c.TopicID, 10)
		return "评论「" + string(body) + "」", reportTargetURL(kind, id, strconv.FormatInt(c.TopicID, 10)), back, true
	case store.ReportUser:
		u, err := s.db.GetUserByID(r.Context(), id)
		if err != nil {
			s.writeStoreError(w, r, err)
			return "", "", "/", false
		}
		return "用户 @" + u.Handle, reportTargetURL(kind, id, u.Handle), "/u/" + url.PathEscape(u.Handle), true
	case store.ReportArticle:
		art, err := s.db.GetArticle(r.Context(), id)
		if err != nil {
			s.writeStoreError(w, r, err)
			return "", "", "/", false
		}
		back := "/columns/" + strconv.FormatInt(id, 10)
		if art.Kind == store.ArticleSolution {
			back = "/solutions/" + strconv.FormatInt(id, 10)
		}
		return "文章《" + art.Title + "》", reportTargetURL(kind, id, art.Kind), back, true
	}
	s.respondError(w, r, http.StatusBadRequest, "举报目标不认识")
	return "", "", "/", false
}

func (s *Server) handleReportCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/", http.StatusSeeOther)
		return
	}
	kind := strings.TrimSpace(r.FormValue("target_kind"))
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("target_id")), 10, 64)
	if err != nil || id <= 0 || !store.ValidReportTarget(kind) {
		s.respondError(w, r, http.StatusBadRequest, "举报目标不认识")
		return
	}
	label, targetURL, back, ok := s.resolveReportTarget(w, r, kind, id)
	if !ok {
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if !store.ValidReportReason(reason) {
		s.respondError(w, r, http.StatusBadRequest, "举报原因不认识")
		return
	}
	key := "report:" + strconv.FormatInt(a.User.ID, 10)
	allowed, err := s.db.LoginAllowed(r.Context(), key, reportWindow, maxReportsHour)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !allowed {
		s.respondError(w, r, http.StatusTooManyRequests,
			"举报得太快了，一小时最多 "+strconv.Itoa(maxReportsHour)+" 条")
		return
	}
	detail := strings.TrimSpace(r.FormValue("detail"))
	_, err = s.db.CreateReport(r.Context(), a.User.ID, kind, id, label, targetURL, reason, detail)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			http.Redirect(w, r, back+"?msg="+url.QueryEscape("已经举报过这条内容，正在排队处理"), http.StatusSeeOther)
			return
		}
		s.respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.db.RegisterLoginFailure(r.Context(), key, reportWindow); err != nil {
		s.log.Error("report counter", "err", err)
	}
	s.log.Info("report created", "kind", kind, "target", id, "by", a.User.Handle)
	http.Redirect(w, r, back+"?msg="+url.QueryEscape("已举报，处理人会看到"), http.StatusSeeOther)
}

type reportRowView struct {
	ID           int64
	Reporter     string
	ReporterHandle string
	TargetKind   string
	TargetKindLabel string
	TargetLabel  string
	TargetURL    string
	Reason       string
	ReasonLabel  string
	Detail       string
	Status       string
	Handler      string
	Resolution   string
	When         string
}

type reportsData struct {
	Rows   []reportRowView
	Status string
	Open   int
}

func (s *Server) handleReportsQueue(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePerm(w, r, permTopicModerate)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "open" && status != "closed" {
		status = "open"
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	items, err := s.db.ListReports(r.Context(), status, after, 50)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	open, err := s.db.CountOpenReports(r.Context())
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := reportsData{Status: status, Open: open}
	for _, rep := range items {
		reporter := rep.ReporterHandle
		if rep.ReporterName != "" {
			reporter = rep.ReporterName
		}
		if reporter == "" {
			reporter = "已注销"
		}
		d.Rows = append(d.Rows, reportRowView{
			ID: rep.ID, Reporter: reporter, ReporterHandle: rep.ReporterHandle,
			TargetKind: rep.TargetKind, TargetKindLabel: store.ReportTargetLabel(rep.TargetKind),
			TargetLabel: rep.TargetLabel, TargetURL: rep.TargetURL,
			Reason: rep.Reason, ReasonLabel: store.ReportReasonLabel(rep.Reason),
			Detail: rep.Detail, Status: rep.Status, Handler: rep.HandlerHandle,
			Resolution: rep.Resolution, When: agoMS(rep.CreatedAt),
		})
	}
	s.page(w, r, http.StatusOK, "举报队列", "reports", a.viewer(), msgOf(r), d)
}

func (s *Server) handleReportClose(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePerm(w, r, permTopicModerate)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "举报不存在")
		return
	}
	resolution := strings.TrimSpace(r.FormValue("resolution"))
	if err := s.db.CloseReport(r.Context(), id, a.User.ID, resolution); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.respondError(w, r, http.StatusConflict, "这条举报已经处理过了")
			return
		}
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("report closed", "id", id, "by", a.User.Handle)
	s.audit(r, a, "report.close", strconv.FormatInt(id, 10), "处理举报："+resolution)
	http.Redirect(w, r, "/admin/reports?msg="+url.QueryEscape("已处理"), http.StatusSeeOther)
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	u, err := s.db.GetUserByHandle(r.Context(), r.PathValue("handle"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	back := "/u/" + url.PathEscape(u.Handle)
	if err := s.db.BlockUser(r.Context(), a.User.ID, u.ID); err != nil {
		http.Redirect(w, r, back+"?msg="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	// 拉黑顺手断开双向关注：留着关注关系会让"拉黑"看起来没生效。
	if _, err := s.db.Unfollow(r.Context(), a.User.ID, u.ID); err != nil {
		s.log.Error("unfollow after block", "err", err)
	}
	if _, err := s.db.Unfollow(r.Context(), u.ID, a.User.ID); err != nil {
		s.log.Error("unfollow after block", "err", err)
	}
	s.log.Info("user blocked", "by", a.User.Handle, "target", u.Handle)
	http.Redirect(w, r, back+"?msg="+url.QueryEscape("已拉黑，之后互相发不了私信、关注不了"), http.StatusSeeOther)
}

func (s *Server) handleUnblock(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	u, err := s.db.GetUserByHandle(r.Context(), r.PathValue("handle"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	back := "/u/" + url.PathEscape(u.Handle)
	if _, err := s.db.UnblockUser(r.Context(), a.User.ID, u.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("user unblocked", "by", a.User.Handle, "target", u.Handle)
	http.Redirect(w, r, back+"?msg="+url.QueryEscape("已解除拉黑"), http.StatusSeeOther)
}
