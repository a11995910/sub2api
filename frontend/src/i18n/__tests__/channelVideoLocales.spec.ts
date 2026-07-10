import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import enAdminChannels from '../locales/en/admin/channels'
import zh from '../locales/zh'
import zhAdminChannels from '../locales/zh/admin/channels'

describe.each([
  ['zh', zh, '视频（每秒）', '灵石/秒'],
  ['en', en, 'Video (Per Second)', 'credits/sec'],
])('频道视频计费运行时语言包 %s', (_locale, messages, billingMode, perSecondUnit) => {
  it('包含视频计费选项和每秒价格文案', () => {
    expect(messages.admin.channels.billingMode.video).toBe(billingMode)
    expect(messages.admin.channels.form.videoTiers).toBeTruthy()
    expect(messages.admin.channels.form.defaultVideoPrice).toBeTruthy()
    expect(messages.admin.channels.form.perSecondUnit).toBe(perSecondUnit)
  })
})

describe.each([
  ['zh', zhAdminChannels, '视频（每秒）'],
  ['en', enAdminChannels, 'Video (Per Second)'],
])('频道视频计费模块化语言包 %s', (_locale, messages, billingMode) => {
  it('与运行时聚合文案保持相同的每秒语义', () => {
    expect(messages.channels.billingMode.video).toBe(billingMode)
  })
})
