import { api } from './client'
import type { TimeRule } from './types'

export interface TimeRuleInput {
  name: string
  pattern: string
  priority: number
  enabled?: boolean
}

export const timeRulesApi = {
  list: () => api.get<{ items: TimeRule[]; total: number }>('/time-rules'),
  create: (input: TimeRuleInput) => api.post<TimeRule>('/time-rules', input),
  update: (id: number, input: Partial<TimeRuleInput>) =>
    api.patch<TimeRule>(`/time-rules/${id}`, input),
  remove: (id: number) => api.delete<{ deleted: number }>(`/time-rules/${id}`),
  test: (pattern: string, filename: string) =>
    api.post<{ matched: boolean; taken_at: string }>('/time-rules/test', { pattern, filename }),
}
