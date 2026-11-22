package store

import (
	"context"
	"errors"
	"time"
)

const judgeColumns = `id, name, languages, capabilities, note, last_seen_at, jailed, jailed_known`

// judgeHeartbeatEvery 是 last_seen_at 的写入节流粒度。判机空队列时每 500ms 领一次单，
// 每次都写一遍会让空闲判机变成一台稳定的写库机器；在线状态用 5 秒粒度足够。
const judgeHeartbeatEvery = 5 * time.Second

func scanJudge(row scanner) (Judge, error) {
	var (
		j             Judge
		langs, caps   string
		jailed, known int
	)
	err := row.Scan(&j.ID, &j.Name, &langs, &caps, &j.Note, &j.LastSeenAt, &jailed, &known)
	j.Languages = splitList(langs)
	j.Capabilities = splitList(caps)
	// 两列是分开的两件事：0/0 = "这台机器还没说过"（升级后还没心跳过的老二进制），
	// 0/1 = "它说了开不起来"。把前者显示成后者，就是替一台没开口的机器编一句话。
	// 和 time_min/mem_min 一样先扫成 int：这个库里的布尔都是这么来的，不靠驱动肯不肯转。
	j.Jailed, j.JailedKnown = jailed != 0, known != 0
	return j, notFound(err)
}

// UpsertJudge registers a judge box, or refreshes its advertised toolchains, its
// self-reported capabilities and whether it can put a run inside a jail right now.
//
// jailed 是指针：nil = 这台判机没说过；&true / &false = 它说了有 / 没有。
//
// 心跳本身每 5 秒才写一次：字段没变且 last_seen_at 还新鲜时只读一行就返回。字段变了
// （工具箱、备注、沙盒状态）仍然立刻落库，在线状态的粒度不受影响。
func (db *DB) UpsertJudge(ctx context.Context, name string, langs, caps []string, note string, jailed *bool) (Judge, error) {
	var jail any = 0
	known := 0
	if jailed != nil {
		known = 1
		if *jailed {
			jail = 1
		}
	}
	languages, capabilities := joinList(langs), joinList(caps)
	now := nowMS()

	cur, err := scanJudge(db.SQL.QueryRowContext(ctx,
		`SELECT `+judgeColumns+` FROM judges WHERE name=? COLLATE NOCASE`, name))
	switch {
	case err == nil:
		stale := cur.LastSeenAt <= now-judgeHeartbeatEvery.Milliseconds()
		changed := !sameStrings(cur.Languages, langs) || !sameStrings(cur.Capabilities, caps) ||
			cur.Note != note || cur.JailedKnown != (jailed != nil) ||
			(jailed != nil && cur.Jailed != *jailed)
		if !stale && !changed {
			return cur, nil
		}
		if _, err := db.SQL.ExecContext(ctx,
			`UPDATE judges SET languages=?, capabilities=?, note=?, last_seen_at=?, jailed=?, jailed_known=?
			  WHERE id=?`,
			languages, capabilities, note, now, jail, known, cur.ID); err != nil {
			return Judge{}, err
		}
	case errors.Is(err, ErrNotFound):
		if _, err := db.SQL.ExecContext(ctx,
			`INSERT INTO judges (name, languages, capabilities, note, last_seen_at, jailed, jailed_known)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET languages=excluded.languages, capabilities=excluded.capabilities,
			   note=excluded.note, last_seen_at=excluded.last_seen_at, jailed=excluded.jailed,
			   jailed_known=excluded.jailed_known`,
			name, languages, capabilities, note, now, jail, known); err != nil {
			return Judge{}, err
		}
	default:
		return Judge{}, err
	}
	return scanJudge(db.SQL.QueryRowContext(ctx,
		`SELECT `+judgeColumns+` FROM judges WHERE name=? COLLATE NOCASE`, name))
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (db *DB) TouchJudge(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `UPDATE judges SET last_seen_at=? WHERE id=?`, nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) ListJudges(ctx context.Context) ([]Judge, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT `+judgeColumns+` FROM judges ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Judge
	for rows.Next() {
		j, err := scanJudge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (db *DB) GetJudge(ctx context.Context, name string) (Judge, error) {
	return scanJudge(db.SQL.QueryRowContext(ctx,
		`SELECT `+judgeColumns+` FROM judges WHERE name=? COLLATE NOCASE`, name))
}
