import { api } from './client'
import type { PhoneDevice, PhoneProbe } from './types'

export type PhoneSyncMode = 'incremental' | 'full'

export const phoneApi = {
  devices: () => api.get<{ items: PhoneDevice[]; total: number }>('/phone/devices'),
  probe: (address: string, token: string) =>
    api.post<PhoneProbe>('/phone/probe', { address, token }),
  sync: (address: string, token: string, mode: PhoneSyncMode, tags: string[] = []) =>
    api.post<{ batch_id: number }>('/phone/sync', { address, token, mode, tags }),
  forget: (id: number) => api.delete<{ deleted: boolean }>(`/phone/devices/${id}`),
}
