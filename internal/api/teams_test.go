package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func latestGroupID(t *testing.T, h *harness) int64 {
	t.Helper()
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM teams ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTeamsAndPrivateBank(t *testing.T) {
	h := newHarness(t)
	captain := h.signIn("captain")
	member := h.signIn("member1")
	stranger := h.signIn("stranger1")
	for _, pid := range []string{"TM1", "TM2"} {
		if code, body := h.createProblem(problemValues(pid, "团队题 "+pid)); code != http.StatusOK {
			t.Fatalf("create %s: %d %s", pid, code, body)
		}
	}
	if code, body := h.createProblem(url.Values{
		"pid": {"TMH"}, "title": {"隐藏团队题"}, "visible": {"0"},
	}); code != http.StatusOK {
		t.Fatalf("create hidden problem: %d %s", code, body)
	}

	if code, body := h.postAs(captain, "/teams", url.Values{
		"name": {"Alpha"}, "intro": {"一起做题。"}, "join_code": {"CODE1"},
	}); code != http.StatusOK || !strings.Contains(body, "Alpha") || !strings.Contains(body, "CODE1") {
		t.Fatalf("create team: %d %s", code, body)
	}
	id := latestGroupID(t, h)
	teamPath := "/teams/" + itoa64(id)

	// 公开页看得到成员与简介，但看不到团队题库。
	if code, body := h.getAs(stranger, teamPath); code != http.StatusOK ||
		!strings.Contains(body, "一起做题") || !strings.Contains(body, "加入") {
		t.Fatalf("public team page: %d %s", code, body)
	}
	if _, body := h.getAs(stranger, teamPath); strings.Contains(body, "加题</h2>") {
		t.Fatalf("private bank leaked to stranger: %s", body)
	}
	// 重名拒绝。
	if _, body := h.postAs(member, "/teams", url.Values{"name": {"alpha"}, "intro": {}}); !strings.Contains(body, "已经存在") {
		t.Fatalf("duplicate name: %s", body)
	}

	// 加入码不对进不去，对了才进。
	if code, _ := h.postAs(member, teamPath+"/join", url.Values{"code": {"NOPE"}}); code != http.StatusForbidden {
		t.Fatalf("wrong join code: %d", code)
	}
	if code, body := h.postAs(member, teamPath+"/join", url.Values{"code": {"CODE1"}}); code != http.StatusOK ||
		!strings.Contains(body, "团队题库") {
		t.Fatalf("join: %d %s", code, body)
	}

	// 队员能加公开题；隐藏题只有作者能加。
	if _, body := h.postAs(member, teamPath+"/problems", url.Values{"problem": {"TM1"}}); !strings.Contains(body, "已加入 TM1") {
		t.Fatalf("add problem: %s", body)
	}
	if _, body := h.postAs(member, teamPath+"/problems", url.Values{"problem": {"TMH"}}); !strings.Contains(body, "没公开") {
		t.Fatalf("hidden problem should be refused: %s", body)
	}
	if _, body := h.getAs(stranger, teamPath); strings.Contains(body, "TM1") {
		t.Fatalf("bank problem leaked to stranger: %s", body)
	}
	if code, body := h.getAs(member, teamPath); code != http.StatusOK ||
		!strings.Contains(body, "TM1") || !strings.Contains(body, "已通过 0 / 1 题") {
		t.Fatalf("member bank: %d %s", code, body)
	}

	// 只有队长能改设置与删题、移人。
	if code, _ := h.postAs(member, teamPath, url.Values{"name": {"Hijack"}}); code != http.StatusForbidden {
		t.Fatalf("member update: %d", code)
	}
	if code, _ := h.postAs(member, teamPath+"/problems/TM1/remove", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("member remove problem: %d", code)
	}
	if _, body := h.postAs(captain, teamPath+"/problems/TM1/remove", url.Values{}); !strings.Contains(body, "已移出 TM1") {
		t.Fatalf("captain remove problem: %s", body)
	}
	if code, body := h.postAs(captain, teamPath+"/members/member1/remove", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "已移出 member1") {
		t.Fatalf("remove member: %d %s", code, body)
	}
	if _, body := h.getAs(member, teamPath); strings.Contains(body, "加题</h2>") {
		t.Fatalf("removed member still sees bank: %s", body)
	}

	// 队长不能退出；删团队只删团队。
	if code, _ := h.postAs(captain, teamPath+"/leave", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("captain leave: %d", code)
	}
	if code, body := h.postAs(captain, teamPath+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "团队已删除") {
		t.Fatalf("delete team: %d %s", code, body)
	}
	if _, err := h.db.FindProblem(ctx(), "TM1"); err != nil {
		t.Fatalf("problem should survive team deletion: %v", err)
	}
}
