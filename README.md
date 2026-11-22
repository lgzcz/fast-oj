> [!IMPORTANT]
> 本项目理论上不会再接受UI上的美化，除非是[这样的](https://github.com/liangbinchen2013/fast-oj/pull/4)更新，会接受。

# fast-oj

**在线演示：<https://oj.lbcoj.top/>**

大量异常访问和批量注册会被封 IP（整站 403，按地址或网段，见[IP 封禁](docs/ip-bans.md#ip-封禁与按地址流量)）。

超级无敌简洁的 OJ，UI 极少。**只有网页**：人是唯一的使用者，出题、用例、判题、私信、后台全在浏览器里点。

- 单个可执行文件，零外部服务依赖（SQLite 内嵌，纯 Go 驱动）
- 判机与服务器分离，判机只需要一个出站 HTTP 连接（那是判机的内部协议，网页上没有入口）
- 题面、动态、个人简介、首页语统一走 Markdown + KaTeX 数学公式
- 首页内容（站名、首页语、友情链接、页脚、页脚 API 提示、注册开关、发信配置）在网页后台改，不用重启
- 页面每次请求的处理时间在 0.2–3 ms 之间（200 题 / 500 提交规模，见[性能](docs/performance.md#性能)）

---

## 30 秒跑起来

### Ubuntu / Debian：一条命令

```bash
./run.sh
```

四件事：确认 `go` 和 `g++`（缺了就告诉你 apt 装什么）、编译到 `dist/`、按 `127.0.0.1:8080` 起服务器并顺手带起一台判机、Ctrl-C 时两个一起收掉（不留孤儿判机空轮询）。两个令牌没给就随机生成并打印出来；判机令牌想固定下来就 `export FASTOJ_JUDGE_TOKEN`，**管理令牌别固定**——它"用一次即作废"这一点就是防泄露的那道闸门（见[运维令牌是一次性的](docs/permissions.md#运维令牌是一次性的-admin-token)）。脚本里写死一对默认值迟早跟着仓库传到网上。

```bash
FASTOJ_ADDR=0.0.0.0:8080 ./run.sh   # 同网段都能访问（判机仍然走 127.0.0.1 连它）
JUDGE=0 ./run.sh                    # 只起服务器，判机在另一台机器上
./run.sh -no-register -v            # 其余参数原样传给服务器
FASTOJ_DATA=/srv/fast-oj/data ./run.sh
```

端口上已经有一个 fast-oj 在跑时它会拒绝启动并提示换地址，不会留下一个"看起来起来了但其实没起来"的服务器。

### Windows，或想手动分两步

```powershell
powershell -File build.ps1        # 产出 dist\fast-oj.exe dist\fast-judge.exe
```

```bash
# 1. 服务端（两个令牌必填，留空会拒绝启动）
./dist/fast-oj.exe -addr 127.0.0.1:8080 -data data -admin-token ADM -judge-token JDG

# 2. 判机端（需要 g++ 在 PATH 上，可以和服务器同机；要收 Python 就再装一个 python3）
./dist/fast-judge.exe -server http://127.0.0.1:8080 -token JDG -name box1 -v
```

**Linux 上不要把令牌写在命令行上**：`-token` / `-admin-token` / `-judge-token` 在 Linux 会直接拒绝启动，
只认环境变量 `FASTOJ_JUDGE_TOKEN` / `FASTOJ_ADMIN_TOKEN`。原因是 `/proc/<pid>/cmdline` 是 0444、
所有本地账号都读得到（`hidepid` 默认没开），而判机上跑的正是别人提交的代码——写在 argv 里的令牌
等于发给整台机器，一次逃逸出沙盒的运行就能把它抄走。systemd 里 `-token $FASTOJ_JUDGE_TOKEN` 这种写法
也是同一个问题：`ExecStart` 会先展开再 exec，0600 的 `EnvironmentFile` 就白设了——正确做法是让
`ExecStart` 里不出现令牌，进程自己去读那个文件提供的环境变量。Windows 开发机没有 `/proc`、单机一个人用，
所以只警告不拦。

浏览器打开 <http://127.0.0.1:8080>。**全新库的第一步**：打开 <http://127.0.0.1:8080/admin/claim>，把 `-admin-token` 的值填进去，给 `admin` 账号设一个网页密码，之后就是普通登录。这个页面在旧库上也能用，属于恢复手段，会先警告你要覆盖已有密码——**这一串令牌用一次就作废**，用完之后下一枚打印在服务器日志的 `one-time token rotated` 那一行（见[运维令牌是一次性的](docs/permissions.md#运维令牌是一次性的-admin-token)）。命令行只留一件事——探活：

```bash
curl -s localhost:8080/healthz          # ok
```

运行概览（版本、数据目录、WAL、各项计数、页面清单）现在是网页 `/about`（要 `site.settings`），判机在线情况是网页 `/judges`。

现成的演示脚本：

没有 curl 演示脚本了：以前那些脚本走的正是被撤掉的 API。想试完整流程就在浏览器里注册两个账号 → 导航栏「出题」建题 → 用例页加数据 → 提交代码。

---

## 没有对外 API

以前同一个 URL 会按 `Accept` 与 `?format=` 给浏览器 HTML、给 curl 纯文本、给脚本 JSON，还有「用户 API 令牌」让脚本代替人提交。这套全部撤掉了：

| 撤掉 | 换成 |
|------|------|
| `?format=json` / `?format=text` / `Accept` 协商 | 每个页面只出 HTML |
| 用户 API 令牌（`/settings` 签发、`/account`、`/whoami`） | 浏览器会话，`tokens` 表已删 |
| `POST /submissions`（脚本提交代码） | 题面页的提交表单 |
| `POST` / `PUT` / `DELETE /problems`（JSON 建题改题） | 网页出题：`/problems/new`、`/problems/{pid}/edit` |
| `POST` / `DELETE /problems/{pid}/cases`（JSON + zip 清单） | 网页用例管理：逐条增删改，zip 只做批量导入 |
| `GET` / `POST /users`、`POST /users/{handle}/tokens` | 注册页 + `/admin/perms` 权限控制台 |
| `?wait=秒` 长轮询 | 提交详情页判题中每 2 秒自动刷新 |

保留的两处不是给人用的：`/healthz`（一行 `ok`，给 Docker / 反代探活）和 `/judge/*`（判机协议，只有判机令牌能访问，网页里没有任何入口）。少了一套视图就少一条代码路径：页面和权限判定从此只有一条，浏览器不会比谁多看到一点东西。

网页版页面：首页 `/`（站名 + 首页语 + 统计 + 最新题目 + 动态 + 打卡）、题库 `/problems`、题面 `/problems/{pid}`、出题 `/problems/new`、改题 `/problems/{pid}/edit`、用例 `/problems/{pid}/cases`、动态 `/feed`、提交列表 `/submissions`、提交详情 `/submissions/{id}`、排行榜 `/rank`、个人主页 `/u/{handle}`、关注与粉丝 `/u/{handle}/following` `/followers`、私信 `/messages`、登录注册 `/login` `/register`、设置 `/settings`、站点设置 `/admin/site`、权限控制台 `/admin/perms`、安全审计 `/admin/audit`、判机 `/judges`、运行概览 `/about`（`site.settings`）。

---

## 章节跳转

完整文档按主题拆到 `docs/`，点下面的链接进入对应章节：

| 文档 | 内容 |
|------|------|
| [页面与操作一览](docs/pages.md) | 全部网页路径、方法与所需权限一览表 |
| [题面写作（Markdown + KaTeX）](docs/markdown.md) | 题面写作：Markdown + KaTeX 数学公式 |
| [首页、站点设置与动态](docs/site.md) | 首页、站点设置、动态、站点状态、配色、SMTP 发信与邮箱验证 |
| [关注与私信](docs/social.md) | 关注与粉丝、私信 |
| [每日打卡](docs/checkin.md) | 每日打卡 |
| [积分与名字颜色（`/admin/points`）](docs/points.md) | 积分与名字颜色：谁去算、多久算、六档口径 |
| [用例包约定](docs/case-package.md) | 用例包约定：zip 命名、.in/.out/.ans、同名覆盖 |
| [题目与工单附件](docs/attachments.md) | 题目与工单附件：上传权限、类型白名单、随机落盘与鉴权下载 |
| [题单（训练）](docs/trainings.md) | 题单：可见性、加题规则与进度口径 |
| [收藏与错题本](docs/favorites.md) | 收藏与错题本：私人列表、现算不落库 |
| [题解与专栏](docs/articles.md) | 题解与专栏：挂题规则、草稿与审核删除 |
| [团队与团队题库](docs/teams.md) | 团队：加入码、成员管理与只给队内看的题库 |
| [查重](docs/plagiarism.md) | 查重：同题相似度口径与"只报告不处罚" |
| [两步验证与登录设备](docs/security.md) | 两步验证：TOTP、恢复码、登录设备与 Passkey 口径 |
| [Rating、虚拟比赛与周赛](docs/rating.md) | Rating 结算、虚拟比赛与周赛收集页 |
| [对抗赛（Battle）](docs/battles.md) | 对抗赛：一道题的短时 PK，与未做 Hack 的边界 |
| [图床、头像与封面](docs/images.md) | 图床：类型识别、单槽位头像/封面与公开性 |
| [举报、审核队列与拉黑](docs/reports.md) | 举报：队列结案、拉黑只关直接通道 |
| [判题方式：普通比较 / SPJ 判题器 / 交互题](docs/judging.md) | 判题方式：普通比较 / SPJ 判题器 / 交互题 |
| [Python 提交：护栏装在解释器里，边界还是牢房](docs/python.md) | Python 提交：解释器护栏与牢房边界 |
| [比赛（限时 + 独立榜 + 加入码 + 队伍，OI/IOI/ACM 三种赛制）](docs/contests.md) | 比赛：限时、独立榜、加入码、队伍、OI/IOI/ACM 三赛制 |
| [帖子（求助贴 / 社区贴 + 评论）](docs/topics.md) | 帖子：求助贴 / 社区贴 + 评论 |
| [工单（用户提单，管理员处理）](docs/tickets.md) | 工单：用户提单、管理员处理 |
| [身份与权限](docs/permissions.md) | 身份与权限：细粒度能力位、身份再确认、一次性运维令牌、超级管理员 |
| [注销账号（不是封禁，也不是删除）](docs/account-deletion.md) | 注销账号（不是封禁，也不是删除） |
| [运营页：统计、批量建号与注册限流](docs/operations.md) | 运营页：统计、批量建号、注册限流、人机验证窗 |
| [IP 封禁与按地址流量](docs/ip-bans.md) | IP 封禁与按地址流量 |
| [安全审计流水](docs/audit.md) | 安全审计流水 |
| [判机协议与队列](docs/judge-protocol.md) | 判机协议与队列 |
| [判机隔离（边界必须说清）](docs/judge-isolation.md) | 判机隔离：命名空间牢房、seccomp、落地检查、容器化 |
| [数据与备份](docs/backup.md) | 数据与备份 |
| [参数](docs/flags.md) | 启动参数与环境变量 |
| [开发](docs/development.md) | 开发：构建、测试、演示库 |
| [性能](docs/performance.md) | 性能：读路径预算与浏览器侧的账 |
| [已知限制与后续](docs/limitations.md) | 已知限制与后续 |

> 另有 [M14 比赛实施文档](docs/M14-比赛.md)：比赛功能的历史设计与实现偏差记录。
