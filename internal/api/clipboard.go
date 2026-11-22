package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 剪贴板是登录用户自己的临时记事本：纯文本、只对自己可见、条数封顶。
// 它不是投稿入口，正文不渲染 Markdown，也不会出现在任何列表里。

type clipboardRow struct {
	ID   int64
	Name string
	Body string
	When string
}

type clipboardData struct {
	Rows   []clipboardRow
	Name   string
	Errors []string
	Max    int
}

func (s *Server) clipboardListData(r *http.Request, a actor) (clipboardData, error) {
	items, err := s.db.ListClipboards(r.Context(), a.User.ID)
	if err != nil {
		return clipboardData{}, err
	}
	d := clipboardData{Max: store.MaxClipboards}
	for _, item := range items {
		d.Rows = append(d.Rows, clipboardRow{
			ID: item.ID, Name: item.Name, Body: item.Body, When: agoMS(item.UpdatedAt),
		})
	}
	return d, nil
}

func (s *Server) handleClipboardList(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/clipboard", http.StatusSeeOther)
		return
	}
	d, err := s.clipboardListData(r, a)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.page(w, r, http.StatusOK, "剪贴板", "clipboard", a.viewer(), msgOf(r), d)
}

func (s *Server) handleClipboardCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/clipboard", http.StatusSeeOther)
		return
	}
	name := r.FormValue("name")
	if _, err := s.db.CreateClipboard(r.Context(), a.User.ID, name, r.FormValue("body")); err != nil {
		d, lerr := s.clipboardListData(r, a)
		if lerr != nil {
			s.writeStoreError(w, r, lerr)
			return
		}
		d.Name, d.Errors = name, []string{err.Error()}
		s.page(w, r, http.StatusBadRequest, "剪贴板", "clipboard", a.viewer(), "", d)
		return
	}
	http.Redirect(w, r, "/clipboard?msg="+url.QueryEscape("已保存"), http.StatusSeeOther)
}

func (s *Server) handleClipboardUpdate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/clipboard", http.StatusSeeOther)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "这条剪贴板不存在")
		return
	}
	if err := s.db.UpdateClipboard(r.Context(), id, a.User.ID, r.FormValue("name"), r.FormValue("body")); err != nil {
		if err == store.ErrNotFound {
			s.respondError(w, r, http.StatusNotFound, "这条剪贴板不存在")
			return
		}
		d, lerr := s.clipboardListData(r, a)
		if lerr != nil {
			s.writeStoreError(w, r, lerr)
			return
		}
		d.Errors = []string{err.Error()}
		s.page(w, r, http.StatusBadRequest, "剪贴板", "clipboard", a.viewer(), "", d)
		return
	}
	http.Redirect(w, r, "/clipboard?msg="+url.QueryEscape("已更新"), http.StatusSeeOther)
}

func (s *Server) handleClipboardDelete(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/clipboard", http.StatusSeeOther)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "这条剪贴板不存在")
		return
	}
	removed, err := s.db.DeleteClipboard(r.Context(), id, a.User.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !removed {
		s.respondError(w, r, http.StatusNotFound, "这条剪贴板不存在")
		return
	}
	http.Redirect(w, r, "/clipboard?msg="+url.QueryEscape("已删除"), http.StatusSeeOther)
}
