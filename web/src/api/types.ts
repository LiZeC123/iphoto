export interface Asset {
  hash: string
  original_name: string
  storage_name: string
  rel_path: string
  size: number
  mime: string
  width: number | null
  height: number | null
  taken_at: string | null
  taken_at_source: string | null
  taken_at_override: string | null
  note: string | null
  added_at: string
  status: string
  deleted_at: string | null
}

export interface AssetDetail extends Asset {
  tags: string[]
}

export interface AssetPage {
  items: Asset[]
  total: number
  page: number
  page_size: number
}

export interface ImportScan {
  path: string
  total: number
  unreadable: number
  unsupported: UnsupportedInfo
}

/** 扩展名（含点；无后缀为 "(无后缀)"）与对应文件数。 */
export interface ExtCount {
  ext: string
  count: number
}

/** 扫描/导入时因类型不支持而被跳过的文件，最多列出前 100 个。 */
export interface UnsupportedInfo {
  total: number
  by_ext: ExtCount[]
  files: string[]
  truncated: boolean
}

export interface ImportProgress {
  batch_id: number
  status: 'running' | 'done' | 'cancelled' | 'failed' | 'interrupted'
  total: number
  processed: number
  imported: number
  duplicate_skipped: number
  duplicate_pending: number
  failed: number
  unsupported: UnsupportedInfo
  current?: string
  started_at: string
  finished_at?: string
  /** local（本地目录）或 phone（手机同步）。 */
  source?: 'local' | 'phone'
  device_id?: number
  device_name?: string
  /** 手机同步阶段：listing / downloading。 */
  phase?: string
  bytes_total?: number
  bytes_done?: number
  unchanged?: number
  /** 本轮手机媒体库水位（GENERATION_MODIFIED）。 */
  checkpoint?: number
}

export interface DuplicateItem {
  id: number
  source_path: string
  hash: string
  existing_name: string
  existing_size: number
  existing_taken_at: string | null
  incoming_size: number
}

export type Settings = Record<string, string>

export interface MonthCount {
  month: string
  count: number
}

export interface Tag {
  id: number
  name: string
  count: number
}

export interface Album {
  id: number
  name: string
  type: 'manual' | 'smart'
  description: string | null
  rule_json: string | null
  cover_hash: string | null
  created_at: string
  count: number
}

export interface TimeRule {
  id: number
  name: string
  pattern: string
  priority: number
  enabled: boolean
  builtin: boolean
}

export interface ImportBatch {
  id: number
  source_path: string
  started_at: string
  finished_at: string | null
  status: string
  item_count: number
  source?: 'local' | 'phone'
  device_id?: number
}

/** 已登记的手机设备（不含 Token）。 */
export interface PhoneDevice {
  id: number
  name: string
  model: string
  manufacturer: string
  android_version: string
  address: string
  has_token: boolean
  created_at: string
  last_seen_at: string
  last_sync_at: string | null
  last_checkpoint: number
  last_status: string | null
  last_batch_id: number | null
  /** 同步账本按状态统计：synced / failed / unsupported / missing。 */
  counts: Record<string, number>
}

export interface PhoneKindStat {
  total: number
  pending: number
}

/** 检查连接的结果。 */
export interface PhoneProbe {
  address: string
  device: {
    name: string
    model: string
    manufacturer: string
    android_version: string
    battery_level: number
    charging: boolean
    ip: string
    port: number
  }
  known: boolean
  device_id?: number
  /** 设备已登记但地址与上次不同（可能换了那台手机）。 */
  address_changed: boolean
  last_sync_at: string | null
  last_checkpoint: number
  version: number
  images: PhoneKindStat
  videos: PhoneKindStat
}

export interface VerifyIssue {
  hash: string
  original_name: string
  rel_path: string
  size: number
  problem?: string
}

export interface VerifyReport {
  checked: number
  missing: VerifyIssue[]
  size_mismatch: VerifyIssue[]
  hash_mismatch: VerifyIssue[]
  orphans: string[]
  marked_missing: number
  hashed: number
  duration_ms: number
}
