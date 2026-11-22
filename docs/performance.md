## 性能

`go test -run XXX -bench . ./internal/api/` 与 `TestReadPathP99Budget`，数据规模 200 道题 / 每道 5 个用例 / 500 条提交，Windows 回环，i7-10700KF：

| 路径 | 每次请求 | p50 | p99 |
|------|----------|-----|-----|
| `/healthz` | 0.16 ms | <1 ms | 1.0 ms |
| `/problems?limit=50` | 0.86 ms | 1.0 ms | 2.2 ms |
| `/problems/{pid}`（含 Markdown 渲染） | 0.55 ms | 1.0 ms | 1.5 ms |
| `/submissions?limit=50` | 0.62 ms | 1.0 ms | 1.7 ms |
| `/rank` | 1.15 ms | 1.0 ms | 2.0 ms |

端到端 p50/p99 被 Windows 约 1 ms 的计时粒度顶住，所以看起来都 ≥1 ms；真正的工作量在"每次请求"那一列。

支撑这些数字的取舍：

- 列表一律游标分页（`WHERE id > ?`），不用 `OFFSET`，深页和第一页同价
- 列表查询不取 `source` / `detail` 大字段，只有单条且有权查看时才二次取回
- 待判队列走 `status='queued'` 部分索引
- 一次查询取回整页的用例数/通过数，不做 N+1
- WAL + 预编译语句 + 无 ORM 运行时
- 访问日志默认只记失败与 >50 ms 的请求，日志不吃掉延迟

已知会先劣化的地方：`/rank` 用相关子查询按用户聚合，用户数上千后需要改成物化计数列。

### 浏览器侧的账（F12 限速 3G）

后端 1 ms 级别不代表页面快。Chrome 的 Fast 3G 是 1.6 Mbps（≈200 KB/s）+ 150 ms RTT，
在这个档位下首字节 14 ms、整页 P99 却要 2.3 s——差的那部分是**下载量**：

| 项目 | 改前 | 改后 |
|------|------|------|
| 页面 HTML | 3–11 KB 原样发送 | gzip 后 1.5–2 KB（`/submissions` 10.8 KB → 1.55 KB） |
| `style.css` + `math.js` | 5 KB + 0.9 KB，`max-age=300` | gzip + `?v=<内容哈希>` + `immutable` 一年缓存 |
| KaTeX（CSS 23 KB + JS 275 KB + 字体 1.2 MB） | 每一页都从 CDN 拉 | 已嵌进二进制；只有页面上真的存在公式节点时才拉本站资源 |

所以现在的规则是：`/problems`、`/submissions`、`/rank`、登录注册这些没有公式的页面，
一次外部请求都不发（实测网络面板只剩本站两个资源）；只有题面、写了公式的页脚/首页语
那类页面才付 KaTeX 的钱，而且这部分也来自本站 `/static/`，不再执行第三方脚本。加载逻辑在
`static/math.js`：模板里不留任何 KaTeX 标签，由脚本发现 `span.math` 之后再注入 CSS 与 JS，
资源缺失时原始 TeX 仍然可读。

还要再快可以把关键 CSS 内联进 HTML（省一个 RTT）。

---

---

[← 返回 README](../README.md)
