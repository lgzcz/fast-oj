package api

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// 管理员（feed.moderate）在动态页上也该看到"删除"，而不是只有作者本人有那个按钮。
func TestModeratorCanDeleteAnyPost(t *testing.T) {
	h := newHarness(t)
	author := h.author("post_author")
	moderator := h.author("post_mod", permFeedModerate)

	h.postAs(author, "/feed", url.Values{"body": {"管理员可以删这条"}})
	posts, err := h.db.ListPosts(ctx(), 10, 0, 0, false, 0, false)
	if err != nil || len(posts) == 0 {
		t.Fatalf("post not created: %v", err)
	}
	id := posts[0].ID

	if _, page := h.getAs(moderator, "/feed"); !containsAll(page, fmt.Sprintf("/feed/%d/delete", id), "隐藏") {
		t.Fatalf("管理员看不到删除/隐藏按钮：\n%s", short(page))
	}
	h.postAs(moderator, fmt.Sprintf("/feed/%d/delete", id), url.Values{})
	if posts, err = h.db.ListPosts(ctx(), 10, 0, 0, false, 0, false); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Fatalf("管理员删除后动态还在：%+v", posts)
	}
}

func TestAuthorCanDeleteOwnPost(t *testing.T) {
	h := newHarness(t)
	author := h.author("own_post_author")
	h.postAs(author, "/feed", url.Values{"body": {"自己的动态"}})
	posts, err := h.db.ListPosts(ctx(), 10, 0, 0, false, 0, false)
	if err != nil || len(posts) == 0 {
		t.Fatalf("post not created: %v", err)
	}
	if _, page := h.getAs(author, "/feed"); !containsAll(page, fmt.Sprintf("/feed/%d/delete", posts[0].ID)) {
		t.Fatalf("作者看不到自己的删除按钮：\n%s", short(page))
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
