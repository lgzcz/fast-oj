package store

import (
	"context"
	"database/sql"
)

// Permission is one capability the system can grant.
type Permission struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Staff bool   `json:"staff"`
}

func (db *DB) ListPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT key, label, staff FROM permissions ORDER BY staff DESC, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Permission
	for rows.Next() {
		var (
			p     Permission
			staff int
		)
		if err := rows.Scan(&p.Key, &p.Label, &staff); err != nil {
			return nil, err
		}
		p.Staff = staff != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// EffectivePermissions is the role baseline with that user's individual grants
// and removals applied on top. It is one query per authenticated request.
//
// The super admin is the exception: every capability is on for them no matter what
// the two tables above say. A removal row cannot be created for them through the
// console, but the immunity lives here rather than only in the screen that writes
// those rows, so a hand-edited database cannot strip it either.
func (db *DB) EffectivePermissions(ctx context.Context, userID int64, role string) (map[string]bool, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT key, 1 AS src, 1 AS granted FROM role_permissions WHERE role=?
		 UNION ALL
		 SELECT key, 2 AS src, granted FROM user_permissions
		  WHERE user_id=? AND (granted=0 OR expires_at=0 OR expires_at>?)
		 UNION ALL
		 SELECT p.key, 3 AS src, 1 AS granted
		   FROM permissions p JOIN site_owner o ON o.id=1 AND o.user_id=?`,
		role, userID, nowMS(), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Row order from a UNION ALL is not guaranteed, so precedence is resolved by
	// source number rather than by arrival order: the super admin row (3) beats a
	// per-user override (2), which in turn beats the tier default (1).
	type stamped struct {
		granted bool
		src     int
	}
	best := map[string]stamped{}
	for rows.Next() {
		var (
			key        string
			src, grant int
		)
		if err := rows.Scan(&key, &src, &grant); err != nil {
			return nil, err
		}
		if cur, seen := best[key]; !seen || src >= cur.src {
			best[key] = stamped{granted: grant != 0, src: src}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]bool, len(best))
	for key, v := range best {
		out[key] = v.granted
	}
	return out, nil
}

// UserGrant is one row of the permission editor: what the tier gives, what the
// user's own row says, and what is effective.
type UserGrant struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Staff     bool   `json:"staff"`
	ByRole    bool   `json:"by_role"`
	Override  *bool  `json:"override,omitempty"`
	Effective bool   `json:"effective"`
	// ExpiresAt 是这条单独授予的到期时刻（毫秒，0=永久）；Expired 表示它已经过期，
	// 因此 Effective 不算它，但这一行仍留着让管理员看得见并清理。
	ExpiresAt int64 `json:"expires_at,omitempty"`
	Expired   bool  `json:"expired,omitempty"`
}

func (db *DB) UserGrants(ctx context.Context, userID int64, role string) ([]UserGrant, error) {
	perms, err := db.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	roleSet, err := db.permissionsByRole(ctx, role)
	if err != nil {
		return nil, err
	}
	overrides, err := db.userOverrides(ctx, userID)
	if err != nil {
		return nil, err
	}
	// The editor has to show what the site actually does, and for the super admin
	// that is "everything on", whatever these two tables say.
	owner, err := db.IsOwner(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := make([]UserGrant, 0, len(perms))
	now := nowMS()
	for _, p := range perms {
		g := UserGrant{Key: p.Key, Label: p.Label, Staff: p.Staff, ByRole: roleSet[p.Key]}
		if v, ok := overrides[p.Key]; ok {
			val := v.granted
			g.Override = &val
			g.ExpiresAt = v.expiresAt
			switch {
			case v.granted && v.expiresAt > 0 && v.expiresAt <= now:
				g.Expired = true
				g.Effective = g.ByRole
			default:
				g.Effective = v.granted
			}
		} else {
			g.Effective = g.ByRole
		}
		if owner {
			g.Effective = true
		}
		out = append(out, g)
	}
	return out, nil
}

func (db *DB) permissionsByRole(ctx context.Context, role string) (map[string]bool, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT key FROM role_permissions WHERE role=?`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

type permOverride struct {
	granted   bool
	expiresAt int64
}

func (db *DB) userOverrides(ctx context.Context, userID int64) (map[string]permOverride, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT key, granted, IFNULL(expires_at, 0) FROM user_permissions WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]permOverride{}
	for rows.Next() {
		var (
			key     string
			granted int
			expires int64
		)
		if err := rows.Scan(&key, &granted, &expires); err != nil {
			return nil, err
		}
		out[key] = permOverride{granted: granted != 0, expiresAt: expires}
	}
	return out, rows.Err()
}

// HasEffectiveStaffPermission reports whether the account still holds at least one
// staff capability after per-user overrides are applied. The permission console uses
// it to keep a user.manage delegate from banning or cancelling someone who ranks
// above them; the site owner is refused earlier by the caller.
func (db *DB) HasEffectiveStaffPermission(ctx context.Context, userID int64, role string) (bool, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM permissions p
		    WHERE p.staff=1 AND (
		      (EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role=? AND rp.key=p.key)
		       AND NOT EXISTS (SELECT 1 FROM user_permissions up
		                        WHERE up.user_id=? AND up.key=p.key AND up.granted=0))
		      OR EXISTS (SELECT 1 FROM user_permissions up
		                  WHERE up.user_id=? AND up.key=p.key AND up.granted=1
		                    AND (up.expires_at=0 OR up.expires_at>?))
		    )
		 )`, role, userID, userID, nowMS()).Scan(&n)
	if err != nil {
		return false, err
	}
	return n != 0, nil
}

// SetUserPermission records an explicit grant or removal that does not expire.
// Unknown keys fail rather than silently doing nothing, so a typo cannot look like
// a successful privilege change.
func (db *DB) SetUserPermission(ctx context.Context, userID int64, key string, granted bool) error {
	return db.SetUserPermissionUntil(ctx, userID, key, granted, 0)
}

// SetUserPermissionUntil is SetUserPermission with an expiry for grants. expiresAt
// is a millisecond timestamp, 0 = never. A removal is always permanent: a "deny"
// that silently lapses one day is not something an administrator can reason about.
func (db *DB) SetUserPermissionUntil(ctx context.Context, userID int64, key string, granted bool, expiresAt int64) error {
	if err := db.guardMutable(ctx, userID); err != nil {
		return err
	}
	if err := db.permissionKnown(ctx, key); err != nil {
		return err
	}
	if !granted || expiresAt < 0 {
		expiresAt = 0
	}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO user_permissions (user_id, key, granted, expires_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, key) DO UPDATE SET granted=excluded.granted, expires_at=excluded.expires_at`,
		userID, key, boolInt(granted), expiresAt)
	return err
}

