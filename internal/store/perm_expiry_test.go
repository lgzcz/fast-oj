package store

import (
	"context"
	"testing"
	"time"
)

func TestPermissionExpiry(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	u, err := db.CreateAccount(ctx, AccountInput{
		Handle: "expiring", Email: "expiring@example.com", Password: "correct-horse-battery", Cost: 4,
	})
	if err != nil {
		t.Fatal(err)
	}

	future := time.Now().Add(time.Hour).UnixMilli()
	if err := db.SetUserPermissionUntil(ctx, u.ID, "feed.moderate", true, future); err != nil {
		t.Fatal(err)
	}
	if perms, err := db.EffectivePermissions(ctx, u.ID, u.Role); err != nil || !perms["feed.moderate"] {
		t.Fatalf("未到期授予 = %v %v", perms["feed.moderate"], err)
	}
	grants, err := db.UserGrants(ctx, u.ID, u.Role)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range grants {
		if g.Key == "feed.moderate" {
			found = true
			if g.ExpiresAt != future || g.Expired || !g.Effective {
				t.Errorf("到期前这一行 = %+v", g)
			}
		}
	}
	if !found {
		t.Fatal("权限列表里没有 feed.moderate")
	}

	past := time.Now().Add(-time.Minute).UnixMilli()
	if err := db.SetUserPermissionUntil(ctx, u.ID, "feed.moderate", true, past); err != nil {
		t.Fatal(err)
	}
	if perms, _ := db.EffectivePermissions(ctx, u.ID, u.Role); perms["feed.moderate"] {
		t.Fatal("过期授予仍然生效")
	}
	grants, _ = db.UserGrants(ctx, u.ID, u.Role)
	for _, g := range grants {
		if g.Key == "feed.moderate" && !g.Expired {
			t.Errorf("过期授予没有标记: %+v", g)
		}
	}

	// 管理侧权限过期同样要从"这个人是不是工作人员"里掉出去。
	if err := db.SetUserPermissionUntil(ctx, u.ID, "permission.manage", true, future); err != nil {
		t.Fatal(err)
	}
	if staff, err := db.HasEffectiveStaffPermission(ctx, u.ID, u.Role); err != nil || !staff {
		t.Fatalf("未到期 staff = %v %v", staff, err)
	}
	if err := db.SetUserPermissionUntil(ctx, u.ID, "permission.manage", true, past); err != nil {
		t.Fatal(err)
	}
	if staff, err := db.HasEffectiveStaffPermission(ctx, u.ID, u.Role); err != nil || staff {
		t.Fatalf("过期 staff = %v %v", staff, err)
	}

	// 收回永远是永久的：granted=0 不带到期时间。
	if err := db.SetUserPermissionUntil(ctx, u.ID, "feed.moderate", false, future); err != nil {
		t.Fatal(err)
	}
	if perms, _ := db.EffectivePermissions(ctx, u.ID, u.Role); perms["feed.moderate"] {
		t.Fatal("收回被到期时间放行了")
	}
}

func TestPermissionBatch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	u1, err := db.CreateAccount(ctx, AccountInput{Handle: "batch1", Email: "batch1@example.com", Password: "correct-horse-battery", Cost: 4})
	if err != nil {
		t.Fatal(err)
	}
	u2, err := db.CreateAccount(ctx, AccountInput{Handle: "batch2", Email: "batch2@example.com", Password: "correct-horse-battery", Cost: 4})
	if err != nil {
		t.Fatal(err)
	}
	n, err := db.SetUserPermissions(ctx, []int64{u1.ID, u2.ID}, "feed.moderate", true, 0)
	if err != nil || n != 2 {
		t.Fatalf("批量授予 = %d %v", n, err)
	}
	for _, u := range []User{u1, u2} {
		if perms, _ := db.EffectivePermissions(ctx, u.ID, u.Role); !perms["feed.moderate"] {
			t.Errorf("%s 没有拿到权限", u.Handle)
		}
	}
	n, err = db.ClearUserPermissions(ctx, []int64{u1.ID, u2.ID}, "feed.moderate")
	if err != nil || n != 2 {
		t.Fatalf("批量恢复默认 = %d %v", n, err)
	}
	for _, u := range []User{u1, u2} {
		if perms, _ := db.EffectivePermissions(ctx, u.ID, u.Role); perms["feed.moderate"] {
			t.Errorf("%s 的覆盖行没有被删掉", u.Handle)
		}
	}
	if _, err := db.SetUserPermissions(ctx, []int64{u1.ID}, "no.such.permission", true, 0); err != ErrNotFound {
		t.Errorf("未知权限键 = %v", err)
	}
}
