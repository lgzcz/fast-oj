-- 0045: 题单（训练）。题单是"按某种学习顺序排的一串题"，不是比赛：
-- 它不带计时、不参与榜单，公开题单谁都能看，隐藏题单只有作者和有全见权限的人看得到。
CREATE TABLE trainings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    intro      TEXT    NOT NULL DEFAULT '',
    visible    INTEGER NOT NULL DEFAULT 1,
    owner_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX trainings_public ON trainings(visible, id DESC);

CREATE TABLE training_problems (
    training_id INTEGER NOT NULL REFERENCES trainings(id) ON DELETE CASCADE,
    problem_id  INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    PRIMARY KEY (training_id, problem_id)
);
CREATE INDEX training_problems_order ON training_problems(training_id, position, problem_id);
