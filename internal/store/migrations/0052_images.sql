-- 0052: 图床与头像/封面。图片本身落在 attachments/image/<user>/<random>，
-- 库里只存相对路径与 MIME；/images/{id} 公开读取，未列出的随机文件名不可猜。
CREATE TABLE images (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL CHECK (kind IN ('avatar','cover','post')),
    rel        TEXT    NOT NULL,
    mime       TEXT    NOT NULL,
    size       INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX images_user ON images(user_id, id DESC);
CREATE INDEX images_owner_kind ON images(user_id, kind, id DESC);