// SetUserPermissions grants or revokes one key for a batch of accounts. Every
// target is checked first (owner and cancelled accounts are refused), then the
// whole batch lands in one transaction: a class list is all-or-nothing rather
// than "the first 17 got it".
func (db *DB) SetUserPermissions(ctx context.Context, userIDs []int64, key string, granted bool, expiresAt int64) (int, error) {
	if err := db.permissionKnown(ctx, key); err != nil {
		return 0, err
	}
	for _, id := range userIDs {
		if err := db.guardMutable(ctx, id); err != nil {
			return 0, err
		}
	}
	if !granted || expiresAt < 0 {
		expiresAt = 0
	}
	n := 0
	err := db.InTx(ctx, func(tx *sql.Tx) error {
		for _, id := range userIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO user_permissions (user_id, key, granted, expires_at) VALUES (?, ?, ?, ?)
				 ON CONFLICT(user_id, key) DO UPDATE SET granted=excluded.granted, expires_at=excluded.expires_at`,
				id, key, boolInt(granted), expiresAt); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func (db *DB) permissionKnown(ctx context.Context, key string) error {
	var n int
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM permissions WHERE key=?`, key).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearUserPermissions removes one override from a batch of accounts, returning
// them to whatever their role says.
func (db *DB) ClearUserPermissions(ctx context.Context, userIDs []int64, key string) (int, error) {
	if err := db.permissionKnown(ctx, key); err != nil {
		return 0, err
	}
	for _, id := range userIDs {
		if err := db.guardMutable(ctx, id); err != nil {
			return 0, err
		}
	}
	n := 0
	err := db.InTx(ctx, func(tx *sql.Tx) error {
		for _, id := range userIDs {
			res, err := tx.ExecContext(ctx,
				`DELETE FROM user_permissions WHERE user_id=? AND key=?`, id, key)
			if err != nil {
				return err
			}
			if k, _ := res.RowsAffected(); k > 0 {
				n++
			}
		}
		return nil
	})
	return n, err
}

// ClearUserPermission drops the override, returning the user to their tier default.
func (db *DB) ClearUserPermission(ctx context.Context, userID int64, key string) error {
	if err := db.guardMutable(ctx, userID); err != nil {
		return err
	}
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM user_permissions WHERE user_id=? AND key=?`, userID, key)
	return err
}
