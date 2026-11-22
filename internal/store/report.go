package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	ReportTopic   = "topic"
	ReportComment = "comment"
	ReportUser    = "user"
	ReportArticle = "article"

	ReportSpam  = "spam"
	ReportAbuse = "abuse"
	ReportCheat = "cheat"
	ReportOther = "other"

	MaxReportDetail = 1000
)

func ValidReportTarget(kind string) bool {
	switch kind {
	case ReportTopic, ReportComment, ReportUser, ReportArticle:
		return true
	}
	return false
}

func ValidReportReason(reason string) bool {
	switch reason {
	case ReportSpam, ReportAbuse, ReportCheat, ReportOther:
		return true
	}
	return false
}

func ReportReasonLabel(reason string) string {
	switch reason {
	case ReportSpam:
		return "垃圾广告"
	case ReportAbuse:
		return "辱骂/骚扰"
	case ReportCheat:
		return "抄袭/作弊"
	}
	return "其他"
}

func ReportTargetLabel(kind string) string {
	switch kind {
	case ReportTopic:
		return "帖子"
	case ReportComment:
		return "评论"
	case ReportUser:
		return "用户"
	}
	return "文章"
}

type Report struct {
	ID            int64
	ReporterID    int64
	ReporterHandle string
	ReporterName  string
	TargetKind    string
	TargetID      int64
	TargetLabel   string
	TargetURL     string
	Reason        string
	Detail        string
	Status        string
	HandlerID     int64
	HandlerHandle string
	Resolution    string
	CreatedAt     int64
	UpdatedAt     int64
}

func (db *DB) CreateReport(ctx context.Context, reporterID int64, kind string, targetID int64, label, targetURL, reason, detail string) (Report, error) {
	if !ValidReportTarget(kind) || !ValidReportReason(reason) {
		return Report{}, fmt.Errorf("举报目标或原因不认识")
	}
	if utf8.RuneCountInString(detail) > MaxReportDetail {
		return Report{}, fmt.Errorf("补充说明最长 %d 个字", MaxReportDetail)
	}
	now := nowMS()
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO reports (reporter_id, target_kind, target_id, target_label, target_url, reason, detail, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullInt64(reporterID), kind, targetID, label, targetURL, reason, detail, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Report{}, ErrConflict
		}
		return Report{}, err
	}
	id, _ := res.LastInsertId()
	return db.GetReport(ctx, id)
}

const reportColumns = `r.id, IFNULL(r.reporter_id, 0), IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
	r.target_kind, r.target_id, r.target_label, r.target_url, r.reason, r.detail, r.status,
	IFNULL(r.handler_id, 0), IFNULL(h.handle, ''), r.resolution, r.created_at, r.updated_at`

func scanReport(row scanner) (Report, error) {
	var r Report
	err := row.Scan(&r.ID, &r.ReporterID, &r.ReporterHandle, &r.ReporterName,
		&r.TargetKind, &r.TargetID, &r.TargetLabel, &r.TargetURL, &r.Reason, &r.Detail, &r.Status,
		&r.HandlerID, &r.HandlerHandle, &r.Resolution, &r.CreatedAt, &r.UpdatedAt)
	return r, notFound(err)
}

func (db *DB) GetReport(ctx context.Context, id int64) (Report, error) {
	return scanReport(db.SQL.QueryRowContext(ctx,
		`SELECT `+reportColumns+` FROM reports r
		 LEFT JOIN users u ON u.id=r.reporter_id LEFT JOIN users h ON h.id=r.handler_id WHERE r.id=?`, id))
}

func (db *DB) ListReports(ctx context.Context, status string, after int64, limit int) ([]Report, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + reportColumns + ` FROM reports r
		LEFT JOIN users u ON u.id=r.reporter_id LEFT JOIN users h ON h.id=r.handler_id WHERE 1=1`
	args := []any{}
	if status == "open" || status == "closed" {
		q += ` AND r.status=?`
		args = append(args, status)
	}
	if after > 0 {
		q += ` AND r.id<?`
		args = append(args, after)
	}
	q += ` ORDER BY r.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Report
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) CountOpenReports(ctx context.Context) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports WHERE status='open'`).Scan(&n)
	return n, err
}

func (db *DB) CloseReport(ctx context.Context, id, handlerID int64, resolution string) error {
	if utf8.RuneCountInString(resolution) > MaxReportDetail {
		return fmt.Errorf("处理说明最长 %d 个字", MaxReportDetail)
	}
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE reports SET status='closed', handler_id=?, resolution=?, updated_at=?
		 WHERE id=? AND status='open'`,
		nullInt64(handlerID), resolution, nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) BlockUser(ctx context.Context, blockerID, blockedID int64) error {
	if blockerID == blockedID {
		return fmt.Errorf("不能拉黑自己")
	}
	var one int
	if err := db.SQL.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=?`, blockedID).Scan(&one); err != nil {
		return notFound(err)
	}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO user_blocks (blocker_id, blocked_id, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(blocker_id, blocked_id) DO NOTHING`, blockerID, blockedID, nowMS())
	return err
}

func (db *DB) UnblockUser(ctx context.Context, blockerID, blockedID int64) (bool, error) {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, blockerID, blockedID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// IsBlocked reports whether blocker has blocked blocked (one direction).
func (db *DB) IsBlocked(ctx context.Context, blockerID, blockedID int64) (bool, error) {
	var one int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT 1 FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, blockerID, blockedID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// EitherBlocked is the DM/follow gate: one side blocking is enough to close the
// door, but the two people are not told which side did it.
func (db *DB) EitherBlocked(ctx context.Context, a, b int64) (bool, error) {
	if ok, err := db.IsBlocked(ctx, a, b); err != nil || ok {
		return ok, err
	}
	return db.IsBlocked(ctx, b, a)
}

func (db *DB) ListBlocked(ctx context.Context, blockerID int64, limit int) ([]User, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT `+userColumns+` FROM user_blocks b JOIN users u ON u.id=b.blocked_id
		 WHERE b.blocker_id=? ORDER BY b.created_at DESC LIMIT ?`, blockerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
