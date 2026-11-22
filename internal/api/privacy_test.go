package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func setPrivacy(t *testing.T, h *harness, s session, academic, stealth bool) {
	t.Helper()
	v := url.Values{"dm_policy": {"everyone"}}
	if academic {
		v.Set("academic_mode", "1")
	}
	if stealth {
		v.Set("stealth", "1")
	}
	if code, body := h.postAs(s, "/settings/privacy", v); code != http.StatusOK ||
		!strings.Contains(body, "已保存隐私设置") {
		t.Fatalf("save privacy = %d %s", code, body)
	}
}

func TestAcademicModeHidesSocialProfile(t *testing.T) {
	h := newHarness(t)
	alice := h.signIn("acad_alice")
	bob := h.signIn("acad_bob")
	h.readyProblem("PACAD")
	h.postAs(alice, "/feed", url.Values{"body": {"学术模式下的动态"}})
	h.postAs(alice, "/problems/PACAD/submit", url.Values{"language": {"cpp"}, "source": {"int main(){}"}})

	setPrivacy(t, h, alice, true, false)
	// 设置页读回勾选状态。
	if _, page := h.getAs(alice, "/settings"); !strings.Contains(page, `name="academic_mode" value="1" checked`) {
		t.Fatalf("settings did not keep academic mode:\n%s", short(page))
	}

	// 访客看不到动态、最近提交与关注/粉丝；本人仍然看得到。
	_, public := h.getAs(bob, "/u/acad_alice")
	for _, gone := range []string{"TA 的动态", "最近提交", "粉丝 <b>"} {
		if strings.Contains(public, gone) {
			t.Errorf("academic profile still shows %q:\n%s", gone, short(public))
		}
	}
	if !strings.Contains(public, "Rating") || !strings.Contains(public, "判定分布") {
		t.Errorf("academic profile lost the academic half:\n%s", short(public))
	}
	if _, own := h.getAs(alice, "/u/acad_alice"); !strings.Contains(own, "最近提交") {
		t.Errorf("the owner should still see their own recent submissions:\n%s", short(own))
	}
	// 学术模式只改个人页；公开提交列表不受影响（这一点写进了 docs/community.md）。
	if _, list := h.getAs(bob, "/submissions?user=acad_alice"); !strings.Contains(list, "PACAD") {
		t.Errorf("submission list should stay public:\n%s", short(list))
	}
}

func TestStealthHidesActivityDuringContest(t *testing.T) {
	h := newHarness(t)
	alice := h.signIn("stealth_alice")
	bob := h.signIn("stealth_bob")
	host := h.signIn("stealth_host", permContestCreate)
	h.readyProblem("PSTEALTH")

	h.postAs(alice, "/feed", url.Values{"body": {"隐身期的动态"}})
	h.postAs(alice, "/problems/PSTEALTH/submit", url.Values{"language": {"cpp"}, "source": {"int main(){}"}})
	setPrivacy(t, h, alice, false, true)

	// 没有比赛时一切照常。
	if _, body := h.getAs(bob, "/feed"); !strings.Contains(body, "隐身期的动态") {
		t.Fatalf("feed before contest lost the post:\n%s", short(body))
	}
	if _, body := h.getAs(bob, "/submissions?user=stealth_alice"); !strings.Contains(body, "PSTEALTH") {
		t.Fatalf("submissions before contest lost the row:\n%s", short(body))
	}

	if code, body := h.createContest(host, contestValues("CSTEALTH", "隐身测试赛")); code != http.StatusOK {
		t.Fatalf("create contest = %d %s", code, short(body))
	}
	if _, body := h.getAs(bob, "/feed"); strings.Contains(body, "隐身期的动态") {
		t.Fatalf("feed during contest still shows stealth post:\n%s", short(body))
	}
	if _, body := h.getAs(bob, "/submissions?user=stealth_alice"); strings.Contains(body, "PSTEALTH") {
		t.Fatalf("submission list during contest still shows stealth row:\n%s", short(body))
	}
	if _, body := h.getAs(bob, "/u/stealth_alice"); strings.Contains(body, "最近提交") ||
		strings.Contains(body, "TA 的动态") {
		t.Fatalf("profile during contest still shows stealth activity:\n%s", short(body))
	}
	// 本人与全见权限不受影响。
	if _, body := h.getAs(alice, "/submissions?user=stealth_alice"); !strings.Contains(body, "PSTEALTH") {
		t.Fatalf("owner lost their own submission:\n%s", short(body))
	}
	if _, body := h.getAs(alice, "/u/stealth_alice"); !strings.Contains(body, "最近提交") {
		t.Fatalf("owner lost their own profile activity:\n%s", short(body))
	}
	if _, err := h.db.SQL.ExecContext(ctx(),
		`UPDATE contests SET end_at=? WHERE cid='CSTEALTH'`, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, body := h.getAs(bob, "/feed"); !strings.Contains(body, "隐身期的动态") {
		t.Fatalf("feed did not come back after the contest:\n%s", short(body))
	}
}
