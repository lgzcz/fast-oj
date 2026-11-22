-- 0044: 题目与工单附件。正文只存元数据；文件在数据目录 attachments/ 下，
-- 磁盘名是随机串而不是上传时的名字。下载一律经过鉴权路由 /attachments/{id}。
CREATE TABLE attachments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_kind  TEXT    NOT NULL CHECK (owner_kind IN ('problem','ticket')),
    owner_id    INTEGER NOT NULL,
    uploader_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    name        TEXT    NOT NULL,
    rel_path    TEXT    NOT NULL UNIQUE,
    mime        TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE INDEX attachments_owner ON attachments(owner_kind, owner_id);
