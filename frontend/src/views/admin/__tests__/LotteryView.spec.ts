import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import LotteryView from '../LotteryView.vue'

const getConfig = vi.hoisted(() => vi.fn())
const updateConfig = vi.hoisted(() => vi.fn())
const listDraws = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())
const stepUpRun = vi.hoisted(() => vi.fn((action: () => Promise<unknown>) => action()))

vi.mock('@/api/admin/lottery', () => {
  const api = { getConfig, updateConfig, listDraws }
  return { lotteryAdminAPI: api, default: api }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

function configFixture(prizes: Array<Record<string, unknown>> = []) {
  return {
    enabled: true,
    usage_threshold_m: 1,
    usage_threshold_tokens: 1_000_000,
    award_mode: 'daily_once',
    prizes: [
      ...prizes,
      {
        id: 'thanks',
        name: '谢谢惠顾',
        reward_amount: 0,
        probability_percent: Math.max(0, 100 - prizes.reduce((sum, prize) => sum + Number(prize.probability_percent), 0)),
        is_thanks: true,
      },
    ],
    thanks_probability_percent: 100,
    version: 1,
    updated_at: '2026-08-30T00:00:00Z',
  }
}

function mountView() {
  return mount(LotteryView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        LoadingSpinner: true,
        Icon: true,
        Pagination: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('管理员抽奖功能配置', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    getConfig.mockResolvedValue(configFixture())
    updateConfig.mockImplementation(async (input) => ({ ...configFixture(), ...input }))
    listDraws.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  })

  it('最多允许添加五个余额奖项', async () => {
    const wrapper = mountView()
    await flushPromises()

    const addButton = wrapper.findAll('button').find((button) => button.text().includes('admin.lottery.addPrize'))
    expect(addButton).toBeDefined()
    for (let index = 0; index < 5; index += 1) {
      await addButton!.trigger('click')
    }

    expect(wrapper.findAll('input[name$=".name"]')).toHaveLength(5)
    expect(addButton!.attributes('disabled')).toBeDefined()
  })

  it('概率总和超过百分百时阻止保存', async () => {
    getConfig.mockResolvedValue(configFixture([
      { id: 'a', name: 'A', reward_amount: 1, probability_percent: 60, is_thanks: false },
      { id: 'b', name: 'B', reward_amount: 2, probability_percent: 50, is_thanks: false },
    ]))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.lottery.invalidProbability')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(updateConfig).not.toHaveBeenCalled()
  })

  it('可以切换为每达到门槛就发放一次', async () => {
    const wrapper = mountView()
    await flushPromises()

    const modeButton = wrapper.findAll('button').find((button) => button.text().includes('admin.lottery.perThreshold'))
    expect(modeButton).toBeDefined()
    await modeButton!.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ award_mode: 'per_threshold' }))
    expect(stepUpRun).toHaveBeenCalledTimes(1)
  })
})
