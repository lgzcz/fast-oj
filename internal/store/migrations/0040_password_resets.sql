-- 0040: 忘记密码。令牌只存 SHA-256 哈希；pass_fingerprint 绑定签发时的密码哈希，
-- 之后改过密码，旧链接自然失效。used_at 让"恰好一次"可以和改密码、踢会话放进同一个事务。
CREATE TABLE password_resets (
    token_hash       TEXT    PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pass_fingerprint TEXT    NOT NULL,
    created_at       INTEGER NOT NULL,
    expires_at       INTEGER NOT NULL,
    used_at          INTEGER
);
CREATE INDEX password_resets_user ON password_resets(user_id);
