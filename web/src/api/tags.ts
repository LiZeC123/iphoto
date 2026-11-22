import { api } from './client'
import type { Tag } from './types'

export const tagsApi = {
  list: () => api.get<{ items: Tag[]; total: number }>('/tags'),
  create: (name: string) => api.post<Tag>('/tags', { name }),
  rename: (id: number, name: string) => api.patch<Tag>(`/tags/${id}`, { name }),
  remove: (id: number) => api.delete<{ deleted: number }>(`/tags/${id}`),
  summary: (hashes: string[]) =>
    api.post<{ items: Tag[]; total: number }>('/tags/summary', { hashes }),
  batch: (hashes: string[], add: string[], remove: string[] = []) =>
    api.post<{ hashes: number }>('/assets/batch/tags', {
      hashes,
      add_tags: add,
      remove_tags: remove,
    }),
}
