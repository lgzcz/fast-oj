package api

// 轻量站内通知：只做两类，回复我的帖子、在正文里 @ 了我。它们都发生在一次
// 已经成功的写操作之后，所以通知失败只记日志，不能让评论或帖子跟着回滚。

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

const maxMentionsPerText = 20

func isHandleByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
		c == '_', c == '-', c == '.':
		return true
	}
	return false
}

// mentionHandles 从正文里挑出 @handle。前面的字符也必须是分隔符，否则
// alice@example.com 会被当成一个叫 example.com 的人。
func mentionHandles(text string) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		if i > 0 && isHandleByte(text[i-1]) {
			continue
		}
		j := i + 1
		for j < len(text) && isHandleByte(text[j]) && j-i-1 < 32 {
			j++
		}
		if j == i+1 {
			continue
		}
		h := text[i+1 : j]
		low := strings.ToLower(h)
		if !seen[low] {
			seen[low] = true
			out = append(out, h)
		}
		i = j - 1
		if len(out) >= maxMentionsPerText {
			break
		}
	}
	return out
}

// lookupMention 先按原文找；找不到再按去掉句末标点后的名字找一次。
// "@alice." 在中文句子里很常见，而用户名末尾带点的人极少，这个先后顺序
// 让常见写法能用，同时不改变精确用户名。
func (s *Server) lookupMention(ctx context.Context, raw string) (store.User, bool) {
	u, err := s.db.GetUserByHandle(ctx, raw)
	if err == nil {
		return u, true
	}
	trimmed := strings.TrimRight(raw, ".-_")
	if trimmed == "" || trimmed == raw {
		return store.User{}, false
	}
	u, err = s.db.GetUserByHandle(ctx, trimmed)
	return u, err == nil
}

func shownName(u store.User) string {
	if n := strings.TrimSpace(u.DisplayName); n != "" {
		return n
	}
	return u.Handle
}

// notifyMentions 解析正文里的 @，给每个存在且不是作者本人的账号写一条通知。
// skip 里的账号已经因为同一件事收到过另一条通知（例如楼主同时被回复与被 @），
// 不再重复。返回写成功的条数，便于测试与日志。
func (s *Server) notifyMentions(ctx context.Context, a actor, text, link, where string, skip map[int64]bool) int {
	handles := mentionHandles(text)
	if len(handles) == 0 {
		return 0
	}
	if skip == nil {
		skip = map[int64]bool{}
	}
	items := make([]store.NotificationInput, 0, len(handles))
	for _, raw := range handles {
		u, ok := s.lookupMention(ctx, raw)
		if !ok || u.ID == a.User.ID || skip[u.ID] {
			continue
		}
		skip[u.ID] = true
		items = append(items, store.NotificationInput{
			UserID: u.ID, ActorID: a.User.ID, ActorHandle: a.User.Handle,
			ActorName: shownName(a.User), Kind: "mention",
			Summary: shownName(a.User) + " 在" + where + "提到了你",
			Link:    link,
		})
	}
	if err := s.db.AddNotifications(ctx, items); err != nil {
		s.log.Warn("notifications", "err", err)
		return 0
	}
	return len(items)
}

// notifyTopicReply 是"有人评论了我的帖子"那一半。评论页没有楼中楼，
// 所以回复的对象就是楼主；被 @ 的人另算，且同一件事只收一条。
func (s *Server) notifyTopicReply(ctx context.Context, a actor, topic store.Topic, body string) {
	link := "/topics/" + strconv.FormatInt(topic.ID, 10)
	skip := map[int64]bool{a.User.ID: true}
	if topic.AuthorID != 0 && topic.AuthorID != a.User.ID {
		if err := s.db.AddNotifications(ctx, []store.NotificationInput{{
			UserID: topic.AuthorID, ActorID: a.User.ID, ActorHandle: a.User.Handle,
			ActorName: shownName(a.User), Kind: "reply",
			Summary: shownName(a.User) + " 回复了你的帖子《" + topic.Title + "》",
			Link:    link,
		}}); err != nil {
			s.log.Warn("notifications", "err", err)
		}
		skip[topic.AuthorID] = true
	}
	s.notifyMentions(ctx, a, body, link, "帖子《"+topic.Title+"》的评论", skip)
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/notifications", http.StatusSeeOther)
		return
	}
	list, err := s.db.ListNotifications(r.Context(), a.User.ID, 100)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	unread, err := s.db.CountNotificationsUnread(r.Context(), a.User.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := noticesData{Unread: unread, Shown: len(list)}
	for _, n := range list {
		label := "提到了你"
		if n.Kind == "reply" {
			label = "回复了你"
		}
		d.Rows = append(d.Rows, noticeRow{
			Summary: n.Summary, Link: n.Link, When: agoMS(n.CreatedAt),
			KindLabel: label, Unread: !n.Read,
		})
	}
	s.page(w, r, http.StatusOK, "通知", "notifications", a.viewer(), msgOf(r), d)
}

func (s *Server) handleNotificationsRead(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/notifications", http.StatusSeeOther)
		return
	}
	_, err = s.db.MarkNotificationsRead(r.Context(), a.User.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/notifications?msg="+url.QueryEscape("都标成已读了"), http.StatusSeeOther)
}

type noticeRow struct {
	Summary, Link, When, KindLabel string
	Unread                         bool
}

type noticesData struct {
	Rows          []noticeRow
	Unread, Shown int
}
