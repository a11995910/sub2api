import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

function readPath(source: Record<string, any>, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[key]
  }, source)
}

describe.each([
  ['en', en],
  ['zh', zh],
])('OpenAI Fast/Flex locale %s', (_, locale) => {
  for (const key of [
    'userIds',
    'userIdsHint',
    'userIdPlaceholder',
    'addUserId',
    'removeUserId',
    'userIdsInvalid',
  ]) {
    it(`包含 ${key}`, () => {
      expect(readPath(locale, `admin.settings.openaiFastPolicy.${key}`)).toEqual(
        expect.any(String),
      )
    })
  }
})
