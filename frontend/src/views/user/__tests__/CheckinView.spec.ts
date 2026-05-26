import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import CheckinView from '../CheckinView.vue'

const getOverview = vi.hoisted(() => vi.fn())
const checkin = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())

const messages: Record<string, string> = {
  'checkin.monthProgress': '本月已签到 {count} 天',
  'checkin.title': '每日签到',
  'checkin.checkedToday': '今日已签',
  'checkin.notCheckedToday': '今日未签',
  'checkin.disabled': '签到未开启',
  'checkin.alreadyChecked': '今日已签到',
  'checkin.checkNow': '立即签到',
  'checkin.dailyReward': '每日奖励',
  'checkin.day4Bonus': '第 4 天奖励',
  'checkin.day16Bonus': '第 16 天奖励',
  'checkin.nextBonus': '下一档额外奖励',
  'checkin.currentBalance': '当前余额：{balance}',
  'checkin.monthCalendar': '{year} 年 {month} 月签到表',
  'checkin.rulesTitle': '签到规则',
  'checkin.ruleDaily': '每日只能签到一次，签到成功后发放 {reward}。',
  'checkin.ruleBonus4': '本月第 4 次签到时，额外发放 {reward}。',
  'checkin.ruleBonus16': '本月第 16 次签到时，额外发放 {reward}。',
  'checkin.ruleMonthReset': '签到次数按服务端自然月统计，每月 1 日重新计算。',
  'checkin.daysToBonus': '还差 {days} 天到第 {milestone} 天',
  'checkin.noMoreBonus': '本月额外奖励已全部领取',
  'checkin.weekdays.sun': '日',
  'checkin.weekdays.mon': '一',
  'checkin.weekdays.tue': '二',
  'checkin.weekdays.wed': '三',
  'checkin.weekdays.thu': '四',
  'checkin.weekdays.fri': '五',
  'checkin.weekdays.sat': '六',
  'common.currencyName': '灵石',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        let text = messages[key] ?? key
        for (const [paramKey, value] of Object.entries(params ?? {})) {
          text = text.replace(`{${paramKey}}`, String(value))
        }
        return text
      },
    }),
  }
})

vi.mock('@/api/checkin', () => ({
  checkinAPI: {
    getOverview,
    checkin,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: {
      balance: 12,
    },
    refreshUser,
  }),
}))

function overviewFixture() {
  return {
    settings: {
      enabled: true,
      content: '连续签到领灵石',
      daily_reward: 2.5,
      extra_reward_4: 4,
      extra_reward_16: 16,
    },
    summary: {
      year: 2026,
      month: 5,
      today: '2026-05-26',
      today_checked: false,
      month_count: 2,
      days_in_month: 31,
      records: [],
      next_extra_milestone: 4,
    },
  }
}

describe('CheckinView', () => {
  beforeEach(() => {
    getOverview.mockReset()
    checkin.mockReset()
    refreshUser.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('按接口返回的当前设置展示签到规则奖励数字', async () => {
    getOverview.mockResolvedValue(overviewFixture())

    const wrapper = mount(CheckinView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
        },
      },
    })

    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('每日只能签到一次，签到成功后发放 2.50 灵石。')
    expect(text).toContain('本月第 4 次签到时，额外发放 4.00 灵石。')
    expect(text).toContain('本月第 16 次签到时，额外发放 16.00 灵石。')
  })
})
