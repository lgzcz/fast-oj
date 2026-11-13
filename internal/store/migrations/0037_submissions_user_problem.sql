-- 0037: 题库列表要一次回答"这一页每道题我做过没有"。
--
-- 查询按 (user, problem) 收窄，已有的用户索引只覆盖 user_id，还是要扫一遍这个人的
-- 全部提交；这道索引让那一页的开销只跟这一页的题数有关。
CREATE INDEX idx_submissions_user_problem ON submissions(user_id, problem_id);
