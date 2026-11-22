package judge

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestForbiddenCompileDirective(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
	}{
		{"普通代码", "#include <iostream>\nint main(){return 0;}", 0},
		{"pragma once", "#pragma once\nint main(){return 0;}", 0},
		{"诊断 pragma", "#pragma GCC diagnostic ignored \"-Wall\"\nint main(){return 0;}", 0},
		{"行注释里的 pragma", "// #pragma GCC optimize(\"O3\")\nint main(){return 0;}", 0},
		{"块注释里的 target", "/*\n#pragma GCC target(\"avx2\")\n*/\nint main(){return 0;}", 0},
		{"GCC optimize", "#include <cstdio>\n#pragma GCC optimize(\"O3\")\nint main(){return 0;}", 2},
		{"小写 target", "#pragma gcc target(\"avx2\")\nint main(){return 0;}", 1},
		{"_Pragma 运算符", "_Pragma(\"GCC optimize(\\\"O3\\\")\")\nint main(){return 0;}", 1},
		{"optimize 属性", "__attribute__((optimize(\"O3\"))) int f(){return 0;}", 1},
		{"宏包一层", "#define PRAGMA(x) _Pragma(#x)\nPRAGMA(GCC optimize(\"O3\"))\nint main(){return 0;}", 2},
		{"push_options", "#pragma GCC push_options\nint main(){return 0;}", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, text := forbiddenCompileDirective(c.src)
			if line != c.line {
				t.Fatalf("line = %d (%q), want %d", line, text, c.line)
			}
			if line > 0 && text == "" {
				t.Fatal("命中却没有给出原文")
			}
		})
	}
}

// 命中策略时应当是编译错误，而不是先编一遍再看结果：这样连编译器是否装好都不影响
// 这条判定，也省下一次白跑。
func TestJudgeRejectsRewritePragmas(t *testing.T) {
	tool, _, err := DefaultTools()["cpp"].WithOptions("")
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		SubmissionID: 1, PID: "P1", Language: "cpp",
		Source:      "#pragma GCC optimize(\"O3\")\nint main(){return 0;}",
		TimeLimitMS: 1000, MemLimitKB: 262144,
		Cases: []Case{{Name: "1", Ordinal: 1, Input: []byte(""), Expected: []byte("")}},
	}
	rep := Judge(task, tool, filepath.Join(t.TempDir(), "work"), nil, nil)
	if rep.Verdict != CE {
		t.Fatalf("verdict = %s (%s), want CE", rep.Verdict, rep.Detail)
	}
	if rep.FailedCase != "compile" {
		t.Fatalf("failed case = %q", rep.FailedCase)
	}
	for _, want := range []string{"第 1 行", "#pragma GCC optimize", "C++17 · O2 开"} {
		if !strings.Contains(rep.Detail, want) {
			t.Errorf("detail missing %q: %s", want, rep.Detail)
		}
	}
}
