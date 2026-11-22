package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestReportsAndBlocks(t *testing.T) {
	h := newHarness(t)
	author := h.signIn("postauthor")
	reporter := h.signIn("reporter1")
	mod := h.signIn("reportmod", permTopicModerate)

	if code, body := h.postAs(author, "/topics", url.Values{
		"kind": {"community"}, "title": {"一条帖子"}, "body": {"内容"},
	}); code != http.StatusOK || !strings.Contains(body, "一条帖子") {
		t.Fatalf("create topic: %d %s", code, body)
	}
	var topicID int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM topics ORDER BY id DESC LIMIT 1`).Scan(&topicID); err != nil {
		t.Fatal(err)
	}

	if _, body := h.postAs(reporter, "/reports", url.Values{
		"target_kind": {"topic"}, "target_id": {strconv.FormatInt(topicID, 10)},
		"reason": {"spam"}, "detail": {"重复广告"},
	}); !strings.Contains(body, "已举报") {
		t.Fatalf("report topic: %s", body)
	}
	if _, body := h.postAs(reporter, "/reports", url.Values{
		"target_kind": {"topic"}, "target_id": {strconv.FormatInt(topicID, 10)},
		"reason": {"spam"},
	}); !strings.Contains(body, "已经举报过") {
		t.Fatalf("duplicate report: %s", body)
	}
	if code, _ := h.getAs(reporter, "/admin/reports"); code != http.StatusForbidden {
		t.Fatalf("plain user reached the queue: %d", code)
	}
	if code, body := h.getAs(mod, "/admin/reports"); code != http.StatusOK ||
		!strings.Contains(body, "一条帖子") || !strings.Contains(body, "垃圾广告") {
		t.Fatalf("queue: %d %s", code, body)
	}
	var reportID int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM reports ORDER BY id DESC LIMIT 1`).Scan(&reportID); err != nil {
		t.Fatal(err)
	}
	if code, body := h.postAs(mod, "/admin/reports/"+strconv.FormatInt(reportID, 10)+"/close", url.Values{
		"resolution": {"已提醒"},
	}); code != http.StatusOK || !strings.Contains(body, "已处理") {
		t.Fatalf("close report: %d %s", code, body)
	}
	if _, body := h.getAs(mod, "/admin/reports?status=closed"); !strings.Contains(body, "已提醒") {
		t.Fatalf("closed queue: %s", body)
	}

	// 拉黑：互相私信与关注都关闭，解除后恢复。
	if _, body := h.postAs(reporter, "/u/postauthor/block", url.Values{}); !strings.Contains(body, "已拉黑") {
		t.Fatalf("block: %s", body)
	}
	if _, body := h.postAs(author, "/messages/reporter1", url.Values{"body": {"hi"}}); !strings.Contains(body, "拉黑") {
		t.Fatalf("blocked DM: %s", body)
	}
	if _, body := h.postAs(author, "/u/reporter1/follow", url.Values{}); !strings.Contains(body, "不能关注") {
		t.Fatalf("blocked follow: %s", body)
	}
	if _, body := h.postAs(reporter, "/u/postauthor/unblock", url.Values{}); !strings.Contains(body, "已解除拉黑") {
		t.Fatalf("unblock: %s", body)
	}
	if _, body := h.postAs(author, "/messages/reporter1", url.Values{"body": {"hi"}}); !strings.Contains(body, "已发送") {
		t.Fatalf("DM after unblock: %s", body)
	}
}
