package api

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 题解挂在题上，专栏不挂题；两者共用一张表与同一套权限：
// 作者能改自己的，持 topic.moderate 的人能删任何人的。

const (
	articleWindow     = time.Hour
	maxArticlesHour   = 10
	maxArticlePageLen = 30
)

func articleBase(kind string) string {
	if kind == store.ArticleSolution {
		return "solutions"
	}
	return "columns"
}

func articleKindLabel(kind string) string {
	if kind == store.ArticleSolution {
		return "题解"
	}
	return "专栏"
}

func canEditArticle(a actor, art store.Article) bool {
	return a.User.ID != 0 && art.AuthorID != 0 && art.AuthorID == a.User.ID
}

func canDeleteArticle(a actor, art store.Article) bool {
	return canEditArticle(a, art) || a.can(permTopicModerate)
}

func canSeeArticle(a actor, art store.Article) bool {
	visible := art.Visible || canDeleteArticle(a, art)
	if !visible {
		return false
	}
	if art.Kind != store.ArticleSolution || art.ProblemVisible {
		return true
	}
	return canDeleteArticle(a, art) ||
		(a.User.ID != 0 && a.User.ID == art.ProblemOwnerID) ||
		a.can(permProblemEditAny) || a.can(permProblemPublish)
}

type articleRow struct {
	ID           int64
	Kind         string
	Title        string
	Author       string
	AuthorHandle string
	Color        string
	When         string
	Visible      bool
	HasProblem   bool
	ProblemPID   string
	ProblemTitle string
	CanEdit      bool
}

type articleListData struct {
	Title       string
	Kind        string
	Base        string
	Rows        []articleRow
	Query       string
	Next        int64
	Limit       int
	NewLink     string
	SignedIn    bool
	EmptyText   string
	Problem     string
	ProblemName string
}

func (s *Server) articleRows(items []store.Article, a actor) []articleRow {
	out := make([]articleRow, 0, len(items))
	for _, art := range items {
		name := art.AuthorHandle
		if art.AuthorName != "" {
			name = art.AuthorName
		}
		if name == "" {
			name = "已注销"
		}
		out = append(out, articleRow{
			ID: art.ID, Kind: art.Kind, Title: art.Title, Author: name, AuthorHandle: art.AuthorHandle,
			Color: art.AuthorColor, When: agoMS(art.CreatedAt), Visible: art.Visible,
			HasProblem: art.ProblemID != 0, ProblemPID: art.ProblemPID, ProblemTitle: art.ProblemTitle,
			CanEdit: canDeleteArticle(a, art),
		})
	}
	return out
}

func articlePageLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > 100 {
		return maxArticlePageLen
	}
	return n
}

func nextArticleCursor(items []store.Article, limit int) int64 {
	if len(items) < limit || len(items) == 0 {
		return 0
	}
	return items[len(items)-1].ID
}

func (s *Server) handleArticleList(w http.ResponseWriter, r *http.Request, kind string) {
	a, _ := s.authenticate(r)
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	limit := articlePageLimit(q.Get("limit"))
	search := strings.TrimSpace(q.Get("q"))
	items, err := s.db.ListArticles(r.Context(), store.ArticleFilter{
		Kind: kind, ViewerID: a.User.ID, All: a.can(permTopicModerate),
		Search: search, After: after, Limit: limit,
	})
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	base := "/" + articleBase(kind)
	d := articleListData{
		Title: articleKindLabel(kind), Kind: kind, Base: base,
		Rows: s.articleRows(items, a), Query: search, Limit: limit,
		Next: nextArticleCursor(items, limit), SignedIn: a.User.ID != 0,
		NewLink:   base + "/new",
		EmptyText: "还没有" + articleKindLabel(kind) + "。",
	}
	s.page(w, r, http.StatusOK, d.Title, "articles", a.viewer(), msgOf(r), d)
}

