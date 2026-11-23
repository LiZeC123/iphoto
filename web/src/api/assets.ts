import { api } from './client'
import type { Asset, AssetDetail, AssetPage, MonthCount } from './types'

export interface AssetQuery {
  q?: string
  tag?: string
  untagged?: string
  status?: string
  from?: string
  to?: string
  sort?: string
  page?: number
  page_size?: number
}

function queryString(params: object): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value))
  }
  const s = search.toString()
  return s ? `?${s}` : ''
}

export const assetsApi = {
  list: (query: AssetQuery = {}) => api.get<AssetPage>(`/assets${queryString(query)}`),
  get: (hash: string) => api.get<AssetDetail>(`/assets/${hash}`),
  patch: (hash: string, body: { note?: string; taken_at_override?: string; original_name?: string }) =>
    api.patch<Asset>(`/assets/${hash}`, body),
  rename: (hash: string, name: string) => api.post<Asset>(`/assets/${hash}/rename`, { name }),
  reveal: (hash: string) => api.post<{ path: string }>(`/assets/${hash}/reveal`, {}),
  remove: (hash: string) => api.delete<{ hash: string; status: string }>(`/assets/${hash}`),
  batchExport: (hashes: string[], subdir: string) =>
    api.post<{
      exported: number
      target_dir: string
      failures: { hash: string; error: string }[]
    }>('/assets/batch/export', { hashes, subdir }),
  batchDelete: (hashes: string[]) =>
    api.post<{ results: { hash: string; ok: boolean; error?: string }[] }>('/assets/batch/delete', {
      hashes,
    }),
  timeline: () => api.get<{ months: MonthCount[] }>('/timeline/months'),
  thumbUrl: (hash: string) => `/api/assets/${hash}/thumb`,
  fileUrl: (hash: string) => `/api/assets/${hash}/file`,
}
