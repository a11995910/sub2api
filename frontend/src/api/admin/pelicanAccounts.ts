import { buildApiUrl } from '../client'

export interface PelicanAccountAnswer { text: string; model: string }

// 必须收到成功结束事件；连接中断不能被当作生成成功。
export async function testPelicanAccount(id: number, model: string, effort: string, signal: AbortSignal): Promise<PelicanAccountAnswer> {
  const response = await fetch(buildApiUrl(`/admin/accounts/${id}/test`), {
    method: 'POST',
    headers: { Authorization: `Bearer ${localStorage.getItem('auth_token') || ''}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode: 'pelican', model_id: model, reasoning_effort: effort }),
    signal,
  })
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  const reader = response.body?.getReader()
  if (!reader) throw new Error('未收到测试响应')
  const decoder = new TextDecoder()
  let buffer = ''
  let text = ''
  let actualModel = model
  let complete = false
  const consume = (line: string) => {
    if (!line.startsWith('data:')) return
    const data = line.slice(5).trim()
    if (!data || data === '[DONE]') return
    const event = JSON.parse(data)
    if (event.type === 'content') {
      text += event.text || ''
      if (text.length > 2_000_000) throw new Error('作品内容超过大小限制')
    }
    if (event.type === 'test_start' && event.model) actualModel = event.model
    if (event.type === 'error' || (event.type === 'test_complete' && !event.success)) throw new Error(event.error || '测试失败')
    if (event.type === 'test_complete' && event.success) complete = true
  }
  try {
    while (!complete) {
      const { value, done } = await reader.read()
      buffer += decoder.decode(value, { stream: !done })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) consume(line)
      if (done) { consume(buffer); break }
      if (buffer.length > 2_000_000) throw new Error('测试响应超过大小限制')
    }
    if (!complete) throw new Error('测试连接提前结束，请重试')
    return { text, model: actualModel }
  } finally {
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}
