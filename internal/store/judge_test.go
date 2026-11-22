package store

import (
	"context"
	"testing"
)

// 空队列的判机每 500ms 领一次单；心跳不能每次都写库，但字段变化要立刻生效。
func TestJudgeHeartbeatWriteIsThrottled(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	j, err := db.UpsertJudge(ctx, "hb-box", []string{"cpp"}, []string{"compile"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 5 秒以内、字段没变：只读不写，时间戳原样返回。
	fresh := nowMS() - 1000
	if _, err := db.SQL.ExecContext(ctx,
		`UPDATE judges SET last_seen_at=? WHERE id=?`, fresh, j.ID); err != nil {
		t.Fatal(err)
	}
	got, err := db.UpsertJudge(ctx, "hb-box", []string{"cpp"}, []string{"compile"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSeenAt != fresh {
		t.Fatalf("新鲜心跳仍写了库：%d -> %d", fresh, got.LastSeenAt)
	}

	// 字段改变时不受节流影响：新工具链/备注/沙盒状态要马上出现在 /judges。
	yes := true
	got, err = db.UpsertJudge(ctx, "hb-box", []string{"cpp", "python"}, []string{"compile", "run"}, "新备注", &yes)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got.Languages, []string{"cpp", "python"}) || got.Note != "新备注" ||
		!got.JailedKnown || !got.Jailed {
		t.Fatalf("字段变化被节流吞了：%+v", got)
	}

	// 超过节流窗口：时间戳刷新。
	stale := nowMS() - judgeHeartbeatEvery.Milliseconds() - 1000
	if _, err := db.SQL.ExecContext(ctx,
		`UPDATE judges SET last_seen_at=? WHERE id=?`, stale, j.ID); err != nil {
		t.Fatal(err)
	}
	got, err = db.UpsertJudge(ctx, "hb-box", []string{"cpp", "python"}, []string{"compile", "run"}, "新备注", &yes)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSeenAt <= stale {
		t.Fatalf("过期心跳没刷新：%d", got.LastSeenAt)
	}
}
