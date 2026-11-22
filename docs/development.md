## 开发

```bash
go test ./...            # 含真起 g++ 判题的端到端用例
go vet ./...
gofmt -l .
powershell -File build.ps1 -Test
```

- 模块代理：国内直连 `proxy.golang.org` 会超时，已按 `go env -w GOPROXY=https://goproxy.cn,direct` 配置
- 依赖只有三个：`modernc.org/sqlite`（纯 Go，无 CGO）、`gomarkdown/markdown`、`golang.org/x/sys`
- 交叉编译：`build.ps1` 默认 `GOARCH=amd64`；`-Arch 386` 出 32 位判机框版本
- 三个脚本各管一台机器：`run.sh`（Ubuntu/Debian 一键：查依赖 + 编译 + 服务器带判机）、`build.ps1`（Windows 编译）、`scripts/judge.docker.sh`（判机装进容器），另有 `scripts/judge-isolation.sh`（判机那台机器的隔离与令牌体检/修复，见[落地检查](judge-isolation.md#落地检查牢房到底开起来了没有scriptsjudge-isolationsh)）
- 包结构：`internal/api`（HTTP + 页面）、`internal/store`（SQLite + 迁移 + 队列）、`internal/judge`（状态机 + 执行层）、`internal/judgeagent`（判机端）、`internal/protocol`（两端共享的线上类型）、`internal/casefs`（用例落盘）、`internal/config`

---

---

[← 返回 README](../README.md)
