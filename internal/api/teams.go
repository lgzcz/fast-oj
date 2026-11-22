package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/liangbinchen2013/fast-oj/internal/store"
)

// 团队是站内长期组织；比赛里的「队伍」是赛内的，两者互不影响。

type teamRowView struct {
	ID, Members, Problems int64
	Name, Intro           string
	Owner, OwnerHandle    string
	Mine                  bool
	MyRole                string
}

type teamListData struct {
	Rows     []teamRowView
	Query    string
	Next     int64
	Limit    int
	SignedIn bool
}

func (s *Server) handleTeamList(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	limit := 30
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	search := strings.TrimSpace(q.Get("q"))
	items, err := s.db.ListGroups(r.Context(), store.GroupFilter{
		ViewerID: a.User.ID, Search: search, After: after, Limit: limit,
	})
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	d := teamListData{Query: search, Limit: limit, SignedIn: a.User.ID != 0}
	for _, t := range items {
		name := t.OwnerHandle
		if t.OwnerName != "" {
			name = t.OwnerName
		}
		d.Rows = append(d.Rows, teamRowView{
			ID: t.ID, Name: t.Name, Intro: t.Intro, Owner: name, OwnerHandle: t.OwnerHandle,
			Members: int64(t.Members), Problems: int64(t.Problems), Mine: t.Mine, MyRole: t.MyRole,
		})
	}
	if len(items) == limit {
		d.Next = items[len(items)-1].ID
	}
	s.page(w, r, http.StatusOK, "团队", "teams", a.viewer(), msgOf(r), d)
}

type teamForm struct {
	Name, Intro, JoinCode string
	Errors                []string
}

func parseTeamForm(r *http.Request) teamForm {
	f := teamForm{
		Name:     strings.TrimSpace(r.FormValue("name")),
		Intro:    r.FormValue("intro"),
		JoinCode: strings.TrimSpace(r.FormValue("join_code")),
	}
	if f.Name == "" {
		f.Errors = append(f.Errors, "团队名不能是空的")
	} else if tooLong(f.Name, store.MaxGroupName) {
		f.Errors = append(f.Errors, "团队名最长 "+strconv.Itoa(store.MaxGroupName)+" 个字")
	}
	if strings.ContainsFunc(f.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		f.Errors = append(f.Errors, "团队名里不能有控制字符")
	}
	if tooLong(f.Intro, store.MaxGroupIntro) {
		f.Errors = append(f.Errors, "简介最长 "+strconv.Itoa(store.MaxGroupIntro)+" 个字")
	}
	if f.JoinCode != "" && !validJoinCode(f.JoinCode) {
		f.Errors = append(f.Errors, "加入码只能是字母、数字和连字符，最长 32 位")
	}
	return f
}

type teamEditData struct {
	IsNew  bool
	ID     int64
	Name   string
	Intro  string
	JoinCode string
	Errors []string
	Action string
}

func (s *Server) handleTeamNew(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/teams/new", http.StatusSeeOther)
		return
	}
	s.page(w, r, http.StatusOK, "新建团队", "team_edit", a.viewer(), msgOf(r), teamEditData{
		IsNew: true, Action: "/teams",
	})
}

func (s *Server) handleTeamCreate(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next=/teams/new", http.StatusSeeOther)
		return
	}
	f := parseTeamForm(r)
	if r.FormValue("newcode") == "1" {
		code, err := newJoinCode()
		if err != nil {
			f.Errors = append(f.Errors, "生成加入码失败："+err.Error())
		} else {
			f.JoinCode = code
		}
	}
	d := teamEditData{IsNew: true, Name: f.Name, Intro: f.Intro, JoinCode: f.JoinCode,
		Errors: f.Errors, Action: "/teams"}
	if len(f.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "新建团队", "team_edit", a.viewer(), "", d)
		return
	}
	t, err := s.db.CreateGroup(r.Context(), a.User.ID, f.Name, f.Intro, f.JoinCode)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			d.Errors = []string{"团队名 " + f.Name + " 已经存在"}
		} else {
			d.Errors = []string{err.Error()}
		}
		s.page(w, r, http.StatusConflict, "新建团队", "team_edit", a.viewer(), "", d)
		return
	}
	s.log.Info("team created", "id", t.ID, "name", t.Name, "by", a.User.Handle)
	http.Redirect(w, r, "/teams/"+strconv.FormatInt(t.ID, 10)+"?msg="+
		url.QueryEscape("团队已建好，去题库加题吧"), http.StatusSeeOther)
}

func (s *Server) teamByID(w http.ResponseWriter, r *http.Request, viewerID int64) (store.Group, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.respondError(w, r, http.StatusNotFound, "团队不存在")
		return store.Group{}, false
	}
	t, err := s.db.GetGroup(r.Context(), id, viewerID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return t, false
	}
	return t, true
}

type teamMemberView struct {
	Handle, Name, Color, Role, RoleLabel, When string
	IsYou, CanRemove                          bool
}

