package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// 忘记密码的重置链接。设计上有三条硬要求：
//
//   - 原文令牌只有邮件里有，库里只存 SHA-256 哈希；
//   - 一次只能用一次，且在同一个事务里把"改密码 + 花掉令牌 + 踢掉会话"一起完成；
//   - 令牌绑在签发时的密码指纹上：链接发出之后密码又改过一次，旧链接就作废，
//     哪怕它还没到期。
//
// 指纹是 passhash 的哈希，不是 passhash 本身：这列会在查询里被拿来比较，
// 但没有任何一条路径需要看到 passhash 原文。
const PasswordResetTTL = 30 * time.Minute

var (
	// ErrPasswordResetInvalid 对"没有这条令牌""已经用过""密码已经变过"给同一个回答：
	// 拿着链接的人不需要知道是哪一种，猜令牌的人更不该知道。
	ErrPasswordResetInvalid = errors.New("重置链接无效或已经用过")
	// ErrPasswordResetExpired 可以明说：链接确实存在过，只是窗口关了。
	ErrPasswordResetExpired = errors.New("重置链接已过期，请重新申请")
)

func passFingerprint(passhash string) string {
	sum := sha256.Sum256([]byte(passhash))
	return hex.EncodeToString(sum[:])
}

// CreatePasswordReset 为一个账号开一条重置链接，返回只存在于这一次的原文令牌。
// 新链接会作废这个人手上所有还没用过的旧链接。
func (db *DB) CreatePasswordReset(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = PasswordResetTTL
	}
	var passhash, status string
	err := db.SQL.QueryRowContext(ctx,
		`SELECT passhash, status FROM users WHERE id=?`, userID).Scan(&passhash, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if status != "active" {
		return "", ErrAccountDisabled
	}
	raw, err := NewSecret(32)
	if err != nil {
		return "", err
	}
	now := nowMS()
	err = db.InTx(ctx, func(tx *sql.Tx) error {
		// 过期行没有保留价值；顺手清掉，避免这张表只增不减。
		if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE expires_at < ?`, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM password_resets WHERE user_id=? AND used_at IS NULL`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO password_resets (token_hash, user_id, pass_fingerprint, created_at, expires_at)
			 VALUES (?, ?, ?, ?, ?)`,
			hashToken(raw), userID, passFingerprint(passhash), now, now+ttl.Milliseconds())
		return err
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

// PasswordResetUser 核对一条重置令牌并返回账号 id，但不花掉它。GET 页面用它
// 决定是显示表单还是"链接失效"，所以它必须是只读的。
func (db *DB) PasswordResetUser(ctx context.Context, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, ErrPasswordResetInvalid
	}
	var (
		userID   int64
		fp, hash string
		expires  int64
		status   string
	)
	err := db.SQL.QueryRowContext(ctx,
		`SELECT r.user_id, r.pass_fingerprint, r.expires_at, u.passhash, u.status
		 FROM password_resets r JOIN users u ON u.id=r.user_id
		 WHERE r.token_hash=? AND r.used_at IS NULL`, hashToken(raw)).
		Scan(&userID, &fp, &expires, &hash, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPasswordResetInvalid
	}
	if err != nil {
		return 0, err
	}
	if status != "active" || fp != passFingerprint(hash) {
		return 0, ErrPasswordResetInvalid
	}
	if expires <= nowMS() {
		return 0, ErrPasswordResetExpired
	}
	return userID, nil
}

// ConsumePasswordReset 改掉密码并花掉令牌。bcrypt 放在事务外：它要几十毫秒，
// 不该让整库的写锁陪着等；事务里的那一次重新核对才是"恰好一次"的保证。
func (db *DB) ConsumePasswordReset(ctx context.Context, raw, password string, cost int) error {
	userID, err := db.PasswordResetUser(ctx, raw)
	if err != nil {
		return err
	}
	hash, err := HashPassword(password, cost)
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	now := nowMS()
	return db.InTx(ctx, func(tx *sql.Tx) error {
		var (
			currentUser  int64
			fp, passhash string
			expires      int64
			status       string
		)
		err := tx.QueryRowContext(ctx,
			`SELECT r.user_id, r.pass_fingerprint, r.expires_at, u.passhash, u.status
			 FROM password_resets r JOIN users u ON u.id=r.user_id
			 WHERE r.token_hash=? AND r.used_at IS NULL`, hashToken(raw)).
			Scan(&currentUser, &fp, &expires, &passhash, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPasswordResetInvalid
		}
		if err != nil {
			return err
		}
		// 几个并发的提交只可能有一个走到这里：另一个的 used_at 已经不是 NULL。
		if currentUser != userID || status != "active" || fp != passFingerprint(passhash) {
			return ErrPasswordResetInvalid
		}
		if expires <= now {
			return ErrPasswordResetExpired
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET passhash=? WHERE id=?`, hash, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE password_resets SET used_at=? WHERE token_hash=?`,
			now, hashToken(raw)); err != nil {
			return err
		}
		// 同一个账号的其他未用链接一起作废；所有会话强制下线。
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM password_resets WHERE user_id=? AND used_at IS NULL`, userID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID)
		return err
	})
}
