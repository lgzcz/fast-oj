package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func latestChatRoomID(t *testing.T, h *harness) int64 {
	t.Helper()
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM chat_rooms ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChatRoomsAndMessages(t *testing.T) {
	h := newHarness(t)
	owner := h.signIn("chatowner")
	reader := h.signIn("chatreader")
	mod := h.signIn("chatmod", permTopicModerate)

	if code, body := h.htmlGet("/chat", ""); code != http.StatusOK || !strings.Contains(body, "还没有房间") {
		t.Fatalf("empty chat = %d %s", code, body)
	}
	// 匿名发言要先去登录。
	code, _, hdr := h.postNoRedirect("/chat", url.Values{"name": {"匿名房"}}, "")
	if code != http.StatusSeeOther || !strings.HasPrefix(hdr.Get("Location"), "/login?") {
		t.Fatalf("anonymous room create = %d %q", code, hdr.Get("Location"))
	}
	if code, body := h.postAs(owner, "/chat", url.Values{"name": {"算法闲聊"}}); code != http.StatusOK ||
		!strings.Contains(body, "#算法闲聊") {
		t.Fatalf("create room = %d %s", code, body)
	}
	roomID := latestChatRoomID(t, h)
	path := "/chat/" + strconv.FormatInt(roomID, 10)

	if code, body := h.getAs(reader, path); code != http.StatusOK || !strings.Contains(body, "还没有人说话") {
		t.Fatalf("empty room = %d %s", code, body)
	}
	if code, body := h.postAs(reader, path, url.Values{"body": {"第一句话"}}); code != http.StatusOK ||
		!strings.Contains(body, "第一句话") {
		t.Fatalf("post message = %d %s", code, body)
	}
	// 未登录也能读。
	if _, body := h.htmlGet(path, ""); !strings.Contains(body, "第一句话") || strings.Contains(body, `name="body"`) {
		t.Fatalf("anonymous read = %s", body)
	}

	var msgID int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM chat_messages WHERE room_id=? ORDER BY id DESC LIMIT 1`, roomID).Scan(&msgID); err != nil {
		t.Fatal(err)
	}
	// 别人的消息删不掉；房主或版主可以。
	if code, _ := h.postAs(owner, "/chat/messages/"+strconv.FormatInt(msgID, 10)+"/delete", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("foreign message delete = %d", code)
	}
	if code, body := h.postAs(mod, "/chat/messages/"+strconv.FormatInt(msgID, 10)+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "已删除") || strings.Contains(body, "第一句话") {
		t.Fatalf("moderated message delete = %d %s", code, body)
	}
	// 房间只有房主或版主能删。
	if code, _ := h.postAs(reader, path+"/delete", url.Values{}); code != http.StatusForbidden {
		t.Fatalf("foreign room delete = %d", code)
	}
	if code, body := h.postAs(owner, path+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "房间已删除") {
		t.Fatalf("room delete = %d %s", code, body)
	}
}

func TestClipboardIsPrivateToItsOwner(t *testing.T) {
	h := newHarness(t)
	alice := h.signIn("clip_alice")
	bob := h.signIn("clip_bob")

	if code, body := h.htmlGet("/clipboard", ""); code != http.StatusOK || !strings.Contains(body, "<h1>登录</h1>") {
		t.Fatalf("anonymous clipboard = %d %s", code, body)
	}
	if code, body := h.postAs(alice, "/clipboard", url.Values{"name": {"草稿"}, "body": {"int main(){}"}}); code != http.StatusOK ||
		!strings.Contains(body, "已保存") || !strings.Contains(body, "int main(){}") {
		t.Fatalf("create = %d %s", code, body)
	}
	if _, body := h.getAs(bob, "/clipboard"); strings.Contains(body, "草稿") {
		t.Fatalf("bob sees alice's clipboard:\n%s", body)
	}
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM clipboards WHERE user_id=(SELECT id FROM users WHERE handle='clip_alice')`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := "/clipboard/" + strconv.FormatInt(id, 10)
	if code, _ := h.postAs(bob, path, url.Values{"name": {"偷看"}, "body": {"x"}}); code != http.StatusNotFound {
		t.Fatalf("foreign update = %d", code)
	}
	if code, body := h.postAs(alice, path, url.Values{"name": {"草稿2"}, "body": {"new body"}}); code != http.StatusOK ||
		!strings.Contains(body, "已更新") || !strings.Contains(body, "new body") {
		t.Fatalf("update = %d %s", code, body)
	}
	// 空名字拒收，且不新建。
	if code, body := h.postAs(alice, "/clipboard", url.Values{"name": {"  "}, "body": {"x"}}); code != http.StatusBadRequest ||
		!strings.Contains(body, "名字要 1..") {
		t.Fatalf("blank name = %d %s", code, body)
	}
	if code, body := h.postAs(alice, path+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "已删除") || strings.Contains(body, "new body") {
		t.Fatalf("delete = %d %s", code, body)
	}
}

func TestTopicPolls(t *testing.T) {
	h := newHarness(t)
	alice := h.signIn("pollalice")
	bob := h.signIn("pollbob")

	// 有投票的帖子。
	values := url.Values{
		"kind": {"community"}, "title": {"今天刷哪道"},
		"body":          {"投一票"},
		"poll_question": {"下一场打什么"},
		"poll_options":  {"图论\n数论\nDP"},
	}
	code, body := h.createTopic(alice, values)
	if code != http.StatusOK || !strings.Contains(body, "下一场打什么") ||
		!strings.Contains(body, "共 0 票") || !strings.Contains(body, `value="投票"`) {
		t.Fatalf("create poll topic = %d %s", code, body)
	}
	var id int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM topics WHERE title='今天刷哪道'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := "/topics/" + strconv.FormatInt(id, 10)

	// 匿名可以看结果，但投票要登录。
	if _, body := h.htmlGet(path, ""); !strings.Contains(body, "共 0 票") ||
		!strings.Contains(body, "登录</a> 后才能投票") {
		t.Fatalf("anonymous poll view = %s", body)
	}

	if code, body := h.postAs(alice, path+"/vote", url.Values{"option": {"1"}}); code != http.StatusOK ||
		!strings.Contains(body, "共 1 票") || !strings.Contains(body, "你已投票") {
		t.Fatalf("alice vote = %d %s", code, body)
	}
	if code, body := h.postAs(bob, path+"/vote", url.Values{"option": {"0"}}); code != http.StatusOK ||
		!strings.Contains(body, "共 2 票") {
		t.Fatalf("bob vote = %d %s", code, body)
	}
	// 改投是覆盖，不是新增。
	if code, body := h.postAs(alice, path+"/vote", url.Values{"option": {"0"}}); code != http.StatusOK ||
		!strings.Contains(body, "共 2 票") {
		t.Fatalf("revote = %d %s", code, body)
	}
	var zeroVotes, total int
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT SUM(option_index=0), COUNT(*) FROM poll_votes WHERE topic_id=?`, id).Scan(&zeroVotes, &total); err != nil {
		t.Fatal(err)
	}
	if zeroVotes != 2 || total != 2 {
		t.Fatalf("votes after revote = %d/%d", zeroVotes, total)
	}
	if code, _ := h.postAs(alice, path+"/vote", url.Values{"option": {"9"}}); code != http.StatusBadRequest {
		t.Fatalf("out-of-range vote = %d", code)
	}
	// 没投票的帖子不出现投票区。
	if _, body := h.createTopic(alice, url.Values{"kind": {"community"}, "title": {"普通贴"}, "body": {"x"}}); strings.Contains(body, `class="poll"`) {
		t.Fatalf("plain topic grew a poll:\n%s", body)
	}
	// 只有问题没有选项：整张贴不发。
	if code, body := h.createTopic(alice, url.Values{
		"kind": {"community"}, "title": {"坏投票"}, "body": {"x"}, "poll_question": {"选哪个"},
	}); code != http.StatusBadRequest || !strings.Contains(body, "投票至少要有 2 个选项") {
		t.Fatalf("question only = %d %s", code, body)
	}
	var n int
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT COUNT(*) FROM topics WHERE title='坏投票'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rejected poll still created %d topic(s)", n)
	}
}
