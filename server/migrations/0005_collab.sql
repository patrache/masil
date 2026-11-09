-- v0.1.3 room documents (data-model.md 3.13, v0.1.3 design C1.4, api.md 5.24). Metadata only: a document's content
-- (op log, snapshot, version bodies, lock / delete flags) lives in its CollabHub Durable Object's SQLite.
CREATE TABLE collab_docs (
  id          TEXT PRIMARY KEY,
  space_id    TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  type        TEXT NOT NULL CHECK (type IN ('arch','apicol')),
  title       TEXT NOT NULL,                              -- 1..80
  created_by  TEXT NOT NULL REFERENCES users(id),
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,                           -- last edit reflected by the DO (batched, <= 30s late)
  head_seq    INTEGER NOT NULL DEFAULT 0,                 -- mirrored from the CollabHub DO
  locked      INTEGER NOT NULL DEFAULT 0 CHECK (locked IN (0,1)),
  deleted_at  INTEGER
);
CREATE INDEX idx_collab_docs_space ON collab_docs(space_id, type, updated_at) WHERE deleted_at IS NULL;

CREATE TABLE collab_versions (                            -- metadata; the snapshot body lives in the DO
  doc_id        TEXT NOT NULL REFERENCES collab_docs(id) ON DELETE CASCADE,
  no            INTEGER NOT NULL,                         -- 1.. per doc
  seq           INTEGER NOT NULL,
  name          TEXT,                                     -- 1..60 for named versions; NULL for auto / restore versions
  auto          INTEGER NOT NULL DEFAULT 0 CHECK (auto IN (0,1)),
  restored_from INTEGER,                                  -- version no a restore copied
  created_by    TEXT REFERENCES users(id),                -- NULL for auto versions
  created_at    INTEGER NOT NULL,
  PRIMARY KEY (doc_id, no)
);

CREATE TABLE collab_comments (
  id          TEXT PRIMARY KEY,
  doc_id      TEXT NOT NULL REFERENCES collab_docs(id) ON DELETE CASCADE,
  anchor      TEXT NOT NULL,                              -- 1..128
  author_id   TEXT NOT NULL REFERENCES users(id),
  body        TEXT NOT NULL,                              -- 1..2000; '' when deleted
  root_id     TEXT REFERENCES collab_comments(id),
  resolved_at INTEGER,
  created_at  INTEGER NOT NULL,
  deleted     INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0,1))
);
CREATE INDEX idx_collab_comments_doc ON collab_comments(doc_id, created_at);
