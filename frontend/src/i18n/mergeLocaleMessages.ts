export type LocaleMessages = Record<string, any>

function isMessageObject(value: unknown): value is LocaleMessages {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

// 拆分语言包提供上游新增键，定制语言包覆盖同名路径并补充本项目专属功能文案。
export function mergeLocaleMessages(base: LocaleMessages, custom: LocaleMessages): LocaleMessages {
  const merged: LocaleMessages = { ...base }

  for (const [key, customValue] of Object.entries(custom)) {
    const baseValue = merged[key]
    merged[key] = isMessageObject(baseValue) && isMessageObject(customValue)
      ? mergeLocaleMessages(baseValue, customValue)
      : customValue
  }

  return merged
}
