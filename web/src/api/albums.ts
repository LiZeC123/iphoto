import { api } from './client'
import type { Album, AssetPage, MonthCount } from './types'

export interface AlbumInput {
  name: string
  type: 'manual' | 'smart'
  description?: string
  rule_json?: unknown
}

export const albumsApi = {
  list: (q = '', type = '') => {
    const search = new URLSearchParams()
    if (q.trim()) search.set('q', q.trim())
    if (type) search.set('type', type)
    const qs = search.toString()
    return api.get<{ items: Album[]; total: number }>(`/albums${qs ? `?${qs}` : ''}`)
  },
  create: (input: AlbumInput) => api.post<Album>('/albums', input),
  get: (id: number) => api.get<Album>(`/albums/${id}`),
  update: (id: number, input: { name?: string; description?: string; rule_json?: unknown }) =>
    api.patch<Album>(`/albums/${id}`, input),
  remove: (id: number) => api.delete<{ deleted: number }>(`/albums/${id}`),
  items: (id: number, page = 1, pageSize = 200, from = '', to = '') => {
    const search = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (from) search.set('from', from)
    if (to) search.set('to', to)
    return api.get<AssetPage>(`/albums/${id}/items?${search.toString()}`)
  },
  months: (id: number) => api.get<{ months: MonthCount[] }>(`/albums/${id}/months`),
  addItems: (id: number, hashes: string[]) =>
    api.post<{ added: number }>(`/albums/${id}/items`, { hashes }),
  removeItems: (id: number, hashes: string[]) =>
    api.deleteBody<{ removed: number }>(`/albums/${id}/items`, { hashes }),
}
