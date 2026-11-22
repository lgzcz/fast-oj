package store

import "context"

// Attachment is one uploaded file attached to a problem or a ticket. The body
// lives on disk (attachfs); this row is the only record of who uploaded what.
type Attachment struct {
	ID         int64
	OwnerKind  string // problem / ticket
	OwnerID    int64
	UploaderID int64
	Name       string
	RelPath    string
	MIME       string
	Size       int64
	CreatedAt  int64
}

const (
	AttachProblem = "problem"
	AttachTicket  = "ticket"
)

func ValidAttachKind(kind string) bool {
	return kind == AttachProblem || kind == AttachTicket
}

func (db *DB) CreateAttachment(ctx context.Context, a *Attachment) (int64, error) {
	a.CreatedAt = nowMS()
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO attachments (owner_kind, owner_id, uploader_id, name, rel_path, mime, size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.OwnerKind, a.OwnerID, nullInt64(a.UploaderID), a.Name, a.RelPath, a.MIME, a.Size, a.CreatedAt)
	if err != nil {
		return 0, err
	}
	a.ID, _ = res.LastInsertId()
	return a.ID, nil
}

func (db *DB) ListAttachments(ctx context.Context, kind string, ownerID int64) ([]Attachment, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, owner_kind, owner_id, IFNULL(uploader_id, 0), name, rel_path, mime, size, created_at
		 FROM attachments WHERE owner_kind=? AND owner_id=? ORDER BY id`, kind, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.OwnerKind, &a.OwnerID, &a.UploaderID,
			&a.Name, &a.RelPath, &a.MIME, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) GetAttachment(ctx context.Context, id int64) (Attachment, error) {
	var a Attachment
	err := db.SQL.QueryRowContext(ctx,
		`SELECT id, owner_kind, owner_id, IFNULL(uploader_id, 0), name, rel_path, mime, size, created_at
		 FROM attachments WHERE id=?`, id).
		Scan(&a.ID, &a.OwnerKind, &a.OwnerID, &a.UploaderID, &a.Name, &a.RelPath, &a.MIME, &a.Size, &a.CreatedAt)
	return a, notFound(err)
}

func (db *DB) DeleteAttachment(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM attachments WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOwnerAttachments removes every row of one problem/ticket and returns the
// relative paths so the caller can delete the files.
func (db *DB) DeleteOwnerAttachments(ctx context.Context, kind string, ownerID int64) ([]string, error) {
	items, err := db.ListAttachments(ctx, kind, ownerID)
	if err != nil {
		return nil, err
	}
	if _, err := db.SQL.ExecContext(ctx,
		`DELETE FROM attachments WHERE owner_kind=? AND owner_id=?`, kind, ownerID); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(items))
	for _, a := range items {
		paths = append(paths, a.RelPath)
	}
	return paths, nil
}
