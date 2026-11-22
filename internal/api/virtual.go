package api

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 周赛不自动排期：host 照常创建比赛，勾上「周赛」，这里只是一个收集页。

type weeklyData struct {
	Rows    []contestListRow
	SignedIn bool
}

func (s *Server) handleWeekly(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	page, err := s.db.ListWeeklyContests(r.Context(), after, limit, a.canHostContests())
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	now := nowMs()
	d := weeklyData{SignedIn: a.User.ID != 0}
	for _, item := range page.Items {
		label, class := phaseView(item.Contest, now)
		d.Rows = append(d.Rows, contestListRow{
			CID: item.CID, Title: item.Title, Phase: label, PhaseClass: class,
			Mode: contestModeLabel(item.Mode), Start: formatContestClock(item.StartAt),
			End: formatContestClock(item.EndAt), Private: item.JoinCode != "",
			Hidden: !item.Visible, BoardOpen: item.StandingOpen,
			Rated: item.Rated, Weekly: item.Weekly,
		})
	}
	s.page(w, r, http.StatusOK, "周赛", "weekly", a.viewer(), msgOf(r), d)
}

func nowMs() int64 { return time.Now().UnixMilli() }

type virtualRow struct {
	ID, ContestID int64
	CID, Title    string
	Mode          string
	Window        string
	State         string
	Running       bool
}

type virtualListData struct {
	Rows []virtualRow
}

func (s *Server) handleVirtualList(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/virtual", http.StatusSeeOther)
		return
	}
	items, err := s.db.ListVirtualContests(r.Context(), a.User.ID, 50)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	now := time.Now().UnixMilli()
	d := virtualListData{}
	for _, v := range items {
		state := "已结束"
		running := v.Running(now)
		if running {
			state = "进行中，剩余 " + formatSpan(v.EndAt-now)
		} else if v.FinishedAt == 0 {
			state = "时间到"
		}
		d.Rows = append(d.Rows, virtualRow{
			ID: v.ID, ContestID: v.ContestID, CID: v.CID, Title: v.Title,
			Mode: contestModeLabel(v.Mode), Window: formatContestClock(v.StartAt) + " —— " + formatContestClock(v.EndAt),
			State: state, Running: running,
		})
	}
	s.page(w, r, http.StatusOK, "虚拟比赛", "virtual_list", a.viewer(), msgOf(r), d)
}

func (s *Server) handleVirtualStart(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	c, err := s.db.FindContest(r.Context(), r.PathValue("key"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !c.Visible && !a.canManageContest(c) {
		s.respondError(w, r, http.StatusNotFound, "没有这场比赛："+c.CID)
		return
	}
	if c.Phase(time.Now().UnixMilli()) != store.PhaseOver {
		s.respondError(w, r, http.StatusConflict, "比赛还没结束，不能开虚拟赛")
		return
	}
	_, joined := s.db.ContestMemberOf(r.Context(), c.ID, a.User.ID)
	if !joined && !a.canManageContest(c) {
		s.respondError(w, r, http.StatusForbidden, "先加入这场比赛，才能用它的题目开虚拟赛")
		return
	}
	v, err := s.db.StartVirtualContest(r.Context(), a.User.ID, c.ID)
	if err != nil {
		s.respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.log.Info("virtual contest started", "id", v.ID, "contest", c.CID, "by", a.User.Handle)
	http.Redirect(w, r, "/virtual/"+strconv.FormatInt(v.ID, 10), http.StatusSeeOther)
}

type virtualProblemRow struct {
	PID      string
	Title    string
	Points   int
	Max      int
	Solved   bool
	Attempts int
}

type virtualData struct {
	ID, ContestID int64
	CID, Title    string
	Mode          string
	Window        string
	State         string
	Running       bool
	Started       string
	Problems      []virtualProblemRow
	Total, MaxTotal, Solved int
}

func (s *Server) virtualByID(w http.ResponseWriter, r *http.Request, a actor) (store.VirtualContest, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "虚拟比赛不存在")
		return store.VirtualContest{}, false
	}
	v, err := s.db.GetVirtualContest(r.Context(), id, a.User.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return v, false
	}
	return v, true
}

func (s *Server) handleVirtualGet(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	v, ok := s.virtualByID(w, r, a)
	if !ok {
		return
	}
	now := time.Now().UnixMilli()
	running := v.Running(now)
	state := "已结束"
	if running {
		state = "进行中，剩余 " + formatSpan(v.EndAt-now)
	} else if v.FinishedAt == 0 {
		state = "时间到（之后的新提交不再计入）"
	}
	score, err := s.db.VirtualScore(r.Context(), v)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := virtualData{
		ID: v.ID, ContestID: v.ContestID, CID: v.CID, Title: v.Title,
		Mode: contestModeLabel(v.Mode),
		Window: formatContestClock(v.StartAt) + " —— " + formatContestClock(v.EndAt),
		State: state, Running: running, Started: agoMS(v.CreatedAt),
		Total: score.Total, MaxTotal: score.MaxTotal, Solved: score.Solved,
	}
	for _, p := range score.Problems {
		d.Problems = append(d.Problems, virtualProblemRow{
			PID: p.PID, Title: p.Title, Points: p.Points, Max: p.Max,
			Solved: p.Solved, Attempts: p.Attempts,
		})
	}
	s.page(w, r, http.StatusOK, "虚拟比赛 "+v.Title, "virtual", a.viewer(), msgOf(r), d)
}

func (s *Server) handleVirtualFinish(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	v, ok := s.virtualByID(w, r, a)
	if !ok {
		return
	}
	if err := s.db.FinishVirtualContest(r.Context(), v.ID, a.User.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("virtual contest finished", "id", v.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/virtual/"+strconv.FormatInt(v.ID, 10)+"?msg="+
		url.QueryEscape("虚拟比赛已结束，成绩定格"), http.StatusSeeOther)
}
