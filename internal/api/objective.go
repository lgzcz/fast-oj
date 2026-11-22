package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 客观题：服务端即时评分的单选自测。它不走判机、不进比赛、不计 Rating，
// 也不允许一套题被改到把已有作答对不上——编辑会整组替换题目并保留作答记录，
// 旧的作答就按新题重新对分，这一点写在页面上。

type objectiveRow struct {
	ID          int64
	Title       string
	Owner       string
	OwnerHandle string
	Questions   int
	Visible     bool
	CanEdit     bool
	When        string
}

type objectiveListData struct {
	Rows      []objectiveRow
	SignedIn  bool
	CanCreate bool
}

type objectiveOptionView struct {
	Letter   string
	Text     string
	Selected bool
}

type objectiveQuestionView struct {
	Ordinal int
	Prompt  string
	Options []objectiveOptionView
	Correct bool
	Chosen  string
	Answer  string
}

type objectiveAttemptView struct {
	Score    int
	Total    int
	Attempts int
	When     string
}

type objectiveData struct {
	ID          int64
	Title       string
	Body        string
	Owner       string
	OwnerHandle string
	Visible     bool
	Questions   []objectiveQuestionView
	Attempt     *objectiveAttemptView
	ShowResult  bool
	SignedIn    bool
	CanEdit     bool
	CanDelete   bool
	MaxScore    int
}

func canEditObjective(a actor, o store.ObjectiveSet) bool {
	return a.User.ID != 0 && (a.User.ID == o.OwnerID || a.can(permProblemEditAny))
}

func letterAt(i int) string {
	if i < 0 || i > 25 {
		return "—"
	}
	return string(rune('A' + i))
}

func (s *Server) handleObjectiveList(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	items, err := s.db.ListObjectiveSets(r.Context(), a.User.ID, a.can(permProblemEditAny), 100)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := objectiveListData{SignedIn: a.User.ID != 0, CanCreate: a.can(permProblemCreate)}
	for _, o := range items {
		owner := o.OwnerHandle
		if o.OwnerName != "" {
			owner = o.OwnerName
		}
		d.Rows = append(d.Rows, objectiveRow{
			ID: o.ID, Title: o.Title, Owner: owner, OwnerHandle: o.OwnerHandle,
			Questions: o.Questions, Visible: o.Visible, CanEdit: canEditObjective(a, o),
			When: agoMS(o.CreatedAt),
		})
	}
	s.page(w, r, http.StatusOK, "客观题", "objective_list", a.viewer(), msgOf(r), d)
}

type objectiveFormData struct {
	Title   string
	Body    string
	Raw     string
	Visible bool
	Errors  []string
	Action  string
}

func (s *Server) handleObjectiveNew(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/objective/new", http.StatusSeeOther)
		return
	}
	if !a.can(permProblemCreate) {
		s.respondError(w, r, http.StatusForbidden, "缺少权限："+permProblemCreate)
		return
	}
	s.page(w, r, http.StatusOK, "新建客观题", "objective_edit", a.viewer(), msgOf(r), objectiveFormData{
		Visible: true, Action: "/objective",
	})
}

func (s *Server) parseObjectiveForm(r *http.Request) (objectiveFormData, []store.ObjectiveQuestion) {
	d := objectiveFormData{
		Title: strings.TrimSpace(r.FormValue("title")), Body: r.FormValue("body"),
		Raw: r.FormValue("questions"), Visible: r.FormValue("visible") == "1",
	}
	questions, err := store.ParseObjectiveQuestions(d.Raw)
	if err != nil {
		d.Errors = append(d.Errors, err.Error())
	}
	if d.Title == "" {
		d.Errors = append(d.Errors, "标题不能是空的")
	} else if tooLong(d.Title, store.MaxObjectiveTitle) {
		d.Errors = append(d.Errors, fmt.Sprintf("标题最长 %d 个字符", store.MaxObjectiveTitle))
	}
	if tooLong(d.Body, store.MaxObjectiveBody) {
		d.Errors = append(d.Errors, fmt.Sprintf("说明最长 %d 个字符", store.MaxObjectiveBody))
	}
	return d, questions
}