type teamProblemView struct {
	PID, Title, Difficulty string
	Tags                   []string
	Solved, Hidden         bool
	AddedBy                string
}

type teamData struct {
	ID          int64
	Name        string
	Intro       string
	Owner       string
	OwnerHandle string
	When        string
	Members     []teamMemberView
	Problems    []teamProblemView
	IsMember    bool
	IsCaptain   bool
	CanManage   bool
	CanJoin     bool
	SignedIn    bool
	JoinCode    string
	Solved      int
}

func (s *Server) handleTeamGet(w http.ResponseWriter, r *http.Request) {
	a, _ := s.authenticate(r)
	t, ok := s.teamByID(w, r, a.User.ID)
	if !ok {
		return
	}
	members, err := s.db.ListGroupMembers(r.Context(), t.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	owner := t.OwnerHandle
	if t.OwnerName != "" {
		owner = t.OwnerName
	}
	if owner == "" {
		owner = "已注销"
	}
	d := teamData{
		ID: t.ID, Name: t.Name, Intro: t.Intro, Owner: owner, OwnerHandle: t.OwnerHandle,
		When: agoMS(t.CreatedAt), IsMember: t.Mine, IsCaptain: t.MyRole == "captain",
		CanManage: t.MyRole == "captain", SignedIn: a.User.ID != 0,
		CanJoin: a.User.ID != 0 && !t.Mine,
	}
	if d.IsCaptain {
		d.JoinCode = t.JoinCode
	}
	for _, m := range members {
		d.Members = append(d.Members, teamMemberView{
			Handle: m.Handle, Name: m.Name(), Color: m.Color, Role: m.Role,
			RoleLabel: map[string]string{"captain": "队长", "member": "队员"}[m.Role],
			When: agoMS(m.JoinedAt), IsYou: m.UserID == a.User.ID,
			CanRemove: d.IsCaptain && m.Role != "captain",
		})
	}
	if t.Mine {
		items, err := s.db.ListGroupProblems(r.Context(), t.ID, a.User.ID)
		if err != nil {
			s.writeStoreError(w, r, err)
			return
		}
		for _, p := range items {
			if p.Solved {
				d.Solved++
			}
			d.Problems = append(d.Problems, teamProblemView{
				PID: p.PID, Title: p.Title, Difficulty: store.DifficultyLabel(p.Difficulty),
				Tags: p.Tags, Solved: p.Solved, Hidden: !p.Visible, AddedBy: p.AddedBy,
			})
		}
	}
	s.page(w, r, http.StatusOK, "团队 "+t.Name, "team", a.viewer(), msgOf(r), d)
}

// teamForManage resolves the team and requires the captain.
func (s *Server) teamForManage(w http.ResponseWriter, r *http.Request) (actor, store.Group, bool) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return actor{}, store.Group{}, false
	}
	t, ok := s.teamByID(w, r, a.User.ID)
	if !ok {
		return a, t, false
	}
	if t.MyRole != "captain" {
		s.respondError(w, r, http.StatusForbidden, "只有队长能改团队设置")
		return a, t, false
	}
	return a, t, true
}

func (s *Server) handleTeamEdit(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.teamForManage(w, r)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, "编辑团队", "team_edit", a.viewer(), msgOf(r), teamEditData{
		ID: t.ID, Name: t.Name, Intro: t.Intro, JoinCode: t.JoinCode,
		Action: "/teams/" + strconv.FormatInt(t.ID, 10),
	})
}

