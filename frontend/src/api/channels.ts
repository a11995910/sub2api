/**
 * User Channels API endpoints (non-admin)
 * 用户侧「可用渠道」聚合查询：渠道 + 用户可访问的分组 + 支持模型（含定价）。
 */

import { apiClient } from './client'
import type { BillingMode } from '@/constants/channel'

export interface UserAvailableGroup {
  id: number
  name: string
  platform: string
  /** 'standard' | 'subscription' — 订阅分组视觉加深，和 API 密钥页保持一致。 */
  subscription_type: string
  /** 分组默认倍率。用户专属倍率（若有）通过 /groups/rates 获取后在前端 join。 */
  rate_multiplier: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  /** true = 专属分组（小范围授权）；false = 公开分组。 */
  is_exclusive: boolean
  /** 是否允许该分组使用图片生成能力。 */
  allow_image_generation: boolean
  /** 是否对该分组的图片生成结果自动执行 4K 超分。 */
  image_super_resolution_enabled?: boolean
  /** true = 图片生成使用独立倍率；false = 共用当前有效分组倍率。 */
  image_rate_independent: boolean
  /** 是否将每次请求缓存命中 token 的四分之一划入输入 token 重新计费。 */
  cache_hit_quarter_to_input_enabled: boolean
  /** 图片生成独立倍率，仅 image_rate_independent=true 时生效。 */
  image_rate_multiplier: number
  /** 1K 图片生成单价；为空时后端计费会回退默认图片价格。 */
  image_price_1k: number | null
  /** 2K 图片生成单价；为空时后端计费会回退默认图片价格。 */
  image_price_2k: number | null
  /** 4K 图片生成单价；为空时后端计费会回退默认图片价格。 */
  image_price_4k: number | null
  video_rate_independent?: boolean
  video_rate_multiplier?: number
  video_price_480p?: number | null
  video_price_720p?: number | null
  video_price_1080p?: number | null
}

export interface UserPricingInterval {
  min_tokens: number
  max_tokens: number | null
  tier_label?: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  per_request_price: number | null
}

export interface UserSupportedModelPricing {
  billing_mode: BillingMode
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: UserPricingInterval[]
}

export interface UserSupportedModel {
  name: string
  platform: string
  /** token = 文本/普通按次，image = 图片，video = 视频。旧后端可能没有该字段，前端需兜底。 */
  kind?: 'token' | 'image' | 'video'
  pricing: UserSupportedModelPricing | null
}

/**
 * 渠道下单个平台的子视图：用户可访问的分组 + 该平台支持的模型。
 * 后端把一个渠道按平台聚合成 sections，前端可以把渠道名作为 row-group
 * 一次渲染，后面按 sections 顺序用 rowspan 铺开。
 */
export interface UserChannelPlatformSection {
  platform: string
  groups: UserAvailableGroup[]
  supported_models: UserSupportedModel[]
}

export interface UserAvailableChannel {
  name: string
  description: string
  platforms: UserChannelPlatformSection[]
}

/** 列出当前用户可见的「可用渠道」（与 /groups/available 保持一致，返回平数组）。 */
export async function getAvailable(options?: { signal?: AbortSignal }): Promise<UserAvailableChannel[]> {
  const { data } = await apiClient.get<UserAvailableChannel[]>('/channels/available', {
    signal: options?.signal
  })
  return data
}

export const userChannelsAPI = { getAvailable }

export default userChannelsAPI
