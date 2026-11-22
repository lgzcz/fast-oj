package api

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// 临时权限：到期之后自动失效，页面仍把它标出来，管理员能看见并清理。
func TestPermissionExpiryThroughConsole(t *testing.T) {
	h := newHarness(t)
	chief := h.author("chief2300", permPermissionManage)
	h.author("student2300")

	if page := h.unlock(chief, "/admin/perms"); !strings.Contains(page, "身份已确认") {
		t.Fatalf("确认身份失败：\n%s", short(page))
	}
	_, page := h.postAs(chief, "/admin/perms/u/student2300", url.Values{
		"key": {permProblemCreate}, "action": {"grant"}, "days": {"30"},
	})
	if !strings.Contains(page, "student2300 → problem.create") || !h.granted("student2300", permProblemCreate) {
		t.Fatalf("带到期时间的授予失败：\n%s", short(page))
	}
	var expires int64
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT expires_at FROM user_permissions up JOIN users u ON u.id=up.user_id
		 WHERE u.handle='student2300' AND up.key=?`, permProblemCreate).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if expires <= time.Now().UnixMilli() {
		t.Fatalf("expires_at = %d，应该在将来", expires)
	}
	if _, page := h.getAs(chief, "/admin/perms/u/student2300"); !strings.Contains(page, "（至 ") {
		t.Errorf("权限页没有写到期时间：\n%s", short(page))
	}

	// 把它推到过去：生效值立刻变成无，页面上留着"已过期"这条线索。
	if _, err := h.db.SQL.ExecContext(ctx(), `UPDATE user_permissions SET expires_at=? WHERE user_id=? AND key=?`,
		time.Now().Add(-time.Minute).UnixMilli(), h.user("student2300").ID, permProblemCreate); err != nil {
		t.Fatal(err)
	}
	if h.granted("student2300", permProblemCreate) {
		t.Fatal("过期权限仍然生效")
	}
	if _, page := h.getAs(chief, "/admin/perms/u/student2300"); !strings.Contains(page, "已过期") {
		t.Errorf("过期权限没有标出来：\n%s", short(page))
	}
}

// 批量授权：一次改一个班，并把自己、超级管理员和不存在的人跳过。
func TestPermissionBatchGrant(t *testing.T) {
	h := newHarness(t)
	chief := h.author("chief2400", permPermissionManage)
	h.author("class2401")
	h.author("class2402")
	if page := h.unlock(chief, "/admin/perms"); !strings.Contains(page, "身份已确认") {
		t.Fatalf("确认身份失败：\n%s", short(page))
	}

	_, page := h.postAs(chief, "/admin/perms/batch", url.Values{
		"handles": {"class2401\nclass2402, chief2400；ghost2400"},
		"key":     {permFeedModerate}, "action": {"grant"},
	})
	if !strings.Contains(page, "改了 2 个账号") || !strings.Contains(page, "跳过 2 个") {
		t.Fatalf("批量授予 = %s", short(page))
	}
	for _, who := range []string{"class2401", "class2402"} {
		if !h.granted(who, permFeedModerate) {
			t.Errorf("%s 没有拿到权限", who)
		}
	}
	if h.granted("chief2400", permFeedModerate) {
		t.Error("批量授权改到了自己")
	}

	// 批量收回同样一次生效。
	if _, page := h.postAs(chief, "/admin/perms/batch", url.Values{
		"handles": {"class2401 class2402"},
		"key":     {permFeedModerate}, "action": {"revoke"},
	}); !strings.Contains(page, "改了 2 个账号") {
		t.Fatalf("批量收回 = %s", short(page))
	}
	for _, who := range []string{"class2401", "class2402"} {
		if h.granted(who, permFeedModerate) {
			t.Errorf("%s 的权限没有被收回", who)
		}
	}

	// 恢复默认会把覆盖行直接删掉（上一批收回留下的是 granted=0 的行）。
	if _, page := h.postAs(chief, "/admin/perms/batch", url.Values{
		"handles": {"class2401"}, "key": {permFeedModerate}, "action": {"clear"},
	}); !strings.Contains(page, "改了 1 个账号") {
		t.Fatalf("批量恢复默认 = %s", short(page))
	}
	var rows int
	if err := h.db.SQL.QueryRowContext(ctx(),
		`SELECT COUNT(*) FROM user_permissions WHERE user_id=? AND key=?`,
		h.user("class2401").ID, permFeedModerate).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("恢复默认后覆盖行 = %d %v", rows, err)
	}

	// 空名单当面说清楚，而不是一次静默的空操作。
	if _, page := h.postAs(chief, "/admin/perms/batch", url.Values{
		"handles": {""}, "key": {permFeedModerate}, "action": {"grant"},
	}); !strings.Contains(page, "先填用户名") {
		t.Errorf("空名单没有提示：\n%s", short(page))
	}
}
