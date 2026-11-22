package api

import (
	"net/http"
	"strings"
	"testing"
)

const similarSourceA = `#include <bits/stdc++.h>
using namespace std;
int main() {
    int n; cin >> n;
    long long s = 0;
    for (int i = 0; i < n; i++) { int x; cin >> x; s += x; }
    cout << s << endl;
    return 0;
}`

func TestPlagiarismNormalization(t *testing.T) {
	a := normalizeSource(similarSourceA)
	b := normalizeSource(strings.ReplaceAll(strings.ReplaceAll(similarSourceA,
		"int main() {", "int main() { // entry"), "long long s = 0;", "long long s=0; /* sum */"))
	if got := jaccard(shingleSet(a), shingleSet(b)); got < 95 {
		t.Fatalf("formatting/comment changes should barely move similarity, got %d", got)
	}
	if shingleSet(normalizeSource("int main(){return 0;}")) != nil {
		t.Fatal("short sources should be skipped")
	}
	if a, b := normalizeSource(`int main(){cout<<"a";}`), normalizeSource(`int main(){cout<<"b";}`);
		strings.Join(a, " ") != strings.Join(b, " ") {
		t.Fatalf("string literals should normalize away: %v vs %v", a, b)
	}
}

func TestPlagiarismPage(t *testing.T) {
	h := newHarness(t)
	if code, body := h.createProblem(problemValues("PL1", "查重题")); code != http.StatusOK {
		t.Fatalf("create problem: %d %s", code, body)
	}
	other := `#include <iostream>
int main() {
    long long a = 0, b = 1;
    while (b < 100000) { long long c = a + b; a = b; b = c; }
    std::cout << b;
    return 0;
}`
	for handle, src := range map[string]string{
		"subA": similarSourceA,
		"subB": strings.ReplaceAll(similarSourceA, "    ", "  "),
		"subC": other,
	} {
		h.signIn(handle)
		if _, err := h.db.SQL.ExecContext(ctx(),
			`INSERT INTO submissions (user_id, problem_id, language, source, status, verdict, total_score, created_at)
			 VALUES (?, (SELECT id FROM problems WHERE pid='PL1'), 'cpp', ?, 'done', 'AC', 100, 1)`,
			h.dbUserID(t, handle), src); err != nil {
			t.Fatal(err)
		}
	}
	// 同一个人的两份相似代码不该被报成一对。
	sameUID := h.dbUserID(t, "subA")
	if _, err := h.db.SQL.ExecContext(ctx(),
		`INSERT INTO submissions (user_id, problem_id, language, source, status, verdict, total_score, created_at)
		 VALUES (?, (SELECT id FROM problems WHERE pid='PL1'), 'cpp', ?, 'done', 'AC', 100, 2)`,
		sameUID, similarSourceA); err != nil {
		t.Fatal(err)
	}

	plain := h.signIn("plagplain")
	if code, _ := h.getAs(plain, "/admin/plagiarism?problem=PL1"); code != http.StatusForbidden {
		t.Fatalf("normal user should be refused, got %d", code)
	}
	viewer := h.signIn("plagview", permSubmissionViewAny)
	code, body := h.getAs(viewer, "/admin/plagiarism?problem=PL1")
	if code != http.StatusOK || !strings.Contains(body, "100%") ||
		!strings.Contains(body, "/u/subA") || !strings.Contains(body, "/u/subB") {
		t.Fatalf("plagiarism page: %d %s", code, body)
	}
	if strings.Contains(body, "/u/subC") {
		t.Fatalf("unrelated source should not pair: %s", body)
	}
	// 表单页不需要先填题。
	if code, body := h.getAs(viewer, "/admin/plagiarism"); code != http.StatusOK || !strings.Contains(body, "查这道题") {
		t.Fatalf("empty form: %d %s", code, body)
	}
}
