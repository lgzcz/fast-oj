package api

// 题目与工单附件：上传走各自页面的 multipart 表单，下载只有一个入口
// /attachments/{id}，在那里重新判一次"你能不能看这个东西"。文件本体在数据目录
// 下随机命名，页面上的名字只用于显示与 Content-Disposition。

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

const (
	// maxAttachmentSize 是单个附件的内建上限；部署方可以把 -max-upload 调得更小，
	// 不能把它调得更大——附件从来不是传大数据集的主路。
	maxAttachmentSize = 16 << 20
	// attachmentFormRoom 是 multipart 头与其它字段的余量。
	attachmentFormRoom = 1 << 20
)

// allowedAttachmentMIME 只认浏览器能安全展示或原样下载的类型。SVG、HTML 不在里面：
// 它们一旦 inline 就是同源页面，必须拒在存储之前。
var allowedAttachmentMIME = map[string]bool{
	"application/pdf": true,
	"application/zip": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"text/plain":      true,
}

func (s *Server) attachmentLimit() int64 {
	limit := int64(maxAttachmentSize)
	if cap := s.uploadLimit(); cap > 0 && cap < limit {
		limit = cap
	}
	return limit
}

func sizeText(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KB"
	default:
		return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MB"
	}
}

type attachmentRow struct {
	ID   int64
	Name string
	// Size is already human text (the template has no formatter).
	Size string
	When string
}

func attachmentRows(items []store.Attachment) []attachmentRow {
	out := make([]attachmentRow, 0, len(items))
	for _, a := range items {
		out = append(out, attachmentRow{ID: a.ID, Name: a.Name, Size: sizeText(a.Size), When: agoMS(a.CreatedAt)})
	}
	return out
}

// attachmentName 只留一个平坦的显示名：去掉任何目录、控制字符与首尾点。
// 它只进页面与 Content-Disposition，从不参与磁盘路径。
func attachmentName(raw string) string {
	raw = strings.ReplaceAll(raw, "\\", "/")
	name := path.Base(raw)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.Trim(name, "."))
	if name == "" {
		name = "附件"
	}
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}
	return name
}

// readAttachment reads the multipart file, enforces the size cap and decides the
// MIME by content, not by what the uploader typed.
func (s *Server) readAttachment(w http.ResponseWriter, r *http.Request) (name, mimeType string, body []byte, errMsg string) {
	limit := s.attachmentLimit()
	r.Body = http.MaxBytesReader(w, r.Body, limit+attachmentFormRoom)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return "", "", nil, "上传内容读不出来：附件最大 " + sizeText(limit)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", "", nil, "没有选文件"
	}
	defer file.Close()
	body, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return "", "", nil, "读取上传内容失败"
	}
	if len(body) == 0 {
		return "", "", nil, "空文件不算附件"
	}
	if int64(len(body)) > limit {
		return "", "", nil, "附件超过上限 " + sizeText(limit)
	}
	mediaType, _, err := mime.ParseMediaType(http.DetectContentType(body))
	if err != nil {
		return "", "", nil, "认不出这个文件的类型"
	}
	if !allowedAttachmentMIME[mediaType] {
		return "", "", nil, "这个类型不允许上传（只收 PDF、ZIP、纯文本与 PNG/JPEG/GIF/WebP 图片）"
	}
	return attachmentName(header.Filename), mediaType, body, ""
}

func (s *Server) saveAttachment(w http.ResponseWriter, r *http.Request, a actor, kind string, ownerID int64, back string) {
	name, mimeType, body, bad := s.readAttachment(w, r)
	if bad != "" {
		http.Redirect(w, r, back+"?msg="+url.QueryEscape(bad), http.StatusSeeOther)
		return
	}
	rel, err := s.attach.Save(kind, ownerID, body)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	id, err := s.db.CreateAttachment(r.Context(), &store.Attachment{
		OwnerKind: kind, OwnerID: ownerID, UploaderID: a.User.ID,
		Name: name, RelPath: rel, MIME: mimeType, Size: int64(len(body)),
	})
	if err != nil {
		_ = s.attach.Remove(rel)
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("attachment uploaded", "id", id, "kind", kind, "owner", ownerID, "by", a.User.Handle)
	http.Redirect(w, r, back+"?msg="+url.QueryEscape("已上传附件 "+name), http.StatusSeeOther)
}

func (s *Server) handleProblemAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	p, ok := s.problemByKey(w, r)
	if !ok {
		return
	}
	if !a.canEditProblem(p) {
		s.respondError(w, r, http.StatusForbidden, "只有题目作者或带 problem.edit.any 的人能加附件")
		return
	}
	if !s.ensureProblemEditable(w, r, a, p) {
		return
	}
	s.saveAttachment(w, r, a, store.AttachProblem, p.ID, "/problems/"+url.PathEscape(p.PID))
}

