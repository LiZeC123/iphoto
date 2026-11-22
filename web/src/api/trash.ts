import { api } from './client'
import type { AssetPage } from './types'

export const trashApi = {
  list: (page = 1, pageSize = 100) =>
    api.get<AssetPage>(`/trash?page=${page}&page_size=${pageSize}`),
  restore: (hashes: string[]) =>
    api.post<{ restored: number; results: { hash: string; ok: boolean; error?: string }[] }>(
      '/trash/restore',
      { hashes },
    ),
  empty: () => api.post<{ deleted_records: number; deleted_files: number }>('/trash/empty', {}),
}
