import { api } from './client'

export interface Health {
  status: string
  version: string
  schema_version: number
  time: string
  library: string
  runtime: string
  port: number
  ffmpeg: boolean
}

export const fetchHealth = () => api.get<Health>('/health')
