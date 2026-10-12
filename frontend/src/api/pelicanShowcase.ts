
import { apiClient } from './client'

export interface PelicanShowcaseItem {
  id: number
  group_id: number
  model_id: string
  reasoning_effort: string
  latency_ms: number
  generated_at: string
  response_text?: string
}

export interface PelicanShowcaseGroup {
  id: number
  name: string
  platform: string
  items: PelicanShowcaseItem[]
}

export interface PelicanShowcaseView {
  enabled: boolean
  api_enabled?: boolean
  max_items: number
  retention_days: number
  groups: PelicanShowcaseGroup[]
}

export async function getShowcase(options?: { signal?: AbortSignal }): Promise<PelicanShowcaseView> {
  const { data } = await apiClient.get<PelicanShowcaseView>('/pelican-showcase', { signal: options?.signal })
  return data
}

export async function getShowcaseItem(id: number): Promise<PelicanShowcaseItem> {
  const { data } = await apiClient.get<PelicanShowcaseItem>(`/pelican-showcase/items/${id}`)
  return data
}

export async function removeShowcaseItem(id: number): Promise<void> {
  await apiClient.delete(`/admin/pelican-showcase/items/${id}`)
}

export const pelicanShowcaseAPI = { getShowcase, getShowcaseItem, removeShowcaseItem }

export default pelicanShowcaseAPI