func (s *Server) handleTeamUpdate(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.teamForManage(w, r)
	if !ok {
		return
	}
	f := parseTeamForm(r)
	if r.FormValue("newcode") == "1" {
		code, err := newJoinCode()
		if err != nil {
			f.Errors = append(f.Errors, "生成加入码失败："+err.Error())
		} else {
			f.JoinCode = code
		}
	}
	d := teamEditData{ID: t.ID, Name: f.Name, Intro: f.Intro, JoinCode: f.JoinCode,
		Errors: f.Errors, Action: "/teams/" + strconv.FormatInt(t.ID, 10)}
	if len(f.Errors) > 0 {
		s.page(w, r, http.StatusBadRequest, "编辑团队", "team_edit", a.viewer(), "", d)
		return
	}
	if err := s.db.UpdateGroup(r.Context(), t.ID, f.Name, f.Intro, f.JoinCode); err != nil {
		if errors.Is(err, store.ErrConflict) {
			d.Errors = []string{"团队名 " + f.Name + " 已经存在"}
			s.page(w, r, http.StatusConflict, "编辑团队", "team_edit", a.viewer(), "", d)
			return
		}
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("team updated", "id", t.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/teams/"+strconv.FormatInt(t.ID, 10)+"?msg="+
		url.QueryEscape("已保存"), http.StatusSeeOther)
}

func (s *Server) handleTeamDelete(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.teamForManage(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteGroup(r.Context(), t.ID); err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("team deleted", "id", t.ID, "by", a.User.Handle)
	s.audit(r, a, "team.delete", strconv.FormatInt(t.ID, 10), "删团队："+t.Name)
	http.Redirect(w, r, "/teams?msg="+url.QueryEscape("团队已删除"), http.StatusSeeOther)
}

func (s *Server) handleTeamJoin(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	t, ok := s.teamByID(w, r, a.User.ID)
	if !ok {
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if _, err := s.db.JoinGroup(r.Context(), t.ID, a.User.ID, code); err != nil {
		switch {
		case errors.Is(err, store.ErrGroupJoinCode):
			s.respondError(w, r, http.StatusForbidden, "加入码不对")
		case errors.Is(err, store.ErrGroupFull):
			s.respondError(w, r, http.StatusConflict, err.Error())
		default:
			s.writeStoreError(w, r, err)
		}
		return
	}
	s.log.Info("team joined", "id", t.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/teams/"+strconv.FormatInt(t.ID, 10)+"?msg="+
		url.QueryEscape("已加入 "+t.Name), http.StatusSeeOther)
}

func (s *Server) handleTeamLeave(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	t, ok := s.teamByID(w, r, a.User.ID)
	if !ok {
		return
	}
	if err := s.db.LeaveGroup(r.Context(), t.ID, a.User.ID); err != nil {
		if errors.Is(err, store.ErrGroupCaptain) {
			s.respondError(w, r, http.StatusForbidden, "队长不能退出；不要这个团队就删掉它")
			return
		}
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("team left", "id", t.ID, "by", a.User.Handle)
	http.Redirect(w, r, "/teams?msg="+url.QueryEscape("已退出 "+t.Name), http.StatusSeeOther)
}

func (s *Server) handleTeamMemberRemove(w http.ResponseWriter, r *http.Request) {
	a, t, ok := s.teamForManage(w, r)
	if !ok {
		return
	}
	u, err := s.db.GetUserByHandle(r.Context(), r.PathValue("handle"))
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if err := s.db.RemoveGroupMember(r.Context(), t.ID, u.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.respondError(w, r, http.StatusNotFound, "这个人不在队里（或他是队长）")
			return
		}
		s.writeStoreError(w, r, err)
		return
	}
	s.log.Info("team member removed", "id", t.ID, "target", u.Handle, "by", a.User.Handle)
	http.Redirect(w, r, "/teams/"+strconv.FormatInt(t.ID, 10)+"?msg="+
		url.QueryEscape("已移出 "+u.Handle), http.StatusSeeOther)
}

func (s *Server) handleTeamProblemAdd(w http.ResponseWriter, r *http.Request) {
	a, err := s.authenticate(r)
	if err != nil || a.User.ID == 0 {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return
	}
	t, ok := s.teamByID(w, r, a.User.ID)
	if !ok {
		return
	}
	if !t.Mine {
		s.respondError(w, r, http.StatusForbidden, "只有队员能往团队题库加题")
		return
	}
	key := strings.TrimSpace(r.FormValue("problem"))
	if key == "" {
		s.backToTeam(w, r, t.ID, "要填题目编号")
		return
	}
	p, err := s.db.FindProblem(r.Context(), key)
	if err != nil {
		s.backToTeam(w, r, t.ID, "找不到题目 "+key)
		return
	}
	if !p.Visible && !a.canEditProblem(p) {
		s.backToTeam(w, r, t.ID, "题目 "+p.PID+" 没公开，只有它的作者或持 "+permProblemEditAny+" 的人能放进团队题库")
		return
	}
	if err := s.db.AddGroupProblem(r.Context(), t.ID, p.ID, a.User.ID); err != nil {
		s.backToTeam(w, r, t.ID, err.Error())
		return
	}
	s.backToTeam(w, r, t.ID, "已加入 "+p.PID+"："+p.Title)
}

func (s *Server) handleTeamProblemRemove(w http.ResponseWriter, r *http.Request) {
	_, t, ok := s.teamForManage(w, r)
	if !ok {
		return
	}
	p, err := s.db.FindProblem(r.Context(), r.PathValue("problemKey"))
	if err != nil {
		s.backToTeam(w, r, t.ID, "找不到题目")
		return
	}
	removed, err := s.db.RemoveGroupProblem(r.Context(), t.ID, p.ID)
	if err != nil {
		s.writeStoreError(w, r, err)
		return
	}
	if !removed {
		s.backToTeam(w, r, t.ID, "这道题不在团队题库里")
		return
	}
	s.backToTeam(w, r, t.ID, "已移出 "+p.PID)
}

func (s *Server) backToTeam(w http.ResponseWriter, r *http.Request, id int64, note string) {
	http.Redirect(w, r, "/teams/"+strconv.FormatInt(id, 10)+"?msg="+url.QueryEscape(note), http.StatusSeeOther)
}
