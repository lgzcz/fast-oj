package api

import (
	"strings"
	"testing"
)

const statementSource = `# 题目描述

输入两个整数 $a, b$，输出 $a+b$。平均值写成块级公式：

$$ \frac{a+b}{2} \times x_1 $$

## 输入格式

` + "```" + `cpp
int a, b;
std::cin >> a >> b;
` + "```" + `

## 数据范围

| 档 | a 的范围 | 分值 |
|----|----------|------|
| 1  | 0..10    | 40   |
| 2  | 0..1e9   | 60   |

- 行内代码用 ` + "`std::cin`" + ` 读入
- 链接：[官网](https://example.com/oj)
`

func dangerousStatement() string {
	return "裸 HTML：<script>alert(1)</script>\n\n" +
		"图片事件：<img src=x onerror=\"alert(2)\">\n\n" +
		"伪协议：[点我](javascript:alert(3))\n\n" +
		"数据协议：[文档](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)\n\n" +
		"实体编码：[点我](&#106;avascript:alert(4))\n\n" +
		"十六进制实体：[点我](&#x6a;avascript:alert(5))\n\n" +
		"命名实体：[点我](javascript&colon;alert(6))\n"
}

func TestRenderMarkdownKeepsOJFeatures(t *testing.T) {
	out := string(renderMarkdownHTML(statementSource))
	// The MathJax extension turns $...$ / $$...$$ into these spans with the TeX
	// kept inside as \( \) / \[ \]. That is the contract the KaTeX bootstrap in
	// the page template relies on, so assert it here rather than scanning text.
	for _, want := range []string{
		"<h1>题目描述</h1>", "<table>", "<th>分值</th>",
		`<pre><code class="language-cpp">`,
		`<span class="math inline">\(a, b\)</span>`,
		`<span class="math display">\[ \frac{a+b}{2} \times x_1 \]</span>`,
		`<a href="https://example.com/oj" target="_blank" rel="nofollow">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in rendered statement:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownRefusesInjectedHTML(t *testing.T) {
	out := string(renderMarkdownHTML(dangerousStatement()))
	for _, forbidden := range []string{"<script", "</script>", "onerror", "javascript:", "data:text/html"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("leaked %q:\n%s", forbidden, out)
		}
	}
	// The text is still shown, just not as live markup.
	if !strings.Contains(out, "alert") {
		t.Errorf("content was dropped instead of escaped:\n%s", out)
	}
}

func TestRenderMarkdownLuoguExtensions(t *testing.T) {
	src := `::::info[外层标题]{open}
外层

:::success[内层]
**内层内容**
:::
::::

:::align{center}
居中
:::

:::epigraph[——otto]
引言
:::

::cute-table{tuack}
| 名 | 值 |
|----|----|
| a  | 1  |

- [ ] 未做
- [x] 已做
`
	out := string(renderMarkdownHTML(src))
	for _, want := range []string{
		`<details class="md-box md-box-info" open><summary>外层标题</summary>`,
		`<details class="md-box md-box-success"><summary>内层</summary>`,
		`<strong>内层内容</strong>`,
		`<div class="md-align md-align-center">`,
		`<blockquote class="md-epigraph">`,
		`<footer>——otto</footer>`,
		`<div class="md-cute-table">`,
		`<table>`,
		`<li class="task-item"><input type="checkbox" disabled> 未做`,
		`<li class="task-item"><input type="checkbox" disabled checked> 已做`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ":::") {
		t.Errorf("container fences leaked into HTML:\n%s", out)
	}
}

func TestUnclosedContainerStaysText(t *testing.T) {
	out := string(renderMarkdownHTML(":::info\n没有闭合\n"))
	if !strings.Contains(out, ":::info") {
		t.Errorf("unclosed container was consumed:\n%s", out)
	}
}

func TestRenderMarkdownEmptyIsNothing(t *testing.T) {
	if got := renderMarkdownHTML("   \n\t"); got != "" {
		t.Fatalf("blank statement rendered %q", got)
	}
	if got := renderMarkdownHTML(""); got != "" {
		t.Fatalf("nil statement rendered %q", got)
	}
}

func TestSafeURLPolicy(t *testing.T) {
	for _, bad := range []string{
		"javascript:alert(1)", "JavaScript:x", " data:text/html,x", "vbscript:y",
		"file:///c:/x", "about:blank", "blob:https://x/1",
		// 渲染器会先反转义实体再写 href；闸门必须看同一个形状。
		"&#106;avascript:alert(1)", "&#x6a;avascript:alert(1)",
		"javascript&colon;alert(1)", "jav&#x61;script:alert(1)",
		"&#100;ata:text/html;base64,x",
	} {
		if safeURL([]byte(bad)) {
			t.Errorf("%s must be refused", bad)
		}
	}
	for _, good := range []string{"https://example.com", "http://a.b/c", "/relative/path", "./sibling", "../up", "#anchor", "mailto:a@b.c", "foo/bar", "?page=2", "//cdn.example/x"} {
		if !safeURL([]byte(good)) {
			t.Errorf("%s must be allowed", good)
		}
	}
}
