import { afterEach, describe, expect, it, vi } from 'vitest'
import { testPelicanAccount } from '../admin/pelicanAccounts'
vi.mock('../client', () => ({ buildApiUrl: (path: string) => `/api/v1${path}` }))
function stream(chunks: string[]) {
  const bytes = new TextEncoder()
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: new ReadableStream({
    start(controller) { for (const chunk of chunks) controller.enqueue(bytes.encode(chunk)); controller.close() },
  }) }))
}
afterEach(() => vi.unstubAllGlobals())
describe('账号作品流', () => {
  it('支持跨网络块拆分的事件及没有结尾换行的完成事件', async () => {
    stream(['data: {"type":"test_start","model":"真实模型"}\r\n\r\ndata: {"type":"con', 'tent","text":"<svg></svg>"}\n\ndata: {"type":"test_complete","success":true}'])
    await expect(testPelicanAccount(7, '', '', new AbortController().signal)).resolves.toEqual({ text: '<svg></svg>', model: '真实模型' })
    expect(JSON.parse(vi.mocked(fetch).mock.calls[0][1]?.body as string)).toMatchObject({ mode: 'pelican' })
  })
  it('拒绝没有完成事件的截断作品', async () => {
    stream(['data: {"type":"content","text":"<svg></svg>"}\n'])
    await expect(testPelicanAccount(7, '', '', new AbortController().signal)).rejects.toThrow('提前结束')
  })
  it('传播上游失败原因', async () => {
    stream(['data: {"type":"error","error":"额度耗尽"}\n'])
    await expect(testPelicanAccount(7, '', '', new AbortController().signal)).rejects.toThrow('额度耗尽')
  })
})
