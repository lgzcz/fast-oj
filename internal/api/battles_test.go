package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func latestBattleID(t *testing.T, h *harness) int64 {
	t.Helper()
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM battles ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBattlePKBoard(t *testing.T) {
	h := newHarness(t)
	host := h.signIn("battlehost", permContestCreate)
	player := h.signIn("battleplayer")
	other := h.signIn("battleother")
	h.readyProblem("PBAT")

	now := time.Now()
	values := url.Values{
		"title": {"周末十分钟"}, "problem": {"PBAT"}, "visible": {"1"},
		"start_at": {now.Add(-time.Hour).Format(contestLayoutMini)},
		"end_at":   {now.Add(time.Hour).Format(contestLayoutMini)},
	}
	if code, body := h.postAs(host, "/battles", values); code != http.StatusOK ||
		!strings.Contains(body, "周末十分钟") {
		t.Fatalf("create battle: %d %s", code, body)
	}
	id := latestBattleID(t, h)
	path := "/battles/" + strconv.FormatInt(id, 10)

	if _, body := h.getAs(other, path); !strings.Contains(body, "还没有人加入") {
		t.Fatalf("empty board: %s", body)
	}
	if _, body := h.postAs(player, path+"/join", url.Values{}); !strings.Contains(body, "已加入") {
		t.Fatalf("join: %s", body)
	}
	if _, body := h.postAs(other, path+"/join", url.Values{}); !strings.Contains(body, "已加入") {
		t.Fatalf("second join: %s", body)
	}

	_, page := h.postAs(player, "/problems/PBAT/submit", url.Values{
		"language": {"cpp"}, "source": {"int main(){}"},
	})
	h.scoreSubmission(h.submissionID(page), "AC", 2)
	if code, body := h.getAs(player, path); code != http.StatusOK ||
		!strings.Contains(body, "battleplayer") || !strings.Contains(body, "2 / 2") {
		t.Fatalf("board: %d %s", code, body)
	}

	// 结束后不能再加入；榜单保留。
	if _, err := h.db.SQL.ExecContext(ctx(),
		`UPDATE battles SET end_at=? WHERE id=?`, now.Add(-time.Minute).UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
	late := h.signIn("battlelate")
	if code, _ := h.postAs(late, path+"/join", url.Values{}); code != http.StatusConflict {
		t.Fatalf("late join: %d", code)
	}
	if _, body := h.getAs(other, path); !strings.Contains(body, "已结束") {
		t.Fatalf("ended board: %s", body)
	}

	// 只有发起人或比赛管理员能删。
	if code, _ := h.postAs(player, path+"/delete", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("foreign delete: %d", code)
	}
	if code, body := h.postAs(host, path+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "对抗赛已删除") {
		t.Fatalf("delete: %d %s", code, body)
	}
}
