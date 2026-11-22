-- 0043: 判机失联后的自动重派。attempts 在每次领取时 +1；租约过期时
-- 未用完次数的提交回到队列（清掉上一次的逐用例行），用完才落成 abandoned/IE。
-- 管理员手动重判与手动重排会把 attempts 归零，等同于"再给三次机会"。
ALTER TABLE submissions ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
