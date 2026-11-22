-- 0056: 客观题（单选自测）。不计入比赛榜，也不走判机队列。
CREATE TABLE objective_sets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    visible    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX objective_sets_list ON objective_sets(id DESC);

CREATE TABLE objective_questions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    set_id       INTEGER NOT NULL REFERENCES objective_sets(id) ON DELETE CASCADE,
    ordinal      INTEGER NOT NULL,
    prompt       TEXT    NOT NULL,
    options      TEXT    NOT NULL,
    answer_index INTEGER NOT NULL,
    created_at   INTEGER NOT NULL
);
CREATE UNIQUE INDEX objective_questions_order ON objective_questions(set_id, ordinal);

CREATE TABLE objective_attempts (
    set_id     INTEGER NOT NULL REFERENCES objective_sets(id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    answers    TEXT    NOT NULL,
    score      INTEGER NOT NULL,
    total      INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (set_id, user_id)
);
