import type { UserAvailableGroup, UserSupportedModelPricing } from '@/api/channels'
import {
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
} from '@/constants/channel'
import { isSeedanceVideoModel } from '@/utils/modelKind'

export type VideoResolution = '480p' | '720p' | '1080p'
export type VideoBillingUnit = 'second' | 'request'
export type VideoPriceSource = 'group' | 'channel_interval' | 'channel_default' | 'system_default'

export interface VideoPriceInput {
  group: UserAvailableGroup
  pricing: UserSupportedModelPricing | null
  modelName: string
  resolution: VideoResolution
  userGroupRate?: number
}

export interface VideoPriceQuote {
  basePrice: number
  effectivePrice: number
  billingUnit: VideoBillingUnit
  source: VideoPriceSource
}

/** 与 Grok 视频端点保持一致：video-1.5 文生视频实际按标准视频模型路由和计费。 */
export function normalizeVideoBillingModelName(modelName: string, hasReferenceImage: boolean): string {
  const normalizedModel = modelName.trim()
  if (normalizedModel.toLowerCase().startsWith('grok-imagine-video-1.5') && !hasReferenceImage) {
    return 'grok-imagine-video'
  }
  return normalizedModel
}

function groupVideoPrice(group: UserAvailableGroup, resolution: VideoResolution): number | null | undefined {
  switch (resolution) {
    case '480p':
      return group.video_price_480p
    case '720p':
      return group.video_price_720p
    case '1080p':
      return group.video_price_1080p
  }
}

function effectiveVideoRate(input: VideoPriceInput): number {
  if (input.group.video_rate_independent) {
    return input.group.video_rate_multiplier ?? 1
  }
  return input.userGroupRate ?? input.group.rate_multiplier ?? 1
}

function quote(
  input: VideoPriceInput,
  basePrice: number,
  billingUnit: VideoBillingUnit,
  source: VideoPriceSource,
): VideoPriceQuote {
  return {
    basePrice,
    effectivePrice: basePrice * effectiveVideoRate(input),
    billingUnit,
    source,
  }
}

function systemDefaultVideoPrice(modelName: string, resolution: VideoResolution): number | null {
  const normalizedModel = modelName.trim().toLowerCase()
  if (isSeedanceVideoModel(normalizedModel)) {
    if (normalizedModel.includes('seedance-2-0-mini')) {
      if (resolution === '480p') return 0.04
      if (resolution === '720p') return 0.08
      return null
    }
    if (normalizedModel.includes('seedance-2-0-fast')) {
      if (resolution === '480p') return 0.06
      if (resolution === '720p') return 0.12
      return null
    }
    if (normalizedModel.includes('seedance-2-0')) {
      if (resolution === '480p') return 0.07
      if (resolution === '720p') return 0.15
      if (resolution === '1080p') return 0.37
    }
    return null
  }
  if (normalizedModel.startsWith('grok-imagine-video-1.5')) {
    switch (resolution) {
      case '480p':
        return 0.08
      case '720p':
        return 0.14
      case '1080p':
        return 0.25
    }
  }
  if (normalizedModel.startsWith('grok-imagine-video')) {
    if (resolution === '480p') return 0.05
    if (resolution === '720p') return 0.07
  }
  return null
}

export function videoResolutionsForModel(modelName: string): VideoResolution[] {
  const normalizedModel = modelName.trim().toLowerCase()
  if (
    normalizedModel.startsWith('grok-imagine-video-1.5') ||
    (isSeedanceVideoModel(normalizedModel) &&
      !normalizedModel.includes('seedance-2-0-fast') &&
      !normalizedModel.includes('seedance-2-0-mini'))
  ) {
    return ['480p', '720p', '1080p']
  }
  return ['480p', '720p']
}

/**
 * 按运行时计费顺序解析视频报价，并保留历史图片/按次渠道价格的按次语义。
 * `null` 表示价格缺失；数值 `0` 是有效的显式零价，不能继续回退。
 */
export function resolveVideoPriceQuote(input: VideoPriceInput): VideoPriceQuote | null {
  if (input.pricing?.billing_mode === BILLING_MODE_TOKEN) {
    return null
  }

  const groupPrice = groupVideoPrice(input.group, input.resolution)
  if (groupPrice != null) {
    return quote(input, groupPrice, 'second', 'group')
  }

  const billingUnit: VideoBillingUnit = input.pricing?.billing_mode === BILLING_MODE_VIDEO
    ? 'second'
    : 'request'
  const intervalPrice = input.pricing?.intervals.find(
    (interval) => interval.tier_label === input.resolution,
  )?.per_request_price
  if (intervalPrice != null) {
    return quote(input, intervalPrice, billingUnit, 'channel_interval')
  }

  if (input.pricing?.per_request_price != null) {
    return quote(input, input.pricing.per_request_price, billingUnit, 'channel_default')
  }

  if (input.pricing && input.pricing.billing_mode !== BILLING_MODE_VIDEO) {
    return null
  }

  const systemPrice = systemDefaultVideoPrice(input.modelName, input.resolution)
  return systemPrice == null
    ? null
    : quote(input, systemPrice, 'second', 'system_default')
}
