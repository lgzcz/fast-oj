-- 0050: rating、虚拟比赛与周赛。
ALTER TABLE users ADD COLUMN rating INTEGER NOT NULL DEFAULT 1200;
ALTER TABLE contests ADD COLUMN rated INTEGER NOT NULL DEFAULT 0;
ALTER TABLE contests ADD COLUMN rating_done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE contests ADD COLUMN weekly INTEGER NOT NULL DEFAULT 0;

-- 每场计分比赛只结算一次；这两列一起构成"谁在这场比赛前后是多少分"的账。
CREATE TABLE contest_ratings (
    contest_id    INTEGER NOT NULL REFERENCES contests(id) ON DELETE CASCADE,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    before_rating INTEGER NOT NULL,
    after_rating  INTEGER NOT NULL,
    delta         INTEGER NOT NULL,
    created_at    INTEGER NOT NULL,
    PRIMARY KEY (contest_id, user_id)
);
CREATE INDEX contest_ratings_user ON contest_ratings(user_id, contest_id DESC);

-- 虚拟比赛是"用原来的时长重打一场已经结束的比赛"：只给自己计时，
-- 不进比赛的正式榜，也不参与 rating。
CREATE TABLE virtual_contests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contest_id  INTEGER NOT NULL REFERENCES contests(id) ON DELETE CASCADE,
    start_at    INTEGER NOT NULL,
    end_at      INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);
CREATE INDEX virtual_contests_user ON virtual_contests(user_id, id DESC);
