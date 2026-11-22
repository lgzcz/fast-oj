package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 题单是学习路径，不是比赛：没有报名、没有计时，也不进榜单。
// 公开题单谁都能看；隐藏题单只有作者和持 problem.edit.any 的人看得到。

type trainingRow struct {
	ID          int64
	Title       string
	Intro       string
	Owner       string
	OwnerHandle string
	When        string
	Problems    int
	Visible     bool
	Mine        bool
}

type trainingProblemRow struct {
	Position int
	PID      string
	Title    string
	Difficulty string
	Tags     []string
	Solved   bool
	Hidden   bool
}

type trainingListData struct {
	Rows     []trainingRow
	Query    string
	Next     int64
	Limit    int
	SignedIn bool
}

func trainingRows(items []store.Training, a actor) []trainingRow {
	out := make([]trainingRow, 0, len(items))
	for _, t := range items {
		name := t.OwnerHandle
		if t.OwnerName != "" {
			name = t.OwnerName
		}
		if name == "" {
			name = "已注销"
		}
		out = append(out, trainingRow{
			ID: t.ID, Title: t.Title, Intro: t.Intro, Owner: name, OwnerHandle: t.OwnerHandle,
			When: agoMS(t.CreatedAt), Problems: t.Problems, Visible: t.Visible,
			Mine: t.OwnerID != 0 && t.OwnerID == a.User.ID,
		})
	}
	return out
}

func (s *Server) handleTrainingList(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	limit := 30
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	search := strings.TrimSpace(q.Get("q"))
	items, err := s.db.ListTrainings(r.Context(), store.TrainingFilter{
		ViewerID: a.User.ID, All: a.can(permProblemEditAny),
		Search: search, After: after, Limit: limit,
	})
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := trainingListData{Rows: trainingRows(items, a), Query: search, Limit: limit, SignedIn: a.User.ID != 0}
	if len(items) == limit {
		d.Next = items[len(items)-1].ID
	}
	s.page(w, r, http.StatusOK, "题单", "trainings", a.viewer(), msgOf(r), d)
}

type trainingForm struct {
	Title   string
	Intro   string
	Visible bool
	Errors  []string
}

func parseTrainingForm(r *http.Request) trainingForm {
	f := trainingForm{
		Title:   strings.TrimSpace(r.FormValue("title")),
		Intro:   r.FormValue("intro"),
		Visible: r.FormValue("visible") == "1",
	}
	if f.Title == "" {
		f.Errors = append(f.Errors, "标题不能是空的")
	} else if tooLong(f.Title, store.MaxTrainingTitle) {
		f.Errors = append(f.Errors, "标题最长 "+strconv.Itoa(store.MaxTrainingTitle)+" 个字")
	}
	if tooLong(f.Intro, store.MaxTrainingIntro) {
		f.Errors = append(f.Errors, "说明最长 "+strconv.Itoa(store.MaxTrainingIntro)+" 个字")
	}
	return f
}

type trainingEditData struct {
	IsNew   bool
	ID      int64
	Title   string
	Intro   string
	Visible bool
	Errors  []string
	Action  string
}

func (s *Server) handleTrainingNew(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/trainings/new", http.StatusSeeOther)
		return
	}
	s.page(w, r, http.StatusOK, "新建题单", "training_edit", a.viewer(), msgOf(r), trainingEditData{
		IsNew: true, Visible: true, Action: "/trainings",
	})
}

func (s *Server) handleTrainingCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/trainings/new", http.StatusSeeOther)
		return
	}
	f := parseTrainingForm(r)
	if len(f.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "新建题单", "training_edit", a.viewer(), "", trainingEditData{
			IsNew: true, Title: f.Title, Intro: f.Intro, Visible: f.Visible, Errors: f.Errors,
			Action: "/trainings",
		})
		return
	}
	t, err := s.db.CreateTraining(r.Context(), a.User.ID, f.Title, f.Intro, f.Visible)
	if err != nil {
		s.page(w, r, http.StatusBadRequest, "新建题单", "training_edit", a.viewer(), "", trainingEditData{
			IsNew: true, Title: f.Title, Intro: f.Intro, Visible: f.Visible,
			Errors: []string{err.Error()}, Action: "/trainings",
		})
		return
	}
	s.log.Info("training created", "id", t.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/trainings/"+strconv.FormatInt(t.ID, 10)+"?msg="+
		url.QueryEscape("题单已建好，现在往里加题"), http.StatusSeeOther)
}

// trainingForEdit resolves {id} and applies the management right: the author
// keeps editing their own list, and problem.edit.any can fix anyone's.
func (s *Server) trainingForEdit(w http.ResponseWriter, r *http.Request) (actor, store.Training, bool) {
	t, err := s.trainingByID(w, r)
	if err != nil {
		return actor{}, t, false
	}
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return actor{}, t, false
	}
	if !a.can(permProblemEditAny) && (t.OwnerID == 0 || t.OwnerID != a.User.ID) {
		s.respondError(w, r, http.StatusForbidden,
			"这个题单由他人创建，需要 "+permProblemEditAny+" 才能修改")
		return a, t, false
	}
	return a, t, true
}

func (s *Server) trainingByID(w http.ResponseWriter, r *http.Request) (store.Training, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "题单不存在")
		return store.Training{}, store.ErrNotFound
	}
	t, err := s.db.GetTraining(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return t, err
	}
	return t, nil
}

