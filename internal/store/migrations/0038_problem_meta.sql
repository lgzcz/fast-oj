-- 题库的三元信息：难度、来源、标签。都是可选展示，不影响判题。
ALTER TABLE problems ADD COLUMN difficulty INTEGER NOT NULL DEFAULT 0;
ALTER TABLE problems ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE problems ADD COLUMN tags TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_problems_difficulty ON problems(difficulty);
