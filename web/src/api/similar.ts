import { api } from './client'
import type { Asset } from './types'

export type SimilarStatus = 'idle' | 'running' | 'done' | 'cancelled' | 'failed'

export interface SimilarProgress {
  id: number
  status: SimilarStatus
  total: number
  processed: number
  groups: number
  failures: number
  cached: number
  computed: number
  current?: string
  threshold: number
  algorithm: string
  from?: string
  started_at?: string
  finished_at?: string
  error?: string
}

export interface SimilarGroupImage extends Asset {
  /** 与组内上一张的汉明距离；组内第一张没有该字段。 */
  distance_from_prev?: number
}

export interface SimilarGroup {
  id: number
  images: SimilarGroupImage[]
}

export interface SimilarResult {
  groups: SimilarGroup[]
  group_count: number
  total: number
  scanned: number
  failures: number
  cached: number
  computed: number
  truncated: boolean
  cancelled: boolean
  threshold: number
  algorithm: string
  from?: string
  duration_ms: number
  finished_at: string
}

export const similarApi = {
  detect: (from: string, algorithm: string, threshold: number) =>
    api.post<{ id: number }>('/similar/detect', { from, algorithm, threshold }),
  progress: () => api.get<SimilarProgress>('/similar/progress'),
  result: () => api.get<SimilarResult>('/similar/result'),
  cancel: () => api.post<{ status: string }>('/similar/cancel', {}),
}
