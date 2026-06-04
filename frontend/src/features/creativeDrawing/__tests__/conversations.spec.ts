import { afterEach, describe, expect, it } from 'vitest'
import {
  buildStoredImageUrl,
  loadCreativeConversations,
  normalizeStoredImageBase64,
  resultToReferenceImage,
  saveCreativeConversations,
  type CreativeConversation
} from '@/features/creativeDrawing/conversations'

const STORAGE_KEY = 'sub2api:creative-drawing:conversations'

function createConversation(): CreativeConversation {
  return {
    id: 'conversation-1',
    title: '测试创作',
    createdAt: '2026-05-21T10:00:00.000Z',
    updatedAt: '2026-05-21T10:00:00.000Z',
    turns: [
      {
        id: 'turn-1',
        prompt: '画一张海报',
        mode: 'generate',
        model: 'gpt-image-2',
        count: 1,
        size: '',
        outputFormat: 'webp',
        sizeSelection: {
          mode: 'auto',
          aspectRatio: '',
          resolution: 'auto',
          customRatio: '16:9',
          customWidth: '1024',
          customHeight: '1024'
        },
        references: [],
        images: [
          {
            id: 'image-1',
            url: 'data:image/webp;base64,QUJD',
            source_url: 'http://192.0.2.10:3000/images/generated.webp',
            image_store_id: 'stored-image-1',
            b64_json: 'QUJD',
            output_format: 'webp'
          }
        ],
        status: 'success',
        createdAt: '2026-05-21T10:00:00.000Z'
      }
    ]
  }
}

describe('creativeDrawing conversations', () => {
  afterEach(() => {
    localStorage.clear()
  })

  it('保存本地历史时把大图正文留在图片缓存索引中，读取时可恢复可展示图片', () => {
    saveCreativeConversations([createConversation()])

    const raw = localStorage.getItem(STORAGE_KEY)
    expect(raw).toBeTruthy()
    const stored = JSON.parse(raw || '[]') as CreativeConversation[]
    expect(stored[0].turns[0].images[0].url).toBe('http://192.0.2.10:3000/images/generated.webp')
    expect(stored[0].turns[0].images[0].source_url).toBe('http://192.0.2.10:3000/images/generated.webp')
    expect(stored[0].turns[0].images[0].image_store_id).toBe('stored-image-1')
    expect(stored[0].turns[0].images[0].b64_json).toBeUndefined()

    const loaded = loadCreativeConversations()
    const image = loaded[0].turns[0].images[0]
    expect(buildStoredImageUrl({ ...image, b64_json: 'QUJD' })).toBe('data:image/webp;base64,QUJD')
    expect(resultToReferenceImage({ ...image, b64_json: 'QUJD' }, 0)?.dataUrl).toBe('data:image/webp;base64,QUJD')
    expect(buildStoredImageUrl({ url: 'file-service://file_123', b64_json: 'QUJD', output_format: 'webp' })).toBe('data:image/webp;base64,QUJD')
  })

  it('不会把远程图片 URL 当作 base64 渲染或写入缓存', () => {
    const url = 'https://static.example.com/images/generated.png'

    expect(normalizeStoredImageBase64(url)).toBe('')
    expect(buildStoredImageUrl({ url, b64_json: url, output_format: 'png' })).toBe(url)
    expect(buildStoredImageUrl({
      url: '',
      source_url: url,
      b64_json: url,
      output_format: 'png'
    })).toBe(url)
  })

  it('受保护的 chatgpt2api 图片地址没有 base64 时不作为展示源', () => {
    const url = 'http://192.220.24.46:3000/images/2026/06/04/generated.png'

    expect(normalizeStoredImageBase64(url)).toBe('')
    expect(buildStoredImageUrl({ url, b64_json: url, output_format: 'png' })).toBe('')
    expect(buildStoredImageUrl({ url: '', source_url: url, b64_json: url, output_format: 'png' })).toBe('')
    expect(buildStoredImageUrl({ url, b64_json: 'UE5H', output_format: 'png' })).toBe('data:image/png;base64,UE5H')
    expect(resultToReferenceImage({ id: 'image-1', url, output_format: 'png' }, 0)).toBeNull()
  })

  it('读取持久化历史时把遗留生成中轮次标记为中断', () => {
    const conversation = createConversation()
    conversation.turns[0].status = 'generating'
    conversation.turns[0].images = []
    localStorage.setItem(STORAGE_KEY, JSON.stringify([conversation]))

    const loaded = loadCreativeConversations()
    expect(loaded[0].turns[0].status).toBe('error')
    expect(loaded[0].turns[0].error).toContain('页面刷新')
  })

  it('带后端任务 ID 的生成中轮次刷新后保持生成中，等待任务同步回写', () => {
    const conversation = createConversation()
    conversation.turns[0].status = 'generating'
    conversation.turns[0].taskId = 'task-1'
    conversation.turns[0].images = []
    localStorage.setItem(STORAGE_KEY, JSON.stringify([conversation]))

    const loaded = loadCreativeConversations()
    expect(loaded[0].turns[0].status).toBe('generating')
    expect(loaded[0].turns[0].taskId).toBe('task-1')
    expect(loaded[0].turns[0].error).toBeUndefined()
  })
})
