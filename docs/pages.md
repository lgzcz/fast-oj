## 页面与操作一览

没有对外 API：下面每一个路径都只出 HTML，浏览器会话是唯一的人用凭据。

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| `GET` | `/healthz` | 公开 | 探活，会真连一次数据库 |
| `GET` | `/about` | `site.settings` | 运行概览页：版本、数据目录、WAL、各项计数、页面清单（含部署路径，只给运维看） |
| `GET` | `/` | 公开 | 首页：站名 + 首页语 + 统计 + 最新题目 + 动态 + 打卡 |
| `GET` | `/problems` | 公开 | 列表，`?q=` `?limit=` `?after=` |
| `GET` | `/problems/{key}` | 公开 | 题面 + 样例（`key` 可为编号或数字 id） |
| `GET` `/POST` | `/problems/new` `/problems` | `problem.create` | 网页出题：编号、标题、题面（可预览）、时限、内存、语言、可见性 |
| `GET` `/POST` | `/problems/{key}/edit` `/problems/{key}` | 该题 owner 或 `problem.edit.any` | 网页改题；改可见性另需 `problem.publish` |
| `POST` | `/problems/{key}/delete` | 该题 owner 或 `problem.edit.any` | 删题（连带用例文件） |
| `GET` | `/problems/{key}/cases` | owner 或 `testcase.manage` | 用例管理页：列表、大小、加一条、zip 导入、清空 |
| `POST` | `/problems/{key}/cases` | 同上 | 加一条用例（名称、分值、样例、输入、输出） |
| `POST` | `/problems/{key}/cases/upload` | 同上 | 批量导入 zip（`名字.in` + `名字.out` 或 `名字.ans`，同名覆盖） |
| `GET` `/POST` | `/problems/{key}/cases/{id}/edit` | 同上 | 改单条用例；超过 256KB 的文件只改元信息，内容走 zip |
| `POST` | `/problems/{key}/cases/batch` | 同上 | 批量改分值与捆绑：一次保存整张表，一个事务 |
| `POST` | `/problems/{key}/cases/{id}/delete` | 同上 | 删一条用例（连带它的文件） |
| `POST` | `/problems/{key}/cases/wipe` | 同上 | 清空用例 |
| `GET` | `/problems/{key}/cases/{name}/{in\|out}` | 样例公开，判题需 admin/判机 | 取单个用例正文 |
| `POST` | `/problems/{key}/judge_program` | `judge_program.edit` | 存/撤本题的判题器或交互器（`kind=checker\|interactor`，源码留空即撤掉） |
| `POST` | `/problems/{key}/attachments` | 该题 owner 或 `problem.edit.any` | 传附件（PDF / 图片 / zip / 文本，单文件 ≤ 16MB，每题 20 个） |
| `POST` | `/problems/{key}/attachments/{id}/delete` | 同上 | 删一个附件（连带落盘文件） |
| `POST` | `/problems/{key}/submit` | 登录用户 | 网页表单提交 |
| `GET` | `/trainings` | 公开 | 题单列表，`?q=` `?after=` `?limit=`；未公开题单只对作者与 `problem.edit.any` 出现 |
| `GET` `/POST` | `/trainings/new` `/trainings` | 登录用户 | 建题单（标题、说明、是否公开） |
| `GET` | `/trainings/{id}` | 公开（未公开题单限作者） | 题单正文与题目表；登录后显示每题 AC 状态与完成度 |
| `GET` `/POST` | `/trainings/{id}/edit` `/trainings/{id}` | 作者或 `problem.edit.any` | 改题单；删题单走 `POST /trainings/{id}/delete` |
| `POST` | `/trainings/{id}/problems` | 同上 | 按题目编号加题；未公开的题只有它的作者能放进来 |
| `POST` | `/trainings/{id}/problems/{problemKey}/remove` | 同上 | 把题移出题单（题目与提交不受影响） |
| `GET` | `/submissions` | 公开 | 列表，`?user=`（或 `me`）`&problem=&status=&limit=&after=`；`status` 是管理员找"卡住的提交"的入口（`queued\|compiling\|running\|done\|abandoned`） |
| `GET` | `/submissions/{id}` | 公开 | 结果。判机每完成一个用例就回报一行，未判完时页面每 2 秒自动刷新，逐用例行一条条冒出来 |
| `POST` | `/submissions/{id}/rejudge` | `submission.rejudge` | 重新判题：退回队列，清掉上一次的判定、得分与逐用例（见[重判](judge-isolation.md#重判旧提交不会因为判机升级自己改判)） |
| `GET` | `/contests` | 公开 | 比赛列表：状态（未开始/进行中/已结束）、时间、赛制、题数、人数、是否封榜；`?q=` `?limit=` `?after=` |
| `GET` | `/contests/{key}` | 公开 | 比赛主页：时间、规则、题目（成员与主办方看得到）、名单、加入/退出 |
| `GET` `/POST` | `/contests/new` `/contests` | `contest.create` | 网页建赛：编号（留空自动 `C0001`）、标题、起止时间、赛制、加入码、每队人数、罚时、封榜、可见性 |
| `GET` `/POST` | `/contests/{key}/edit` `/contests/{key}` | 该赛 owner 或 `contest.manage.any` | 改赛与挂题：题目下拉、比赛内分值、题号标签、上下调序、移下、删除比赛 |
| `POST` | `/contests/{key}/problems` | 同上 | 挂题（未公开的题目只有能编辑它的人能挂）；同一题再提交一次就是改它的分值 |
| `POST` | `/contests/{key}/problems/{problemKey}/remove\|move` | 同上 | 移下 / 调顺序（移下的题历史成绩照算） |
| `POST` | `/contests/{key}/join` | 登录用户 | 加入；私有赛校验加入码，已结束不再收人 |
| `POST` | `/contests/{key}/leave` | 成员 | 退出（既有提交仍是比赛内记录） |
| `POST` | `/contests/{key}/teams` | 成员 | `op=create\|join\|leave`：建队并进队 / 加入同名队 / 退队 |
| `GET` | `/contests/{key}/standings` | 见[比赛](contests.md#比赛限时--独立榜--加入码--队伍oiioiacm-三种赛制) | 该比赛独立榜，不影响 `/rank`；OI 比赛结束前除主办方外看不到 |
| `GET` | `/contests/{key}/p/{题号}` | 成员或主办方 | 比赛内题号入口，进去的提交自动归属该比赛 |
| `GET` | `/rank` | 公开 | 排行榜：按去重后的 AC 题数排，另有「积分」那一列（活跃度读数，见[积分与名字颜色](points.md#积分与名字颜色adminpoints)） |
| `GET` | `/topics` | 公开 | 社区贴列表（统一管理页），`?q=` `?after=` `?limit=`；登录后每行带上「你能删」的删除按钮 |
| `GET` `/POST` | `/topics/new` `/topics` | 登录用户 | 发帖：类型（求助 / 社区）、标题、正文（可预览）、题号 |
| `GET` | `/topics/{id}` | 公开 | 帖子正文与评论；未登录只能看 |
| `POST` | `/topics/{id}/comment` | 登录用户 | 发表评论（每人每小时 60 条） |
| `POST` | `/topics/{id}/delete` | 本人或 `topic.moderate` | 删帖（评论一起没） |
| `POST` | `/topics/comments/{id}/delete` | 本人或 `topic.moderate` | 删一条评论 |
| `GET` | `/tickets` | 登录用户 | 工单列表：`?scope=mine\|queue\|all` `?status=` `?q=` `?after=`（后两种要 `ticket.handle`） |
| `GET` `/POST` | `/tickets/new` `/tickets` | 登录用户 | 提交工单（申诉 / 故障建议 / 举报 / 账号 / 其他），可挂题目或提交 |
| `GET` | `/tickets/{id}` | 作者或 `ticket.handle` | 工单详情与流水；**陌生人拿到 404**，不确认存在 |
| `POST` | `/tickets/{id}/reply` | 作者或处理人 | 追问 / 回复（每人每小时 30 条） |
| `POST` | `/tickets/{id}/status` | 处理人任意；作者只能关自己那张 | 改状态并留一条流水 |
| `POST` | `/tickets/{id}/delete` | `ticket.handle` | 删工单（回复一起没） |
| `POST` | `/tickets/{id}/attachments` | 作者或处理人 | 传附件（同一套类型与大小限制，每单 20 个） |
| `POST` | `/tickets/{id}/attachments/{attachment}/delete` | 同上 | 删一个附件 |
| `GET` | `/attachments/{id}` | 按归属判定 | 下载附件：题目附件跟随题目可见性（比赛内题目还要在赛内）；工单附件只有作者与处理人能看到；图片内联，其余按附件下载 |
| `GET` | `/problems/{key}/topics` | 公开 | 这道题的求助贴；从这里进发帖表单会锁主题目 |
| `POST` | `/problems/{key}/favorite` | 登录用户 | 收藏 / 取消收藏（同一个开关，再点一次就是取消） |
| `GET` | `/favorites` | 登录用户 | 我的收藏；题目被隐藏后仍列出，但打开题面照常按可见性判定 |
| `GET` | `/review` | 登录用户 | 错题本：提交过但还没 AC 的题，按最后一次提交倒序 |
| `GET` | `/problems/{key}/solutions` | 题目可见性 | 这道题的题解列表；题目未公开时陌生人 404 |
| `GET` `/POST` | `/solutions/new` `/solutions` | 登录用户 + `topic.post` | 写题解（必须填题目；每小时最多 10 篇） |
| `GET` | `/solutions/{id}` | 公开（草稿限作者与 `topic.moderate`） | 题解正文 |
| `GET` `/POST` | `/solutions/{id}/edit` `/solutions/{id}` | 作者 | 改题解；`POST /solutions/{id}/delete` 由作者或 `topic.moderate` 删 |
| `GET` `/POST` | `/columns/new` `/columns` | 登录用户 + `topic.post` | 写专栏（不挂题） |
| `GET` | `/columns` | 公开（草稿限作者与 `topic.moderate`） | 专栏列表，`?q=` `?after=` `?limit=` |
| `GET` | `/columns/{id}` | 同上 | 专栏正文 |
| `GET` `/POST` | `/columns/{id}/edit` `/columns/{id}` | 作者 | 改专栏；`POST /columns/{id}/delete` 由作者或 `topic.moderate` 删 |
| `GET` | `/feed` | 公开 | 动态时间线，`?user=` `?after=` `?limit=` |
| `POST` | `/feed` | 登录用户 | 发一条动态（每人每小时 20 条） |
| `POST` | `/feed/{id}/delete` | 本人或 `feed.moderate` | 删除 |
| `POST` | `/feed/{id}/hide` | `feed.moderate` | 隐藏 / 恢复（不删数据） |
| `GET` | `/u/{handle}` | 公开 | 个人主页 |
| `GET` | `/u/{handle}/following` | 公开 | 关注名单 |
| `GET` | `/u/{handle}/followers` | 公开 | 粉丝名单 |
| `POST` | `/u/{handle}/follow` | 登录用户 | 关注；`/unfollow` 取消 |
| `GET` | `/messages` | 登录用户 | 我的私信会话列表（未读数在最后一条那列） |
| `GET` | `/notifications` | 登录用户 | 站内通知：回复我的帖子、@ 我的人；`POST /notifications/read` 全部标为已读（带 CSRF） |
| `GET` | `/messages/{handle}` | 登录用户 | 一对一对话，打开即标记已读 |
| `POST` | `/messages/{handle}` | 登录用户 | 发一条私信（受对方收信策略限制） |
| `POST` | `/settings/privacy` | 登录用户 | 设置谁可以给我发私信 |
| `POST` | `/checkin` | 登录用户 | 每日打卡，返回连续天数 |
| `GET` `/POST` | `/login` `/logout` | — | 邮箱或用户名 + 密码登录，session cookie |
| `GET` `/POST` | `/forgot` | 公开（要配 SMTP） | 找回密码：填邮箱，寄一条 30 分钟、一次性的重置链接；不暴露账号是否存在 |
| `GET` `/POST` | `/reset` | 邮件里的链接 | 设置新密码；成功后退掉该账号全部会话。令牌绑签发时的密码指纹 |
| `POST` | `/verify` | 任何人（一次性令牌） | 人机验证窗里那一下勾选：等待没结束就不认，核销过即作废，`js=1` 回 204 否则 303 |
| `GET` `/POST` | `/register` | 公开（可关） | 自助注册；开了「注册需要验证邮箱」时会寄出一封带链接的验证信 |
| `GET` `/POST` | `/settings` `/settings/*` | 登录用户 | 资料、密码、私信策略、邮箱验证状态与重新发送、**自己的配色**（`POST /settings/theme`） |
| `GET` | `/verify?token=…` | 邮件里的链接 | 验证邮箱：一次有效，用过/过期都给出原因 |
| `GET` | `/docs` `/updates` | 公开 | 使用说明与用户可见的更新日志 |
| `GET` | `/sitemap.xml` | 公开 | 公开题目与比赛的站点地图；隐藏内容不出现 |
| `POST` | `/judge/claim` | 判机令牌 | 心跳 + 领取一条（空队列返回 404 `queue empty`） |
| `GET` | `/judge/submissions/{id}/cases/{ordinal}/{in\|out}?judge=NAME` | 判机令牌 + 本机确实领到了这条工单 | 取这条工单的用例正文（含判题用例）。按「第 N 条」取而不是按用例 id 取：拿到令牌的人因此读不到自己没被派到的题的数据 |
| `POST` | `/judge/submissions/{id}/status` | 判机令牌 | 进度 + 逐用例回报（`compiling` → `running`） |
| `POST` | `/judge/submissions/{id}/result` | 判机令牌 | 终局判定与逐用例结果 |
| `GET` | `/judges` | `judge.view` | 判机清单：能判的语言、**此刻有没有牢房**、自报能力、最近心跳、备注；没有一台有牢房时页面顶部当面警告 |
| `GET` `/POST` | `/admin/site` | `site.settings` | 上半页是运行状态（磁盘各部分、评测队列、评测机），下半页是设置：站名、首页语、友情链接、页脚、API 提示、六个功能开关、提交速率两道闸、发信（SMTP）配置 |
| `POST` | `/admin/site/test-email` | `site.settings` | 按表单里刚填的值发一封测试邮件，失败原因原样显示在这一页 |
| `GET` | `/admin/perms` | `permission.manage` 或 `user.manage` | 账号列表（带积分那一列）与权限控制台 |
| `GET` | `/admin/perms/u/{handle}` | `permission.manage` 或 `user.manage` | 单个账号：权限矩阵、标签、名字颜色、角色与状态（没确认身份时改不了要命的那几项） |
| `POST` | `/admin/perms/unlock` | `permission.manage` 或 `user.manage` | 再输一次自己的密码，换取 2 分钟之内改权限的资格 |
| `POST` | `/admin/perms/u/{handle}` | `permission.manage` | 授予 / 收回 / 恢复默认（不能改自己；要先确认身份）。授予可带 `days=1..3650`，留空永久 |
| `POST` | `/admin/perms/batch` | `permission.manage` | 批量授予 / 收回 / 恢复默认（≤100 个用户名；整批一个事务；要先确认身份） |
| `POST` | `/admin/users/{handle}/role` | `user.manage` | 改角色（要先确认身份） |
| `POST` | `/admin/users/{handle}/status` | `user.manage` | 封禁 / 解封（封禁踢掉会话；要先确认身份） |
| `POST` | `/admin/users/{handle}/tags` | `user.manage` | 改名字后面的标签，形如 `admin [作弊者]`（最多 3 个、每个 12 字；不进那道身份确认） |
| `POST` | `/admin/users/{handle}/color` | `user.manage` | 手动指定名字颜色：紫（管理员）/ 棕（处罚）/ 放回跟着积分走。蓝绿橙红不能手选 |
| `GET` `/POST` | `/admin/points` | `site.settings` | 积分与颜色：四个维度的单位分与上限、衰减、重算间隔、三档门槛；保存 / 只重算 / 保存并立即重算，下面是分值预览 |
| `POST` | `/admin/users/{handle}/cancel` | `user.manage` | 注销账号：抹掉身份、释放用户名，记录留着署名成封禁用户（要敲一遍用户名，也要先确认身份；站主、自己、名下有题或比赛时拒绝） |
| `GET` | `/admin/accounts/new` | `user.manage` | 批量建号：名单模式或「前缀 + 起始号 + 个数」 |
| `POST` | `/admin/accounts` | `user.manage` | 一次建一批，初始密码只在这一个页面出现一次 |
| `GET` | `/admin/stats` | `stats.view` | 站点统计：人数、提交、人均、通过率、判定分布、GET/POST 请求量 |
| `GET` | `/admin/ips` | `ip.manage` | IP 封禁名单 + 按地址流量 Top20；来源全是 127.0.0.1 时自动收起 |
| `POST` | `/admin/ips/ban` | `ip.manage` | 封一个 IP 或 CIDR 网段（整站 403；环回地址拒绝） |
| `POST` | `/admin/ips/unban/{ip}` | `ip.manage` | 解封，缓存立即失效 |
| `GET` | `/admin/audit` | `ip.manage` | 安全审计流水：谁在什么时候动了谁的权限、账号、题目、用例与站点设置；只读、只追加 |

出题在浏览器里走：导航栏「出题」→ 填编号、标题、题面（可以「预览」看渲染效果，不保存）→ 保存后直接落到用例页，
逐条加输入输出或用 zip 批量导入；答案不唯一或要一问一答，就在用例页最下面贴一份判题器或交互器（要 `judge_program.edit`）。
表格里最后四行是判机协议：只有判机令牌能用，网页上没有任何入口，
`/healthz` 与它们是两个非 HTML 的端点。

---

---

[← 返回 README](../README.md)
