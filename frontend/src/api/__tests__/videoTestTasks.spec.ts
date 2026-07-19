import { beforeEach, describe, expect, it, vi } from 'vitest'

const apiClientMock = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))

vi.mock('@/api/client', () => ({ apiClient: apiClientMock }))

import {
  deleteVideoTestTask,
  fetchVideoTestTaskContent,
  listVideoTestTasks,
  refreshVideoTestTask,
} from '@/api/videoTestTasks'

describe('videoTestTasks api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('按分页读取当前用户的视频测试任务', async () => {
    const page = { items: [], total: 0, page: 2, page_size: 10 }
    apiClientMock.get.mockResolvedValueOnce({ data: page })

    await expect(listVideoTestTasks(2, 10)).resolves.toEqual(page)
    expect(apiClientMock.get).toHaveBeenCalledWith('/model-test/video-tasks', {
      params: { page: 2, page_size: 10 },
    })
  })

  it('刷新和删除任务使用内部任务 ID', async () => {
    const task = { id: 'local /1', status: 'in_progress' }
    apiClientMock.post.mockResolvedValueOnce({ data: task })
    apiClientMock.delete.mockResolvedValueOnce({})

    await expect(refreshVideoTestTask('local /1')).resolves.toEqual(task)
    await deleteVideoTestTask('local /1')

    expect(apiClientMock.post).toHaveBeenCalledWith('/model-test/video-tasks/local%20%2F1/refresh')
    expect(apiClientMock.delete).toHaveBeenCalledWith('/model-test/video-tasks/local%20%2F1')
  })

  it('通过登录态接口下载已完成视频', async () => {
    const blob = new Blob(['video'], { type: 'video/mp4' })
    apiClientMock.get.mockResolvedValueOnce({ data: blob })

    await expect(fetchVideoTestTaskContent('local-1')).resolves.toBe(blob)
    expect(apiClientMock.get).toHaveBeenCalledWith('/model-test/video-tasks/local-1/content', {
      responseType: 'blob',
    })
  })
})
