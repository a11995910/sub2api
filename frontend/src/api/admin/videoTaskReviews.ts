import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface VideoTaskReview {
  id: number
  request_id: string
  upstream_task_id: string
  platform: string
  user_id: number
  user_email: string
  username: string
  api_key_id: number
  api_key_name: string
  group_id?: number
  account_id: number
  model: string
  upstream_model: string
  resolution: string
  duration_seconds: number
  reference_image_count: number
  estimated_cost: number
  actual_cost?: number
  task_status: string
  billing_status: string
  poll_count: number
  last_polled_at?: string
  next_poll_at: string
  last_poll_error: string
  created_at: string
  updated_at: string
}

export interface VideoTaskReviewQuery {
  page?: number
  page_size?: number
  search?: string
  user_id?: number
  api_key_id?: number
  account_id?: number
  platform?: string
  model?: string
  task_status?: string
  billing_status?: string
  start_time?: string
  end_time?: string
}

async function list(params: VideoTaskReviewQuery): Promise<PaginatedResponse<VideoTaskReview>> {
  const { data } = await apiClient.get('/admin/video-task-reviews', { params })
  return data
}

async function recheck(id: number): Promise<{ success: boolean }> {
  const { data } = await apiClient.post(`/admin/video-task-reviews/${id}/recheck`)
  return data
}

async function confirmFailed(id: number, reason: string): Promise<{ success: boolean }> {
  const { data } = await apiClient.post(`/admin/video-task-reviews/${id}/confirm-failed`, { reason })
  return data
}

async function confirmSucceeded(id: number, reason: string): Promise<{ success: boolean }> {
  const { data } = await apiClient.post(`/admin/video-task-reviews/${id}/confirm-succeeded`, { reason })
  return data
}

export const videoTaskReviewsAPI = { list, recheck, confirmFailed, confirmSucceeded }

export default videoTaskReviewsAPI