type trainingData struct {
	ID          int64
	Title       string
	Intro       string
	Owner       string
	OwnerHandle string
	When        string
	Visible     bool
	CanManage   bool
	SignedIn    bool
	Problems    []trainingProblemRow
	Solved      int
	Progress    int
}

func (s *Server) handleTrainingGet(w http.ResponseWriter, r *http.Request) {
	t, err := s.trainingByID(w, r)
	if err != nil {
		return
	}
	a, _ := s.authenticate(r)
	canManage := a.can(permProblemEditAny) || (t.OwnerID != 0 && t.OwnerID == a.User.ID)
	if !t.Visible && !canManage {
		s.respondError(w, r, http.StatusNotFound, "题单不存在")
		return
	}
	items, err := s.db.TrainingProblems(r.Context(), t.ID, a.User.ID, canManage || a.can(permProblemEditAny))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := trainingData{
		ID: t.ID, Title: t.Title, Intro: t.Intro, When: agoMS(t.CreatedAt),
		Visible: t.Visible, CanManage: canManage, SignedIn: a.User.ID != 0,
	}
	if t.OwnerHandle != "" {
		d.Owner, d.OwnerHandle = t.OwnerHandle, t.OwnerHandle
		if t.OwnerName != "" {
			d.Owner = t.OwnerName
		}
	} else {
		d.Owner = "已注销"
	}
	for _, p := range items {
		if p.Solved {
			d.Solved++
		}
		d.Problems = append(d.Problems, trainingProblemRow{
			Position: p.Position, PID: p.PID, Title: p.Title,
			Difficulty: store.DifficultyLabel(p.Difficulty), Tags: p.Tags,
			Solved: p.Solved, Hidden: !p.Visible,
		})
	}
	if len(items) > 0 {
		d.Progress = d.Solved * 100 / len(items)
	}
	s.page(w, r, http.StatusOK, "题单 "+t.Title, "training", a.viewer(), msgOf(r), d)
}

func (s *Server) handleTrainingEdit(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.trainingForEdit(w, r)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, "编辑题单", "training_edit", a.viewer(), msgOf(r), trainingEditData{
		ID: t.ID, Title: t.Title, Intro: t.Intro, Visible: t.Visible,
		Action: "/trainings/" + strconv.FormatInt(t.ID, 10),
	})
}

func (s *Server) handleTrainingUpdate(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.trainingForEdit(w, r)
	if !ok {
		return
	}
	f := parseTrainingForm(r)
	d := trainingEditData{
		ID: t.ID, Title: f.Title, Intro: f.Intro, Visible: f.Visible, Errors: f.Errors,
		Action: "/trainings/" + strconv.FormatInt(t.ID, 10),
	}
	if len(f.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "编辑题单", "training_edit", a.viewer(), "", d)
		return
	}
	if err := s.db.UpdateTraining(r.Context(), t.ID, f.Title, f.Intro, f.Visible); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("training updated", "id", t.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/trainings/"+strconv.FormatInt(t.ID, 10)+"?msg="+
		url.QueryEscape("已保存"), http.StatusSeeOther)
}

func (s *Server) handleTrainingDelete(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.trainingForEdit(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteTraining(r.Context(), t.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("training deleted", "id", t.ID, "by", a.User.Handle)
	s.audit(r, a, "training.delete", strconv.FormatInt(t.ID, 10), "删题单："+t.Title)
	http.Redirect(w, r, "/trainings?msg="+url.QueryEscape("题单已删除"), http.StatusSeeOther)
}

func (s *Server) backToTraining(w http.ResponseWriter, r *http.Request, t store.Training, note string) {
	http.Redirect(w, r, "/trainings/"+strconv.FormatInt(t.ID, 10)+"?msg="+url.QueryEscape(note), http.StatusSeeOther)
}

func (s *Server) handleTrainingProblemAdd(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.trainingForEdit(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.FormValue("problem"))
	if key == "" {
		s.backToTraining(w, r, t, "要填题目编号")
		return
	}
	p, err := s.db.FindProblem(r.Context(), key)
	if err != nil {
		s.backToTraining(w, r, t, "找不到题目 "+key)
		return
	}
	if !p.Visible && !a.canEditProblem(p) {
		s.backToTraining(w, r, t, "题目 "+p.PID+" 没公开，只有它的作者或持 "+permProblemEditAny+" 的人能放进题单")
		return
	}
	if err := s.db.AddTrainingProblem(r.Context(), t.ID, p.ID); err != nil {
		s.backToTraining(w, r, t, err.Error())
		return
	}
	s.backToTraining(w, r, t, "已加入 "+p.PID+"："+p.Title)
}

func (s *Server) handleTrainingProblemRemove(w http.ResponseWriter, r *http.Request) {
	_, t, ok := s.trainingForEdit(w, r)
	if !ok {
		return
	}
	p, err := s.db.FindProblem(r.Context(), r.PathValue("problemKey"))
	if err != nil {
		s.backToTraining(w, r, t, "找不到题目")
		return
	}
	removed, err := s.db.RemoveTrainingProblem(r.Context(), t.ID, p.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !removed {
		s.backToTraining(w, r, t, "这道题不在题单里")
		return
	}
	s.backToTraining(w, r, t, "已移出 "+p.PID)
}
