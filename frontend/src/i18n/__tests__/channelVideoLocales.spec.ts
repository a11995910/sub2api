import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe.each([
  ['zh', zh, '视频（按次）', '灵石/秒'],
  ['en', en, 'Video (Per Request)', 'credits/sec'],
])('频道视频计费运行时语言包 %s', (_locale, messages, billingMode, perSecondUnit) => {
  it('包含视频计费选项和每秒价格文案', () => {
    expect(messages.admin.channels.billingMode.video).toBe(billingMode)
    expect(messages.admin.channels.form.videoTiers).toBeTruthy()
    expect(messages.admin.channels.form.defaultVideoPrice).toBeTruthy()
    expect(messages.admin.channels.form.perSecondUnit).toBe(perSecondUnit)
  })
})