func (s *Server) handleObjectiveCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/objective/new", http.StatusSeeOther)
		return
	}
	if !a.can(permProblemCreate) {
		s.respondError(w, r, http.StatusForbidden, "缺少权限："+permProblemCreate)
		return
	}
	d, questions := s.parseObjectiveForm(r)
	d.Action = "/objective"
	if len(d.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "新建客观题", "objective_edit", a.viewer(), "", d)
		return
	}
	set, err := s.db.CreateObjectiveSet(r.Context(), a.User.ID, d.Title, d.Body, d.Visible, questions)
	if err != nil {
		d.Errors = []string{err.Error()}
		s.page(w, r, http.StatusBadRequest, "新建客观题", "objective_edit", a.viewer(), "", d)
		return
	}
	s.log.Info("objective set created", "id", set.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/objective/"+strconv.FormatInt(set.ID, 10)+"?msg="+url.QueryEscape("已发布"), http.StatusSeeOther)
}

func (s *Server) objectiveByID(w http.ResponseWriter, r *http.Request) (actor, store.ObjectiveSet, bool) {
	a, _ := s.authenticate(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "客观题不存在")
		return a, store.ObjectiveSet{}, false
	}
	set, err := s.db.GetObjectiveSet(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, r, err)
		return a, set, false
	}
	return a, set, true
}

