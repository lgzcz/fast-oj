package api

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 对抗赛是围绕一道题的短时 PK：窗口内的练习提交按最好得分排名。
// 它不是正式比赛，不进 rating；也还没做 Hack（提交攻击用例）那一半。

type battleRow struct {
	ID          int64
	Title       string
	PID         string
	ProblemName string
	Phase       string
	PhaseClass  string
	Start, End  string
	People      int
	Joined      bool
	Visible     bool
	Owner       string
	OwnerHandle string
}

type battleListData struct {
	Rows     []battleRow
	Next     int64
	Limit    int
	SignedIn bool
	CanHost  bool
}

func battlePhase(b store.Battle, now int64) (string, string) {
	switch {
	case now < b.StartAt:
		return "未开始", "soon"
	case now >= b.EndAt:
		return "已结束", "over"
	}
	return "进行中", "live"
}

func (s *Server) handleBattleList(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	items, err := s.db.ListBattles(r.Context(), store.BattleFilter{
		ViewerID: a.User.ID, All: a.can(permContestManageAny) || a.can(permContestCreate),
		After: after, Limit: limit,
	})
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	now := time.Now().UnixMilli()
	d := battleListData{SignedIn: a.User.ID != 0, CanHost: a.can(permContestCreate), Limit: limit}
	for _, b := range items {
		phase, class := battlePhase(b, now)
		name := b.OwnerHandle
		if b.OwnerName != "" {
			name = b.OwnerName
		}
		d.Rows = append(d.Rows, battleRow{
			ID: b.ID, Title: b.Title, PID: b.PID, ProblemName: b.ProblemTitle,
			Phase: phase, PhaseClass: class,
			Start: formatContestClock(b.StartAt), End: formatContestClock(b.EndAt),
			People: b.Participants, Joined: b.Joined, Visible: b.Visible,
			Owner: name, OwnerHandle: b.OwnerHandle,
		})
	}
	if len(items) == limit {
		d.Next = items[len(items)-1].ID
	}
	s.page(w, r, http.StatusOK, "对抗赛", "battles", a.viewer(), msgOf(r), d)
}

func (s *Server) handleBattleNew(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/battles/new", http.StatusSeeOther)
		return
	}
	if !a.can(permContestCreate) {
		s.respondError(w, r, http.StatusForbidden, "缺少权限："+permContestCreate)
		return
	}
	now := time.Now()
	s.page(w, r, http.StatusOK, "新建对抗赛", "battle_edit", a.viewer(), msgOf(r), battleEditData{
		Visible: true, StartText: now.Add(5 * time.Minute).Format(contestLayoutMini),
		EndText: now.Add(2 * time.Hour).Format(contestLayoutMini), Action: "/battles",
	})
}

type battleEditData struct {
	Title, Problem, StartText, EndText string
	Visible                            bool
	Errors                             []string
	Action                             string
}

func (s *Server) handleBattleCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/battles/new", http.StatusSeeOther)
		return
	}
	if !a.can(permContestCreate) {
		s.respondError(w, r, http.StatusForbidden, "缺少权限："+permContestCreate)
		return
	}
	d := battleEditData{
		Title: strings.TrimSpace(r.FormValue("title")), Problem: strings.TrimSpace(r.FormValue("problem")),
		StartText: strings.TrimSpace(r.FormValue("start_at")), EndText: strings.TrimSpace(r.FormValue("end_at")),
		Visible: r.FormValue("visible") == "1", Action: "/battles",
	}
	start, okStart := parseContestTime(d.StartText)
	end, okEnd := parseContestTime(d.EndText)
	if d.Title == "" || tooLong(d.Title, store.MaxBattleTitle) {
		d.Errors = append(d.Errors, fmt.Sprintf("标题要 1..%d 个字", store.MaxBattleTitle))
	}
	if d.Problem == "" {
		d.Errors = append(d.Errors, "要填题目编号")
	}
	if !okStart || !okEnd {
		d.Errors = append(d.Errors, "时间格式形如 2026-01-02T15:04")
	} else if end <= start {
		d.Errors = append(d.Errors, "结束时间必须晚于开始时间")
	}
	var p store.Problem
	if d.Problem != "" {
		p, err = s.db.FindProblem(r.Context(), d.Problem)
		if err != nil {
			d.Errors = append(d.Errors, "找不到题目 "+d.Problem)
		} else if !p.Visible && !a.canEditProblem(p) {
			d.Errors = append(d.Errors, "对抗赛默认公开，未公开的题不能拿来开")
		}
	}
	if len(d.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "新建对抗赛", "battle_edit", a.viewer(), "", d)
		return
	}
	b, err := s.db.CreateBattle(r.Context(), a.User.ID, p.ID, d.Title, start, end, d.Visible)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("battle created", "id", b.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/battles/"+strconv.FormatInt(b.ID, 10)+"?msg="+
		url.QueryEscape("对抗赛已建好"), http.StatusSeeOther)
}

