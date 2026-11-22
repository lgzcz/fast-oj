## Python 提交：护栏装在解释器里，边界还是牢房

判机收 `c` / `cpp` / `python`。Python 那一支不编译：判机把源码写成 `main.py`，在同一目录里
再落一份 `internal/judge/pyguard.py`，然后用

```
python3 -I -B -S -X utf8 <工作目录>/pyguard.py <工作目录>/main.py
```

跑起来——`-I` 让 `PYTHONPATH` 之类的环境变量进不来，`-S` 不跑 `site`（少一条把 site-packages
塞进 `sys.path` 的路，`pip` 更进不来），`-B` 不落 `.pyc`，`-X utf8` 让学生打印中文时不跟着
`LANG=C` 变乱码。

- **时限原样算**：解释器启动那几十毫秒 CPU 也算在学生的用时里，不放宽。
- **内存只算学生那一份**，但题面内存低于 **32MB** 时站点直接拒收 Python：实测 CPython 3.14
  空跑常驻就要 ~9MB，再 import 一点标准库只多不少——那种题面判的不是学生的程序，是解释器。
  提交页会把这句话当面说明，而不是让提交进去撞一条 MLE。

### 护栏挡什么

`pyguard.py` 装的是一个 CPython **审计钩子**（`sys.addaudithook`，装上就摘不掉，学生代码里
`exec`/`eval` 出来的东西同样经过它）。挡下的是"离开这次运行"的那几扇门：`subprocess`、
`socket`/`ssl`/`select`/`selectors`/`asyncio`、`ctypes`、`multiprocessing`、`pty`、`webbrowser`、
`http`/`urllib`/`ftplib`/`smtplib` 等网络库，以及 `pip`/`ensurepip`/`venv`/`setuptools`/`distutils`。
撞到钩子时学生看到的是一条 `RuntimeError: 判机不允许……`，判定是 **RE**，原因在 stderr 里。

名单**只列真能出门的那几扇**，这有实测依据：`pathlib` 在 3.14 会去 `import fcntl`，早先那份
名单把 `fcntl` 也禁了，于是 `import pathlib` 直接炸——而且只在 Linux 上炸（Windows 没有这个模块）。
同理 `os`、`shutil`、`signal`、`threading` 都留着：禁 `os` 会让 `random`/`pathlib`/`tempfile`
一起塌。有一条测试同时盯这两半（该挡的真挡住、常用的还能用）。

**护栏是纵深，不是边界**。它假定牢房开着——"不许离开这次运行的目录"这条线是 chroot 画的。
所以判机有两道闸门：

1. **启动自测**（`judge.PythonProbe`）：一次正常程序必须跑通，一次想起子进程的必须被挡下来，
   而且当前配置必须真能建牢房。任何一条不过，就把 `python` 从这台判机的名单上摘掉，并把原因
   挂在 `/judges` 的备注里——站点因此不会摆出一个"能选但判不了"的选项。
2. **领单时**（`internal/judgeagent`）：`!judge.Jailed()` 而工单里有任何一份要跑的 Python
   （提交、Python 写的判题器或交互器都算）→ 判机故障 + 那句原因。

`/judges` 上能看到这台机器实际的口径：`沙盒已启用（…）；Python 自测：…` 或者被摘掉的原因。

### 判题器与交互器也能用 Python 写

`BuildAux` 对不编译的工具会把那份护栏一起落进目录，交回"解释器 + 护栏 + 源码"这条命令行，
所以 SPJ 与交互器的 `argv` 约定和 C++ 完全一致（`argv[1..3]` = 输入 / 选手输出 / 答案，
退出码 `0/1/7`）。作者那份 Python 与学生那份拿到的是同一层护栏、同一个牢房、同一份
`10 秒 / 512MB / 8 进程` 的预算。

### 部署

判机那台机器要有 `python3`（`Dockerfile.judge` 里 `apt install python3`）。**服务器与判机要一起升**：
旧判机不认识工单里的 `python`，会按"这台机器没有这门语言"回一条判机故障——那条提交废了，
但比悄悄用另一套口径判掉要好。启动日志与 `/judges` 会写明它到底有没有这门语言。

---

---

[← 返回 README](../README.md)