// handleProblemSolutions is the 题解 tab of one problem. A hidden problem keeps
// its solutions hidden from everyone who cannot see the problem itself.
func (s *Server) handleProblemSolutions(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	p, ok := s.problemByKey(w, r)
	if !ok {
		return
	}
	if !p.Visible && !a.canEditProblem(p) && !a.can(permProblemPublish) {
		s.respondError(w, r, http.StatusNotFound, "problem "+p.PID+" not found")
		return
	}
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	limit := articlePageLimit(q.Get("limit"))
	items, err := s.db.ListArticles(r.Context(), store.ArticleFilter{
		Kind: store.ArticleSolution, ProblemID: p.ID, ViewerID: a.User.ID,
		All: a.can(permTopicModerate), After: after, Limit: limit,
	})
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.page(w, r, http.StatusOK, "题解 "+p.PID, "articles", a.viewer(), msgOf(r), articleListData{
		Title: "题解 · " + p.PID + " " + p.Title, Kind: store.ArticleSolution, Base: "/solutions",
		Rows: s.articleRows(items, a), Limit: limit, Next: nextArticleCursor(items, limit),
		SignedIn:  a.User.ID != 0,
		NewLink:   "/solutions/new?problem=" + url.QueryEscape(p.PID),
		EmptyText: "这道题还没有题解。",
		Problem:   p.PID, ProblemName: p.Title,
	})
}

func (s *Server) articleByID(w http.ResponseWriter, r *http.Request, kind string) (store.Article, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "文章不存在")
		return store.Article{}, false
	}
	art, err := s.db.GetArticle(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return art, false
	}
	if art.Kind != kind {
		s.respondError(w, r, http.StatusNotFound, "文章不存在")
		return art, false
	}
	return art, true
}

func (s *Server) handleArticleGet(w http.ResponseWriter, r *http.Request, kind string) {
	art, ok := s.articleByID(w, r, kind)
	if !ok {
		return
	}
	a, _ := s.authenticate(r)
	if !canSeeArticle(a, art) {
		s.respondError(w, r, http.StatusNotFound, "文章不存在")
		return
	}
	author := art.AuthorHandle
	if art.AuthorName != "" {
		author = art.AuthorName
	}
	if author == "" {
		author = "已注销"
	}
	back := "/" + articleBase(kind)
	if kind == store.ArticleSolution && art.ProblemPID != "" {
		back = "/problems/" + url.PathEscape(art.ProblemPID) + "/solutions"
	}
	s.page(w, r, http.StatusOK, art.Title, "article", a.viewer(), msgOf(r), map[string]any{
		"ID": art.ID, "Kind": art.Kind, "KindLabel": articleKindLabel(art.Kind),
		"Title": art.Title, "Author": author, "AuthorHandle": art.AuthorHandle,
		"Color": art.AuthorColor, "When": agoMS(art.CreatedAt), "Updated": agoMS(art.UpdatedAt),
		"Visible": art.Visible, "Body": renderMarkdownHTML(art.Body),
		"HasProblem": art.ProblemID != 0, "ProblemPID": art.ProblemPID, "ProblemTitle": art.ProblemTitle,
		"CanEdit": canEditArticle(a, art), "CanDelete": canDeleteArticle(a, art),
		"Back": back, "Base": "/" + articleBase(kind),
	})
}

type articleForm struct {
	Kind        string
	IsNew       bool
	ID          int64
	Title       string
	Body        string
	ProblemKey  string
	ProblemName string
	Visible     bool
	Errors      []string
	Action      string
	Back        string
	Preview     template.HTML
}

