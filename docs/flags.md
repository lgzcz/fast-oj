## 参数

服务端 `fast-oj`（同名环境变量在括号里）：

| 参数 | 默认 | 说明 |
|------|------|------|
| `-addr` | `127.0.0.1:8080` | 监听地址（`FASTOJ_ADDR`） |
| `-data` | `./data` | 数据目录（`FASTOJ_DATA`） |
| `-db` | `<data>/fast-oj.db` | 单独指定库文件（`FASTOJ_DB`） |
| `-admin-token` | 可留空 | 运维令牌（`FASTOJ_ADMIN_TOKEN`），**用一次即作废**：留空则由服务端生成并打印一次。**Linux 上只能用环境变量**：写在命令行上会拒绝启动 |
| `-judge-token` | 必填 | 判机令牌（`FASTOJ_JUDGE_TOKEN`）。同上 |
| `-max-source` | 262144 | 源码/JSON 请求体上限 |
| `-max-upload` | 0 = 不额外设闸 | 用例包整包字节上限（`FASTOJ_MAX_UPLOAD`）。0 时仍有内置 512 MB 安全上限；公开站建议显式设成 512 MB 以下 |
| `-max-case` | 0 = 不额外设闸 | 单个测试点正文的字节上限（`FASTOJ_MAX_CASE`）。0 时仍有内置 256 MB 安全上限；整包另有 512 MB 起步的总预算 |

> 这两条默认不再是"无界"：中央目录声明的大小会先被预检，读取时还会按剩余预算流式截断，
> 所以"压缩后很小、解压后很大"的包也会被 413 挡下。公开注册站仍建议显式收紧，例如
> `FASTOJ_MAX_CASE=67108864`（64 MB）、`FASTOJ_MAX_UPLOAD=536870912`（512 MB）——
> 上限应小于可用内存 ÷ 同时导入的人数。整包 zip 与包内每一条正文在导入时是一次性读进
> 内存的，判机判这条用例时也要把整条正文读进内存（`judge.Case.Input`）。
| `-bcrypt-cost` | 11 | 口令哈希强度（10 偏弱，14 登录会明显卡） |
| `-secure-cookies` | 关 | 给 session cookie 加 `Secure`，上 TLS 后打开 |
| `-no-register` | 关 | 关闭自助注册（与 `/admin/site` 的开关取与） |
| `-register-per-minute` | 1 | 一台机器每分钟可注册几个账号。全班排队注册那天可以调大（如 6） |
| `-submit-per-minute` | 10 | 一个人每分钟可提交几次（`FASTOJ_SUBMIT_PER_MINUTE`）。判机是共享资源，队列长度就是所有人的等待时间；**按人算不按 IP 算**——一个机房 NAT 出一个地址是常态，按 IP 限会在第一节课把整班人挡在门外。0 = 不限。**这一份只是默认值**：`/admin/site` 里填了数字就以它为准，改限速不用重启服务 |
| `-problem-per-minute` | 20 | 一个人每分钟可建几道题。建题比提交贵（每题带出目录与文件），批量导入走用例页的 zip。0 = 不限 |
| `-human-verify-max-seconds` | 20 | 人机验证窗那次等待的秒数上限（`FASTOJ_HUMAN_VERIFY_MAX`）。同一地址注册到第二个号、登录错到第 3 次起要等，越频繁等得越久，到这条封顶。0 = 整套关掉，只剩上面那几条限流 |
| —（无 flag） | — | 发信（SMTP）**没有启动参数**：全部在 `/admin/site` 那一页改。只在库里的项为空时退回去读 `FASTOJ_SMTP_HOST` / `_PORT` / `_USERNAME` / `_PASSWORD` / `_FROM` / `_TLS`，方便把密码留在部署环境里 |
| `-v` | 关 | 记录每个请求（默认只记失败与 >50ms 的请求，以及登录失败） |

判机端 `fast-judge`：

| 参数 | 默认 | 说明 |
|------|------|------|
| `-server` | `http://127.0.0.1:8080` | 服务器地址（`FASTOJ_SERVER`） |
| `-token` | 必填 | 判机令牌（`FASTOJ_JUDGE_TOKEN`）。**Linux 上只能用环境变量**：`/proc/<pid>/cmdline` 所有本地账号可读，写在命令行上直接拒绝启动 |
| `-name` | 主机名 | 判机框名字（`FASTOJ_JUDGE_NAME`） |
| `-dir` | 用户缓存目录 | 工作目录（`FASTOJ_JUDGE_DIR`），每次判题用 `sid` 子目录，跑完即删 |
| `-poll` | 500ms | 空队列轮询间隔 |
| `-backoff` | 3s | 出错后的退避 |
| `-tools` | 全部可用 | 只申报指定语言 |
| `-run-as uid:gid` | 空 | root 判机把提交降到这个 uid（`FASTOJ_JUDGE_RUN_AS`）；Windows 上直接拒绝启动。开了沙盒时它是牢房外的第二道身份；**没开沙盒又留空、而判机是 root 时，判机拒绝跑提交**（不敢把 root 交给学生代码，也不再给一句看不懂的 `EACCES`） |
| `-sandbox` | `auto` | 判题沙盒：`auto` 能开就开、开不了就在 `/judges` 标注；`on` 建不出牢房就不判（报 `IE`）；`off` 明确不隔离（`FASTOJ_JUDGE_SANDBOX`） |
| `-once` | — | 最多判一条就退出 |

---

---

[← 返回 README](../README.md)
