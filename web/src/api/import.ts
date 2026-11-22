import { api } from './client'
import type { DuplicateItem, ImportProgress, ImportScan } from './types'

export interface ResolveResult {
  updated: number
  replaced: number
  skipped: number
}

export const importApi = {
  scan: (path: string) => api.post<ImportScan>('/import/scan', { path }),
  start: (path: string, tags: string[] = []) =>
    api.post<{ batch_id: number }>('/import/start', { path, tags }),
  active: () => api.get<{ batch_id: number }>('/import/active'),
  progress: (batchId: number) => api.get<ImportProgress>(`/import/${batchId}/progress`),
  cancel: (batchId: number) =>
    api.post<{ batch_id: number; status: string }>(`/import/${batchId}/cancel`, {}),
  duplicates: (batchId: number) =>
    api.get<{ items: DuplicateItem[] }>(`/import/${batchId}/duplicates`),
  resolve: (batchId: number, items: { id: number; action: 'skip' | 'replace' }[]) =>
    api.post<ResolveResult>(`/import/${batchId}/duplicates/resolve`, { items }),
  cleanup: (batchId: number) => api.post<{ deleted: number }>(`/import/${batchId}/cleanup`, {}),
  reportUrl: (batchId: number) => `/api/import/${batchId}/report.csv`,
}
