package judge

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidIOFileName(t *testing.T) {
	good := []string{"game.in", "game.out", "test1.in", "a_b-c.txt", "A.OUT"}
	for _, name := range good {
		if !ValidIOFileName(name) {
			t.Errorf("%q 应该允许", name)
		}
	}
	bad := []string{"", ".", "..", ".hidden", "a/b.in", `a\b.in`, "../x", "a b.in", "a:b",
		"main.cpp", "MAIN.C", "program.exe", "pyguard.py", "con", "NUL", "x.", strings.Repeat("a", 65)}
	for _, name := range bad {
		if ValidIOFileName(name) {
			t.Errorf("%q 应该拒绝", name)
		}
	}
	if err := ValidateIOFiles("", ""); err != nil {
		t.Errorf("空对应该允许: %v", err)
	}
	if err := ValidateIOFiles("game.in", ""); err == nil {
		t.Error("只填一个应该报错")
	}
	if err := ValidateIOFiles("game.in", "GAME.IN"); err == nil {
		t.Error("同名应该报错")
	}
}

// 文件 IO 的完整路径：输入落进 game.in，程序写 game.out，判机读文件比较，
// stdout 上的东西不参与判定。
func TestFileIOJudging(t *testing.T) {
	tool := requireToolchain(t)
	src := `#include <cstdio>
int main(){
  FILE *in = fopen("game.in", "r");
  if (!in) return 2;
  int a, b;
  if (fscanf(in, "%d%d", &a, &b) != 2) return 3;
  fclose(in);
  FILE *out = fopen("game.out", "w");
  if (!out) return 4;
  fprintf(out, "%d\n", a + b);
  fclose(out);
  printf("this stdout is ignored\n");
  return 0;
}`
	rep := judgeWithIO(t, tool, src, "game.in", "game.out", sumCases(), 2000, 262144)
	if rep.Verdict != AC || rep.Score != 100 {
		t.Fatalf("文件 IO 求和题 = %s %d/%d (%s)", rep.Verdict, rep.Score, rep.TotalScore, rep.Detail)
	}
}

func TestFileIOMissingOutputIsWA(t *testing.T) {
	tool := requireToolchain(t)
	src := `int main(){ return 0; }`
	rep := judgeWithIO(t, tool, src, "game.in", "game.out", sumCases()[:1], 2000, 262144)
	if rep.Verdict != WA {
		t.Fatalf("没有输出文件 = %s (%s), want WA", rep.Verdict, rep.Detail)
	}
	if !strings.Contains(rep.Detail, "没有生成输出文件 game.out") {
		t.Errorf("detail = %q", rep.Detail)
	}
}

// 文件名不合法是题目配置错误，不该先带着路径拼进工作目录再跑。
func TestFileIORejectsUnsafeNames(t *testing.T) {
	tool, _, err := DefaultTools()["cpp"].WithOptions("")
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		SubmissionID: 1, PID: "P1", Language: "cpp", Source: "int main(){return 0;}",
		TimeLimitMS: 1000, MemLimitKB: 262144,
		InputFile: "../game.in", OutputFile: "game.out",
		Cases: []Case{{Name: "1", Ordinal: 1, Input: []byte(""), Expected: []byte("")}},
	}
	rep := Judge(task, tool, filepath.Join(t.TempDir(), "work"), nil, nil)
	if rep.Verdict != IE || !strings.Contains(rep.Detail, "problem IO") {
		t.Fatalf("不安全文件名 = %s (%s), want IE", rep.Verdict, rep.Detail)
	}
}

func judgeWithIO(t *testing.T, tool Tool, src, in, out string, cases []Case, timeMS, memKB int) Report {
	t.Helper()
	requireRunnable(t)
	task := Task{
		SubmissionID: 1, PID: "P1", Language: "cpp", Source: src,
		TimeLimitMS: timeMS, MemLimitKB: memKB, Cases: cases,
		InputFile: in, OutputFile: out,
	}
	return Judge(task, tool, filepath.Join(t.TempDir(), "work"), nil, nil)
}
