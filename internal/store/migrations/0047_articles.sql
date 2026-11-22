-- 0047: 题解与专栏。同一张表：题解挂在题上，专栏不挂题。
-- 草稿（visible=0）只有作者与审核者看得到。
CREATE TABLE articles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT    NOT NULL CHECK (kind IN ('solution','column')),
    problem_id INTEGER REFERENCES problems(id) ON DELETE CASCADE,
    author_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    visible    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (kind <> 'solution' OR problem_id IS NOT NULL)
);
CREATE INDEX articles_solution ON articles(problem_id, id DESC);
CREATE INDEX articles_column ON articles(kind, visible, id DESC);
