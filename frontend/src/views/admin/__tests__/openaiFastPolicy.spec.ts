import { describe, expect, it } from 'vitest'

import { normalizeOpenAIFastPolicyUserIDs } from '../openaiFastPolicy'

describe('normalizeOpenAIFastPolicyUserIDs', () => {
  it('接受空列表和唯一的正安全整数', () => {
    expect(normalizeOpenAIFastPolicyUserIDs([])).toEqual([])
    expect(normalizeOpenAIFastPolicyUserIDs([1, 42, 9007199254740991])).toEqual([
      1, 42, 9007199254740991,
    ])
  })

  it.each([
    [[0]],
    [[-1]],
    [[1.5]],
    [['']],
    [[Number.MAX_SAFE_INTEGER + 1]],
    [[42, 42]],
  ])('拒绝非法或重复用户 ID：%j', (input) => {
    expect(normalizeOpenAIFastPolicyUserIDs(input)).toBeNull()
  })
})