type battleBoardRow struct {
	Rank     int
	Handle   string
	Name     string
	Color    string
	Attempts int
	Best     int
	Solved   bool
	IsMe     bool
}

type battleData struct {
	ID          int64
	Title       string
	PID         string
	ProblemName string
	Owner       string
	OwnerHandle string
	Phase       string
	PhaseClass  string
	Window      string
	Countdown   string
	People      int
	Max         int
	Rows        []battleBoardRow
	Joined      bool
	CanJoin     bool
	CanLeave    bool
	CanDelete   bool
	CanManage   bool
	SignedIn    bool
}

func (s *Server) battleByID(w http.ResponseWriter, r *http.Request, viewerID int64) (store.Battle, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "对抗赛不存在")
		return store.Battle{}, false
	}
	b, err := s.db.GetBattle(r.Context(), id, viewerID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return b, false
	}
	return b, true
}

func (s *Server) handleBattleGet(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	b, ok := s.battleByID(w, r, a.User.ID)
	if !ok {
		return
	}
	canManage := a.User.ID != 0 && (a.User.ID == b.OwnerID || a.can(permContestManageAny))
	if !b.Visible && !canManage {
		s.respondError(w, r, http.StatusNotFound, "对抗赛不存在")
		return
	}
	now := time.Now().UnixMilli()
	phase, class := battlePhase(b, now)
	d := battleData{
		ID: b.ID, Title: b.Title, PID: b.PID, ProblemName: b.ProblemTitle,
		Owner: b.OwnerHandle, OwnerHandle: b.OwnerHandle,
		Phase: phase, PhaseClass: class,
		Window: formatContestClock(b.StartAt) + " —— " + formatContestClock(b.EndAt),
		People: b.Participants, Joined: b.Joined, CanManage: canManage, CanDelete: canManage,
		SignedIn: a.User.ID != 0,
		CanJoin:  a.User.ID != 0 && !b.Joined && now < b.EndAt,
		CanLeave: a.User.ID != 0 && b.Joined && now < b.EndAt,
	}
	if b.OwnerName != "" {
		d.Owner = b.OwnerName
	}
	if now < b.StartAt {
		d.Countdown = "距开始还有 " + formatSpan(b.StartAt-now)
	} else if now < b.EndAt {
		d.Countdown = "距结束还有 " + formatSpan(b.EndAt-now)
	} else {
		d.Countdown = "已结束"
	}
	max, err := s.db.ProblemScoreTotal(r.Context(), b.ProblemID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d.Max = max
	rows, err := s.db.BattleBoard(r.Context(), b)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Best != rows[j].Best {
			return rows[i].Best > rows[j].Best
		}
		if rows[i].Attempts != rows[j].Attempts {
			return rows[i].Attempts < rows[j].Attempts
		}
		return rows[i].Handle < rows[j].Handle
	})
	for i, row := range rows {
		d.Rows = append(d.Rows, battleBoardRow{
			Rank: i + 1, Handle: row.Handle, Name: row.Name, Color: row.Color,
			Attempts: row.Attempts, Best: row.Best, Solved: row.Solved,
			IsMe: a.User.ID != 0 && row.UserID == a.User.ID,
		})
	}
	s.page(w, r, http.StatusOK, "对抗赛 "+b.Title, "battle", a.viewer(), msgOf(r), d)
}

func (s *Server) handleBattleJoin(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	b, ok := s.battleByID(w, r, a.User.ID)
	if !ok {
		return
	}
	if _, err := s.db.JoinBattle(r.Context(), b.ID, a.User.ID, time.Now().UnixMilli()); err != nil {
		s.respondError(w, r, http.StatusConflict, err.Error())
		return
	}
	http.Redirect(w, r, "/battles/"+strconv.FormatInt(b.ID, 10)+"?msg="+
		url.QueryEscape("已加入，去题面提交吧"), http.StatusSeeOther)
}

func (s *Server) handleBattleLeave(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	b, ok := s.battleByID(w, r, a.User.ID)
	if !ok {
		return
	}
	if _, err := s.db.LeaveBattle(r.Context(), b.ID, a.User.ID, time.Now().UnixMilli()); err != nil {
		s.respondError(w, r, http.StatusConflict, err.Error())
		return
	}
	http.Redirect(w, r, "/battles/"+strconv.FormatInt(b.ID, 10)+"?msg="+
		url.QueryEscape("已退出"), http.StatusSeeOther)
}

func (s *Server) handleBattleDelete(w http.ResponseWriter, r *http.Request) {
	b, ok := s.battleByID(w, r, 0)
	if !ok {
		return
	}
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	if a.User.ID != b.OwnerID && !a.can(permContestManageAny) {
		s.respondError(w, r, http.StatusForbidden, "只有发起人或持 "+permContestManageAny+" 的人能删")
		return
	}
	if err := s.db.DeleteBattle(r.Context(), b.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("battle deleted", "id", b.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/battles?msg="+url.QueryEscape("对抗赛已删除"), http.StatusSeeOther)
}
