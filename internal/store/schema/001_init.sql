-- dev.md 第 4 章数据模型。WAL / foreign_keys 等 PRAGMA 由 store.Open 设置。

CREATE TABLE IF NOT EXISTS assets (
  hash              TEXT PRIMARY KEY,
  original_name     TEXT NOT NULL,
  storage_name      TEXT NOT NULL,
  rel_path          TEXT NOT NULL,
  size              INTEGER NOT NULL,
  mime              TEXT NOT NULL,
  width             INTEGER,
  height            INTEGER,
  taken_at          TEXT,
  taken_at_source   TEXT,
  taken_at_override TEXT,
  note              TEXT,
  added_at          TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'ready',
  deleted_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_assets_taken_at ON assets(taken_at);
CREATE INDEX IF NOT EXISTS idx_assets_status ON assets(status);
CREATE INDEX IF NOT EXISTS idx_assets_added_at ON assets(added_at);

CREATE TABLE IF NOT EXISTS tags (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE IF NOT EXISTS asset_tags (
  hash   TEXT NOT NULL REFERENCES assets(hash) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (hash, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_asset_tags_tag ON asset_tags(tag_id);

CREATE TABLE IF NOT EXISTS albums (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL CHECK (type IN ('manual', 'smart')),
  description TEXT,
  rule_json   TEXT,
  cover_hash  TEXT,
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS album_items (
  album_id INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  hash     TEXT NOT NULL REFERENCES assets(hash) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  PRIMARY KEY (album_id, hash)
);
CREATE INDEX IF NOT EXISTS idx_album_items_hash ON album_items(hash);

CREATE TABLE IF NOT EXISTS time_parse_rules (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  name     TEXT NOT NULL,
  pattern  TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  enabled  INTEGER NOT NULL DEFAULT 1,
  builtin  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS import_batches (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  source_path TEXT NOT NULL,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  status      TEXT NOT NULL DEFAULT 'running',
  stats_json  TEXT
);

CREATE TABLE IF NOT EXISTS import_items (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id    INTEGER NOT NULL REFERENCES import_batches(id) ON DELETE CASCADE,
  source_path TEXT NOT NULL,
  hash        TEXT,
  status      TEXT NOT NULL,
  message     TEXT
);
CREATE INDEX IF NOT EXISTS idx_import_items_batch ON import_items(batch_id);

CREATE TABLE IF NOT EXISTS app_settings (
  key   TEXT PRIMARY KEY,
  value TEXT
);

CREATE TABLE IF NOT EXISTS operations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  type       TEXT NOT NULL,
  payload    TEXT,
  created_at TEXT NOT NULL
);

-- 内置时间解析规则（dev.md 4.4 示例）。
INSERT INTO time_parse_rules (name, pattern, priority, enabled, builtin)
SELECT 'IMG_日期时间', 'IMG_(?P<year>\d{4})(?P<month>\d{2})(?P<day>\d{2})_(?P<hour>\d{2})(?P<minute>\d{2})(?P<second>\d{2})', 10, 1, 1
WHERE NOT EXISTS (SELECT 1 FROM time_parse_rules);

INSERT INTO time_parse_rules (name, pattern, priority, enabled, builtin)
SELECT 'Screenshot_日期时间', 'Screenshot_(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})-(?P<hour>\d{2})-(?P<minute>\d{2})-(?P<second>\d{2})', 20, 1, 1
WHERE NOT EXISTS (SELECT 1 FROM time_parse_rules WHERE name = 'Screenshot_日期时间');
