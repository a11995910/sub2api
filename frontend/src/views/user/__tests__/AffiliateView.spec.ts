import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AffiliateView from '../AffiliateView.vue'

const getAffiliateDetail = vi.hoisted(() => vi.fn())
const transferAffiliateQuota = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())
const copyToClipboard = vi.hoisted(() => vi.fn())

const messages: Record<string, string> = {
  'affiliate.title': '邀请返利',
  'affiliate.description': '邀请新用户注册，并将返利额度转入账户余额',
  'affiliate.yourCode': '我的邀请码',
  'affiliate.inviteLink': '邀请链接',
  'affiliate.copyCode': '复制邀请码',
  'affiliate.copyLink': '复制链接',
  'affiliate.loadFailed': '加载邀请返利数据失败',
  'affiliate.rewardCard.badge': '重要邀请奖励',
  'affiliate.rewardCard.title': '获得 {days} 天「{group}」使用权',
  'affiliate.rewardCard.standardDescription':
    '好友完成支付后立即发放，天数可累加；请求按该分组 {rate} 倍率正常扣余额，到期后自动回到默认分组。',
  'affiliate.rewardCard.subscriptionDescription': '好友完成支付后立即发放，天数可累加；请求按订阅额度消耗。',
  'affiliate.rewardCard.countdown': '当前剩余 {time}',
  'affiliate.rewardCard.expired': '已到期',
  'affiliate.stats.rebateRate': '我的返利比例',
  'affiliate.stats.rebateRateHint': '被邀请用户每次充值后你可获得的返利比例',
  'affiliate.stats.invitedUsers': '邀请人数',
  'affiliate.stats.availableQuota': '可转返利额度',
  'affiliate.stats.totalQuota': '历史返利额度',
  'affiliate.stats.frozenQuota': '冻结中',
  'affiliate.transfer.title': '返利额度转余额',
  'affiliate.transfer.description': '将当前可用返利额度一键转入账户余额',
  'affiliate.transfer.button': '转入余额',
  'affiliate.transfer.transferring': '转入中...',
  'affiliate.transfer.empty': '当前没有可转入额度',
  'affiliate.transfer.success': '已转入余额：{amount}',
  'affiliate.invitees.title': '已邀请用户',
  'affiliate.invitees.empty': '暂无邀请记录',
  'affiliate.invitees.columns.email': '邮箱',
  'affiliate.invitees.columns.username': '用户名',
  'affiliate.invitees.columns.rebate': '返利明细',
  'affiliate.invitees.columns.joinedAt': '注册时间',
  'affiliate.tips.title': '使用说明',
  'affiliate.tips.line1': '将邀请码或邀请链接分享给新用户。',
  'affiliate.tips.line2': '被邀请用户充值后，你可获得 {rate} 的返利额度。',
  'affiliate.tips.line3': '返利额度可随时转入账户余额。',
  'affiliate.tips.line4': '新产生的返利需要经过冻结期后才能提现。',
  'affiliate.tips.paymentRewardStandard':
    '被邀请用户完成支付后，你还会获得 {days} 天「{group}」使用权；请求按该分组 {rate} 倍率正常扣余额，天数可累加，到期后自动回到默认分组。',
  'affiliate.tips.paymentRewardSubscription':
    '被邀请用户完成支付后，你还会获得 {days} 天「{group}」订阅使用权；请求按订阅额度消耗，天数可累加。',
  'affiliate.tips.rewardGroupFallback': '分组 #{id}',
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

vi.mock('@/api/user', () => ({
  default: {
    getAffiliateDetail,
    transferAffiliateQuota,
  },
  getAffiliateDetail,
  transferAffiliateQuota,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('@/utils/format', () => ({
  formatCurrency: (value: number) => `${Number(value || 0).toFixed(2)} 灵石`,
  formatDateTime: (value?: string | null) => value || '',
}))

function detailFixture(paymentReward: Record<string, unknown>) {
  return {
    user_id: 10,
    aff_code: 'AFF-CODE',
    inviter_id: null,
    aff_count: 0,
    aff_quota: 0,
    aff_frozen_quota: 0,
    aff_history_quota: 0,
    effective_rebate_rate_percent: 20,
    payment_reward: paymentReward,
    invitees: [],
  }
}

async function mountAffiliateView() {
  const wrapper = mount(AffiliateView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('AffiliateView', () => {
  beforeEach(() => {
    getAffiliateDetail.mockReset()
    transferAffiliateQuota.mockReset()
    refreshUser.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
  })

  it('展示标准专属分组支付奖励说明', async () => {
    getAffiliateDetail.mockResolvedValue(detailFixture({
      group_id: 9,
      group_name: 'VIP 专线',
      validity_days: 5,
      reward_mode: 'standard_group_access',
      rate_multiplier: 0.7,
      current_expires_at: new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toISOString(),
    }))

    const wrapper = await mountAffiliateView()
    const text = wrapper.text()

    expect(text).toContain('重要邀请奖励')
    expect(text).toContain('获得 5 天「VIP 专线」使用权')
    expect(text).toContain('当前剩余')
    expect(text).toContain('被邀请用户完成支付后，你还会获得 5 天「VIP 专线」使用权')
    expect(text).toContain('请求按该分组 0.7 倍率正常扣余额')
    expect(text).toContain('到期后自动回到默认分组')
  })

  it('展示订阅分组支付奖励说明', async () => {
    getAffiliateDetail.mockResolvedValue(detailFixture({
      group_id: 12,
      group_name: 'Claude 订阅',
      validity_days: 7,
      reward_mode: 'subscription_quota',
      rate_multiplier: 1,
    }))

    const wrapper = await mountAffiliateView()
    const text = wrapper.text()

    expect(text).toContain('被邀请用户完成支付后，你还会获得 7 天「Claude 订阅」订阅使用权')
    expect(text).toContain('请求按订阅额度消耗，天数可累加')
  })
})
