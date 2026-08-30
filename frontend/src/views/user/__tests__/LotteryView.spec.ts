import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import LotteryView from '../LotteryView.vue'

const getOverview = vi.hoisted(() => vi.fn())
const draw = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())

vi.mock('@/api/lottery', () => ({
  lotteryAPI: { getOverview, draw },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ refreshUser }),
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => value,
  formatSpiritStones: (value: number) => `${value.toFixed(2)} 灵石`,
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

function overviewFixture() {
  return {
    config: {
      enabled: true,
      usage_threshold_m: 1,
      usage_threshold_tokens: 1_000_000,
      award_mode: 'per_threshold',
      prizes: [
        { id: 'small', name: '1 灵石', reward_amount: 1, probability_percent: 25, is_thanks: false },
        { id: 'thanks', name: '谢谢惠顾', reward_amount: 0, probability_percent: 75, is_thanks: true },
      ],
      thanks_probability_percent: 75,
      version: 2,
      updated_at: '2026-08-30T00:00:00Z',
    },
    state: {
      available_chances: 1,
      total_earned: 3,
      total_drawn: 2,
      today_usage_tokens: 3_400_000,
      today_threshold_tokens: 1_000_000,
      today_award_mode: 'per_threshold',
      today_awarded_chances: 3,
      today_next_target_tokens: 4_000_000,
      today_qualified: true,
    },
    today_usage_m: 3.4,
    today_next_target_m: 4,
    today_progress_percent: 40,
    recent_draws: [],
  }
}

function mountView() {
  return mount(LotteryView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        LoadingSpinner: true,
        Icon: true,
      },
    },
  })
}

describe('用户抽奖页', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })))
    getOverview.mockResolvedValue(overviewFixture())
    draw.mockResolvedValue({
      draw: {
        id: 9,
        user_id: 7,
        prize_id: 'small',
        prize_name: '1 灵石',
        reward_amount: 1,
        probability_percent: 25,
        config_version: 2,
        chance_before: 1,
        chance_after: 0,
        balance_after: 12,
        created_at: '2026-08-30T10:00:00Z',
      },
      available_chances: 0,
      new_balance: 12,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('按服务端结果完成动画并更新机会与流水', async () => {
    const wrapper = mountView()
    await flushPromises()

    const drawButton = wrapper.findAll('button').find((button) => button.text().includes('lottery.drawNow'))
    expect(drawButton).toBeDefined()
    await drawButton!.trigger('click')
    await flushPromises()

    expect(draw).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('lottery.drawing')

    vi.advanceTimersByTime(3300)
    await flushPromises()

    expect(wrapper.text()).toContain('1 灵石')
    expect(wrapper.text()).toContain('12.00 灵石')
    expect(wrapper.text()).toContain('lottery.noChance')
    expect(refreshUser).toHaveBeenCalledTimes(1)
  })
})
