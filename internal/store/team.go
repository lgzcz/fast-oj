package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrGroupJoinCode = errors.New("加入码不对")
	ErrGroupFull     = errors.New("团队人数已满")
	ErrGroupCaptain  = errors.New("只有队长能做这个")
)

const (
	MaxGroupName    = 60
	MaxGroupIntro   = 5000
	MaxGroupMembers = 200
	MaxGroupProblems = 500
	// 一个人同时加入的团队数：团队不是群聊，别让列表被拉满。
	MaxGroupsPerUser = 20
)

type Group struct {
	ID           int64
	Name         string
	Intro        string
	JoinCode     string
	OwnerID      int64
	OwnerHandle  string
	OwnerName    string
	Members      int
	Problems     int
	CreatedAt    int64
	UpdatedAt    int64
	Mine         bool
	MyRole       string
}

type GroupMember struct {
	UserID      int64
	Handle      string
	DisplayName string
	Color       string
	Role        string
	JoinedAt    int64
}

func (m GroupMember) Name() string {
	if strings.TrimSpace(m.DisplayName) != "" {
		return m.DisplayName
	}
	return m.Handle
}

type GroupProblem struct {
	Problem
	AddedBy   string
	AddedAt   int64
	Solved    bool
}

const teamColumns = `t.id, t.name, t.intro, t.join_code, IFNULL(t.owner_id, 0),
	IFNULL(u.handle, ''), IFNULL(u.display_name, ''), t.created_at, t.updated_at,
	(SELECT COUNT(*) FROM team_members tm WHERE tm.team_id=t.id),
	(SELECT COUNT(*) FROM team_problems tp WHERE tp.team_id=t.id),
	EXISTS(SELECT 1 FROM team_members tm WHERE tm.team_id=t.id AND tm.user_id=?),
	IFNULL((SELECT tm.role FROM team_members tm WHERE tm.team_id=t.id AND tm.user_id=?), '')`

func scanGroup(row scanner) (Group, error) {
	var t Group
	var mine int
	err := row.Scan(&t.ID, &t.Name, &t.Intro, &t.JoinCode, &t.OwnerID,
		&t.OwnerHandle, &t.OwnerName, &t.CreatedAt, &t.UpdatedAt,
		&t.Members, &t.Problems, &mine, &t.MyRole)
	t.Mine = mine != 0
	return t, notFound(err)
}

func (db *DB) GetGroup(ctx context.Context, id, viewerID int64) (Group, error) {
	return scanGroup(db.SQL.QueryRowContext(ctx,
		`SELECT `+teamColumns+` FROM teams t LEFT JOIN users u ON u.id=t.owner_id WHERE t.id=?`,
		nullInt64(viewerID), nullInt64(viewerID), id))
}

func (db *DB) CreateGroup(ctx context.Context, ownerID int64, name, intro, joinCode string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxGroupName {
		return Group{}, fmt.Errorf("团队名要 1..%d 个字", MaxGroupName)
	}
	if utf8.RuneCountInString(intro) > MaxGroupIntro {
		return Group{}, fmt.Errorf("简介最长 %d 个字", MaxGroupIntro)
	}
	var owned int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM teams WHERE owner_id=?`, ownerID).Scan(&owned); err != nil {
		return Group{}, err
	}
	if owned >= 5 {
		return Group{}, fmt.Errorf("每个人最多创建 5 个团队")
	}
	now := nowMS()
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO teams (name, intro, join_code, owner_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`, name, intro, joinCode, ownerID, now, now)
	if err != nil {
		return Group{}, conflict(err, "团队名")
	}
	id, _ := res.LastInsertId()
	if _, err := db.SQL.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role, joined_at) VALUES (?, ?, 'captain', ?)`,
		id, ownerID, now); err != nil {
		return Group{}, err
	}
	return db.GetGroup(ctx, id, ownerID)
}

type GroupFilter struct {
	ViewerID int64
	Search   string
	After    int64
	Limit    int
}

func (db *DB) ListGroups(ctx context.Context, f GroupFilter) ([]Group, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	q := `SELECT ` + teamColumns + ` FROM teams t LEFT JOIN users u ON u.id=t.owner_id WHERE 1=1`
	args := []any{nullInt64(f.ViewerID), nullInt64(f.ViewerID)}
	if f.Search != "" {
		like := "%" + escapeLike(f.Search) + "%"
		q += ` AND (t.name LIKE ? ESCAPE '\' OR t.intro LIKE ? ESCAPE '\')`
		args = append(args, like, like)
	}
	if f.After > 0 {
		q += ` AND t.id < ?`
		args = append(args, f.After)
	}
	q += ` ORDER BY t.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		t, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) UpdateGroup(ctx context.Context, id int64, name, intro, joinCode string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxGroupName {
		return fmt.Errorf("团队名要 1..%d 个字", MaxGroupName)
	}
	if utf8.RuneCountInString(intro) > MaxGroupIntro {
		return fmt.Errorf("简介最长 %d 个字", MaxGroupIntro)
	}
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE teams SET name=?, intro=?, join_code=?, updated_at=? WHERE id=?`,
		name, intro, joinCode, nowMS(), id)
	if err != nil {
		return conflict(err, "团队名")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteGroup(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM teams WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) GroupRole(ctx context.Context, teamID, userID int64) (string, error) {
	if userID == 0 {
		return "", nil
	}
	var role string
	err := db.SQL.QueryRowContext(ctx,
		`SELECT role FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

// JoinGroup is idempotent for someone already inside. The code is checked before
// the capacity check so a wrong code never tells a stranger how full the team is.
func (db *DB) JoinGroup(ctx context.Context, teamID, userID int64, code string) (Group, error) {
	t, err := db.GetGroup(ctx, teamID, userID)
	if err != nil {
		return t, err
	}
	if t.Mine {
		return t, nil
	}
	if t.JoinCode != "" && code != t.JoinCode {
		return t, ErrGroupJoinCode
	}
	if t.Members >= MaxGroupMembers {
		return t, ErrGroupFull
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_members WHERE user_id=?`, userID).Scan(&count); err != nil {
		return t, err
	}
	if count >= MaxGroupsPerUser {
		return t, fmt.Errorf("最多同时加入 %d 个团队", MaxGroupsPerUser)
	}
	if _, err := db.SQL.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)
		 ON CONFLICT(team_id, user_id) DO NOTHING`, teamID, userID, nowMS()); err != nil {
		return t, err
	}
	return db.GetGroup(ctx, teamID, userID)
}

func (db *DB) LeaveGroup(ctx context.Context, teamID, userID int64) error {
	role, err := db.GroupRole(ctx, teamID, userID)
	if err != nil {
		return err
	}
	if role == "" {
		return ErrNotFound
	}
	if role == "captain" {
		return ErrGroupCaptain
	}
	_, err = db.SQL.ExecContext(ctx,
		`DELETE FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID)
	return err
}

