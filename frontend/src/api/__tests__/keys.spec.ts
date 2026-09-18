import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, put } = vi.hoisted(() => ({
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    post,
    put,
  },
}))

import { create, update } from '@/api/keys'

describe('keys api', () => {
  beforeEach(() => {
    post.mockReset()
    put.mockReset()
    post.mockResolvedValue({ data: {} })
    put.mockResolvedValue({ data: {} })
  })

  it('sends openai fast mode flag when creating a key', async () => {
    await create('Fast Key', 13, undefined, [], [], 0, undefined, undefined, true)

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'Fast Key',
      group_id: 13,
      openai_fast_mode_enabled: true,
    })
  })

  it('sends openai fast mode flag when updating a key', async () => {
    await update(7, { openai_fast_mode_enabled: false })

    expect(put).toHaveBeenCalledWith('/keys/7', {
      openai_fast_mode_enabled: false,
    })
  })
})
