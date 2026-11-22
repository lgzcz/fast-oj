package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func latestObjectiveID(t *testing.T, h *harness) int64 {
	t.Helper()
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM objective_sets ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func objectiveValues(title, questions string, visible bool) url.Values {
	v := url.Values{"title": {title}, "body": {"自测用"}, "questions": {questions}}
	if visible {
		v.Set("visible", "1")
	}
	return v
}

func TestObjectiveCreateAnswerAndScore(t *testing.T) {
	h := newHarness(t)
	author := h.signIn("obj_author", permProblemCreate)
	user := h.signIn("obj_user")

	// 没有出题权限的人进不了建题页。
	plain := h.signIn("obj_plain")
	if code, _ := h.getAs(plain, "/objective/new"); code != http.StatusForbidden {
		t.Fatalf("plain new = %d", code)
	}

	questions := "1+1 等于几？|1|2|3|B\nHTTP 默认端口是？|80|443|22|A"
	if code, body := h.postAs(author, "/objective", objectiveValues("计算机基础", questions, true)); code != http.StatusOK ||
		!strings.Contains(body, "计算机基础") || !strings.Contains(body, "等于几") {
		t.Fatalf("create = %d %s", code, body)
	}
	id := latestObjectiveID(t, h)
	path := "/objective/" + strconv.FormatInt(id, 10)

	// 匿名可以读题，但拿不到作答表单。
	if code, body := h.htmlGet(path, ""); code != http.StatusOK ||
		!strings.Contains(body, "HTTP 默认端口是") || strings.Contains(body, `type="radio"`) {
		t.Fatalf("anonymous read = %d %s", code, body)
	}
	if _, body := h.htmlGet("/objective", ""); !strings.Contains(body, "计算机基础") {
		t.Fatalf("list lost the set: %s", body)
	}

	// 第一题对、第二题错：1/2。
	code, body := h.postAs(user, path+"/answer", url.Values{"q1": {"1"}, "q2": {"2"}})
	if code != http.StatusOK || !strings.Contains(body, "1 / 2") ||
		!strings.Contains(body, "答对") || !strings.Contains(body, "你选 C，正确 A") {
		t.Fatalf("first attempt = %d %s", code, body)
	}
	// 再交一次：全对，并记录第 2 次。
	code, body = h.postAs(user, path+"/answer", url.Values{"q1": {"1"}, "q2": {"0"}})
	if code != http.StatusOK || !strings.Contains(body, "2 / 2") || !strings.Contains(body, "第 2 次作答") {
		t.Fatalf("second attempt = %d %s", code, body)
	}
	// 越界的选项按未答算。
	if _, body := h.postAs(user, path+"/answer", url.Values{"q1": {"99"}, "q2": {"0"}}); !strings.Contains(body, "1 / 2") {
		t.Fatalf("out-of-range should be wrong: %s", body)
	}

	// 作者可以改题；改完已有作答按新题重判。
	if code, body := h.postAs(author, path, objectiveValues("计算机基础（改）", "新题？|是|否|A", true)); code != http.StatusOK ||
		!strings.Contains(body, "计算机基础（改）") || !strings.Contains(body, "新题？") {
		t.Fatalf("update = %d %s", code, body)
	}
	if code, _ := h.postAs(user, path, objectiveValues("抢改", "x|y|z|A", true)); code != http.StatusForbidden {
		t.Fatalf("foreign update = %d", code)
	}
	// 旧作答（第一题选 B）在新题（答案 A）上重算为 0/1。
	if _, body := h.getAs(user, path); !strings.Contains(body, "0 / 1") {
		t.Fatalf("stale attempt was not retallied:\n%s", body)
	}

	// 坏行整张拒收，且不落库。
	before := latestObjectiveID(t, h)
	if code, body := h.postAs(author, "/objective", objectiveValues("坏题", "只有题目|A", true)); code != http.StatusBadRequest ||
		!strings.Contains(body, "至少要") {
		t.Fatalf("bad questions = %d %s", code, body)
	}
	if after := latestObjectiveID(t, h); after != before {
		t.Fatalf("rejected set was stored: %d -> %d", before, after)
	}

	// 删除后列表里不再有它。
	if code, body := h.postAs(author, path+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "客观题已删除") {
		t.Fatalf("delete = %d %s", code, body)
	}
	if code, _ := h.htmlGet(path, ""); code != http.StatusNotFound {
		t.Fatalf("deleted set still readable: %d", code)
	}
}

func TestObjectiveHiddenSetStaysPrivate(t *testing.T) {
	h := newHarness(t)
	author := h.signIn("obj_hidden_author", permProblemCreate)
	other := h.signIn("obj_hidden_other")

	if code, body := h.postAs(author, "/objective",
		objectiveValues("内部自测", "2+2？|3|4|B", false)); code != http.StatusOK {
		t.Fatalf("create hidden = %d %s", code, body)
	}
	id := latestObjectiveID(t, h)
	path := "/objective/" + strconv.FormatInt(id, 10)
	if code, _ := h.getAs(other, path); code != http.StatusNotFound {
		t.Fatalf("foreign hidden read = %d", code)
	}
	if _, body := h.getAs(other, "/objective"); strings.Contains(body, "内部自测") {
		t.Fatalf("hidden set leaked into the list:\n%s", body)
	}
	if _, body := h.getAs(author, path); !strings.Contains(body, "内部自测") {
		t.Fatalf("author lost the hidden set: %s", body)
	}
	if code, _ := h.postAs(other, path+"/answer", url.Values{"q1": {"1"}}); code != http.StatusNotFound {
		t.Fatalf("foreign hidden answer = %d", code)
	}
}
