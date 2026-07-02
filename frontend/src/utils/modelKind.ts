import { BILLING_MODE_IMAGE } from '@/constants/channel'
import type {
  UserAvailableGroup,
  UserSupportedModel,
  UserSupportedModelPricing,
} from '@/api/channels'

export type ModelKind = 'token' | 'image'

/**
 * 旧后端没有 kind 字段时，前端用计费模式和模型名兜底识别图片模型。
 * 这里仅用于展示分组归属，不参与真实路由或扣费判断。
 */
export function modelKindFromPricing(
  pricing: UserSupportedModelPricing | null | undefined,
  modelName = '',
): ModelKind {
  if (pricing?.billing_mode === BILLING_MODE_IMAGE || isImageModelName(modelName)) {
    return 'image'
  }
  return 'token'
}

export function resolveModelKind(model: Pick<UserSupportedModel, 'kind' | 'name' | 'pricing'>): ModelKind {
  return model.kind === 'image' ? 'image' : modelKindFromPricing(model.pricing, model.name)
}

export function filterGroupsByModelKind(
  groups: UserAvailableGroup[] | undefined,
  kind: ModelKind,
): UserAvailableGroup[] {
  return (groups || []).filter((group) =>
    kind === 'image' ? group.allow_image_generation : !group.allow_image_generation,
  )
}

export function selectAvailableModelKind<T extends { kind: ModelKind }>(
  models: T[],
  preferred: ModelKind,
): ModelKind {
  if (models.some((model) => model.kind === preferred)) {
    return preferred
  }
  const fallback: ModelKind = preferred === 'token' ? 'image' : 'token'
  return models.some((model) => model.kind === fallback) ? fallback : preferred
}

function isImageModelName(name: string): boolean {
  const normalized = name.trim().toLowerCase()
  return normalized.startsWith('gpt-image-') || normalized === 'image-2'
}