func (s *Server) handleObjectiveGet(w http.ResponseWriter, r *http.Request) {
	a, set, ok := s.objectiveByID(w, r)
	if !ok {
		return
	}
	editable := canEditObjective(a, set)
	if !set.Visible && !editable {
		s.respondError(w, r, http.StatusNotFound, "客观题不存在")
		return
	}
	questions, err := s.db.ObjectiveQuestions(r.Context(), set.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := objectiveData{
		ID: set.ID, Title: set.Title, Body: set.Body, Owner: set.OwnerHandle,
		OwnerHandle: set.OwnerHandle, Visible: set.Visible, SignedIn: a.User.ID != 0,
		CanEdit: editable, CanDelete: editable, MaxScore: len(questions),
	}
	if set.OwnerName != "" {
		d.Owner = set.OwnerName
	}
	answers := make([]int, 0)
	if a.User.ID != 0 {
		if att, has, err := s.db.ObjectiveAttemptFor(r.Context(), set.ID, a.User.ID); err != nil {
			s.writeStoreError(w, r, err)
			return
		} else if has {
			answers = att.Answers
			view := objectiveAttemptView{Score: att.Score, Total: att.Total, Attempts: att.Attempts, When: agoMS(att.Updated)}
			d.Attempt = &view
		}
	}
	d.Questions = objectiveQuestionViews(questions, answers, false)
	s.page(w, r, http.StatusOK, set.Title, "objective", a.viewer(), msgOf(r), d)
}

func objectiveQuestionViews(questions []store.ObjectiveQuestion, answers []int, result bool) []objectiveQuestionView {
	out := make([]objectiveQuestionView, 0, len(questions))
	for i, q := range questions {
		chosen := -1
		if i < len(answers) {
			chosen = answers[i]
		}
		v := objectiveQuestionView{Ordinal: q.Ordinal, Prompt: q.Prompt}
		for j, text := range q.Options {
			v.Options = append(v.Options, objectiveOptionView{
				Letter: letterAt(j), Text: text, Selected: j == chosen,
			})
		}
		if result {
			v.Correct = chosen == q.Answer
			if chosen >= 0 && chosen < len(q.Options) {
				v.Chosen = letterAt(chosen)
			} else {
				v.Chosen = "未答"
			}
			v.Answer = letterAt(q.Answer)
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) handleObjectiveAnswer(w http.ResponseWriter, r *http.Request) {
	a, set, ok := s.objectiveByID(w, r)
	if !ok {
		return
	}
	if a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/objective/"+strconv.FormatInt(set.ID, 10), http.StatusSeeOther)
		return
	}
	if !set.Visible && !canEditObjective(a, set) {
		s.respondError(w, r, http.StatusNotFound, "客观题不存在")
		return
	}
	questions, err := s.db.ObjectiveQuestions(r.Context(), set.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	answers := make([]int, len(questions))
	for i, q := range questions {
		n, err := strconv.Atoi(r.FormValue("q" + strconv.Itoa(q.Ordinal)))
		if err != nil || n < 0 || n >= len(q.Options) {
			n = -1
		}
		answers[i] = n
	}
	score, total := store.ScoreObjective(questions, answers)
	if err := s.db.SaveObjectiveAttempt(r.Context(), set.ID, a.User.ID, answers, score, total); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	att, _, err := s.db.ObjectiveAttemptFor(r.Context(), set.ID, a.User.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	editable := canEditObjective(a, set)
	d := objectiveData{
		ID: set.ID, Title: set.Title, Body: set.Body, Owner: set.OwnerHandle,
		OwnerHandle: set.OwnerHandle, Visible: set.Visible, SignedIn: true,
		CanEdit: editable, CanDelete: editable, MaxScore: total, ShowResult: true,
		Questions: objectiveQuestionViews(questions, answers, true),
		Attempt:   &objectiveAttemptView{Score: att.Score, Total: att.Total, Attempts: att.Attempts, When: agoMS(att.Updated)},
	}
	if set.OwnerName != "" {
		d.Owner = set.OwnerName
	}
	s.page(w, r, http.StatusOK, set.Title, "objective", a.viewer(), "已交卷，下面是判分结果", d)
}

func (s *Server) handleObjectiveEdit(w http.ResponseWriter, r *http.Request) {
	a, set, ok := s.objectiveByID(w, r)
	if !ok {
		return
	}
	if !canEditObjective(a, set) {
		s.respondError(w, r, http.StatusForbidden, "只有作者或持 "+permProblemEditAny+" 的人能改")
		return
	}
	questions, err := s.db.ObjectiveQuestions(r.Context(), set.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.page(w, r, http.StatusOK, "编辑客观题", "objective_edit", a.viewer(), msgOf(r), objectiveFormData{
		Title: set.Title, Body: set.Body, Raw: store.FormatObjectiveQuestions(questions),
		Visible: set.Visible, Action: "/objective/" + strconv.FormatInt(set.ID, 10),
	})
}

func (s *Server) handleObjectiveUpdate(w http.ResponseWriter, r *http.Request) {
	a, set, ok := s.objectiveByID(w, r)
	if !ok {
		return
	}
	if !canEditObjective(a, set) {
		s.respondError(w, r, http.StatusForbidden, "只有作者或持 "+permProblemEditAny+" 的人能改")
		return
	}
	d, questions := s.parseObjectiveForm(r)
	d.Action = "/objective/" + strconv.FormatInt(set.ID, 10)
	if len(d.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "编辑客观题", "objective_edit", a.viewer(), "", d)
		return
	}
	if err := s.db.UpdateObjectiveSet(r.Context(), set.ID, d.Title, d.Body, d.Visible, questions); err != nil {
		d.Errors = []string{err.Error()}
		s.page(w, r, http.StatusBadRequest, "编辑客观题", "objective_edit", a.viewer(), "", d)
		return
	}
	http.Redirect(w, r, "/objective/"+strconv.FormatInt(set.ID, 10)+"?msg="+url.QueryEscape("已保存；已有作答会按新题重新判分"), http.StatusSeeOther)
}

func (s *Server) handleObjectiveDelete(w http.ResponseWriter, r *http.Request) {
	a, set, ok := s.objectiveByID(w, r)
	if !ok {
		return
	}
	if !canEditObjective(a, set) {
		s.respondError(w, r, http.StatusForbidden, "只有作者或持 "+permProblemEditAny+" 的人能删")
		return
	}
	if err := s.db.DeleteObjectiveSet(r.Context(), set.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("objective set deleted", "id", set.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/objective?msg="+url.QueryEscape("客观题已删除"), http.StatusSeeOther)
}
