import { afterEach, describe, expect, it, vi } from 'vitest'
import { pelicanAccountsAPI } from '../admin/pelicanAccounts'
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: api }))
afterEach(() => vi.resetAllMocks())
describe('后台账号作品接口', () => {
  it('全部账号一次提交且不创建持续的浏览器流', async () => {
    api.post.mockResolvedValue({ data: [{ account_id: 1, run_id: '任务', status: 'running' }] })
    const results = await pelicanAccountsAPI.start({ all: true, models: { openai: '模型' }, reasoning_effort: 'high' })
    expect(api.post).toHaveBeenCalledWith('/admin/pelican-account-tests/run', { all: true, models: { openai: '模型' }, reasoning_effort: 'high' })
    expect(results[0].status).toBe('running')
  })
  it('刷新时读取服务端结果并传递查询取消信号', async () => {
    const signal = new AbortController().signal
    api.get.mockResolvedValue({ data: [{ account_id: 1, status: 'success' }] })
    expect(await pelicanAccountsAPI.list(signal)).toHaveLength(1)
    expect(api.get).toHaveBeenCalledWith('/admin/pelican-account-tests', { signal })
  })
  it('停止使用账号与运行标识成对匹配', async () => {
    api.post.mockResolvedValue({})
    await pelicanAccountsAPI.stop([{ account_id: 1, run_id: '旧任务' }])
    expect(api.post).toHaveBeenCalledWith('/admin/pelican-account-tests/stop', { runs: [{ account_id: 1, run_id: '旧任务' }] })
  })
})
