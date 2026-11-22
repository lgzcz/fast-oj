package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 投票挂在帖子上：一个问题，2..8 个选项，一人一票且可以改。
// options 用换行分隔存原文，读取时再拆；顺序就是页面上的顺序。
const (
	MaxPollQuestion = 200
	MaxPollOptions  = 8
	MaxPollOption   = 100
)

type Poll struct {
	TopicID  int64
	Question string
	Options  []string
	Votes    []int
	MyVote   int // -1 = 没投
	Total    int
}

// ValidatePoll reports whether the optional poll fields make a usable poll.
// Both blank means "no poll", which is the common case.
func ValidatePoll(question, rawOptions string) error {
	question = strings.TrimSpace(question)
	if question == "" && strings.TrimSpace(rawOptions) == "" {
		return nil
	}
	if question == "" {
		return fmt.Errorf("投票要写一个问题")
	}
	if utf8.RuneCountInString(question) > MaxPollQuestion {
		return fmt.Errorf("问题最长 %d 个字", MaxPollQuestion)
	}
	if strings.TrimSpace(rawOptions) == "" {
		return fmt.Errorf("投票至少要有 2 个选项")
	}
	_, err := ParsePollOptions(rawOptions)
	return err
}

func ParsePollOptions(raw string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > MaxPollOption {
			return nil, fmt.Errorf("每个选项最长 %d 个字", MaxPollOption)
		}
		out = append(out, line)
		if len(out) > MaxPollOptions {
			return nil, fmt.Errorf("最多 %d 个选项", MaxPollOptions)
		}
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("投票至少要有 2 个选项")
	}
	return out, nil
}

func (db *DB) CreatePoll(ctx context.Context, topicID int64, question, rawOptions string) error {
	question = strings.TrimSpace(question)
	if question == "" || utf8.RuneCountInString(question) > MaxPollQuestion {
		return fmt.Errorf("问题要 1..%d 个字", MaxPollQuestion)
	}
	options, err := ParsePollOptions(rawOptions)
	if err != nil {
		return err
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO polls (topic_id, question, options, created_at) VALUES (?, ?, ?, ?)`,
		topicID, question, strings.Join(options, "\n"), nowMS())
	return err
}

func (db *DB) GetPoll(ctx context.Context, topicID, viewerID int64) (Poll, bool, error) {
	var p Poll
	var raw string
	err := db.SQL.QueryRowContext(ctx,
		`SELECT topic_id, question, options FROM polls WHERE topic_id=?`, topicID).
		Scan(&p.TopicID, &p.Question, &raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, false, nil
		}
		return p, false, err
	}
	p.Options = strings.Split(raw, "\n")
	p.Votes = make([]int, len(p.Options))
	p.MyVote = -1
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT option_index, COUNT(*) FROM poll_votes WHERE topic_id=? GROUP BY option_index`, topicID)
	if err != nil {
		return p, true, err
	}
	for rows.Next() {
		var idx, n int
		if err := rows.Scan(&idx, &n); err != nil {
			rows.Close()
			return p, true, err
		}
		if idx >= 0 && idx < len(p.Votes) {
			p.Votes[idx] = n
			p.Total += n
		}
	}
	rows.Close()
	if viewerID != 0 {
		_ = db.SQL.QueryRowContext(ctx,
			`SELECT option_index FROM poll_votes WHERE topic_id=? AND user_id=?`, topicID, viewerID).
			Scan(&p.MyVote)
	}
	return p, true, nil
}

func (db *DB) VotePoll(ctx context.Context, topicID, userID int64, index int) error {
	var raw string
	if err := db.SQL.QueryRowContext(ctx, `SELECT options FROM polls WHERE topic_id=?`, topicID).Scan(&raw); err != nil {
		return notFound(err)
	}
	options := strings.Split(raw, "\n")
	if index < 0 || index >= len(options) {
		return fmt.Errorf("没有这个选项")
	}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO poll_votes (topic_id, user_id, option_index, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(topic_id, user_id) DO UPDATE SET option_index=excluded.option_index, created_at=excluded.created_at`,
		topicID, userID, index, nowMS())
	return err
}
