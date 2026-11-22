-- 0042: 权限到期。expires_at=0 表示永久；只对 granted=1 的行生效。
-- 收回（granted=0）没有到期时间：一条会自己失效的"禁止"会让被收回的人某天
-- 悄悄拿回角色自带的能力，那种"恢复"没有人会预料到。
ALTER TABLE user_permissions ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0;