func (s *Server) handleArticleNew(w http.ResponseWriter, r *http.Request, kind string) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	if msg, banned := a.bannedFrom(permTopicPost, "发文"); banned {
		s.respondError(w, r, http.StatusForbidden, msg)
		return
	}
	d := articleForm{
		Kind: kind, IsNew: true, Visible: true,
		Action: "/" + articleBase(kind), Back: "/" + articleBase(kind),
	}
	if kind == store.ArticleSolution {
		key := strings.TrimSpace(r.URL.Query().Get("problem"))
		if key != "" {
			if p, err := s.db.FindProblem(r.Context(), key); err == nil {
				d.ProblemKey, d.ProblemName = p.PID, p.Title
			}
		}
	}
	s.page(w, r, http.StatusOK, "写"+articleKindLabel(kind), "article_edit", a.viewer(), msgOf(r), d)
}

func (s *Server) parseArticleForm(r *http.Request, kind string) (articleForm, []string) {
	f := articleForm{
		Kind: kind, Title: strings.TrimSpace(r.FormValue("title")),
		Body: r.FormValue("body"), ProblemKey: strings.TrimSpace(r.FormValue("problem")),
		Visible: r.FormValue("visible") == "1",
	}
	var errs []string
	if f.Title == "" {
		errs = append(errs, "标题不能是空的")
	} else if tooLong(f.Title, store.MaxArticleTitle) {
		errs = append(errs, fmt.Sprintf("标题最长 %d 个字", store.MaxArticleTitle))
	}
	if tooLong(f.Body, store.MaxArticleBody) {
		errs = append(errs, fmt.Sprintf("正文最长 %d 个字", store.MaxArticleBody))
	}
	return f, errs
}

func (s *Server) handleArticleCreate(w http.ResponseWriter, r *http.Request, kind string) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/"+articleBase(kind)+"/new", http.StatusSeeOther)
		return
	}
	f, errs := s.parseArticleForm(r, kind)
	d := articleForm{
		Kind: kind, IsNew: true, Title: f.Title, Body: f.Body, ProblemKey: f.ProblemKey,
		Visible: f.Visible, Errors: errs, Action: "/" + articleBase(kind), Back: "/" + articleBase(kind),
	}
	var problemID int64
	if kind == store.ArticleSolution {
		if f.ProblemKey == "" {
			errs = append(errs, "题解要写清是哪道题")
		} else if p, err := s.db.FindProblem(r.Context(), f.ProblemKey); err != nil {
			errs = append(errs, "找不到题目 "+f.ProblemKey)
		} else {
			problemID, d.ProblemName = p.ID, p.Title
			if !p.Visible && !a.canEditProblem(p) {
				errs = append(errs, "题目 "+p.PID+" 没公开，只有它的作者或有发布权的人能写题解")
			}
		}
	}
	d.Errors = errs
	if r.FormValue("preview") == "1" {
		d.Preview = renderMarkdownHTML(f.Body)
		s.page(w, r, http.StatusOK, "写"+articleKindLabel(kind), "article_edit", a.viewer(), "", d)
		return
	}
	if len(errs) > 0 {
		s.page(w, r, http.StatusBadRequest, "写"+articleKindLabel(kind), "article_edit", a.viewer(), "", d)
		return
	}
	key := fmt.Sprintf("article:%d", a.User.ID)
	allowed, err := s.db.LoginAllowed(r.Context(), key, articleWindow, maxArticlesHour)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !allowed {
		d.Errors = []string{fmt.Sprintf("写得太快了，一小时最多 %d 篇", maxArticlesHour)}
		s.page(w, r, http.StatusTooManyRequests, "写"+articleKindLabel(kind), "article_edit", a.viewer(), "", d)
		return
	}
	art, err := s.db.CreateArticle(r.Context(), kind, problemID, a.User.ID, f.Title, f.Body, f.Visible)
	if err != nil {
		d.Errors = []string{err.Error()}
		s.page(w, r, http.StatusBadRequest, "写"+articleKindLabel(kind), "article_edit", a.viewer(), "", d)
		return
	}
	if _, err := s.db.RegisterLoginFailure(r.Context(), key, articleWindow); err != nil {
		s.log.Error("article counter", "err", err)
	}
	s.log.Info("article created", "id", art.ID, "kind", kind, "by", a.User.Handle)
	http.Redirect(w, r, "/"+articleBase(kind)+"/"+strconv.FormatInt(art.ID, 10)+"?msg="+
		url.QueryEscape("已发布"), http.StatusSeeOther)
}

