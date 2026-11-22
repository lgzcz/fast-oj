package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestMentionHandles(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"@alice 看一下", []string{"alice"}},
		{"请 (@bob) 回答", []string{"bob"}},
		{"alice@example.com 不是提及", nil},
		{"@ALICE @alice", []string{"ALICE"}},
		{"@alice. 句号不是名字的一部分", []string{"alice."}},
		{"@@", nil},
	}
	for _, c := range cases {
		got := mentionHandles(c.text)
		if len(got) != len(c.want) {
			t.Fatalf("mentionHandles(%q) = %v, want %v", c.text, got, c.want)
		}
		for i := range got {
			if !strings.EqualFold(got[i], c.want[i]) {
				t.Errorf("mentionHandles(%q)[%d] = %q, want %q", c.text, i, got[i], c.want[i])
			}
		}
	}
}

// 一条评论最多产生两条通知：楼主收到"回复"，被 @ 的人收到"提及"；
// 同一个人不会因为两种理由各收一条。
func TestNotificationsForReplyAndMention(t *testing.T) {
	h := newHarness(t)
	alice := h.signIn("alice2200")
	bob := h.signIn("bob2200")

	code, body := h.createTopic(alice, url.Values{
		"kind": {"community"}, "title": {"集合讨论"},
		"body": {"请 @bob2200 来看。顺带一提 alice@example.com 不是提及，@ghost2200 也不存在。"},
	})
	if code != http.StatusOK || !strings.Contains(body, "集合讨论") {
		t.Fatalf("发帖 = %d：\n%s", code, short(body))
	}
	var topicID int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM topics WHERE title='集合讨论'`).Scan(&topicID); err != nil {
		t.Fatal(err)
	}

	// 被 @ 的 bob 在导航栏看到角标，通知页写清是谁、在哪里提到的。
	if n, err := h.db.CountNotificationsUnread(ctx(), mustUserID(t, h, "bob2200")); err != nil || n != 1 {
		t.Fatalf("bob 未读 = %d %v", n, err)
	}
	_, nav := h.getAs(bob, "/topics")
	if !strings.Contains(nav, `href="/notifications"`) || !strings.Contains(nav, `badge bad">1`) {
		t.Errorf("导航栏没有通知角标：\n%s", short(nav))
	}
	code, notices := h.getAs(bob, "/notifications")
	if code != http.StatusOK || !strings.Contains(notices, "提到了你") ||
		!strings.Contains(notices, "集合讨论") || !strings.Contains(notices, "未读") {
		t.Fatalf("bob 通知页 = %d：\n%s", code, short(notices))
	}

	// 楼主被 @ 时不会同时收到"回复"与"提及"两条；评论里的 @ 就是那条通知。
	if code, body := h.postAs(bob, "/topics/"+strconv.FormatInt(topicID, 10)+"/comment",
		url.Values{"body": {"我来了，@alice2200 你也看看。"}}); code != http.StatusOK {
		t.Fatalf("评论 = %d：\n%s", code, short(body))
	}
	var aliceNotices int
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT COUNT(*) FROM notifications n JOIN users u ON u.id=n.user_id
		 WHERE u.handle='alice2200'`).Scan(&aliceNotices); err != nil {
		t.Fatal(err)
	}
	if aliceNotices != 1 {
		t.Errorf("alice 收到 %d 条通知，应该只有 comment 触发的那一条", aliceNotices)
	}
	_, alicePage := h.getAs(alice, "/notifications")
	if !strings.Contains(alicePage, "回复了你的帖子") || !strings.Contains(alicePage, "集合讨论") {
		t.Errorf("楼主没看到回复通知：\n%s", short(alicePage))
	}

	// 动态里的 @ 也算。
	if code, body := h.postAs(bob, "/feed", url.Values{"body": {"@alice2200 看这个"}}); code != http.StatusOK ||
		!strings.Contains(body, "已发布") {
		t.Fatalf("发动态 = %d：\n%s", code, short(body))
	}
	if _, page := h.getAs(alice, "/notifications"); !strings.Contains(page, "动态") || !strings.Contains(page, "提到了你") {
		t.Errorf("动态提及没进通知页：\n%s", short(page))
	}

	// 全部标为已读之后角标清零。
	if code, page := h.postAs(alice, "/notifications/read", url.Values{}); code != http.StatusOK ||
		!strings.Contains(page, "都标成已读了") {
		t.Fatalf("标记已读 = %d：\n%s", code, short(page))
	}
	if n, err := h.db.CountNotificationsUnread(ctx(), mustUserID(t, h, "alice2200")); err != nil || n != 0 {
		t.Errorf("已读后未读 = %d %v", n, err)
	}
	_, after := h.getAs(alice, "/notifications")
	if strings.Contains(after, "未读</span>") {
		t.Errorf("已读之后还有未读标记：\n%s", short(after))
	}
}

func mustUserID(t *testing.T, h *harness, handle string) int64 {
	t.Helper()
	u, err := h.db.GetUserByHandle(ctx(), handle)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
