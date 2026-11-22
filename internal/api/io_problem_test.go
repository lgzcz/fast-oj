package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// 文件 IO 是题目属性：表单存下两个文件名，题面与样例按文件名展示，判机照它落盘。
func TestProblemFileIOFormAndPage(t *testing.T) {
	h := newHarness(t)
	values := problemValues("P7100", "文件 IO 题")
	values.Set("io_input", "game.in")
	values.Set("io_output", "game.out")
	if code, body := h.createProblem(values); code != http.StatusOK {
		t.Fatalf("建文件 IO 题 = %d：\n%s", code, short(body))
	}
	h.uploadCases("P7100")

	_, page := h.htmlGet("/problems/P7100", "")
	for _, want := range []string{"文件 IO", "game.in", "game.out", "输入文件 <code>game.in</code>", "输出文件 <code>game.out</code>"} {
		if !strings.Contains(page, want) {
			t.Errorf("题面缺少 %q", want)
		}
	}

	_, edit := h.get("/problems/P7100/edit", h.adminToken())
	if !strings.Contains(edit, `name="io_input"`) || !strings.Contains(edit, `value="game.in"`) ||
		!strings.Contains(edit, `value="game.out"`) {
		t.Errorf("编辑页没把两个文件名带回来：\n%s", short(edit))
	}

	// 只填一半是出题人的笔误，必须当面拒绝并保留已填的内容。
	half := problemValues("P7101", "半个文件 IO")
	half.Set("io_input", "only.in")
	code, body := h.postBearer("/problems", half, h.adminToken())
	if code != http.StatusBadRequest || !strings.Contains(body, "成对填写") {
		t.Fatalf("只填输入文件名 = %d：\n%s", code, short(body))
	}
	if !strings.Contains(body, `value="only.in"`) {
		t.Error("拒绝后没有保留已填的输入文件名")
	}
}

// 老判机不认识文件 IO：派过去它会按 stdin/stdout 判，交回一条看起来正常的假结果。
func TestFileIOProblemWaitsForCapableJudge(t *testing.T) {
	h := newHarness(t)
	student := h.signIn("iostu2200")
	values := problemValues("P2200", "文件 IO 判题")
	values.Set("io_input", "game.in")
	values.Set("io_output", "game.out")
	if code, body := h.createProblem(values); code != http.StatusOK {
		t.Fatalf("建题 = %d：\n%s", code, short(body))
	}
	h.uploadCases("P2200")
	if code, body := h.submitCode(student, "P2200", "cpp", "int main(){}"); code != http.StatusOK {
		t.Fatalf("提交 = %d：\n%s", code, short(body))
	}

	legacy := []string{"options", "checker", "interactor"}
	if code, body := h.json(http.MethodPost, "/judge/claim", h.judge,
		claimBody("box-no-fileio", legacy)); code != http.StatusNotFound {
		t.Fatalf("老判机领到了文件 IO 工单 = %d：\n%s", code, short(body))
	}
	task := h.claimTask(t, "box-fileio")
	if task.PID != "P2200" {
		t.Fatalf("会文件 IO 的判机应当拿到工单，得到 %s", task.PID)
	}
	if task.InputFile != "game.in" || task.OutputFile != "game.out" {
		t.Errorf("工单没带文件名：%+v", task)
	}
}

// 交互器与文件 IO 是两种互斥的对话方式，保存时就该说清楚。
func TestInteractorRejectedOnFileIOProblem(t *testing.T) {
	h := newHarness(t)
	keeper := h.author("ioauthor2300", permTestcaseManage, permJudgeProgram)
	values := problemValues("P2300", "文件 IO")
	values.Set("io_input", "game.in")
	values.Set("io_output", "game.out")
	if code, body := h.createProblem(values); code != http.StatusOK {
		t.Fatalf("建题 = %d：\n%s", code, short(body))
	}
	code, body := h.postAs(keeper, "/problems/P2300/judge_program", url.Values{
		"kind": {"interactor"}, "language": {"cpp"}, "source": {"int main(){return 0;}"},
	})
	if code != http.StatusOK || !strings.Contains(body, "文件 IO") {
		t.Fatalf("文件 IO 题配交互器 = %d：\n%s", code, short(body))
	}
	_, page := h.get("/problems/P2300/cases", h.adminToken())
	if strings.Contains(page, "int main(){return 0;}") {
		t.Error("被拒绝的交互器源码还是保存下去了")
	}
}
