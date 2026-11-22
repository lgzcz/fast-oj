package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 群聊是站内的公共房间：谁都能读，登录后才能发言。消息只存纯文本，
// 页面按行原样展示，不走 Markdown——聊天里的链接不该被渲染成可点击的东西。
const (
	chatWindow      = time.Minute
	maxChatsMinute  = 30
	roomWindow      = time.Hour
	maxRoomsPerHour = 5
)

type chatRoomRow struct {
	ID          int64
	Name        string
	Owner       string
	OwnerHandle string
	Messages    int
	When        string
}

type chatListData struct {
	Rows     []chatRoomRow
	SignedIn bool
	Name     string
	Errors   []string
}

func (s *Server) handleChatList(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	rooms, err := s.db.ListChatRooms(r.Context(), 100)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := chatListData{SignedIn: a.User.ID != 0}
	for _, room := range rooms {
		owner := room.OwnerHandle
		if room.OwnerName != "" {
			owner = room.OwnerName
		}
		d.Rows = append(d.Rows, chatRoomRow{
			ID: room.ID, Name: room.Name, Owner: owner, OwnerHandle: room.OwnerHandle,
			Messages: room.Messages, When: agoMS(room.CreatedAt),
		})
	}
	s.page(w, r, http.StatusOK, "群聊", "chat", a.viewer(), msgOf(r), d)
}

func (s *Server) handleChatCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/chat", http.StatusSeeOther)
		return
	}
	key := "chatroom:" + strconv.FormatInt(a.User.ID, 10)
	allowed, err := s.db.LoginAllowed(r.Context(), key, roomWindow, maxRoomsPerHour)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !allowed {
		s.respondError(w, r, http.StatusTooManyRequests, "开房太快了，一小时最多 "+strconv.Itoa(maxRoomsPerHour)+" 个")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	room, err := s.db.CreateChatRoom(r.Context(), a.User.ID, name)
	if err != nil {
		rooms, lerr := s.db.ListChatRooms(r.Context(), 100)
		if lerr != nil {
			s.writeStoreError(w, r, lerr)
			return
		}
		d := chatListData{SignedIn: true, Name: name, Errors: []string{err.Error()}}
		for _, item := range rooms {
			owner := item.OwnerHandle
			if item.OwnerName != "" {
				owner = item.OwnerName
			}
			d.Rows = append(d.Rows, chatRoomRow{
				ID: item.ID, Name: item.Name, Owner: owner, OwnerHandle: item.OwnerHandle,
				Messages: item.Messages, When: agoMS(item.CreatedAt),
			})
		}
		s.page(w, r, http.StatusBadRequest, "群聊", "chat", a.viewer(), "", d)
		return
	}
	if _, err := s.db.RegisterLoginFailure(r.Context(), key, roomWindow); err != nil {
		s.log.Error("chat room counter", "err", err)
	}
	s.log.Info("chat room created", "id", room.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/chat/"+strconv.FormatInt(room.ID, 10), http.StatusSeeOther)
}

type chatMessageRow struct {
	ID        int64
	Name      string
	Handle    string
	Color     string
	Body      string
	When      string
	CanDelete bool
}

type chatRoomData struct {
	ID          int64
	Name        string
	Owner       string
	OwnerHandle string
	Messages    []chatMessageRow
	SignedIn    bool
	CanDelete   bool
	MaxBody     int
}

func (s *Server) chatRoomByID(w http.ResponseWriter, r *http.Request) (actor, store.ChatRoom, bool) {
	a, _ := s.authenticate(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "房间不存在")
		return a, store.ChatRoom{}, false
	}
	room, err := s.db.GetChatRoom(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return a, store.ChatRoom{}, false
	}
	return a, room, true
}

func (s *Server) handleChatGet(w http.ResponseWriter, r *http.Request) {
	a, room, ok := s.chatRoomByID(w, r)
	if !ok {
		return
	}
	messages, err := s.db.ListChatMessages(r.Context(), room.ID, 0, 100)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	moderator := a.can(permTopicModerate)
	d := chatRoomData{
		ID: room.ID, Name: room.Name, Owner: room.OwnerHandle, OwnerHandle: room.OwnerHandle,
		SignedIn: a.User.ID != 0, CanDelete: a.User.ID != 0 && (a.User.ID == room.OwnerID || moderator),
		MaxBody: store.MaxChatBody,
	}
	if room.OwnerName != "" {
		d.Owner = room.OwnerName
	}
	// 存储按新到旧取，页面按旧到新排：最后一句在底部，符合聊天习惯。
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		name := m.Handle
		if m.Name != "" {
			name = m.Name
		}
		canDelete := a.User.ID != 0 && (a.User.ID == m.UserID || moderator)
		d.Messages = append(d.Messages, chatMessageRow{
			ID: m.ID, Name: name, Handle: m.Handle, Color: m.Color, Body: m.Body,
			When: agoMS(m.CreatedAt), CanDelete: canDelete,
		})
	}
	s.page(w, r, http.StatusOK, "#"+room.Name, "chat_room", a.viewer(), msgOf(r), d)
}

func (s *Server) handleChatPost(w http.ResponseWriter, r *http.Request) {
	a, room, ok := s.chatRoomByID(w, r)
	if !ok {
		return
	}
	if a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	key := "chat:" + strconv.FormatInt(a.User.ID, 10)
	allowed, err := s.db.LoginAllowed(r.Context(), key, chatWindow, maxChatsMinute)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !allowed {
		s.respondError(w, r, http.StatusTooManyRequests, "发得太快了，一分钟最多 "+strconv.Itoa(maxChatsMinute)+" 句")
		return
	}
	msg, err := s.db.AddChatMessage(r.Context(), room.ID, a.User.ID, r.FormValue("body"))
	if err != nil {
		s.respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.db.RegisterLoginFailure(r.Context(), key, chatWindow); err != nil {
		s.log.Error("chat counter", "err", err)
	}
	http.Redirect(w, r, "/chat/"+strconv.FormatInt(room.ID, 10)+"#m"+strconv.FormatInt(msg.ID, 10), http.StatusSeeOther)
}

func (s *Server) handleChatMessageDelete(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/chat", http.StatusSeeOther)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "消息不存在")
		return
	}
	roomID, removed, err := s.db.DeleteChatMessage(r.Context(), id, a.User.ID, a.can(permTopicModerate))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !removed {
		s.respondError(w, r, http.StatusForbidden, "只能删自己的消息")
		return
	}
	http.Redirect(w, r, "/chat/"+strconv.FormatInt(roomID, 10)+"?msg="+url.QueryEscape("已删除"), http.StatusSeeOther)
}

func (s *Server) handleChatRoomDelete(w http.ResponseWriter, r *http.Request) {
	a, room, ok := s.chatRoomByID(w, r)
	if !ok {
		return
	}
	if a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	moderator := a.can(permTopicModerate)
	if a.User.ID != room.OwnerID && !moderator {
		s.respondError(w, r, http.StatusForbidden, "只有房主或持 "+permTopicModerate+" 的人能删房间")
		return
	}
	var err error
	if moderator {
		err = s.db.DeleteChatRoomAny(r.Context(), room.ID)
	} else {
		err = s.db.DeleteChatRoom(r.Context(), room.ID, a.User.ID)
	}
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("chat room deleted", "id", room.ID, "by", a.User.Handle, "moderated", moderator)
	http.Redirect(w, r, "/chat?msg="+url.QueryEscape("房间已删除"), http.StatusSeeOther)
}