// articleForEdit resolves {id} and requires authorship.
func (s *Server) articleForEdit(w http.ResponseWriter, r *http.Request, kind string) (actor, store.Article, bool) {
	art, ok := s.articleByID(w, r, kind)
	if !ok {
		return actor{}, art, false
	}
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return actor{}, art, false
	}
	if !canEditArticle(a, art) {
		s.respondError(w, r, http.StatusForbidden, "这篇文章由他人撰写，只有作者能改")
		return a, art, false
	}
	return a, art, true
}

func (s *Server) handleArticleEdit(w http.ResponseWriter, r *http.Request, kind string) {
	a, art, ok := s.articleForEdit(w, r, kind)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, "编辑"+articleKindLabel(kind), "article_edit", a.viewer(), msgOf(r), articleForm{
		Kind: kind, ID: art.ID, Title: art.Title, Body: art.Body, ProblemKey: art.ProblemPID,
		ProblemName: art.ProblemTitle, Visible: art.Visible,
		Action: "/" + articleBase(kind) + "/" + strconv.FormatInt(art.ID, 10),
		Back:   "/" + articleBase(kind) + "/" + strconv.FormatInt(art.ID, 10),
	})
}

func (s *Server) handleArticleUpdate(w http.ResponseWriter, r *http.Request, kind string) {
	a, art, ok := s.articleForEdit(w, r, kind)
	if !ok {
		return
	}
	f, errs := s.parseArticleForm(r, kind)
	d := articleForm{
		Kind: kind, ID: art.ID, Title: f.Title, Body: f.Body, ProblemKey: art.ProblemPID,
		ProblemName: art.ProblemTitle, Visible: f.Visible, Errors: errs,
		Action: "/" + articleBase(kind) + "/" + strconv.FormatInt(art.ID, 10),
		Back:   "/" + articleBase(kind) + "/" + strconv.FormatInt(art.ID, 10),
	}
	if r.FormValue("preview") == "1" {
		d.Preview = renderMarkdownHTML(f.Body)
		s.page(w, r, http.StatusOK, "编辑"+articleKindLabel(kind), "article_edit", a.viewer(), "", d)
		return
	}
	if len(errs) > 0 {
		s.page(w, r, http.StatusBadRequest, "编辑"+articleKindLabel(kind), "article_edit", a.viewer(), "", d)
		return
	}
	if err := s.db.UpdateArticle(r.Context(), art.ID, f.Title, f.Body, f.Visible); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("article updated", "id", art.ID, "kind", kind, "by", a.User.Handle)
	http.Redirect(w, r, "/"+articleBase(kind)+"/"+strconv.FormatInt(art.ID, 10)+"?msg="+
		url.QueryEscape("已保存"), http.StatusSeeOther)
}

func (s *Server) handleArticleDelete(w http.ResponseWriter, r *http.Request, kind string) {
	art, ok := s.articleByID(w, r, kind)
	if !ok {
		return
	}
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	if !canDeleteArticle(a, art) {
		s.respondError(w, r, http.StatusForbidden, "这篇文章由他人撰写，需要 "+permTopicModerate+" 才能删除")
		return
	}
	if _, err := s.db.DeleteArticle(r.Context(), art.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("article deleted", "id", art.ID, "kind", kind, "by", a.User.Handle)
	back := "/" + articleBase(kind)
	if kind == store.ArticleSolution && art.ProblemPID != "" {
		back = "/problems/" + url.PathEscape(art.ProblemPID) + "/solutions"
	}
	http.Redirect(w, r, back+"?msg="+url.QueryEscape("已删除"), http.StatusSeeOther)
}
