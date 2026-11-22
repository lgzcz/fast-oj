## 判机隔离（边界必须说清）

当前支持语言：**C / C++ / Python**（C/C++ 走 `g++ -x c++ -std=c++17 -O2 -w -static`，Python 见[Python 提交](python.md#python-提交护栏装在解释器里边界还是牢房)）。命令一律是 argv 数组，绝不拼 shell 字符串。

Windows 本地判机（`runner_windows.go` + `runner.go`）：

- 内存上限用 Job Object 落实：`JOB_OBJECT_LIMIT_PROCESS_MEMORY` 管单进程，`JOB_OBJECT_LIMIT_JOB_MEMORY` 管整棵树（上限的 4 倍），实测 32 MB 上限下 `malloc(512MB)` 直接返回 `NULL`；无上限时如实报出峰值 526 MB
- **进程数上限** `JOB_OBJECT_LIMIT_ACTIVE_PROCESS`（提交默认 4，编译 16）：没有它，一个 `fork` 循环可以躲在单进程内存上限下面把机器的进程表和 CPU 吃光
- CPU 时间取进程退出状态里的 FILETIME，内存取 Job 的 `PeakProcessMemoryUsed`
- 超时用 wall-clock 兜底（`max(2×时限, 2s)`）并整树击杀 —— 只限 CPU 的话，一个 `sleep` 型程序能把判机槽位永久占死
- **环境变量一律不继承**：判题进程只看到 `PATH`/`SystemRoot`/`WINDIR` 与指向临时工作目录的 `TEMP`/`TMP`/`HOME`/`USERPROFILE`。fast-judge 自己的令牌、数据库路径、服务单元导出的任何秘密，`getenv` 都读不到
- 启动时把边界打印一次，避免有人在日常使用的机器上把判机当成沙箱
- **Windows 上内存超限通常表现为 `RE`（分配被拒）而不是 `MLE`**，Linux sandbox 才给得出明确的 MLE

**Windows 上仍未隔离：文件系统、网络、以及"能不能弹窗口到你的桌面"。** 判题程序就是以 fast-judge 那个用户的身份在跑，能读写该用户能读写的一切，`system("shutdown -s -t 3600")` 这类调用会真的被系统接受 —— Job Object 的 UI 限制在 Vista 之后就被 Windows 作废了，`shutdown.exe` 也不是靠 `ExitWindowsEx` 的句柄限制能拦的。Linux 上没有这个问题：判题沙盒已经把网络与文件系统这两道关上了（见下）。

| 你要的强度 | 做法 |
|-----------|------|
| 本机自测、只跑自己的题 | 现在这样就够 |
| 接受陌生人提交 | Linux 上开沙盒（默认就开），并给它一个专用账号或容器；**别在登录用的账号上跑 fast-judge**（Windows 上判机没有这层保护） |
| 生产 | 判机放 Linux 容器 + 开沙盒（`docker-compose.judge.yml` 已经把需要的 cap 加好了）；要连内核攻击面一起收，再叠 seccomp（尚未做，见[已知限制](limitations.md#已知限制与后续)） |

### 判题沙盒（`internal/jail`：一次运行一个牢房）

Linux 上每次判题运行都被关进一组命名空间，靠的是**判机 exec 自己**：Go 没法在 fork 与
exec 之间插东西，所以 `setrlimit`、`mount`、`chroot` 这些必须由子进程在 exec 提交程序
之前自己做完。牢房里：

| 面 | 手段 | 实测结果 |
|----|------|---------|
| 网络 | `CLONE_NEWNET` | 只剩一条 DOWN 的 lo；`connect(8.8.8.8:53)` 返回 `ENETUNREACH`。**阿里云元数据 `100.100.100.200` 一并废掉** |
| 目录 | `CLONE_NEWNS` + 把系统目录只读 bind + `chroot` 到这次运行的工作目录 | `/etc/passwd`、`/var`、别人的源码都不存在；`fopen("/etc/xxx","w")` 失败 |
| 进程 | `CLONE_NEWIPC`/`CLONE_NEWUTS` + `RLIMIT_NPROC` | fork 炸弹被内核掐住（实测 400 次 fork 在 8 的额度下被判 TLE 并整组杀掉） |
| 时间 | 判机墙上时钟 + 整组击杀，另有 `RLIMIT_CPU` 宽两秒兜底 | 死循环仍是 `TLE`，且给出实测值 |
| 内存 | 判机实测 `rusage` + `/proc` 轮询熔断，`RLIMIT_AS` 只留 512 MB 余量 | 吃 300 MB 而限 64 MB 仍是 **`MLE`**，不会被抢先打成 `RE`；连加载都放不下的大镜像也判 `MLE`（见[判定口径](#判定口径judgerun)） |
| 身份 | root 判机降到 `-run-as` 的 uid；非 root 判机走非特权 user namespace，最后落到一个**未映射**的 uid；root 判机若既没沙盒又没 `-run-as`，**拒绝跑提交** | 宿主看到的是 nobody，判机的库、令牌、别人的提交都读不到；**root 永远不会成为提交的身份** |

`-sandbox=on` 是硬要求：启动时真建一次牢房，建不出来判机就**拒绝启动**（`systemctl status fast-judge` 会看到原因），不会悄悄按不隔离的路子重跑。
`-sandbox=auto`（默认）反过来：**启动时**开不了就**不再尝试**，在启动日志与 `/judges` 上写明"未隔离"，判题照常。以前 auto 是"开不了也每条都试一次"，于是每台开不了命名空间的机器都会把每条工单判成 `IE`——那条已经修掉了。注意"判题照常"只管启动这一次结论：跑到一半牢房坏了是停下来，不是继续（见[牢房坏了走哪条信道说](#牢房坏了走哪条信道说-jailstatus)）。
牢房建不起来时判机报 `IE` 而不是 `RE`：内核不给权限，不该让学生去查自己的代码。

**而"牢房建不起来"这件事只从一条一次一用的信道读，绝不从子进程的输出猜**。四个实测踩过的坑：

- **自测的成败不能看退出码**。`unshare()` 被内核拒掉时 exec 根本没有发生，Go 把整条
  fork→unshare→exec 的失败报成 `fork/exec <路径>: permission denied`，退出码还是 0。
  只看退出码就会把"这台开不了牢房"记成"沙盒已启用"，判机于是边在日志里说谎、边把每条
  提交判成 `IE`（阿里云 ECS 上撞到的正是这条）。自测认的是**判定**：任何一次 `IE` 都是失败。
  同理，`SetSandbox` 换配置时必须把上一次自测的结论清掉——`probeUnavailable` 是"按这套
  配置这台机器开不了"的答案，留着它下一次自测根本不去建牢房，于是又在牢房外把 `/bin/true`
  跑成功、报"已启用"。
- **不能开 `CLONE_NEWPID`**。Go 是 fork 之后再 `unshare()` 的，而 `unshare(CLONE_NEWPID)`
  只把调用者的**子进程**放进新 pid 命名空间，被 exec 的程序自己还在原来那个；留着它
  拿不到好处，还会让被 exec 起来的 Go 程序（判机自己）在起线程时 `EINVAL`。
- **`RLIMIT_AS` 不能卡到判定线上**。卡紧之后超内存的申请直接 `ENOMEM` 崩掉，判定从
  `MLE` 变成 `RE`，等于用安全性换掉了判题口径。
- **牢房坏了没有，不能拿子进程的输出当判据**（2026-11-01 修掉的洞）。jailer 过去把失败
  声明写成 stderr 上一行 `OJ-JAIL: …`，判机按行首认——可那份 stderr 同时是**编译器与选手
  程序**的输出。一条提交打印一行假声明（连执行都不用：`#line 1 "OJ-JAIL: x"` 加一条
  `#error`，g++ 会让这句诊断占满第一行）就能让判机认定"这台机器开不了牢房"，于是
  **此后所有人的提交都不再建牢房**；判机以 root 跑、没写 `-run-as` 的那台机器上，下一条
  提交打印 `getuid()` 得到 `0`。ECS 上从伪造到 root 执行，全程一条提交。

### 牢房坏了走哪条信道说（`jail.Status`）

失败的事实现在只走一条一次一用的信道，它不经过任何选手碰得到的东西：

1. 判机在 spawn 之前 `os.CreateTemp` 建一个 **0600** 的空文件，并生成 16 字节随机暗号；
   两个值一起放进 jailer 的 spec（也就是它的命令行）。
2. jailer 在**进牢房之前**把这个 fd 握过来（chroot 之后那个路径就不存在了，而失败可能落在
   chroot 前后任意一步），走到 exec 前一刻写一行"exec 起来了"，任何一步失败就写一行
   "失败+原因+暗号"，然后按 `FailExit=70` 退出。
3. 判机只认**写着这一把暗号**的行。fd 带 `O_CLOEXEC`，exec 之后选手进程连它都没有；路径
   与暗号都不在选手的命令行、环境或那份文件系统里；文件在宿主上属于判机 0600。伪造者能
   写出一模一样的**文字**，写不出这一把的**暗号**。
4. 判据另有半个兜底：退出码 70、而信道上既没有 exec 记号也没有失败声明——那就是 jailer
   连程序都没交出去、又没能写上话。选手自己 `exit(70)` 撞不到它，因为它被 exec 起来时
   exec 记号已经在了（`TestProgramExiting70IsNotAJailFailure`）。
5. `OJ-JAIL:` 那句 stderr 仍然打，但它只剩给人看的作用：判机上没有一处拿它当判据。

**运行时坏掉就停，不停就是裸跑**。启动自测的"这台开不了"与跑到一半"jailer 亲口说建不起来"
是两件事：后者记成 `probeBroken`，`judge.SandboxBroken()` 为真，判机主循环**退出领单并结束
进程**（`Restart=always` 把它带回来，重启时的自测重新回答"这台到底开不开得了"）。旧行为是
把一次中途失败当成"这台不隔离"，从此安静地裸跑下去——那等于把一条已经确认成立的安全前提
悄悄撤掉。

**判机是 root、既没牢房又没 `-run-as` 时，拒绝跑提交**，而不是"就按 root 跑吧"。把 root
交给学生代码永远不是可接受的默认值；这里也没选"兜底降到 nobody"——那要求提交能走进判机给它
准备的工作目录，而生产上那个目录的祖先（`/var/lib/fast-oj` 是 `0750`）正是**故意**不给别人
进的，实测 `fork/exec` 直接 `EACCES`，等于每条提交换来一句看不懂的判机故障。所以这条是一句
明白的拒绝，已知的两种形态各归各位：容器判机显式写 `-run-as 65534:65534`（`/work` 是
`mode=1777` 的 tmpfs，降权后进得去），root 起步的裸机判机开沙盒；判机不是 root 时行为不变
（`TestRootJudgeWithoutSandboxRefusesToRun` 把这三种都钉住了）。

判机容器要多给三个能力：`SYS_ADMIN` + `NET_ADMIN`（开 mount/network 命名空间）、
`SYS_PTRACE`（读降权子进程的 `/proc/<pid>/statm`，内存熔断靠它）。Docker **默认 seccomp
profile 不需要放开**，实测照跑。

### 系统调用过滤（seccomp 黑名单）

牢房建好、降完权、上限额之后，exec 学生程序之前最后一件事是装一层 seccomp-bpf
（`internal/jail/seccomp_linux.go`）：**黑名单**，命中就返回 `EPERM`。挡掉的是

- `socket` / `connect` / `bind` / `listen` / `send*` / `recv*` / `shutdown` / `socketpair` /
  `accept*` —— 断网本来靠 `CLONE_NEWNET`（命名空间里没有第二张网卡），这一层挡的是内核攻击面；
- `ptrace`、`process_vm_readv/writev` —— 「看着判机正在跑谁的哪道题」这条路；
- `mount` / `umount2` / `pivot_root` / `chroot` / `setns` / `unshare` —— 在牢房里再动挂载表或换一个牢房；
- `reboot` / `kexec_load` / `swapon` / `swapoff` / `bpf` / `userfaultfd`。

配上 `PR_SET_NO_NEW_PRIVS`（内核要求它与非特权时装过滤器结伴），并用
`SECCOMP_FILTER_FLAG_TSYNC` 一次装到**所有线程**——Go 与 Python 都是多线程的，`prctl`
那条路只管当前线程，"看着装了其实没装"是最坏的一种失败。

三个决定都是有依据的，改的时候别绕过去：

- **为什么返回 `EPERM` 而不是 `KILL`**：`SECCOMP_RET_KILL_THREAD` 只杀一个线程，其余的
  永远等（实测 Go/Python 都表现为挂住不返回，判机只能靠墙上时钟收，那条判定不可用）；
  `KILL_PROCESS` 又把"学生调了一个被挡的调用"变成一条没有原因的信号死。EPERM 让调用失败、
  程序自己报错，学生看到的是一句能读懂的 `PermissionError`。
- **为什么不是白名单**：实测白名单要 150 条以上才装得下 g++ 与 CPython（换发行版就变），
  装漏一个就是把"这台判机判不了题"变成常态；而要能编译就必须放行 `execve`/`clone3`/`wait4`，
  于是它连"不许起进程"这件本该由它解决的问题都没解决。
- **为什么装在 `LookPath` 之后**：牢房自己还要用 `unshare`/`mount`/`chroot` 这些被挡的调用，
  早装一步就死在建牢房半路上（装不上时按"牢房没建起来"出去，判机报判机故障，不让学生背）。

`internal/jail/seccomp_linux_test.go` 里有一条测试专门盯着 cBPF 的跳转方向：跳转写反**不会
报错**——内核照样收下过滤器，程序照跑，只是什么都没挡。实测就是这样让 `socket()` 与
`ptrace()` 在"装了 seccomp 的牢房"里双双成功。所以那条测试不测"装上了没有"，而是顺着指令流
走一遍，检查"号命中"这条路真的走到 `RET ERRNO`；`TestJailHasNoNetwork` 再在真牢房里断言
`socket=-1 errno=1`、`ptrace=-1 errno=1`。

`/proc` 现在以 `hidepid=2` 挂进牢房：只换新的 `/proc` 挡不住读
`/proc/<别人的 pid>/cmdline`、`environ`，而那些里能看到判机在执行谁的哪道题。

### 落地检查：牢房到底开起来了没有（`scripts/judge-isolation.sh`）

「代码里有沙盒」与「这台机器上真的在开沙盒」是两件事。Ubuntu 24.04 起
`kernel.apparmor_restrict_unprivileged_userns=1`，非 root 的判机 `unshare(CLONE_NEWUSER)`
会被内核直接拒掉——判机于是照常判题，只是**没有隔离**：提交在判机自己的身份下、在宿主
文件系统上跑。这件事只在启动日志里占一行，没人盯日志就等于没人知道。

```bash
sudo bash scripts/judge-isolation.sh --check      # 只看，不动任何东西
```

它真跑一次 `unshare -Urm true`（按判机那个身份），报四件事：运行身份、user namespace
开不开得了、判机令牌有没有写在命令行上、`EnvironmentFile` 的权限。`/judges` 网页上现在
也直接写着每台机器的「隔离」状态，没有一台有牢房时页面顶部红字警告。

三条修好的路，每条走完都**当场重新实测**一次，不接受"配好了"的推断：

- `--root`：把判机改成 root 跑。这是非特权 userns 被拦的机器上牢房开得起来的那条路——
  学生程序仍然在降权之后才被 exec（`internal/jail` 的 `drop()`），拿到的是 nobody/未映射
  的 uid、无网络、看不到宿主文件系统。
- `--apparmor`：保持非 root，给判机那个可执行文件装一份允许 userns 的 profile。这条路
  对内核与 AppArmor 版本敏感，所以脚本自己会验：装完实测不通就报出来并给撤掉的命令，
  不留半套配置。
- `--rotate-token`：换判机令牌，并用一个 systemd drop-in 把 `-token` 从 `ExecStart` 里
  彻底去掉。**令牌写在命令行上等于发给整台机器**（`/proc/<pid>/cmdline` 是 0444），
  而 systemd 会先展开 `ExecStart` 里的 `$VAR` 再 exec，所以 0600 的 `EnvironmentFile`
  也白设。判机二进制的 `-token` 默认值本来就是那个环境变量；新版判机在 Linux 上
  **拒绝**以命令行令牌启动。

⚠️ 部署顺序：新版判机看到 `-token` 在命令行上就不启动。所以上新二进制时要么先把
`ExecStart` 里的 `-token` 去掉（`--rotate-token` 就是干这个的），要么判机会直接停掉。
两个服务的令牌来自同一个 `EnvironmentFile`，换令牌要**一起重启**，不然队列会卡在 401。

### 判机容器化（`Dockerfile.judge` + `docker-compose.judge.yml`）

把提交代码关进容器，是现在最省事的一层真隔离：Docker Desktop 的容器跑在轻量 VM 里，
判题程序连你登录用的账号都不是同一个。

```bash
# 1. 服务器照旧在宿主上跑
./dist/fast-oj.exe -addr 127.0.0.1:18080 -data data -admin-token ADM -judge-token JDG

# 2. 交叉编译 + 构建镜像 + 起容器（只需出站连接，没暴露任何端口）
FASTOJ_JUDGE_TOKEN=JDG bash scripts/judge.docker.sh          # 本地已有镜像时直接复用
REBUILD=1 FASTOJ_JUDGE_TOKEN=JDG bash scripts/judge.docker.sh  # 装了新工具链时强制重建
SOURCE=1 FASTOJ_JUDGE_TOKEN=JDG bash scripts/judge.docker.sh   # 本机没有 Go：把编译挪进容器
```

`docker build` 每次都要向 Docker Hub 取 `FROM` 的元数据，网络不通就整镜像失败；脚本
因此带了一条离线回退：把新编译的二进制 `docker cp` 进临时容器再 `docker commit`
（commit 时显式写回 `ENTRYPOINT`，否则会把临时容器的 `sleep` 提交进去）。等价于只重跑
那一层 COPY，工具链没变时完全够用。

两份 Dockerfile 的分工，别混用：

| 文件 | 怎么出二进制 | 需要什么 |
|------|--------------|----------|
| `Dockerfile.judge` | 本机先交叉编译，`COPY dist/linux_amd64/fast-judge` | 本机有 Go；`dist/linux_amd64/` 里**要有那个文件**（空目录会让构建在 COPY 那一步失败） |
| `Dockerfile.judge.source` | 容器里 `golang` 阶段编（`SOURCE=1` 走这条） | 构建上下文里有 `go.mod`、`go.sum`、`cmd/`、`internal/`；一次网络取模块（默认 `GOPROXY=https://goproxy.cn,direct`） |

`.dockerignore` 因此**不能用 `*` 减白名单**：那样上下文里就没有模块文件，后一份在
`COPY go.mod` / `go mod tidy` 那一步直接撞死。它现在是逐条列的黑名单——挡的是
`data/`（几十 MB 演示库与用例）、`.git/`、`.tmp/`、`*.exe` 这些，源码留在上下文里。

容器里跑的约束（下面这套已在 Docker Desktop 29.7.2 / linux 引擎上实测）：

| 手段 | 挡住什么 |
|------|----------|
| `cap_drop ALL` + `cap_add SETUID,SETGID,KILL,SYS_ADMIN,NET_ADMIN,SYS_PTRACE`，`no-new-privileges` | 只保留"把提交降权""杀掉它""开牢房的那三个能力"，其余一律没有 |
| `-run-as 65534:65534`（判机 root 起步，提交降到 nobody） | 提交读不到判机进程的环境（`/proc/1/environ`）与判机令牌，跨 uid 之后内核直接拒绝 |
| `read_only` 根文件系统 + `/work` tmpfs（512 MiB，**必须带 `exec`**） | 改不了镜像里的任何东西；写盘有额度。Docker 给 `--tmpfs` 默认挂 `noexec`，而编译产物就在 `/work`，漏写 `exec` 时判题全部 `IE` |
| `mem_limit` / `memswap_limit`（不给 swap） | 超内存被 cgroup 拦下，实测 512 MiB 的提交报出 524940 KB 并判 `MLE` |
| `pids_limit 256` + `--ulimit nproc=128` | fork 炸弹 |
| `--ulimit fsize=64MiB`、`core=0`、`cpu=120` | 写满磁盘、留下 core 文件、CPU 兜底 |
| 判机侧 `Setpgid` + 整组击杀 | 超时的进程树不会变成孤儿继续吃 CPU |

判机以前不发 rlimit 给子进程：Go 在 Linux 上没有"spawn 时带 rlimit"的通道
（`SysProcAttr.Rlimits` 只有 BSD 有），而绕道 shell 的 `ulimit` 会让判定结果依赖镜像里
装的是 dash 还是 bash。现在这一条由**牢房**解决：jailer 在 `exec` 提交程序之前自己
`setrlimit`，所以内核层面有硬顶，而判定仍然来自判机的**测量**（CPU 取 `wait4` 的
rusage，内存取 `ru_maxrss`），Linux 容器的 `MLE` 因此比 Windows 的 `RE` 更准确。
cgroup 仍然是最外面那层（`mem_limit`/`pids_limit`），牢房是它里面一层。

### 判定口径（`judge.Run`）

- **实测 CPU 时间超过本题时限就是 `TLE`**，即使程序自己正常退出了。原来只有 wall-clock
  到点才给 TLE，一个跑掉 1038ms 却 `return 0` 的程序会被判成 AC。
- 每个测试点允许多跑 **50 ms** 才被砍（`wall = 时限 + 50ms`，最小 250 ms 兜住进程创建）。
  这 50 ms 不是为了放宽时限，是为了让程序**跑完到能被测量**：1000ms 的题，
  1050 / 1038 / 1001ms 都算超时，各自报出自己的数字。
- 被中途杀掉的运行只能给出**下界**，网页与文本视图给它们加 `>` 前缀：
  `>1050ms`、`>289.00M`；自然跑完的运行仍然显示精确值。
- 内存不再等容器 OOM：判机每 20 ms 轮询一次峰值，超过**本题上限 + 16 MB** 立刻整组击杀
  并判 `MLE`（Linux 读 `/proc/<pid>/statm`，Windows 读 Job 的峰值）。实测 256 MB 上限的题，
  一个申请 512 MB 的程序在 289 MB 处被砍，报 `>289.00M`。
- **全局数组把镜像本身撑过上限的那种崩溃，判 `MLE` 而不是 `RE`**（`internal/judge/elfimage.go`）。
  轮询熔断量的是**驻留**内存，而一个 `vector<edge> g[30000000]` 型的程序根本走不到"驻留很多"
  那一步：牢房的 `RLIMIT_AS`（上限 + 512 MB）装不下它，内核扩 brk 覆盖 `.bss` 那一步**静默
  失败**，进程起得来、碰到第一个没映射上的页就被打死（判机那台 ECS 实测：`p_memsz=915.6MiB`
  的二进制在 `ulimit -v 786432` 下退出码 139、驻留几 MB）。生产提交 #43 交出来的因此是一句
  自相矛盾的话——`运行时错误 · 内存 17.52M`，而真相反是他要占的地址空间是本题 256 MB 上限的
  四倍多（`readelf` 实测那份形状 `p_memsz` 之和 = 1187504 KB）。现在判机在跑之前解析一次可执行文件的
  `PT_LOAD` 段之和（`p_memsz`，含 `.bss`；只读文件头，不需要跑起来），**三个条件同时成立**才
  改判：判定是 `RE`、进程是**被信号打死**（读 wait status 的 `Signaled()`——Go 把信号死记成退出码
  `-1`，从退出码反推不出来）、镜像需求超过"上限 + 判机那 512 MB 余量"（连这点宽容都放不下，才算
  根本没跑起来）。改判时内存那一格**保留实测驻留值**，需求写进判机信息。ECS 上端到端实测的
  一条（960 MB 全局数组、256 MB 上限，`TestHugeStaticImageJudgesMLENotRE`）：
  `内存超限 · 内存 7.52M` + 判机信息
  `程序本身要 916.37MB 内存（静态数组、代码与只读数据加起来），本题上限只有 256MB，判机额外给的
  512MB 余量也放不下：还没开始跑，地址空间就已经不够，第一条指令附近就被内核打死；signal: segmentation fault`
  —— 老师一眼看得出孩子的问题是数组开大了。
  镜像读不出来（Windows 的 PE、被壳处理过的文件、题目没设内存上限）就一律不动：**它只负责解释
  一次已经发生的崩溃，不当任何判定的必要条件**。

#### 重判：旧提交不会因为判机升级自己改判

上面那条修正只影响**之后**的判题。库里已有的判定是当时那一次运行的结果——判机换了、口径变了、
用例改了，旧提交都还是那句旧话，页面上就多出一句已经不对的"事实"。`POST /submissions/{id}/rejudge`
（要 `submission.rejudge`，详情页与列表行末各一条按钮）就是把这个动作补上：那条提交退回队列，
判机按今天的口径重跑一次。

- **清掉的**：判定、得分、用时与内存、`>` 下界标记、失败用例、判机信息、判机编号、开始与判完的
  时间，以及上一台判机回报的那句"我这次用的口径"（`toolchain`）——留着它，页面上就又有一句替
  旧判定背书的谎。**逐用例行一起删**，免得新一次的进度和上一次的混在一张表里。
  **不动的**：题目总分 `total_score`（那是题目的分值表，不是判机的产出）、源码、提交时间、所属比赛。
- **正在被判的那一条也允许重判**：`judge_id` 被清成 `NULL`，那台判机跑完来报时会先在归属检查上吃
  403（`TestRejudgedSubmissionIgnoresTheOldJudge`），盖不到新结果。它本地的进程还会跑完那一次，
  白烧一格 CPU，这是明知且接受的代价。
- **排队中的那条按下去是空操作**：它没有上一次可清，页面如实说"这条已经在队列里等判了"，
  列表上也就不给那条摆按钮。
- **重判后的提交排在队列最前**：判机按 id 取最老的那条 `queued`，旧提交 id 小，于是插到队首。
  管理员按下按钮是想要一个答案，不是想要一次排队；但要**批量**重判时得知道这会让新提交多等一会儿。
- **谁跟着变**：排行榜与比赛分是读时算的，重判完立刻跟着变；积分由后台那个定时重算在下一轮补上。
  重判**不可撤回**——旧的判定已经被清掉，要留证据就先备份库。
- **留痕**：每一次重判进审计一条 `submission.rejudge`，写明是谁按的、重判之前是什么状态与判定。
  列表的 `?status=` 筛选项是配套的那只手：停在队列里的、判机超时的，混在一堆已完成的记录里翻不出来。

### 判机容器的两个坑

两个坑是实测踩出来的，都进了代码与测试：

- **`cap_add` 里少了 `KILL`**：降权之后判机（uid 0）给 uid 65534 的子进程发 SIGKILL 会
  `EPERM`。表现是超时的程序杀不掉、判机卡在输出管道上、整条队列停摆（要等容器
  `--ulimit cpu` 收尸）。现在 `Kill` 会把错误带出来，并且最多再等 5 秒就带着
  `TLE` 与原因返回，绝不无限占着判机槽位；回归用例是 `TestDroppedSubmissionStillTimesOut`
  （需要 root 判机，用 `FASTOJ_TEST_RUN_AS_UID=65534` 打开）。
- **`internal: true` 网络不能用**：它把 `host.docker.internal` 一起挡了
  （`dial tcp 192.168.65.254:18080: network is unreachable`），判机领不到工单。所以
  compose 里默认不开，**提交程序的出口网络目前没有被限制**——要单独掐掉，得给每次运行
  开一个无网络的命名空间（容器内 root + `CAP_NET_ADMIN` + `unshare -n`，再降权），
  这条归到外部 sandbox 那一期。挡住的关键面仍然是：宿主的文件系统、进程与关机权限。

输出：每路最多 1 MiB，超出即按 `WA` 判（已经打印出来的内容不可能再对上答案）。

---

---

[← 返回 README](../README.md)
