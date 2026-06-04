import { describe, expect, it } from 'vitest'
import type { CreativeDrawingTask } from '@/api/creativeDrawing'
import type { CreativeTurn } from '@/features/creativeDrawing/conversations'
import {
  shouldFetchFullCreativeTaskResult,
  shouldHydrateCreativeTaskFromList
} from '@/features/creativeDrawing/taskResults'

function createTask(overrides: Partial<CreativeDrawingTask> = {}): CreativeDrawingTask {
  return {
    id: 'task-1',
    user_id: 1,
    api_key_id: 13,
    conversation_id: 'conversation-1',
    turn_id: 'turn-1',
    mode: 'generate',
    model: 'gpt-image-2',
    prompt: '画一只小猪',
    count: 1,
    output_format: 'png',
    reference_images: [],
    status: 'success',
    images: [{
      id: 'image-1',
      url: 'http://192.0.2.10:3000/images/generated.png',
      output_format: 'png'
    }],
    created_at: '2026-06-04T10:49:20.000+08:00',
    updated_at: '2026-06-04T10:54:13.000+08:00',
    ...overrides
  }
}

function createTurn(overrides: Partial<CreativeTurn> = {}): CreativeTurn {
  return {
    id: 'turn-1',
    taskId: 'task-1',
    prompt: '画一只小猪',
    mode: 'generate',
    model: 'gpt-image-2',
    count: 1,
    size: '',
    outputFormat: 'png',
    sizeSelection: {
      mode: 'auto',
      aspectRatio: '',
      resolution: 'auto',
      customRatio: '16:9',
      customWidth: '1024',
      customHeight: '1024'
    },
    references: [],
    images: [],
    status: 'success',
    createdAt: '2026-06-04T10:49:20.000+08:00',
    ...overrides
  }
}

describe('creativeDrawing task result hydration', () => {
  it('成功任务只有受保护 URL 时需要补拉任务详情', () => {
    const task = createTask()

    expect(shouldFetchFullCreativeTaskResult(task)).toBe(true)
    expect(shouldHydrateCreativeTaskFromList(task, 0)).toBe(true)
  })

  it('列表任务已经带 base64 时不需要补拉详情', () => {
    const task = createTask({
      images: [{
        id: 'image-1',
        url: 'http://192.0.2.10:3000/images/generated.png',
        b64_json: 'UE5H',
        output_format: 'png'
      }]
    })

    expect(shouldFetchFullCreativeTaskResult(task)).toBe(false)
    expect(shouldHydrateCreativeTaskFromList(task, 0)).toBe(false)
  })

  it('本地轮次已经有 base64 缓存时不重复补拉详情', () => {
    const task = createTask()
    const turn = createTurn({
      images: [{
        id: 'image-1',
        url: 'data:image/png;base64,UE5H',
        b64_json: 'UE5H',
        output_format: 'png'
      }]
    })

    expect(shouldFetchFullCreativeTaskResult(task, turn)).toBe(false)
    expect(shouldHydrateCreativeTaskFromList(task, 0, turn)).toBe(false)
  })

  it('没有本地轮次时只自动补拉最近任务，避免列表恢复拖慢页面', () => {
    const task = createTask()

    expect(shouldHydrateCreativeTaskFromList(task, 7)).toBe(true)
    expect(shouldHydrateCreativeTaskFromList(task, 8)).toBe(false)
  })
})
