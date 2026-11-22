package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGlobalSearchCoversPublicContent(t *testing.T) {
	h := newHarness(t)
	author := h.signIn("search_author")
	viewer := h.signIn("search_viewer")
	h.readyProblem("PSRCH")

	if code, body := h.postAs(author, "/columns", url.Values{
		"title": {"搜索关键词专栏"}, "body": {"正文"}, "visible": {"1"},
	}); code != http.StatusOK {
		t.Fatalf("column = %d %s", code, body)
	}
	if code, body := h.postAs(author, "/trainings", url.Values{
		"title": {"搜索关键词题单"}, "intro": {"说明"}, "visible": {"1"},
	}); code != http.StatusOK {
		t.Fatalf("training = %d %s", code, body)
	}

	// 题目按标题命中；用户按用户名命中。
	if code, body := h.getAs(viewer, "/search?q=ready"); code != http.StatusOK ||
		!strings.Contains(body, "PSRCH") || !strings.Contains(body, "共 1 条结果") {
		t.Fatalf("problem search = %d %s", code, body)
	}
	if _, body := h.getAs(viewer, "/search?q="+url.QueryEscape("搜索关键词")); !strings.Contains(body, "搜索关键词专栏") ||
		!strings.Contains(body, "搜索关键词题单") {
		t.Fatalf("article/training search lost a hit:\n%s", short(body))
	}
	if _, body := h.getAs(viewer, "/search?q=search_viewer"); !strings.Contains(body, "/u/search_viewer") {
		t.Fatalf("user search lost the hit:\n%s", short(body))
	}
	if _, body := h.getAs(viewer, "/search?q="+url.QueryEscape("完全没有的词语")); !strings.Contains(body, "没有找到") {
		t.Fatalf("empty result page:\n%s", short(body))
	}

	// 隐藏题、草稿文章与未公开题单不出现。
	if code, _ := h.createProblem(url.Values{
		"pid": {"PSRCHID"}, "title": {"ready hidden"}, "time_ms": {"1000"}, "mem_mb": {"256"},
		"visible": {"0"},
	}); code != http.StatusOK {
		t.Fatalf("hidden problem create = %d", code)
	}
	if code, _ := h.postAs(author, "/columns", url.Values{
		"title": {"草稿文章标题"}, "body": {"草稿文章正文标记"},
	}); code != http.StatusOK {
		t.Fatalf("draft column = %d", code)
	}
	if code, _ := h.postAs(author, "/trainings", url.Values{
		"title": {"隐藏题单标题"}, "intro": {"隐藏题单说明标记"},
	}); code != http.StatusOK {
		t.Fatalf("hidden training = %d", code)
	}
	if _, body := h.getAs(viewer, "/search?q=ready"); strings.Contains(body, "PSRCHID") {
		t.Fatalf("hidden problem leaked:\n%s", short(body))
	}
	if _, body := h.getAs(viewer, "/search?q="+url.QueryEscape("草稿文章正文标记")); strings.Contains(body, "草稿文章标题") {
		t.Fatalf("draft article leaked:\n%s", short(body))
	}
	if _, body := h.getAs(viewer, "/search?q="+url.QueryEscape("隐藏题单说明标记")); strings.Contains(body, "隐藏题单标题") {
		t.Fatalf("hidden training leaked:\n%s", short(body))
	}
	// 作者自己能搜到自己的草稿与隐藏题单。
	if _, body := h.getAs(author, "/search?q="+url.QueryEscape("草稿文章正文标记")); !strings.Contains(body, "草稿文章标题") {
		t.Fatalf("author lost their own draft:\n%s", short(body))
	}
}

func TestSearchEmptyQueryShowsTheForm(t *testing.T) {
	h := newHarness(t)
	code, body := h.htmlGet("/search", "")
	if code != http.StatusOK || !strings.Contains(body, `action="/search"`) ||
		strings.Contains(body, "条结果") {
		t.Fatalf("empty search = %d %s", code, body)
	}
}
