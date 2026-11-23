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
