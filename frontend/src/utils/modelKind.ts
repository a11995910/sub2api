import { BILLING_MODE_IMAGE, BILLING_MODE_VIDEO } from '@/constants/channel'
import type {
  UserAvailableGroup,
  UserSupportedModel,
  UserSupportedModelPricing,
} from '@/api/channels'

export type ModelKind = 'token' | 'image' | 'video'

/**
 * 旧后端没有 kind 字段时，前端用计费模式和模型名兜底识别图片、视频模型。
 * 这里仅用于展示分组归属，不参与真实路由或扣费判断。
 */
export function modelKindFromPricing(
  pricing: UserSupportedModelPricing | null | undefined,
  modelName = '',
): ModelKind {
  if (pricing?.billing_mode === BILLING_MODE_VIDEO || isVideoModelName(modelName)) {
    return 'video'
  }
  if (pricing?.billing_mode === BILLING_MODE_IMAGE || isImageModelName(modelName)) {
    return 'image'
  }
  return 'token'
}

export function resolveModelKind(model: Pick<UserSupportedModel, 'kind' | 'name' | 'pricing'>): ModelKind {
  if (isVideoModelName(model.name)) return 'video'
  if (model.kind === 'image' || model.kind === 'video') return model.kind
  return modelKindFromPricing(model.pricing, model.name)
}

export function filterGroupsByModelKind(
  groups: UserAvailableGroup[] | undefined,
  kind: ModelKind,
): UserAvailableGroup[] {
  return (groups || []).filter((group) =>
    kind === 'image' || kind === 'video' ? group.allow_image_generation : !isLegacyOpenAIImageGroup(group),
  )
}

export function selectAvailableModelKind<T extends { kind: ModelKind }>(
  models: T[],
  preferred: ModelKind,
): ModelKind {
  if (preferred === 'video' && models.length > 0) {
    return 'video'
  }
  if (models.some((model) => model.kind === preferred)) {
    return preferred
  }
  for (const fallback of (['token', 'image', 'video'] as ModelKind[])) {
    if (models.some((model) => model.kind === fallback)) return fallback
  }
  return preferred
}

export function filterModelsByIntent<T extends { kind: ModelKind }>(models: T[], kind: ModelKind): T[] {
  if (kind === 'video') return models
  return models.filter((model) => model.kind === kind)
}

function isImageModelName(name: string): boolean {
  const normalized = name.trim().toLowerCase()
  return normalized.startsWith('gpt-image-') || normalized === 'image-2'
}

export function isVideoModelName(name: string): boolean {
  const normalized = name.trim().toLowerCase()
  return normalized.startsWith('grok-imagine-video') || isSeedanceVideoModel(normalized)
}

export function isSeedanceVideoModel(name: string): boolean {
  return name.trim().toLowerCase().startsWith('dreamina-seedance-')
}

function isLegacyOpenAIImageGroup(group: UserAvailableGroup): boolean {
  // OpenAI 兼容图片分组历史上只承载 image-2；Grok 等原生多模态平台的图片能力是附加能力。
  return group.allow_image_generation && group.platform === 'openai'
}
