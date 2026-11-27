-- 手机导入（PhoneDock）：设备登记与同步账本。
-- 设备身份用「厂商+型号」，地址单独存（手机 IP 变化不影响增量续接）。

CREATE TABLE IF NOT EXISTS phone_devices (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  device_key      TEXT NOT NULL UNIQUE,   -- 小写的 manufacturer|model
  name            TEXT NOT NULL,          -- 展示名，例如 "Xiaomi 22101317C"
  model           TEXT NOT NULL,
  manufacturer    TEXT NOT NULL,
  android_version TEXT NOT NULL,
  last_addr       TEXT NOT NULL,          -- 规范化后的 http://host:port
  token           TEXT NOT NULL,          -- 手机端 4 位 Token（局域网单用户，明文保存）
  created_at      TEXT NOT NULL,
  last_seen_at    TEXT NOT NULL,
  last_sync_at    TEXT,
  last_checkpoint INTEGER NOT NULL DEFAULT 0,
  last_status     TEXT,
  last_batch_id   INTEGER
);

-- 同步账本：每台设备上每个媒体一条。
-- 作用：取消/重扫时免于重复下载；失败项可重试；全量重扫只比 generation，不重传。
CREATE TABLE IF NOT EXISTS phone_media (
  device_id           INTEGER NOT NULL REFERENCES phone_devices(id) ON DELETE CASCADE,
  kind                TEXT NOT NULL CHECK (kind IN ('image', 'video')),
  media_id            INTEGER NOT NULL,
  display_name        TEXT NOT NULL,
  generation_modified INTEGER NOT NULL,
  size                INTEGER NOT NULL,
  date_added          INTEGER NOT NULL,
  width               INTEGER,
  height              INTEGER,
  hash                TEXT,
  status              TEXT NOT NULL,      -- synced | failed | unsupported | missing
  message             TEXT,
  updated_at          TEXT NOT NULL,
  PRIMARY KEY (device_id, kind, media_id)
);

CREATE INDEX IF NOT EXISTS idx_phone_media_status ON phone_media(device_id, status);

-- 导入批次区分来源：旧的本地导入批次默认 local，历史数据无需回填。
ALTER TABLE import_batches ADD COLUMN source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE import_batches ADD COLUMN device_id INTEGER;
