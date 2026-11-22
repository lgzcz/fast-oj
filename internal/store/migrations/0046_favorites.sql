-- 0046: 题目收藏。收藏是私人的，不参与题目可见性判断：
-- 题目后来藏起来，收藏者仍能在自己的列表里看到它，但点进去照常 404。
CREATE TABLE favorites (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, problem_id)
);
CREATE INDEX favorites_list ON favorites(user_id, created_at DESC);
