package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 轻量查重：只比较同一道题、同一语言、已 AC 的提交，按规范化后的
// 4-token 滑动窗口算 Jaccard。它只产出"值得人工看一眼"的名单，
// 不自动扣分、不自动封号——重命名变量与改写结构都会让分数掉下去，
// 误伤与漏报都该由人判断。

const (
	plagiarismShingle = 4
	plagiarismMinTokens = 20
	plagiarismPercent = 80
	plagiarismMaxPairs = 100
	plagiarismMaxSources = 300
)

func normalizeSource(src string) []string {
	// 预处理行（#include / #define）通常两边都一样，留着只会把分数整体抬高。
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	s := b.String()
	tokens := make([]string, 0, len(s)/4)
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
		case c == '"' || c == '\'':
			quote := c
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				if s[i] == quote {
					i++
					break
				}
				i++
			}
			tokens = append(tokens, "S")
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			tokens = append(tokens, s[i:j])
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < len(s) && (isIdentPart(s[j]) || s[j] == '.') {
				j++
			}
			tokens = append(tokens, "N")
			i = j
		default:
			tokens = append(tokens, string(c))
			i++
		}
	}
	return tokens
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

func shingleSet(tokens []string) map[string]struct{} {
	if len(tokens) < plagiarismMinTokens {
		return nil
	}
	out := make(map[string]struct{}, len(tokens)-plagiarismShingle+1)
	for i := 0; i+plagiarismShingle <= len(tokens); i++ {
		out[strings.Join(tokens[i:i+plagiarismShingle], "\x1f")] = struct{}{}
	}
	return out
}

// jaccard 返回 0..100 的整数百分比。
func jaccard(a, b map[string]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	if len(a) > len(b) {
		a, b = b, a
	}
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return inter * 100 / union
}

type plagiarismPair struct {
	Similarity            int
	Language              string
	AID, BID              int64
	AHandle, BHandle      string
	AName, BName          string
	AWhen, BWhen          string
}

type plagiarismData struct {
	Query    string
	Problem  string
	ProblemName string
	Checked  int
	Pairs    []plagiarismPair
	Percent  int
	MinTokens int
	TooShort int
}

func (s *Server) handlePlagiarism(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePerm(w, r, permSubmissionViewAny)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("problem"))
	d := plagiarismData{Query: query, Percent: plagiarismPercent, MinTokens: plagiarismMinTokens}
	if query != "" {
		p, err := s.db.FindProblem(r.Context(), query)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		items, err := s.db.ListAcceptedSources(r.Context(), p.ID, plagiarismMaxSources)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		d.Problem, d.ProblemName, d.Checked = p.PID, p.Title, len(items)

		type prepared struct {
			item  store.SourceSubmission
			shing map[string]struct{}
		}
		byLang := map[string][]prepared{}
		for _, it := range items {
			sh := shingleSet(normalizeSource(it.Source))
			if sh == nil {
				d.TooShort++
				continue
			}
			byLang[it.Language] = append(byLang[it.Language], prepared{item: it, shing: sh})
		}
		for lang, list := range byLang {
			for i := 0; i < len(list); i++ {
				for j := i + 1; j < len(list); j++ {
					a, b := list[i], list[j]
					if a.item.UserID == b.item.UserID {
						continue
					}
					sim := jaccard(a.shing, b.shing)
					if sim < plagiarismPercent {
						continue
					}
					an, bn := a.item.Name, b.item.Name
					if an == "" {
						an = a.item.Handle
					}
					if bn == "" {
						bn = b.item.Handle
					}
					d.Pairs = append(d.Pairs, plagiarismPair{
						Similarity: sim, Language: lang,
						AID: a.item.ID, BID: b.item.ID,
						AHandle: a.item.Handle, BHandle: b.item.Handle,
						AName: an, BName: bn,
						AWhen: agoMS(a.item.CreatedAt), BWhen: agoMS(b.item.CreatedAt),
					})
				}
			}
		}
		sort.Slice(d.Pairs, func(i, j int) bool { return d.Pairs[i].Similarity > d.Pairs[j].Similarity })
		if len(d.Pairs) > plagiarismMaxPairs {
			d.Pairs = d.Pairs[:plagiarismMaxPairs]
		}
	}
	s.page(w, r, http.StatusOK, "查重", "plagiarism", a.viewer(), msgOf(r), d)
}
