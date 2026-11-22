-- 0054: 群聊、剪贴板与帖子投票。
CREATE TABLE chat_rooms (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    owner_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX chat_rooms_list ON chat_rooms(id DESC);

CREATE TABLE chat_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    room_id    INTEGER NOT NULL REFERENCES chat_rooms(id) ON DELETE CASCADE,
    user_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
    body       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX chat_messages_room ON chat_messages(room_id, id DESC);

CREATE TABLE clipboards (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX clipboards_user ON clipboards(user_id, id DESC);

CREATE TABLE polls (
    topic_id   INTEGER PRIMARY KEY REFERENCES topics(id) ON DELETE CASCADE,
    question   TEXT    NOT NULL,
    options    TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE poll_votes (
    topic_id     INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    option_index INTEGER NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (topic_id, user_id)
);
