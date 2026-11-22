package api

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="

func (h *harness) uploadImage(s session, path, filename string, body []byte, follow bool) (int, string) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("csrf", s.csrf); err != nil {
		h.t.Fatal(err)
	}
	file, err := mw.CreateFormFile("file", filename)
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
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if follow && resp.StatusCode == http.StatusSeeOther {
		code, page := h.htmlWithCookie(http.MethodGet, resp.Header.Get("Location"), s.cookie)
		return code, page
	}
	return resp.StatusCode, readAll(h.t, resp)
}

func TestProfileImagesAndImageHost(t *testing.T) {
	h := newHarness(t)
	user := h.signIn("picturer")
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}

	// 头像：multipart 上传后设置页带出预览，个人页也引用同一张。
	code, body := h.uploadImage(user, "/settings/avatar", "me.png", png, true)
	if code != http.StatusOK || !strings.Contains(body, "头像已更新") || !strings.Contains(body, "/images/") {
		t.Fatalf("avatar upload: %d %s", code, body)
	}
	var imageID int64
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT id FROM images WHERE kind='avatar' ORDER BY id DESC LIMIT 1`).Scan(&imageID); err != nil {
		t.Fatal(err)
	}
	code, _ = h.htmlGet("/images/"+strconv.FormatInt(imageID, 10), "")
	if code != http.StatusOK {
		t.Fatalf("serve image: %d", code)
	}
	var storedMIME string
	if err := h.db.SQL.QueryRowContext(ctx(), `SELECT mime FROM images WHERE id=?`, imageID).Scan(&storedMIME); err != nil {
		t.Fatal(err)
	}
	if storedMIME != "image/png" {
		t.Fatalf("mime = %q", storedMIME)
	}
	if _, profile := h.htmlGet("/u/picturer", ""); !strings.Contains(profile, "/images/"+itoa64(imageID)) {
		t.Fatalf("profile should show avatar: %s", profile)
	}

	// 按内容识别：文本改名成 .png 也要拒。
	code, _ = h.uploadImage(user, "/settings/avatar", "fake.png", []byte("hello, not an image"), false)
	if code != http.StatusBadRequest {
		t.Fatalf("fake image: %d", code)
	}

	// 图床：上传正文图、列表、删除。
	code, body = h.uploadImage(user, "/images/upload", "post.png", png, true)
	if code != http.StatusOK || !strings.Contains(body, "已上传") || !strings.Contains(body, "正文图") {
		t.Fatalf("post image: %d %s", code, body)
	}
	var postID int64
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT id FROM images WHERE kind='post' ORDER BY id DESC LIMIT 1`).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if code, body := h.postAs(user, "/images/"+strconv.FormatInt(postID, 10)+"/delete", url.Values{}); code != http.StatusOK ||
		!strings.Contains(body, "已删除") {
		t.Fatalf("delete image: %d %s", code, body)
	}
	// 不属于自己的图不能删。
	other := h.signIn("pictures2")
	if code, _ := h.postAs(other, "/images/"+strconv.FormatInt(imageID, 10)+"/delete", url.Values{}); code != http.StatusNotFound {
		t.Fatalf("foreign delete: %d", code)
	}
}
