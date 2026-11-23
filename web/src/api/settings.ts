import { api } from './client'
import type { ImportBatch, Settings, VerifyReport } from './types'

export const settingsApi = {
  all: () => api.get<Settings>('/settings'),
  update: (values: Settings) => api.put<{ updated: number }>('/settings', values),
  verify: (full: boolean) => api.post<VerifyReport>(`/settings/verify?full=${full ? 1 : 0}`, {}),
  batches: (limit = 20) => api.get<{ items: ImportBatch[] }>(`/import/batches?limit=${limit}`),
  shutdown: () => api.post<{ status: string }>('/shutdown', { confirm: true }),
}

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

export function basename(path: string): string {
  const parts = path.split(/[\\/]/)
  return parts[parts.length - 1] || path
}
