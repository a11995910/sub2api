import { apiClient } from '@/api/client'

export type VideoTestTaskStatus = 'queued' | 'in_progress' | 'completed' | 'failed'

export interface VideoTestTask {
  id: string
  api_key_id: number
  group_id: number
  upstream_task_id: string
  platform: string
  model: string
  prompt: string
  resolution?: string
  duration_seconds?: number
  reference_image_count: number
  status: VideoTestTaskStatus
  progress?: number
  response?: unknown
  error_message?: string
  last_poll_error?: string
  last_polled_at?: string
  completed_at?: string
  failed_at?: string
  created_at: string
  updated_at: string
}

export interface VideoTestTaskPage {
  items: VideoTestTask[]
  total: number
  page: number
  page_size: number
}

export async function listVideoTestTasks(page = 1, pageSize = 20): Promise<VideoTestTaskPage> {
  const { data } = await apiClient.get<VideoTestTaskPage>('/model-test/video-tasks', {
    params: { page, page_size: pageSize },
  })
  return data
}

export async function refreshVideoTestTask(id: string): Promise<VideoTestTask> {
  const { data } = await apiClient.post<VideoTestTask>(`/model-test/video-tasks/${encodeURIComponent(id)}/refresh`)
  return data
}

export async function deleteVideoTestTask(id: string): Promise<void> {
  await apiClient.delete(`/model-test/video-tasks/${encodeURIComponent(id)}`)
}

export async function fetchVideoTestTaskContent(id: string): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`/model-test/video-tasks/${encodeURIComponent(id)}/content`, {
    responseType: 'blob',
  })
  return data
}
