package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 收藏与错题本都是"我自己的阅读列表"：登录才看得到，别人无从枚举。

type favoriteRow struct {
	PID        string
	Title      string
	Difficulty string
	Tags       []string
	Hidden     bool
	Status     string
	StatusText string
	StatusClass string
	When       string
}

func (s *Server) handleFavoriteToggle(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	p, ok := s.problemByKey(w, r)
	if !ok {
		return
	}
	back := "/problems/" + url.PathEscape(p.PID)
	if cid, err := strconv.ParseInt(r.FormValue("contest"), 10, 64); err == nil && cid > 0 {
		back += "?contest=" + strconv.FormatInt(cid, 10)
	}
	added, err := s.db.ToggleFavorite(r.Context(), a.User.ID, p.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	note := "已从收藏移除 " + p.PID
	if added {
		note = "已收藏 " + p.PID + "：" + p.Title
	}
	http.Redirect(w, r, back+"?msg="+url.QueryEscape(note), http.StatusSeeOther)
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/favorites", http.StatusSeeOther)
		return
	}
	items, err := s.db.ListFavoriteProblems(r.Context(), a.User.ID, 200)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	ids := make([]int64, len(items))
	for i, p := range items {
		ids[i] = p.ID
	}
	statuses, err := s.db.ProblemStatuses(r.Context(), a.User.ID, ids)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	rows := make([]favoriteRow, 0, len(items))
	for _, p := range items {
		st := statuses[p.ID]
		if st == "" {
			st = store.ProblemUntried
		}
		rows = append(rows, favoriteRow{
			PID: p.PID, Title: p.Title, Difficulty: store.DifficultyLabel(p.Difficulty),
			Tags: p.Tags, Hidden: !p.Visible, Status: st,
			StatusText: problemStatusText(st), StatusClass: "status-" + st,
		})
	}
	s.page(w, r, http.StatusOK, "我的收藏", "favorites", a.viewer(), msgOf(r), map[string]any{
		"Rows": rows,
	})
}

type reviewRow struct {
	PID          string
	Title        string
	Difficulty   string
	Tags         []string
	Attempts     int
	LastVerdict  string
	LastVerdictText string
	LastWhen     string
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/review", http.StatusSeeOther)
		return
	}
	items, err := s.db.ListReviewProblems(r.Context(), a.User.ID, 200)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	rows := make([]reviewRow, 0, len(items))
	for _, p := range items {
		verdict := p.LastVerdict
		text := "还没判完"
		if verdict != "" {
			text = verdictSentence(verdict)
		}
		rows = append(rows, reviewRow{
			PID: p.PID, Title: p.Title, Difficulty: store.DifficultyLabel(p.Difficulty),
			Tags: p.Tags, Attempts: p.Attempts, LastVerdict: verdict,
			LastVerdictText: text, LastWhen: agoMS(p.LastAt),
		})
	}
	s.page(w, r, http.StatusOK, "错题本", "review", a.viewer(), msgOf(r), map[string]any{
		"Rows": rows,
	})
}
