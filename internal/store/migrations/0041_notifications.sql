-- 0041: 站内通知（回复 / @提及）。summary 与 link 在写入时定稿：通知是
-- "当时发生过什么"的记录，不该因为楼主后来改了标题就跟着变。
CREATE TABLE notifications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    actor_handle TEXT    NOT NULL,
    actor_name   TEXT    NOT NULL,
    kind         TEXT    NOT NULL CHECK (kind IN ('reply','mention')),
    summary      TEXT    NOT NULL,
    link         TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    read_at      INTEGER
);
CREATE INDEX notifications_inbox ON notifications(user_id, id DESC);
CREATE INDEX notifications_unread ON notifications(user_id) WHERE read_at IS NULL;
