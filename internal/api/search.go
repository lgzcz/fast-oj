package api

import (
	"net/http"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 全局搜索只查四类公开内容：题目、文章、题单、用户。每类取前 10 条，
// 不翻页、不建索引；大站要更细的搜索就走各自的列表页（题库、专栏、题单都有 ?q=）。
// 私信、工单、剪贴板与未发布内容永远不出现。

const searchPerKind = 10

type searchUserRow struct {
	Handle string
	Name   string
	Color  string
}

type searchProblemRow struct {
	PID        string
	Title      string
	Difficulty string
	Source     string
	Visible    bool
}

type searchData struct {
	Query     string
	Problems  []searchProblemRow
	Articles  []articleRow
	Trainings []trainingRow
	Users     []searchUserRow
	Count     int
	SignedIn  bool
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	d := searchData{Query: query, SignedIn: a.User.ID != 0}
	if query != "" {
		problems, err := s.db.ListProblems(r.Context(), store.ProblemCursor{}, searchPerKind,
			query, !a.can(permProblemEditAny), store.ProblemSortPID)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		for _, p := range problems.Items {
			d.Problems = append(d.Problems, searchProblemRow{
				PID: p.PID, Title: p.Title, Difficulty: store.DifficultyLabel(p.Difficulty),
				Source: p.Source, Visible: p.Visible,
			})
		}

		articles, err := s.db.ListArticles(r.Context(), store.ArticleFilter{
			ViewerID: a.User.ID, All: a.can(permTopicModerate),
			Search: query, Limit: searchPerKind,
		})
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		d.Articles = s.articleRows(articles, a)

		trainings, err := s.db.ListTrainings(r.Context(), store.TrainingFilter{
			ViewerID: a.User.ID, All: a.can(permProblemEditAny),
			Search: query, Limit: searchPerKind,
		})
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		d.Trainings = trainingRows(trainings, a)

		users, err := s.db.SearchUsers(r.Context(), query, searchPerKind)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		for _, u := range users {
			name := u.Handle
			if u.DisplayName != "" {
				name = u.DisplayName
			}
			d.Users = append(d.Users, searchUserRow{Handle: u.Handle, Name: name, Color: u.ColorClass()})
		}
		d.Count = len(d.Problems) + len(d.Articles) + len(d.Trainings) + len(d.Users)
	}
	s.page(w, r, http.StatusOK, "搜索", "search", a.viewer(), msgOf(r), d)
}
