import { afterEach, describe, expect, it } from 'vitest'
import {
  buildStoredImageUrl,
  loadCreativeConversations,
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

  it('保存本地历史时不重复存储 data URL，读取时可恢复可展示图片', () => {
    saveCreativeConversations([createConversation()])

    const raw = localStorage.getItem(STORAGE_KEY)
    expect(raw).toBeTruthy()
    const stored = JSON.parse(raw || '[]') as CreativeConversation[]
    expect(stored[0].turns[0].images[0].url).toBe('')
    expect(stored[0].turns[0].images[0].b64_json).toBe('QUJD')

    const loaded = loadCreativeConversations()
    const image = loaded[0].turns[0].images[0]
    expect(buildStoredImageUrl(image)).toBe('data:image/webp;base64,QUJD')
    expect(resultToReferenceImage(image, 0)?.dataUrl).toBe('data:image/webp;base64,QUJD')
    expect(buildStoredImageUrl({ url: 'file-service://file_123', b64_json: 'QUJD', output_format: 'webp' })).toBe('data:image/webp;base64,QUJD')
  })
})
