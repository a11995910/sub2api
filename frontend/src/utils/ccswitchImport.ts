import type { GroupPlatform } from '@/types'

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-5.6-sol'
export const OPENAI_CC_SWITCH_CODEX_REASONING_EFFORT = 'medium'
export const GROK_CC_SWITCH_MODEL = 'grok-4.5'

export type CcSwitchClientType = 'claude' | 'gemini'

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
  config?: string
}

export interface CcSwitchImportDeeplinkInput {
  baseUrl: string
  platform?: GroupPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript: string
}

/**
 * Balance query CC Switch runs against the imported provider. CC Switch fills
 * `{{baseUrl}}` with the provider's base URL as stored — Codex and Grok imports
 * carry a trailing `/v1` (see `withV1Endpoint`), Claude ones do not, and users
 * may edit it either way afterwards — then evaluates the script, so the URL
 * strips an existing `/v1` instead of blindly appending one (`/v1/v1/usage`
 * is a 404 and CC Switch shows "query failed").
 */
export const CC_SWITCH_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/v1$/, "") + "/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "灵石";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`

function withV1Endpoint(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, '')
  return normalizedBaseUrl.endsWith('/v1') ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`
}

function withoutTrailingSlashes(baseUrl: string): string {
  return baseUrl.replace(/\/+$/, '')
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
        endpoint: `${baseUrl.replace(/\/+$/, '')}/antigravity`
      }
    case 'openai':
      return {
        app: 'codex',
        // CC Switch 使用原始端点，实际 Codex 配置保留单个 /v1。
        endpoint: withoutTrailingSlashes(baseUrl),
        model: OPENAI_CC_SWITCH_CODEX_MODEL,
        config: buildCodexProviderConfig(withV1Endpoint(baseUrl))
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl
      }
    case 'grok':
      return {
        app: 'grokbuild',
        endpoint: withV1Endpoint(baseUrl),
        model: GROK_CC_SWITCH_MODEL
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
  if (config.config) {
    entries.push(['config', btoa(toAsciiJavaScriptSource(config.config))])
  }

  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}

function buildCodexProviderConfig(baseUrl: string): string {
  return JSON.stringify({
    auth: {},
    config: `model_provider = "OpenAI"
model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"
review_model = "${OPENAI_CC_SWITCH_CODEX_MODEL}"
model_reasoning_effort = "${OPENAI_CC_SWITCH_CODEX_REASONING_EFFORT}"
disable_response_storage = true
network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${baseUrl}"
wire_api = "responses"
requires_openai_auth = true

[features]
goals = true`
  })
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
