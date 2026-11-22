-- 0055: 个人页的学术模式与比赛期间的隐身开关。
-- 两列都只影响公开展示：判题、榜单、权限与评分一律照旧。
ALTER TABLE users ADD COLUMN academic_mode INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN stealth INTEGER NOT NULL DEFAULT 0;
