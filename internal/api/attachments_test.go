package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// uploadAttachment posts the multipart form the browser builds: hidden csrf plus
// one field named "file". The 303 is followed, so the returned body is the page
// with its flash message.
func (h *harness) uploadAttachment(s session, path, name string, body []byte) (int, string) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("csrf", s.csrf); err != nil {
		h.t.Fatal(err)
	}
	file, err := mw.CreateFormFile("file", name)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := file.Write(body); err != nil {
		h.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		h.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+path, &buf)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Cookie", s.cookie)
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readAll(h.t, resp)
}

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), []byte("not-a-real-png-body")...)

func TestProblemAttachments(t *testing.T) {
	h := newHarness(t)
	owner := h.signIn("attowner2500", permProblemCreate)
	other := h.signIn("attother2500")
	if code, body := h.postAs(owner, "/problems", problemValues("P2500", "带附件的题")); code != http.StatusOK {
		t.Fatalf("建题 = %d：\n%s", code, short(body))
	}

	if code, body := h.uploadAttachment(other, "/problems/P2500/attachments", "evil.png", pngBytes); code != http.StatusForbidden {
		t.Fatalf("非作者上传 = %d：\n%s", code, short(body))
	}
	// 一份 HTML 不能借附件变成同源页面。
	if code, body := h.uploadAttachment(owner, "/problems/P2500/attachments", "x.html", []byte("<html><script>alert(1)</script></html>")); code != http.StatusOK ||
		!strings.Contains(body, "这个类型不允许上传") {
		t.Fatalf("HTML 上传 = %d：\n%s", code, short(body))
	}
	// 上传时的路径与点号只会留下显示名。
	code, body := h.uploadAttachment(owner, "/problems/P2500/attachments", `..\..\说明书.png`, pngBytes)
	if code != http.StatusOK || !strings.Contains(body, "已上传附件 说明书.png") {
		t.Fatalf("上传 = %d：\n%s", code, short(body))
	}
	if strings.Contains(body, `..\..`) {
		t.Errorf("页面里留下了路径：\n%s", short(body))
	}
	var pid, attID int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM problems WHERE pid='P2500'`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM attachments WHERE owner_kind='problem' AND owner_id=?`, pid).Scan(&attID); err != nil {
		t.Fatal(err)
	}
	_, ownerPage := h.getAs(owner, "/problems/P2500")
	if !strings.Contains(ownerPage, "/attachments/") || !strings.Contains(ownerPage, "说明书.png") ||
		!strings.Contains(ownerPage, "上传附件") {
		t.Fatalf("作者看到的题面没有附件段（pid=%d att=%d）：\n%s", pid, attID, short(ownerPage))
	}
	_, page := h.htmlGet("/problems/P2500", "")
	if !strings.Contains(page, "/attachments/") || !strings.Contains(page, "说明书.png") ||
		strings.Contains(page, "上传附件") {
		t.Fatalf("读者看到的题面附件段不对：\n%s", short(page))
	}

	// 公开题：匿名也能下载，图片以 inline 给出。
	code, body, hdr := h.html(http.MethodGet, "/attachments/"+strconv.FormatInt(attID, 10), "", nil, "")
	if code != http.StatusOK || !bytes.Equal([]byte(body), pngBytes) {
		t.Fatalf("匿名下载 = %d，body=%q", code, body)
	}
	if hdr.Get("Content-Type") != "image/png" || !strings.HasPrefix(hdr.Get("Content-Disposition"), "inline") {
		t.Errorf("响应头 = %v", hdr)
	}

	// 未公开之后，陌生人拿到 404，作者仍能下载。
	if _, err := h.db.SQL.ExecContext(ctx(), `UPDATE problems SET visible=0 WHERE pid='P2500'`); err != nil {
		t.Fatal(err)
	}
	if code, _ := h.htmlGet("/attachments/"+strconv.FormatInt(attID, 10), ""); code != http.StatusNotFound {
		t.Errorf("未公开题的匿名下载 = %d", code)
	}
	if code, _ := h.htmlWithCookie(http.MethodGet, "/attachments/"+strconv.FormatInt(attID, 10), owner.cookie); code != http.StatusOK {
		t.Errorf("作者下载 = %d", code)
	}

	// 删除也要作者/编辑权。
	if code, _ := h.postAs(other, "/problems/P2500/attachments/"+strconv.FormatInt(attID, 10)+"/delete", url.Values{}); code != http.StatusForbidden {
		t.Errorf("非作者删除 = %d", code)
	}
	if code, page := h.postAs(owner, "/problems/P2500/attachments/"+strconv.FormatInt(attID, 10)+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(page, "已删除附件") {
		t.Fatalf("作者删除 = %d：\n%s", code, short(page))
	}
	if code, _ := h.htmlGet("/attachments/"+strconv.FormatInt(attID, 10), ""); code != http.StatusNotFound {
		t.Errorf("删除后还能下载 = %d", code)
	}
}

func TestTicketAttachmentsStayPrivate(t *testing.T) {
	h := newHarness(t)
	author := h.signIn("tkatt2501")
	other := h.signIn("tkatt2502")
	id, code, body := h.fileTicket(author, url.Values{"body": {"截图在附件里"}})
	if code != http.StatusSeeOther {
		t.Fatalf("工单 = %d：\n%s", code, short(body))
	}
	path := "/tickets/" + strconv.FormatInt(id, 10) + "/attachments"
	if code, body := h.uploadAttachment(author, path, "shot.png", pngBytes); code != http.StatusOK ||
		!strings.Contains(body, "已上传附件 shot.png") {
		t.Fatalf("工单附件上传 = %d：\n%s", code, short(body))
	}
	var attID int64
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT id FROM attachments WHERE owner_kind='ticket' AND owner_id=?`, id).Scan(&attID); err != nil {
		t.Fatal(err)
	}
	link := "/attachments/" + strconv.FormatInt(attID, 10)
	if code, _ := h.htmlWithCookie(http.MethodGet, link, author.cookie); code != http.StatusOK {
		t.Errorf("作者下载 = %d", code)
	}
	// 匿名先被送去登录，而不是被告知附件存在。
	if code, page := h.htmlGet(link, ""); code != http.StatusOK || !strings.Contains(page, "登录") {
		t.Errorf("匿名工单附件 = %d：\n%s", code, short(page))
	}
	if code, _ := h.htmlWithCookie(http.MethodGet, link, other.cookie); code != http.StatusNotFound {
		t.Errorf("陌生人下载 = %d", code)
	}
}
