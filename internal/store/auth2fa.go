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

var ErrTOTPNotSet = errors.New("totp not set")

// 登录挑战的有效期短于任何会话；过期的就直接当不存在。
const LoginChallengeTTL = 10 * time.Minute

type TOTPState struct {
	Secret      string
	Enabled     bool
	ConfirmedAt int64
	LastUsedAt  int64
}

func (db *DB) GetTOTP(ctx context.Context, userID int64) (TOTPState, error) {
	var s TOTPState
	var enabled int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT secret, enabled, confirmed_at, last_used_at FROM user_totp WHERE user_id=?`, userID).
		Scan(&s.Secret, &enabled, &s.ConfirmedAt, &s.LastUsedAt)
	if err == sql.ErrNoRows {
		return s, ErrTOTPNotSet
	}
	s.Enabled = enabled != 0
	return s, err
}

// SetTOTPSecret 写入一个尚未确认的 secret；确认之前登录流程不会要求它。
func (db *DB) SetTOTPSecret(ctx context.Context, userID int64, secret string) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO user_totp (user_id, secret, enabled, created_at) VALUES (?, ?, 0, ?)
		 ON CONFLICT(user_id) DO UPDATE SET secret=excluded.secret, enabled=0, confirmed_at=0`,
		userID, secret, nowMS())
	return err
}

func (db *DB) EnableTOTP(ctx context.Context, userID int64) error {
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE user_totp SET enabled=1, confirmed_at=? WHERE user_id=?`, nowMS(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTOTPNotSet
	}
	return nil
}

func (db *DB) DisableTOTP(ctx context.Context, userID int64) error {
	return db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id=?`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID)
		return err
	})
}

func (db *DB) TouchTOTP(ctx context.Context, userID int64) error {
	_, err := db.SQL.ExecContext(ctx,
		`UPDATE user_totp SET last_used_at=? WHERE user_id=?`, nowMS(), userID)
	return err
}

// RecoveryCodeHash 是恢复码在库里的形状：去掉连字符、转大写，再 sha256。
func RecoveryCodeHash(code string) string {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte("fastoj-recovery:" + clean))
	return hex.EncodeToString(sum[:])
}

func (db *DB) ReplaceRecoveryCodes(ctx context.Context, userID int64, codes []string) error {
	return db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
			return err
		}
		now := nowMS()
		for _, code := range codes {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, ?)`,
				userID, RecoveryCodeHash(code), now); err != nil {
				return err
			}
		}
		return nil
	})
}

// ConsumeRecoveryCode 是一张单程票：同一枚恢复码第二次用会返回 false。
func (db *DB) ConsumeRecoveryCode(ctx context.Context, userID int64, code string) (bool, error) {
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE recovery_codes SET used_at=? WHERE user_id=? AND code_hash=? AND used_at IS NULL`,
		nowMS(), userID, RecoveryCodeHash(code))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (db *DB) CountRecoveryCodes(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM recovery_codes WHERE user_id=? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

type LoginChallenge struct {
	ID        string
	UserID    int64
	Next      string
	Attempts  int
	ExpiresAt int64
}

func (db *DB) CreateLoginChallenge(ctx context.Context, userID int64, next string) (LoginChallenge, error) {
	id, err := NewSecret(24)
	if err != nil {
		return LoginChallenge{}, err
	}
	now := nowMS()
	if _, err := db.SQL.ExecContext(ctx,
		`DELETE FROM login_challenges WHERE expires_at <= ?`, now); err != nil {
		return LoginChallenge{}, err
	}
	ch := LoginChallenge{ID: id, UserID: userID, Next: next,
		ExpiresAt: now + LoginChallengeTTL.Milliseconds()}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO login_challenges (id, user_id, next, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		ch.ID, ch.UserID, ch.Next, now, ch.ExpiresAt)
	return ch, err
}

func (db *DB) GetLoginChallenge(ctx context.Context, id string) (LoginChallenge, error) {
	var ch LoginChallenge
	var created int64
	err := db.SQL.QueryRowContext(ctx,
		`SELECT id, user_id, next, attempts, created_at, expires_at FROM login_challenges WHERE id=?`, id).
		Scan(&ch.ID, &ch.UserID, &ch.Next, &ch.Attempts, &created, &ch.ExpiresAt)
	if err != nil {
		return ch, notFound(err)
	}
	if ch.ExpiresAt <= nowMS() {
		return ch, ErrNotFound
	}
	return ch, nil
}

func (db *DB) BumpLoginChallenge(ctx context.Context, id string) (int, error) {
	if _, err := db.SQL.ExecContext(ctx,
		`UPDATE login_challenges SET attempts=attempts+1 WHERE id=?`, id); err != nil {
		return 0, err
	}
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT attempts FROM login_challenges WHERE id=?`, id).Scan(&n)
	return n, err
}

func (db *DB) DeleteLoginChallenge(ctx context.Context, id string) error {
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM login_challenges WHERE id=?`, id)
	return err
}

// SessionInfo 是"登录设备"页要展示的东西。ID 本身是 cookie 的值，
// 只回给会话所有者；页面上只显示前缀。
type SessionInfo struct {
	ID         string
	CreatedAt  int64
	LastSeenAt int64
	ExpiresAt  int64
	IP         string
	UserAgent  string
}

func (db *DB) ListSessions(ctx context.Context, userID int64) ([]SessionInfo, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, created_at, last_seen_at, expires_at, ip, user_agent
		 FROM sessions WHERE user_id=? AND expires_at>? ORDER BY last_seen_at DESC`, userID, nowMS())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var s SessionInfo
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.IP, &s.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (db *DB) RevokeSession(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM sessions WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (db *DB) RevokeOtherSessions(ctx context.Context, userID int64, keepID string) (int, error) {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id=? AND id<>?`, userID, keepID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
