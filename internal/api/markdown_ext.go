package api

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"io"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown/ast"
)

// 洛谷的 Markdown 比其他 CommonMark 实现多几块语法：:::info 一类的折叠框、
// :::align{center}、:::epigraph 引言、::cute-table 表格外观，以及任务列表。
// 这里只同步语法与语义，不复制 Markdown*Palettes 的 Vue 编辑器。

type mdBox struct {
	ast.Container
	Kind  string
	Title string
	Open  bool
}

type mdAlign struct {
	ast.Container
	Side string
}

type mdEpigraph struct {
	ast.Container
	Attribution string
}

type mdCuteTable struct{ ast.Container }

var mdBoxLabels = map[string]string{
	"info":    "提示",
	"success": "成功",
	"warning": "警告",
	"error":   "错误",
}

// luoguParserHook runs before the standard block rules. It consumes a whole
// ::: block (including the closing fence) and hands the inside back to the parser
// as that node's children, so nesting and inline Markdown keep working.
func luoguParserHook(data []byte) (ast.Node, []byte, int) {
	line, _ := mdLine(data)
	if line == nil {
		return nil, nil, 0
	}
	node, colons := parseColonOpener(line)
	if node == nil {
		return nil, nil, 0
	}
	if _, ok := node.(*mdCuteTable); ok {
		body, consumed, ok := mdTableBlock(data)
		if !ok {
			return nil, nil, 0
		}
		return node, body, consumed
	}
	body, consumed, ok := mdColonBody(data, colons)
	if !ok {
		return nil, nil, 0
	}
	return node, body, consumed
}

// mdLine returns one line without its newline plus the offset just past it.
func mdLine(data []byte) ([]byte, int) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return bytes.TrimRight(data[:i], "\r"), i + 1
	}
	return bytes.TrimRight(data, "\r"), len(data)
}

