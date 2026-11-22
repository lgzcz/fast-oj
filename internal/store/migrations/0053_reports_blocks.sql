-- 0053: 举报与用户拉黑。
CREATE TABLE reports (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    reporter_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    target_kind  TEXT    NOT NULL CHECK (target_kind IN ('topic','comment','user','article')),
    target_id    INTEGER NOT NULL,
    target_label TEXT    NOT NULL DEFAULT '',
    target_url   TEXT    NOT NULL DEFAULT '',
    reason       TEXT    NOT NULL CHECK (reason IN ('spam','abuse','cheat','other')),
    detail       TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
    handler_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    resolution   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX reports_queue ON reports(status, id DESC);
CREATE UNIQUE INDEX reports_one_open ON reports(reporter_id, target_kind, target_id) WHERE status='open';

-- 拉黑是单向的：拉黑者看不见对方的私信与关注，反之不自动成立。
CREATE TABLE user_blocks (
    blocker_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (blocker_id, blocked_id)
);
CREATE INDEX user_blocks_blocked ON user_blocks(blocked_id);
