package store

import (
	"context"
	"database/sql"
)

const (
	ImageAvatar = "avatar"
	ImageCover  = "cover"
	ImagePost   = "post"
)

// Image 是一张上传图片的元信息；正文落在附件目录里，文件名随机。
type Image struct {
	ID        int64
	UserID    int64
	Kind      string // avatar / cover / post
	Rel       string
	MIME      string
	Size      int64
	CreatedAt int64
}

func (db *DB) CreateImage(ctx context.Context, img *Image) (Image, error) {
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO images (user_id, kind, rel, mime, size, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		img.UserID, img.Kind, img.Rel, img.MIME, img.Size, nowMS())
	if err != nil {
		return Image{}, err
	}
	img.ID, _ = res.LastInsertId()
	return *img, nil
}

func (db *DB) GetImage(ctx context.Context, id int64) (Image, error) {
	var img Image
	err := db.SQL.QueryRowContext(ctx,
		`SELECT id, user_id, kind, rel, mime, size, created_at FROM images WHERE id=?`, id).
		Scan(&img.ID, &img.UserID, &img.Kind, &img.Rel, &img.MIME, &img.Size, &img.CreatedAt)
	return img, notFound(err)
}

// LatestImage answers the avatar/cover slot: the newest upload of that kind wins.
func (db *DB) LatestImage(ctx context.Context, userID int64, kind string) (Image, bool, error) {
	var img Image
	err := db.SQL.QueryRowContext(ctx,
		`SELECT id, user_id, kind, rel, mime, size, created_at FROM images
		 WHERE user_id=? AND kind=? ORDER BY id DESC LIMIT 1`, userID, kind).
		Scan(&img.ID, &img.UserID, &img.Kind, &img.Rel, &img.MIME, &img.Size, &img.CreatedAt)
	if err == sql.ErrNoRows {
		return img, false, nil
	}
	return img, err == nil, err
}

func (db *DB) ListImages(ctx context.Context, userID int64, kind string, limit int) ([]Image, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, user_id, kind, rel, mime, size, created_at FROM images
		 WHERE user_id=? AND (?='' OR kind=?) ORDER BY id DESC LIMIT ?`, userID, kind, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Image
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.UserID, &img.Kind, &img.Rel, &img.MIME, &img.Size, &img.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func (db *DB) CountImages(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM images WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

func (db *DB) DeleteImage(ctx context.Context, id, userID int64) (Image, bool, error) {
	img, err := db.GetImage(ctx, id)
	if err != nil {
		return img, false, err
	}
	if img.UserID != userID {
		return img, false, nil
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM images WHERE id=?`, id); err != nil {
		return img, false, err
	}
	return img, true, nil
}
