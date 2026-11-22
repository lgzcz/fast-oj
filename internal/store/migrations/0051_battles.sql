-- 0051: 对抗赛（battle）。一场比赛只围绕一道题，参加者各自在窗口内做题，
-- 榜按窗口内的最好得分与首达时间算。它不是正式比赛：不进 rating、不进比赛榜。
CREATE TABLE battles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    owner_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    start_at   INTEGER NOT NULL,
    end_at     INTEGER NOT NULL,
    visible    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    CHECK (end_at > start_at)
);
CREATE INDEX battles_list ON battles(visible, id DESC);

CREATE TABLE battle_participants (
    battle_id INTEGER NOT NULL REFERENCES battles(id) ON DELETE CASCADE,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at INTEGER NOT NULL,
    PRIMARY KEY (battle_id, user_id)
);
CREATE INDEX battle_participants_user ON battle_participants(user_id, battle_id);