func parseColonOpener(line []byte) (ast.Node, int) {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(line)-len(trimmed) > 3 {
		return nil, 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ':' {
		n++
	}
	if n < 2 {
		return nil, 0
	}
	rest := strings.TrimSpace(string(trimmed[n:]))
	if rest == "" {
		return nil, 0
	}
	if node := parseCuteTableOpener(rest); node != nil {
		return node, n
	}
	if n < 3 {
		return nil, 0
	}
	if node := parseBoxOpener(rest); node != nil {
		return node, n
	}
	if node := parseAlignOpener(rest); node != nil {
		return node, n
	}
	if node := parseEpigraphOpener(rest); node != nil {
		return node, n
	}
	return nil, 0
}

func parseBoxOpener(rest string) ast.Node {
	for _, kind := range []string{"info", "success", "warning", "error"} {
		if !strings.HasPrefix(rest, kind) {
			continue
		}
		tail := rest[len(kind):]
		if tail != "" && !strings.HasPrefix(tail, "[") && !strings.HasPrefix(tail, "{") {
			continue
		}
		box := &mdBox{Kind: kind}
		var ok bool
		if strings.HasPrefix(tail, "[") {
			box.Title, tail, ok = mdBracket(tail)
			if !ok {
				return nil
			}
		}
		tail = strings.TrimSpace(tail)
		switch tail {
		case "":
		case "{open}":
			box.Open = true
		default:
			return nil
		}
		return box
	}
	return nil
}

func parseAlignOpener(rest string) ast.Node {
	if !strings.HasPrefix(rest, "align{") {
		return nil
	}
	end := strings.IndexByte(rest, '}')
	if end < 0 || strings.TrimSpace(rest[end+1:]) != "" {
		return nil
	}
	side := strings.TrimSpace(rest[len("align{"):end])
	if side != "center" && side != "right" {
		return nil
	}
	return &mdAlign{Side: side}
}

func parseEpigraphOpener(rest string) ast.Node {
	if !strings.HasPrefix(rest, "epigraph") {
		return nil
	}
	tail := rest[len("epigraph"):]
	ep := &mdEpigraph{}
	if strings.HasPrefix(tail, "[") {
		var ok bool
		ep.Attribution, tail, ok = mdBracket(tail)
		if !ok {
			return nil
		}
	}
	if strings.TrimSpace(tail) != "" {
		return nil
	}
	return ep
}

func parseCuteTableOpener(rest string) ast.Node {
	if !strings.HasPrefix(rest, "cute-table") {
		return nil
	}
	tail := strings.TrimSpace(rest[len("cute-table"):])
	if tail != "" && tail != "{tuack}" {
		return nil
	}
	return &mdCuteTable{}
}

// mdBracket reads one [...] title; the closing bracket must exist.
func mdBracket(s string) (string, string, bool) {
	if !strings.HasPrefix(s, "[") {
		return "", s, false
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return "", s, false
	}
	return s[1:end], s[end+1:], true
}

// mdColonBody finds the closing line made of exactly n colons. Nested blocks use
// more colons, so a ::: line inside a :::: block is content, not its end.
func mdColonBody(data []byte, n int) ([]byte, int, bool) {
	_, pos := mdLine(data)
	start := pos
	closing := strings.Repeat(":", n)
	for pos < len(data) {
		line, next := mdLine(data[pos:])
		if strings.TrimSpace(string(line)) == closing {
			return data[start:pos], pos + next, true
		}
		pos += next
	}
	return nil, 0, false
}

// mdTableBlock is the ::cute-table opener plus the table that follows it. The
// syntax has no closing fence: the table ends at a blank line.
func mdTableBlock(data []byte) ([]byte, int, bool) {
	_, pos := mdLine(data)
	start := pos
	for pos < len(data) {
		line, next := mdLine(data[pos:])
		if len(bytes.TrimSpace(line)) == 0 {
			break
		}
		pos += next
	}
	body := data[start:pos]
	if !bytes.Contains(body, []byte("|")) {
		return nil, 0, false
	}
	return body, pos, true
}

func luoguRenderHook(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	switch n := node.(type) {
	case *mdBox:
		if entering {
			title := n.Title
			if title == "" {
				title = mdBoxLabels[n.Kind]
			}
			open := ""
			if n.Open {
				open = " open"
			}
			fmt.Fprintf(w, `<details class="md-box md-box-%s"%s><summary>%s</summary><div class="md-box-body">`,
				n.Kind, open, stdhtml.EscapeString(title))
		} else {
			io.WriteString(w, `</div></details>`)
		}
		return ast.GoToNext, true
	case *mdAlign:
		if entering {
			fmt.Fprintf(w, `<div class="md-align md-align-%s">`, n.Side)
		} else {
			io.WriteString(w, `</div>`)
		}
		return ast.GoToNext, true
	case *mdEpigraph:
		if entering {
			io.WriteString(w, `<blockquote class="md-epigraph">`)
		} else {
			if n.Attribution != "" {
				fmt.Fprintf(w, `<footer>%s</footer>`, stdhtml.EscapeString(n.Attribution))
			}
			io.WriteString(w, `</blockquote>`)
		}
		return ast.GoToNext, true
	case *mdCuteTable:
		if entering {
			io.WriteString(w, `<div class="md-cute-table">`)
		} else {
			io.WriteString(w, `</div>`)
		}
		return ast.GoToNext, true
	}
	return ast.GoToNext, false
}

// Task list items are the one extension that is easier after rendering: the list
// parser already produced <li>, and raw HTML is skipped, so this only ever sees
// list items this renderer wrote.
var taskItemRe = regexp.MustCompile(`<li>(\s*(?:<p>)?)\[([ xX])\]\s+`)

func decorateTaskLists(body []byte) []byte {
	return taskItemRe.ReplaceAllFunc(body, func(m []byte) []byte {
		sub := taskItemRe.FindSubmatch(m)
		checked := ""
		if string(sub[2]) == "x" || string(sub[2]) == "X" {
			checked = " checked"
		}
		return []byte(`<li class="task-item">` + string(sub[1]) +
			`<input type="checkbox" disabled` + checked + `> `)
	})
}
