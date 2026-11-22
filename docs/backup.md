## 数据与备份

```
data/
├── fast-oj.db        SQLite（WAL 模式，迁移内嵌在二进制里，启动即自动建库）
├── fast-oj.db-wal
├── fast-oj.db-shm
├── cases/<题目id>/<序号>.in|.out    用例正文，库里只存相对路径
└── attachments/<problem|ticket>/<id>/<随机名>  题目与工单附件，库里只存元数据与相对路径
```

停掉服务后整目录拷贝就是备份；不想停服务，用 SQLite 自带的在线备份：

```sh
sudo apt install sqlite3               # 服务器上不一定预装，缺了这条命令会直接报 not found
sudo -u oj sqlite3 data/fast-oj.db ".backup '/var/backups/oj-$(date +%F).db'"
```

`.backup` 走 SQLite 的备份 API，WAL 里已提交但还没 checkpoint 的事务也会带上；**不能**在服务
运行时直接 `cp fast-oj.db`，那样可能少一截 WAL。用例正文是散文件，一起拷。

备份是一份**凭据转储**，不只是业务数据：库里有时效内的 `sessions`（拿到行里的 id 就能冒充
登录）、密码哈希、以及 SMTP 密码。所以备份目录与备份文件都应保持 0600 / 0750，别放进公开
的静态目录或同步盘。SMTP 密码尤其如此：它在 `site_settings` 里是明文（页面不回显，但库里有）。
更稳妥的做法是页面上的密码框留空，改用部署环境里的 `FASTOJ_SMTP_PASSWORD`（以及同组的
`FASTOJ_SMTP_HOST` 等），让密码根本不进数据库。

---

---

[← 返回 README](../README.md)