func (s *Server) handleProblemAttachmentDelete(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	p, ok := s.problemByKey(w, r)
	if !ok {
		return
	}
	if !a.canEditProblem(p) {
		s.respondError(w, r, http.StatusForbidden, "只有题目作者或带 problem.edit.any 的人能删附件")
		return
	}
	if !s.ensureProblemEditable(w, r, a, p) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusBadRequest, "附件编号得是数字")
		return
	}
	att, err := s.db.GetAttachment(r.Context(), id)
	if err != nil || att.OwnerKind != store.AttachProblem || att.OwnerID != p.ID {
		s.respondError(w, r, http.StatusNotFound, "没有这个附件")
		return
	}
	if err := s.db.DeleteAttachment(r.Context(), id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.attach.Remove(att.RelPath); err != nil {
		s.log.Error("attachment file remove", "id", id, "err", err)
	}
	s.log.Info("attachment deleted", "id", id, "pid", p.PID, "by", a.User.Handle)
	http.Redirect(w, r, "/problems/"+url.PathEscape(p.PID)+"?msg="+url.QueryEscape("已删除附件 "+att.Name), http.StatusSeeOther)
}

func (s *Server) handleTicketAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	a, tk, ok := s.ticketByID(w, r)
	if !ok {
		return
	}
	s.saveAttachment(w, r, a, store.AttachTicket, tk.ID, "/tickets/"+strconv.FormatInt(tk.ID, 10))
}

func (s *Server) handleTicketAttachmentDelete(w http.ResponseWriter, r *http.Request) {
	a, tk, ok := s.ticketByID(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("attachment"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusBadRequest, "附件编号得是数字")
		return
	}
	att, err := s.db.GetAttachment(r.Context(), id)
	if err != nil || att.OwnerKind != store.AttachTicket || att.OwnerID != tk.ID {
		s.respondError(w, r, http.StatusNotFound, "没有这个附件")
		return
	}
	if err := s.db.DeleteAttachment(r.Context(), id); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.attach.Remove(att.RelPath); err != nil {
		s.log.Error("attachment file remove", "id", id, "err", err)
	}
	s.log.Info("ticket attachment deleted", "id", id, "ticket", tk.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/tickets/"+strconv.FormatInt(tk.ID, 10)+"?msg="+url.QueryEscape("已删除附件 "+att.Name), http.StatusSeeOther)
}

// handleAttachmentGet is the only way a file leaves storage. Authorization is
// re-derived from the owner row every time; the URL alone proves nothing.
func (s *Server) handleAttachmentGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "没有这个附件")
		return
	}
	att, err := s.db.GetAttachment(r.Context(), id)
	if err != nil {
		s.respondError(w, r, http.StatusNotFound, "没有这个附件")
		return
	}
	a, _ := s.authenticate(r)
	switch att.OwnerKind {
	case store.AttachProblem:
		p, err := s.db.GetProblemByID(r.Context(), att.OwnerID)
		if err != nil {
			s.respondError(w, r, http.StatusNotFound, "没有这个附件")
			return
		}
		contest := s.contestOfProblem(r, a, p)
		if !p.Visible && !a.canEditProblem(p) && !a.can(permProblemPublish) && contest.ID == 0 && !a.canSeeTestcases(p) {
			s.respondError(w, r, http.StatusNotFound, "没有这个附件")
			return
		}
	case store.AttachTicket:
		if a.User.ID == 0 {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
			return
		}
		tk, err := s.db.FindTicket(r.Context(), att.OwnerID)
		if err != nil || (tk.AuthorID != a.User.ID && !a.can(permTicketHandle)) {
			s.respondError(w, r, http.StatusNotFound, "没有这个附件")
			return
		}
	default:
		s.respondError(w, r, http.StatusNotFound, "没有这个附件")
		return
	}

	f, size, err := s.attach.Open(att.RelPath)
	if err != nil {
		s.log.Error("attachment missing", "id", id, "err", err)
		s.respondError(w, r, http.StatusNotFound, "附件文件不见了")
		return
	}
	defer f.Close()

	disposition := "attachment"
	if strings.HasPrefix(att.MIME, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", att.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": att.Name}))
	w.Header().Set("Cache-Control", "private, max-age=300")
	// ServeContent handles Range and HEAD without repeating Content-Length's job
	// by hand; its name argument is display-only because Content-Type is set.
	http.ServeContent(w, r, att.Name, time.UnixMilli(att.CreatedAt), f)
}

// dropAttachments is the delete path shared by problems and tickets: rows first,
// then files. A file that cannot be removed is logged, not raised: the owner row
// is already gone and a 500 here would only confuse the operator.
func (s *Server) dropAttachments(ctx context.Context, kind string, ownerID int64) {
	paths, err := s.db.DeleteOwnerAttachments(ctx, kind, ownerID)
	if err != nil {
		s.log.Error("attachments delete", "kind", kind, "owner", ownerID, "err", err)
		return
	}
	for _, rel := range paths {
		if err := s.attach.Remove(rel); err != nil {
			s.log.Error("attachment file remove", "rel", rel, "err", err)
		}
	}
	if err := s.attach.RemoveOwner(kind, ownerID); err != nil {
		s.log.Error("attachment dir remove", "kind", kind, "owner", ownerID, "err", err)
	}
}
