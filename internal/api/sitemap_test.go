package api

import (
	"net/http"
	"strings"
	"testing"
)

// The sitemap is for crawlers and for people who forgot where a page lives, so it
// must list exactly what an anonymous visitor can already open.
func TestSitemapListsOnlyPublicContent(t *testing.T) {
	h := newHarness(t)
	if code, body := h.createProblem(problemValues("P7000", "公开题")); code != http.StatusOK {
		t.Fatalf("create visible problem = %d %s", code, body)
	}
	hidden := problemValues("P7001", "隐藏题")
	delete(hidden, "visible")
	if code, body := h.postBearer("/problems", hidden, h.adminToken()); code != http.StatusOK {
		t.Fatalf("create hidden problem = %d %s", code, body)
	}

	host := h.signIn("sitemapper", permContestCreate)
	if code, body := h.createContest(host, contestValues("C7000", "公开赛")); code != http.StatusOK {
		t.Fatalf("create visible contest = %d %s", code, body)
	}
	private := contestValues("C7001", "隐藏赛")
	delete(private, "visible")
	if code, body := h.createContest(host, private); code != http.StatusOK {
		t.Fatalf("create hidden contest = %d %s", code, body)
	}

	code, body := h.htmlGet("/sitemap.xml", "")
	if code != http.StatusOK {
		t.Fatalf("sitemap = %d %s", code, body)
	}
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
		"/problems/P7000", "/contests/C7000", "/problems</loc>", "/contests</loc>", "/rank</loc>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"P7001", "C7001"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("sitemap leaked hidden %q:\n%s", unwanted, body)
		}
	}
}

func TestPublicDocsAndUpdatesPages(t *testing.T) {
	h := newHarness(t)

	code, docs := h.htmlGet("/docs", "")
	if code != http.StatusOK {
		t.Fatalf("docs = %d", code)
	}
	for _, want := range []string{"使用说明", "做题与判题", "比赛", "遇到问题", "/tickets"} {
		if !strings.Contains(docs, want) {
			t.Errorf("docs missing %q", want)
		}
	}

	code, updates := h.htmlGet("/updates", "")
	if code != http.StatusOK {
		t.Fatalf("updates = %d", code)
	}
	for _, want := range []string{"更新日志", "sitemap.xml", "KaTeX"} {
		if !strings.Contains(updates, want) {
			t.Errorf("updates missing %q", want)
		}
	}

	// Both pages are linked from every footer, anonymous visitors included.
	for _, path := range []string{"/", "/login"} {
		_, page := h.htmlGet(path, "")
		for _, want := range []string{`href="/docs"`, `href="/updates"`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s footer missing %q", path, want)
			}
		}
	}
}

// The sitemap's absolute links must follow the scheme the visitor actually used,
// which behind a proxy is only knowable from X-Forwarded-Proto.
func TestSitemapHonoursForwardedHTTPS(t *testing.T) {
	h := newHarness(t)
	req, err := http.NewRequest(http.MethodGet, h.srv.URL+"/sitemap.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Fatalf("sitemap = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	body := readAll(t, resp)
	host := strings.TrimPrefix(h.srv.URL, "http://")
	if !strings.HasPrefix(body, `<?xml`) || !strings.Contains(body, "https://"+host) ||
		strings.Contains(body, "http://"+host) {
		t.Errorf("scheme not forwarded:\n%s", body)
	}
}
