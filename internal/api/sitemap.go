package api

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// sitemap.go 是给搜索引擎与"忘了某一页在哪"的人留的入口：只列公开内容，
// 隐藏题、未公开比赛与需要登录的页面都不出现。

type sitemapURL struct {
	Loc string `xml:"loc"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// requestBaseURL 取这次请求的站点根。反向代理后 X-Forwarded-Proto 是唯一知道
// 外面是 https 的地方；Host 只影响站内绝对链接，XML 转义会挡住注入。
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	base := requestBaseURL(r)
	set := sitemapSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs: []sitemapURL{
			{Loc: base + "/"},
			{Loc: base + "/problems"},
			{Loc: base + "/contests"},
			{Loc: base + "/topics"},
			{Loc: base + "/rank"},
		},
	}
	// 题库按编号翻页；只取公开题，最多 5000 条，够一所学校的站用，也不让一次
	// 请求把整个大题库拉进内存。
	cursor := store.ProblemCursor{}
	for len(set.URLs) < 5000 {
		page, err := s.db.ListProblems(r.Context(), cursor, 500, "", true, store.ProblemSortPID)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		for _, p := range page.Items {
			set.URLs = append(set.URLs, sitemapURL{Loc: base + "/problems/" + p.PID})
		}
		if page.NextPID == "" {
			break
		}
		cursor.PID = page.NextPID
	}
	// 比赛列表只含公开的；隐藏比赛与加入码不该出现在站点地图里。
	if page, err := s.db.ListContests(r.Context(), 0, 500, "", false); err == nil {
		for _, c := range page.Items {
			if c.Visible {
				set.URLs = append(set.URLs, sitemapURL{Loc: base + "/contests/" + c.CID})
			}
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(set)
}

// handleSiteDocs 是不用登录也能读的使用说明：注册、提交、比赛、社区与遇到问题时
// 该找谁。完整文档在仓库的 docs/ 里，这一页只留最短的路径。
func (s *Server) handleSiteDocs(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	s.page(w, r, http.StatusOK, "使用说明", "site_docs", a.viewer(), "", nil)
}

// handleUpdates 是站内更新日志：只写用户能感知的变化，不搬 git log。
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	s.page(w, r, http.StatusOK, "更新日志", "updates", a.viewer(), "", nil)
}
