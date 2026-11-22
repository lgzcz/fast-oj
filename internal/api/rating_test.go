package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestContestRatingsApplyOnce(t *testing.T) {
	h := newHarness(t)
	host := h.signIn("ratehost", permContestCreate)
	alice := h.signIn("ratealice")
	bob := h.signIn("ratebob")
	h.readyProblem("PRATE")

	values := contestValues("CRATE", "计分赛")
	values.Set("rated", "1")
	if _, body := h.createContest(host, values); !strings.Contains(body, "比赛已建好") {
		t.Fatalf("create contest:\n%s", body)
	}
	h.mountContestProblems(host, "CRATE", "PRATE")
	h.joinAll("CRATE", alice, bob)
	cid := h.contestID("CRATE")
	h.submitAndJudge(alice, "PRATE", cid, "AC", 2)
	h.submitAndJudge(bob, "PRATE", cid, "WA", 0)

	// 把比赛移到过去，结算器只认 end_at。
	if _, err := h.db.SQL.ExecContext(ctx(),
		`UPDATE contests SET end_at=? WHERE cid='CRATE'`, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if n, err := h.db.ApplyPendingRatings(ctx(), 10); err != nil || n != 2 {
		t.Fatalf("apply ratings = %d, %v", n, err)
	}
	aliceRating := h.dbUserID(t, "ratealice")
	var a, b int
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT rating FROM users WHERE id=?`, aliceRating).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT rating FROM users WHERE handle='ratebob'`).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if a <= 1200 || b >= 1200 || a-b <= 0 {
		t.Fatalf("winner should rise and loser fall: alice=%d bob=%d", a, b)
	}
	// 再跑一次不再加分。
	if n, err := h.db.ApplyPendingRatings(ctx(), 10); err != nil || n != 0 {
		t.Fatalf("second apply = %d, %v", n, err)
	}
	var again int
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT rating FROM users WHERE handle='ratealice'`).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != a {
		t.Fatalf("rating changed on a second run: %d -> %d", a, again)
	}
	// 个人页显示 rating。
	if _, body := h.htmlGet("/u/ratealice", ""); !strings.Contains(body, "Rating") ||
		!strings.Contains(body, `>`+itoa(again)+`<`) {
		t.Fatalf("profile should show rating %d: %s", again, body)
	}
}

func TestVirtualContestAndWeeklyPage(t *testing.T) {
	h := newHarness(t)
	host := h.signIn("virtualhost", permContestCreate)
	h.readyProblem("PVIRT")
	values := contestValues("CVIRT", "虚拟练习赛")
	values.Set("weekly", "1")
	values.Set("start_at", time.Now().Add(-3*time.Hour).Format(contestLayoutMini))
	values.Set("end_at", time.Now().Add(-time.Hour).Format(contestLayoutMini))
	if _, body := h.createContest(host, values); !strings.Contains(body, "比赛已建好") {
		t.Fatalf("create contest:\n%s", body)
	}
	h.mountContestProblems(host, "CVIRT", "PVIRT")

	if _, body := h.htmlGet("/weekly", ""); !strings.Contains(body, "CVIRT") {
		t.Fatalf("weekly page should list the round:\n%s", body)
	}
	if _, body := h.htmlGet("/contests", ""); !strings.Contains(body, "周赛") {
		t.Fatalf("contest list should mark the round:\n%s", body)
	}

	if code, body := h.getAs(host, "/contests/CVIRT"); code != http.StatusOK ||
		!strings.Contains(body, "开始虚拟比赛") {
		t.Fatalf("ended contest should offer a virtual run: %d %s", code, body)
	}
	if code, body := h.postAs(host, "/contests/CVIRT/virtual/start", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "虚拟比赛 · 虚拟练习赛") {
		t.Fatalf("start virtual: %d %s", code, body)
	}
	var vid int64
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT id FROM virtual_contests ORDER BY id DESC LIMIT 1`).Scan(&vid); err != nil {
		t.Fatal(err)
	}

	// 窗口内的一条练习提交计入成绩。
	_, submitPage := h.postAs(host, "/problems/PVIRT/submit", url.Values{
		"language": {"cpp"}, "source": {"int main(){}"},
	})
	if !strings.Contains(submitPage, "提交 #") {
		t.Fatalf("practice submit: %s", submitPage)
	}
	h.scoreSubmission(h.submissionID(submitPage), "AC", 2)
	if code, body := h.getAs(host, "/virtual/"+itoa64(vid)); code != http.StatusOK ||
		!strings.Contains(body, "PVIRT") || !strings.Contains(body, "AC") {
		t.Fatalf("virtual page: %d %s", code, body)
	}

	// 提前结束再点一次不会回到进行中。
	if code, body := h.postAs(host, "/virtual/"+itoa64(vid)+"/finish", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "成绩定格") {
		t.Fatalf("finish: %d %s", code, body)
	}
	// 陌生人不能看别人的虚拟赛。
	stranger := h.signIn("virtualpeek")
	if code, _ := h.getAs(stranger, "/virtual/"+itoa64(vid)); code != http.StatusNotFound {
		t.Fatalf("foreign virtual run: %d", code)
	}
}
