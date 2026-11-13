package api

import (
	stdhtml "html"
	"html/template"
	"net/url"
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
//
// The check has to run on the same text the renderer writes into href: it calls
// html.UnescapeString first, so `&#106;avascript:` would pass a raw prefix check
// and then become javascript: in the browser. Unescape here for the same reason.
func safeURL(raw []byte) bool {
	s := stdhtml.UnescapeString(strings.TrimSpace(string(raw)))
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		// Relative references ("foo/bar", "#anchor", "//host/path") carry no
		// executable scheme of their own.
		return true
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
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
