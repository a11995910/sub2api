import { describe, expect, it } from 'vitest'
import {
  OPENAI_CC_SWITCH_CODEX_MODEL,
  OPENAI_CC_SWITCH_CODEX_REASONING_EFFORT,
  buildCcSwitchImportDeeplink
} from '@/utils/ccswitchImport'
import type { GroupPlatform } from '@/types'

function paramsFromDeeplink(deeplink: string): URLSearchParams {
  const query = deeplink.split('?')[1] || ''
  return new URLSearchParams(query)
}

describe('ccswitchImport utils', () => {
  it('defaults OpenAI CC Switch imports to the current Codex model', () => {
    expect(OPENAI_CC_SWITCH_CODEX_MODEL).toBe('gpt-5.5')
  })

  const baseInput = {
    baseUrl: 'https://api.example.com',
    providerName: 'Sub2API',
    apiKey: 'sk-test',
    usageScript: 'return true'
  }

  it('adds the Codex model parameter for OpenAI imports', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform: 'openai',
        clientType: 'claude'
      })
    )

    expect(params.get('resource')).toBe('provider')
    expect(params.get('app')).toBe('codex')
    expect(params.get('endpoint')).toBe(baseInput.baseUrl)
    expect(params.get('model')).toBe(OPENAI_CC_SWITCH_CODEX_MODEL)
    expect(params.get('configFormat')).toBe('json')
    expect(atob(params.get('usageScript') || '')).toBe(baseInput.usageScript)

    const configPayload = JSON.parse(atob(params.get('config') || ''))
    expect(configPayload.config).toContain(`model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"`)
    expect(configPayload.config).toContain(`review_model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"`)
    expect(configPayload.config).toContain(
      `model_reasoning_effort = "${OPENAI_CC_SWITCH_CODEX_REASONING_EFFORT}"`
    )
    expect(configPayload.config).toContain(`base_url = "${baseInput.baseUrl}"`)
  })

  it('encodes non-Latin1 usage scripts without throwing', () => {
    const usageScript = 'const unit = "灵石"; return unit'
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        usageScript,
        platform: 'anthropic',
        clientType: 'claude'
      })
    )

    const decoded = atob(params.get('usageScript') || '')
    expect(decoded).toContain('"\\u7075\\u77f3"')
    expect(new Function(decoded)()).toBe('灵石')
  })

  it.each([
    { platform: 'anthropic' as GroupPlatform, clientType: 'claude' as const, app: 'claude' },
    { platform: 'gemini' as GroupPlatform, clientType: 'gemini' as const, app: 'gemini' }
  ])('does not add a model parameter for $platform imports', ({ platform, clientType, app }) => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform,
        clientType
      })
    )

    expect(params.get('app')).toBe(app)
    expect(params.get('endpoint')).toBe(baseInput.baseUrl)
    expect(params.has('model')).toBe(false)
  })

  it('keeps Antigravity imports on the selected client endpoint without a model parameter', () => {
    const params = paramsFromDeeplink(
      buildCcSwitchImportDeeplink({
        ...baseInput,
        platform: 'antigravity',
        clientType: 'gemini'
      })
    )

    expect(params.get('app')).toBe('gemini')
    expect(params.get('endpoint')).toBe(`${baseInput.baseUrl}/antigravity`)
    expect(params.has('model')).toBe(false)
  })
})
