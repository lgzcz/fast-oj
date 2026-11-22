-- 0048: 团队与团队题库。比赛里的「队伍」是赛内的，团队是站内长期组织。
CREATE TABLE teams (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL COLLATE NOCASE UNIQUE,
    intro      TEXT    NOT NULL DEFAULT '',
    join_code  TEXT    NOT NULL DEFAULT '',
    owner_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX teams_list ON teams(id DESC);

CREATE TABLE team_members (
    team_id   INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role      TEXT    NOT NULL DEFAULT 'member' CHECK (role IN ('captain','member')),
    joined_at INTEGER NOT NULL,
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user ON team_members(user_id, team_id);

-- 团队题库是一份只给队内看的题单：题目本身仍按自己的可见性走，
-- 未公开的题只有能编辑它的人能加进来。
CREATE TABLE team_problems (
    team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    problem_id INTEGER NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    added_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (team_id, problem_id)
);
CREATE INDEX team_problems_list ON team_problems(team_id, created_at DESC);
