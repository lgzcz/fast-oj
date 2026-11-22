package api

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 图床：头像、封面与正文用图都走同一张 images 表。图片是公开资源；
// 文件名随机、库里只存路径，SVG/HTML 在落盘之前就拒掉。

const (
	maxAvatarSize = 2 << 20
	maxCoverSize  = 8 << 20
	maxImageSize  = 8 << 20
	maxImagesPerUser = 200
)

var allowedImageMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func imageLimit(kind string) int64 {
	switch kind {
	case store.ImageAvatar:
		return maxAvatarSize
	case store.ImageCover:
		return maxCoverSize
	}
	return maxImageSize
}

func (s *Server) readImageUpload(w http.ResponseWriter, r *http.Request, kind string) (mimeType string, body []byte, errMsg string) {
	limit := imageLimit(kind)
	r.Body = http.MaxBytesReader(w, r.Body, limit+attachmentFormRoom)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return "", nil, "上传内容读不出来：图片最大 " + sizeText(limit)
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return "", nil, "没有选文件"
	}
	defer file.Close()
	body, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return "", nil, "读取上传内容失败"
	}
	if len(body) == 0 {
		return "", nil, "空文件不算图片"
	}
	if int64(len(body)) > limit {
		return "", nil, "图片超过上限 " + sizeText(limit)
	}
	mimeType = http.DetectContentType(body)
	if !allowedImageMIME[mimeType] {
		return "", nil, "只收 PNG / JPEG / GIF / WebP；按内容识别，不看扩展名"
	}
	return mimeType, body, ""
}

// saveImage stores one upload and, for avatar/cover, retires the previous slot.
func (s *Server) saveImage(w http.ResponseWriter, r *http.Request, a actor, kind, back string) bool {
	mimeType, body, errMsg := s.readImageUpload(w, r, kind)
	if errMsg != "" {
		s.respondError(w, r, http.StatusBadRequest, errMsg)
		return false
	}
	if kind == store.ImagePost {
		n, err := s.db.CountImages(r.Context(), a.User.ID)
		if err != nil {
			s.writeStoreError(w, r, err)
			return false
		}
		if n >= maxImagesPerUser {
			s.respondError(w, r, http.StatusConflict,
				"每人最多存 "+strconv.Itoa(maxImagesPerUser)+" 张图；先删掉一些不用的")
			return false
		}
	}
	old, hadOld, err := s.db.LatestImage(r.Context(), a.User.ID, kind)
	if err != nil {
		s.writeStoreError(w, r, err)
		return false
	}
	rel, err := s.attach.Save("image", a.User.ID, body)
	if err != nil {
		s.writeStoreError(w, r, err)
		return false
	}
	_, err = s.db.CreateImage(r.Context(), &store.Image{
		UserID: a.User.ID, Kind: kind, Rel: rel, MIME: mimeType, Size: int64(len(body)),
	})
	if err != nil {
		_ = s.attach.Remove(rel)
		s.writeStoreError(w, r, err)
		return false
	}
	// Avatar and cover are single-slot: the old row and its file go away only
	// after the new one is safely in place.
	if (kind == store.ImageAvatar || kind == store.ImageCover) && hadOld {
		if _, _, err := s.db.DeleteImage(r.Context(), old.ID, a.User.ID); err == nil {
			if err := s.attach.Remove(old.Rel); err != nil {
				s.log.Error("remove old avatar/cover", "err", err)
			}
		}
	}
	note := "已上传"
	if kind == store.ImageAvatar {
		note = "头像已更新"
	} else if kind == store.ImageCover {
		note = "封面已更新"
	}
	http.Redirect(w, r, back+"?msg="+url.QueryEscape(note), http.StatusSeeOther)
	return true
}

func (s *Server) handleAvatarUpload(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	s.saveImage(w, r, a, store.ImageAvatar, "/settings")
}

func (s *Server) handleCoverUpload(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	s.saveImage(w, r, a, store.ImageCover, "/settings")
}

type imageRow struct {
	ID        int64
	URL       string
	Kind      string
	KindLabel string
	Size      string
	When      string
	IsAvatar  bool
	IsCover   bool
}

type imagesData struct {
	Rows  []imageRow
	Count int
	Max   int
}

func (s *Server) handleImagesPage(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	items, err := s.db.ListImages(r.Context(), a.User.ID, "", 120)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := imagesData{Max: maxImagesPerUser}
	for _, img := range items {
		label := "正文图"
		switch img.Kind {
		case store.ImageAvatar:
			label = "头像"
		case store.ImageCover:
			label = "封面"
		}
		d.Rows = append(d.Rows, imageRow{
			ID: img.ID, URL: "/images/" + strconv.FormatInt(img.ID, 10),
			Kind: img.Kind, KindLabel: label, Size: sizeText(img.Size), When: agoMS(img.CreatedAt),
			IsAvatar: img.Kind == store.ImageAvatar, IsCover: img.Kind == store.ImageCover,
		})
	}
	if n, err := s.db.CountImages(r.Context(), a.User.ID); err == nil {
		d.Count = n
	}
	s.page(w, r, http.StatusOK, "我的图床", "images", a.viewer(), msgOf(r), d)
}

func (s *Server) handleImagePostUpload(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	s.saveImage(w, r, a, store.ImagePost, "/images")
}

func (s *Server) handleImageGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "图片不存在")
		return
	}
	img, err := s.db.GetImage(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	f, _, err := s.attach.Open(img.Rel)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", img.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Disposition", `inline; filename="image"`)
	http.ServeContent(w, r, "", time.UnixMilli(img.CreatedAt), f)
}

func (s *Server) handleImageDelete(w http.ResponseWriter, r *http.Request) {
	a, err := s.requireSignedIn(w, r)
	if err != nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "图片不存在")
		return
	}
	img, ok, err := s.db.DeleteImage(r.Context(), id, a.User.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !ok {
		s.respondError(w, r, http.StatusNotFound, "图片不存在或不属于你")
		return
	}
	if err := s.attach.Remove(img.Rel); err != nil {
		s.log.Error("remove image file", "err", err)
	}
	http.Redirect(w, r, "/images?msg="+url.QueryEscape("已删除"), http.StatusSeeOther)
}

// 模板只拿到一个可渲染的 URL；空串表示没设置。
func imageURL(id int64) string {
	if id == 0 {
		return ""
	}
	return "/images/" + strconv.FormatInt(id, 10)
}
