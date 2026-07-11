/**
 * 规范化 Fast/Flex 策略中的用户 ID。
 * 返回 null 表示存在空值、非正整数、超出安全整数范围或重复项。
 */
export function normalizeOpenAIFastPolicyUserIDs(
  values: readonly unknown[] | undefined,
): number[] | null {
  if (!values || values.length === 0) return []

  const normalized: number[] = []
  const seen = new Set<number>()
  for (const value of values) {
    const userID = typeof value === 'number' ? value : Number.NaN
    if (!Number.isSafeInteger(userID) || userID <= 0 || seen.has(userID)) {
      return null
    }
    seen.add(userID)
    normalized.push(userID)
  }
  return normalized
}
