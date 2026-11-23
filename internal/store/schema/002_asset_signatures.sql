-- 相似图片检测的指纹缓存（dev.md 第 19 章）。
-- 每个 (asset hash, algorithm) 只存一条，重复检测时直接复用，避免重复解码图片。

CREATE TABLE IF NOT EXISTS asset_signatures (
  hash        TEXT NOT NULL REFERENCES assets(hash) ON DELETE CASCADE,
  algorithm   TEXT NOT NULL,
  value       TEXT NOT NULL,
  computed_at TEXT NOT NULL,
  PRIMARY KEY (hash, algorithm)
);

CREATE INDEX IF NOT EXISTS idx_asset_signatures_algorithm ON asset_signatures(algorithm);

-- 检测按「有效拍摄时间」排序与筛选，表达式索引避免 10k 规模下的全表排序。
CREATE INDEX IF NOT EXISTS idx_assets_effective_time
  ON assets(COALESCE(taken_at_override, taken_at));
