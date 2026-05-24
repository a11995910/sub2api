import type { GroupPlatform } from '@/types'

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-5.4'

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript: string
}

export function resolveCcSwitchImportConfig(
  platform: GroupPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string
): CcSwitchImportConfig {
  switch (platform || 'anthropic') {
    case 'antigravity':
      return {
        app: clientType === 'gemini' ? 'gemini' : 'claude',
        endpoint: `${baseUrl}/antigravity`
      }
    case 'openai':
      return {
        app: 'codex',
        endpoint: baseUrl,
        model: OPENAI_CC_SWITCH_CODEX_MODEL
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl
      }
    default:
      return {
        app: 'claude',
        endpoint: baseUrl
      }
  }
}

export function buildCcSwitchImportDeeplink(input: CcSwitchImportDeeplinkInput): string {
  const config = resolveCcSwitchImportConfig(input.platform, input.clientType, input.baseUrl)
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['apiKey', input.apiKey],
    ['configFormat', 'json'],
    ['usageEnabled', 'true'],
    ['usageScript', btoa(toAsciiJavaScriptSource(input.usageScript))],
    ['usageAutoInterval', '30']
  ]

  if (config.model) {
    entries.splice(2, 0, ['model', config.model])
  }

  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}

function toAsciiJavaScriptSource(source: string): string {
  // CC Switch 的 usageScript 是 base64 后的 JS 源码；先转义非 ASCII 字符，
  // 避免浏览器 btoa 遇到中文单位等字符时报 InvalidCharacterError。
  return Array.from(source, (char) => {
    const firstUnit = char.charCodeAt(0)
    if (firstUnit <= 0x7F) {
      return char
    }
    return Array.from(char)
      .map((part) => {
        const codePoint = part.codePointAt(0)
        if (codePoint == null) return ''
        if (codePoint <= 0xFFFF) {
          return `\\u${codePoint.toString(16).padStart(4, '0')}`
        }
        const normalized = codePoint - 0x10000
        const high = 0xD800 + (normalized >> 10)
        const low = 0xDC00 + (normalized & 0x3FF)
        return `\\u${high.toString(16).padStart(4, '0')}\\u${low.toString(16).padStart(4, '0')}`
      })
      .join('')
  }).join('')
}
