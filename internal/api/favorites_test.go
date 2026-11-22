package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestFavoritesAndReview(t *testing.T) {
	h := newHarness(t)
	reader := h.signIn("bookmarker")
	for _, pid := range []string{"FB1", "FB2", "RW1"} {
		if code, body := h.createProblem(problemValues(pid, "收藏题 "+pid)); code != http.StatusOK {
			t.Fatalf("create %s: %d %s", pid, code, body)
		}
	}
	uid := h.dbUserID(t, "bookmarker")

	if code, body := h.postAs(reader, "/problems/FB1/favorite", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "已收藏 FB1") || !strings.Contains(body, "取消收藏") {
		t.Fatalf("favorite: %d %s", code, body)
	}
	if code, body := h.getAs(reader, "/favorites"); code != http.StatusOK ||
		!strings.Contains(body, "FB1") || !strings.Contains(body, "未尝试") {
		t.Fatalf("favorites list: %d %s", code, body)
	}
	// 重复点一次是取消：收藏页上不再有它。
	if _, body := h.postAs(reader, "/problems/FB1/favorite", url.Values{}); !strings.Contains(body, "已从收藏移除") {
		t.Fatalf("unfavorite: %s", body)
	}
	if _, body := h.getAs(reader, "/favorites"); strings.Contains(body, "FB1") {
		t.Fatalf("favorite should be gone: %s", body)
	}

	// 错题本：有提交没 AC 才出现，AC 之后自动消失。
	if _, err := h.db.SQL.ExecContext(ctx(),
		`INSERT INTO submissions (user_id, problem_id, language, source, status, verdict, total_score, created_at)
		 VALUES (?, (SELECT id FROM problems WHERE pid='RW1'), 'cpp', '', 'done', 'WA', 0, 1)`,
		uid); err != nil {
		t.Fatal(err)
	}
	if code, body := h.getAs(reader, "/review"); code != http.StatusOK ||
		!strings.Contains(body, "RW1") || !strings.Contains(body, "解答错误") {
		t.Fatalf("review list: %d %s", code, body)
	}
	if _, err := h.db.SQL.ExecContext(ctx(),
		`INSERT INTO submissions (user_id, problem_id, language, source, status, verdict, total_score, created_at)
		 VALUES (?, (SELECT id FROM problems WHERE pid='RW1'), 'cpp', '', 'done', 'AC', 100, 2)`,
		uid); err != nil {
		t.Fatal(err)
	}
	if _, body := h.getAs(reader, "/review"); strings.Contains(body, "RW1") {
		t.Fatalf("solved problem should leave review: %s", body)
	}

	// 错题本与收藏都只属于本人。
	other := h.signIn("browser2")
	if _, body := h.getAs(other, "/favorites"); strings.Contains(body, "FB2") {
		t.Fatalf("other reader sees foreign favorites: %s", body)
	}
	if code, _ := h.htmlGet("/favorites", ""); code != http.StatusOK {
		t.Fatalf("anonymous favorites should redirect to login as 200, got %d", code)
	}
}
