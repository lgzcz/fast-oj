package api

import (
	"html/template"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

// Statement Markdown comes from problem authors, but it is still rendered with
// raw HTML skipped and dangerous URL schemes refused: one stray <script> in a
// statement would otherwise run on every contestant's page.
func newMarkdownParser() *parser.Parser {
	// DefinitionLists 会把行首的 ::: 吃成定义列表，和洛谷提示框的围栏直接冲突；
	// 洛谷的 markdown-it 本身也没有定义列表，所以这里把它关掉。
	p := parser.NewWithExtensions(
		(parser.CommonExtensions &^ parser.DefinitionLists) | parser.MathJax)
	// 洛谷的 :::info / :::align / :::epigraph / ::cute-table 在标准块规则之前
	// 被整块领走，内部内容仍然按普通 Markdown 解析。
	p.Opts.ParserHook = luoguParserHook
	return p
}

// safeURL blocks schemes a click could execute. Relative links and http(s),
// which the renderer allows by default, stay usable.
func safeURL(raw []byte) bool {
	s := strings.ToLower(strings.TrimSpace(string(raw)))
	for _, bad := range []string{"javascript:", "data:", "vbscript:", "file:", "blob:", "about:"} {
		if strings.HasPrefix(s, bad) {
			return false
		}
	}
	return true
}

// renderMarkdownHTML takes a string so it can be used directly as a template
// function.
func renderMarkdownHTML(src string) template.HTML {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	// Safelink is what makes IsSafeURLOverride actually consulted; without it the
	// predicate is dead code and javascript: hrefs sail through.
	r := html.NewRenderer(html.RendererOptions{
		Flags:          html.SkipHTML | html.HrefTargetBlank | html.Safelink | html.NofollowLinks,
		RenderNodeHook: luoguRenderHook,
	})
	r.IsSafeURLOverride = safeURL
	body := markdown.ToHTML([]byte(src), newMarkdownParser(), r)
	body = decorateTaskLists(body)
	return template.HTML(body) //nolint:gosec // raw HTML is skipped and URLs filtered above
}