func (db *DB) RemoveGroupMember(ctx context.Context, teamID, userID int64) error {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM team_members WHERE team_id=? AND user_id=? AND role<>'captain'`, teamID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) ListGroupMembers(ctx context.Context, teamID int64) ([]GroupMember, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT u.id, u.handle, IFNULL(u.display_name, ''), IFNULL(u.color, ''), tm.role, tm.joined_at
		 FROM team_members tm JOIN users u ON u.id=tm.user_id
		 WHERE tm.team_id=? ORDER BY CASE tm.role WHEN 'captain' THEN 0 ELSE 1 END, tm.joined_at, u.id`,
		teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.UserID, &m.Handle, &m.DisplayName, &m.Color, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) AddGroupProblem(ctx context.Context, teamID, problemID, addedBy int64) error {
	var one int
	if err := db.SQL.QueryRowContext(ctx, `SELECT 1 FROM problems WHERE id=?`, problemID).Scan(&one); err != nil {
		return notFound(err)
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_problems WHERE team_id=?`, teamID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxGroupProblems {
		return fmt.Errorf("团队题库最多 %d 道题", MaxGroupProblems)
	}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO team_problems (team_id, problem_id, added_by, created_at)
		 VALUES (?, ?, ?, ?) ON CONFLICT(team_id, problem_id) DO NOTHING`,
		teamID, problemID, nullInt64(addedBy), nowMS())
	return err
}

func (db *DB) RemoveGroupProblem(ctx context.Context, teamID, problemID int64) (bool, error) {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM team_problems WHERE team_id=? AND problem_id=?`, teamID, problemID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (db *DB) ListGroupProblems(ctx context.Context, teamID, viewerID int64) ([]GroupProblem, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT p.id, p.pid, p.title, p.statement, p.time_limit_ms, p.mem_limit_kb, p.languages,
			p.visible, IFNULL(p.owner_id, 0), p.created_at, p.updated_at, IFNULL(p.detail_level, 'diff'),
			p.difficulty, IFNULL(p.source, ''), IFNULL(p.tags, ''), IFNULL(p.io_input, ''), IFNULL(p.io_output, ''),
			IFNULL(u.handle, ''), tp.created_at,
			EXISTS(SELECT 1 FROM submissions s WHERE s.user_id=? AND s.problem_id=p.id AND s.verdict='AC')
		 FROM team_problems tp JOIN problems p ON p.id=tp.problem_id
		 LEFT JOIN users u ON u.id=tp.added_by
		 WHERE tp.team_id=? ORDER BY tp.created_at DESC, p.id DESC`, viewerID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupProblem
	for rows.Next() {
		var tp GroupProblem
		var langs, tags string
		var visible, solved int
		if err := rows.Scan(&tp.ID, &tp.PID, &tp.Title, &tp.Statement, &tp.TimeLimitMS, &tp.MemLimitKB,
			&langs, &visible, &tp.OwnerID, &tp.CreatedAt, &tp.UpdatedAt, &tp.DetailLevel,
			&tp.Difficulty, &tp.Source, &tags, &tp.InputFile, &tp.OutputFile,
			&tp.AddedBy, &tp.AddedAt, &solved); err != nil {
			return nil, err
		}
		tp.Languages = splitList(langs)
		tp.Tags = splitList(tags)
		tp.Visible = visible != 0
		tp.Solved = solved != 0
		out = append(out, tp)
	}
	return out, rows.Err()
}
