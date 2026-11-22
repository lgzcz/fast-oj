-- 0049: 二次验证（TOTP + 一次性恢复码）与登录设备管理。
-- 会话本来就记了 ip 与 user_agent；这里只补"用户主动踢掉某台设备"所缺的两条查询。
CREATE TABLE user_totp (
    user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret       TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    confirmed_at INTEGER NOT NULL DEFAULT 0,
    last_used_at INTEGER NOT NULL DEFAULT 0
);

-- 恢复码只存 sha256：数据库被看到也换不回能登录的码。
CREATE TABLE recovery_codes (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT    NOT NULL,
    used_at    INTEGER,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);
CREATE INDEX recovery_codes_user ON recovery_codes(user_id, used_at);

-- 密码已通过、还差第二步的短暂凭证。它不进 sessions：没通过第二步之前，
-- 这个 cookie 不能读任何页面。attempts 用来防止对六位码的暴力尝试。
CREATE TABLE login_challenges (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    next       TEXT    NOT NULL DEFAULT '',
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX login_challenges_expiry ON login_challenges(expires_at);
