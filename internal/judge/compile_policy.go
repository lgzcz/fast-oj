package judge

import (
	"fmt"
	"regexp"
	"strings"
)

// 这份策略回答一个问题：页面写着“C++17 · O2 开”，实际编译也必须就是那一条命令行。
// GCC 的 #pragma optimize / target 会从源码里把命令行覆盖掉，而且没有跨版本的插件
// 可以安静地抹掉它（插件与判机的 GCC 版本强绑定）。所以判机选择明说：这类指令
// 直接按编译错误拒绝，而不是假装没看见、留下一句与实际不符的口径。
//
// 扫描的是去掉注释后的源码。字符串里的 “#pragma GCC optimize” 也会命中——宁可让
// 一句玩笑输出换来一次明确的 CE，也不放过拼装出来的绕过（宏、_Pragma、token 粘贴）。

var (
	// #pragma optimize / #pragma GCC optimize / #pragma GCC target ...
	pragmaDirective = regexp.MustCompile(`(?i)\bpragma\s+(?:gcc\s+)?(?:optimize|target|push_options|pop_options)\b`)
	// _Pragma("GCC optimize ...")；_Pragma 里的字符串也会被这条看见。
	pragmaOperator = regexp.MustCompile(`(?i)_pragma\s*\(\s*"(?:gcc\s+)?(?:optimize|target)\b`)
	// __attribute__((optimize("O3"))) / __attribute__((target("avx2")))
	optimizeAttr = regexp.MustCompile(`(?is)__attribute__\s*\(\([^)]*\b(?:optimize|target)\s*\(`)
)

// forbiddenCompileDirective 返回第一条改写编译口径的指令所在行（从 1 起）与原文。
// 没有命中时 line 为 0。
func forbiddenCompileDirective(source string) (line int, text string) {
	cleaned := stripComments(source)
	lines := strings.Split(cleaned, "\n")
	for i, l := range lines {
		low := strings.ToLower(l)
		switch {
		case pragmaDirective.MatchString(l), pragmaOperator.MatchString(l):
			return i + 1, clipText(strings.TrimSpace(l), 120)
		case strings.Contains(low, "__attribute__") &&
			(strings.Contains(low, "optimize") || strings.Contains(low, "target")):
			return i + 1, clipText(strings.TrimSpace(l), 120)
		}
		// 宏或 token 粘贴可能把 #pragma 拼到下一行再展开。只要这一行里同时出现
		// “pragma” 与 optimize/target，就当成用户在试图改口径；拼出 “GCC optimize”
		// 的宏调用没有 pragma 字样，也在这条里截住。
		if strings.Contains(low, "pragma") &&
			(strings.Contains(low, "optimize") || strings.Contains(low, "target")) {
			return i + 1, clipText(strings.TrimSpace(l), 120)
		}
		if strings.Contains(low, "gcc optimize") || strings.Contains(low, "gcc target") ||
			strings.Contains(low, "gcc push_options") || strings.Contains(low, "gcc pop_options") {
			return i + 1, clipText(strings.TrimSpace(l), 120)
		}
	}
	if optimizeAttr.MatchString(cleaned) {
		return 1, "__attribute__((optimize/target))"
	}
	return 0, ""
}

// compilePolicyError 是命中之后给提交人的那一段话。带上这次真正的编译口径，
// 让他知道去掉 pragma 之后会按什么来判。
func compilePolicyError(line int, text, toolchain string) string {
	origin := fmt.Sprintf("第 %d 行", line)
	if text != "" {
		origin += "「" + text + "」"
	}
	label := toolchain
	if label == "" {
		label = "命令行给出的口径"
	}
	return fmt.Sprintf("编译失败：%s试图改写判题固定的编译口径（本题按 %s 编译）。"+
		"#pragma GCC optimize / target、_Pragma 与 __attribute__((optimize/target)) 一律不允许，去掉后再提交。", origin, label)
}

// stripComments 把 // 与 /* */ 注释换成空格，保留换行、字符串与字符字面量，
// 这样行号还是原文件的行号，而 _Pragma 的字符串内容仍然能被看见。
func stripComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	inBlock := false
	inString := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inBlock {
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				b.WriteString("  ")
				i++
				inBlock = false
				continue
			}
			if c == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if inString != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(src[i+1])
				i++
				continue
			}
			if c == inString {
				inString = 0
			}
			continue
		}
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			b.WriteString("  ")
			i++
			inBlock = true
		case c == '"' || c == '\'':
			inString = c
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
