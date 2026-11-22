package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func latestTrainingID(t *testing.T, h *harness) int64 {
	t.Helper()
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM trainings ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTrainings(t *testing.T) {
	h := newHarness(t)
	owner := h.signIn("trainer", permProblemCreate)
	other := h.signIn("reader")

	for _, pid := range []string{"TR1", "TR2"} {
		if code, body := h.createProblem(problemValues(pid, "题单题 "+pid)); code != http.StatusOK {
			t.Fatalf("create problem %s: %d %s", pid, code, body)
		}
	}

	if code, body := h.postAs(owner, "/trainings", url.Values{
		"title": {"入门十题"}, "intro": {"从模拟开始。"}, "visible": {"1"},
	}); code != http.StatusOK || !strings.Contains(body, "入门十题") {
		t.Fatalf("create training: %d %s", code, body)
	}
	id := latestTrainingID(t, h)

	if code, body := h.postAs(owner, "/trainings/"+strconv.FormatInt(id, 10)+"/problems", url.Values{
		"problem": {"TR1"},
	}); code != http.StatusOK || !strings.Contains(body, "已加入 TR1") {
		t.Fatalf("add problem: %d %s", code, body)
	}
	if code, body := h.getAs(other, "/trainings/"+strconv.FormatInt(id, 10)); code != http.StatusOK ||
		!strings.Contains(body, "TR1") || !strings.Contains(body, "未做") {
		t.Fatalf("public training detail: %d %s", code, body)
	}

	// 隐藏题单不出现在别人的列表里，直接访问也是 404；作者自己仍看得到。
	if code, body := h.postAs(owner, "/trainings", url.Values{
		"title": {"草稿题单"}, "visible": {"0"},
	}); code != http.StatusOK || !strings.Contains(body, "草稿题单") {
		t.Fatalf("create hidden training: %d %s", code, body)
	}
	hiddenID := latestTrainingID(t, h)
	if _, body := h.getAs(other, "/trainings"); strings.Contains(body, "草稿题单") {
		t.Fatalf("hidden training leaked into list: %s", body)
	}
	if code, _ := h.getAs(other, "/trainings/"+strconv.FormatInt(hiddenID, 10)); code != http.StatusNotFound {
		t.Fatalf("hidden training detail: %d", code)
	}
	if code, body := h.getAs(owner, "/trainings/"+strconv.FormatInt(hiddenID, 10)); code != http.StatusOK ||
		!strings.Contains(body, "草稿题单") {
		t.Fatalf("owner hidden training: %d %s", code, body)
	}

	// 别人没公开的题不能挂进题单，不然公开题单会把隐藏题的标题带出去。
	if code, body := h.createProblem(url.Values{
		"pid": {"TRH"}, "title": {"隐藏题"}, "visible": {"0"},
	}); code != http.StatusOK {
		t.Fatalf("create hidden problem: %d %s", code, body)
	}
	if _, body := h.postAs(owner, "/trainings/"+strconv.FormatInt(id, 10)+"/problems", url.Values{
		"problem": {"TRH"},
	}); !strings.Contains(body, "没公开") {
		t.Fatalf("hidden problem should be refused: %s", body)
	}
	if _, body := h.getAs(other, "/trainings/"+strconv.FormatInt(id, 10)); strings.Contains(body, "TRH") {
		t.Fatalf("hidden problem leaked into training: %s", body)
	}

	// 移出一道题只影响题单，题目本身还在。
	if code, body := h.postAs(owner, "/trainings/"+strconv.FormatInt(id, 10)+"/problems/TR1/remove", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "已移出 TR1") {
		t.Fatalf("remove problem: %d %s", code, body)
	}
	if _, err := h.db.FindProblem(ctx(), "TR1"); err != nil {
		t.Fatalf("problem should survive removal: %v", err)
	}

	// 陌生账号不能改别人的题单。
	if code, _ := h.postAs(other, "/trainings/"+strconv.FormatInt(id, 10), url.Values{"title": {"劫持"}}); code != http.StatusForbidden {
		t.Fatalf("foreign update: %d", code)
	}
}

func TestTrainingSolvedProgress(t *testing.T) {
	h := newHarness(t)
	owner := h.signIn("trainer2", permProblemCreate)
	if code, body := h.createProblem(problemValues("TRP", "进度题")); code != http.StatusOK {
		t.Fatalf("create problem: %d %s", code, body)
	}
	p, err := h.db.FindProblem(ctx(), "TRP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.SQL.ExecContext(ctx(),
		`INSERT INTO submissions (user_id, problem_id, language, source, status, verdict, total_score, created_at)
		 VALUES (?, ?, 'cpp', '', 'done', 'AC', 100, 1)`, h.dbUserID(t, "trainer2"), p.ID); err != nil {
		t.Fatal(err)
	}
	if code, body := h.postAs(owner, "/trainings", url.Values{
		"title": {"进度"}, "visible": {"1"},
	}); code != http.StatusOK {
		t.Fatalf("create training: %d %s", code, body)
	}
	id := latestTrainingID(t, h)
	if _, body := h.postAs(owner, "/trainings/"+strconv.FormatInt(id, 10)+"/problems", url.Values{"problem": {"TRP"}}); !strings.Contains(body, "已加入") {
		t.Fatalf("add problem: %s", body)
	}
	if code, body := h.getAs(owner, "/trainings/"+strconv.FormatInt(id, 10)); code != http.StatusOK ||
		!strings.Contains(body, "已通过 1 / 1 题") || !strings.Contains(body, "status-solved") {
		t.Fatalf("progress: %d %s", code, body)
	}
}

func (h *harness) dbUserID(t *testing.T, handle string) int64 {
	t.Helper()
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM users WHERE handle=?`, handle).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
