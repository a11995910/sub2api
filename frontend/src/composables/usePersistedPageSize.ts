import { getConfiguredTableDefaultPageSize, normalizeTablePageSize } from '@/utils/tablePreferences'

const STORAGE_KEY = 'table-page-size'
const SOURCE_KEY = 'table-page-size-source'
const SOURCE_USER = 'user'
const SIGNATURE_KEY = 'table-page-size-config-signature'

const currentConfigSignature = (): string => {
  const config = typeof window !== 'undefined' ? window.__APP_CONFIG__ : null
  return JSON.stringify({
    default: config?.table_default_page_size ?? null,
    options: Array.isArray(config?.table_page_size_options) ? config.table_page_size_options : null
  })
}

export function getPersistedPageSize(fallback = getConfiguredTableDefaultPageSize()): number {
  const configuredDefault = getConfiguredTableDefaultPageSize() || fallback
  if (typeof window !== 'undefined') {
    try {
      const storedSignature = window.localStorage.getItem(SIGNATURE_KEY)
      if (window.localStorage.getItem(SOURCE_KEY) !== SOURCE_USER || storedSignature !== currentConfigSignature()) {
        return normalizeTablePageSize(configuredDefault)
      }
      const stored = window.localStorage.getItem(STORAGE_KEY)
      if (stored !== null) {
        const parsed = Number(stored)
        if (Number.isFinite(parsed)) {
          return normalizeTablePageSize(parsed)
        }
      }
    } catch (error) {
      console.warn('Failed to read persisted page size:', error)
    }
  }
  return normalizeTablePageSize(configuredDefault)
}

export function setPersistedPageSize(size: number): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_KEY, String(size))
    window.localStorage.setItem(SOURCE_KEY, SOURCE_USER)
    window.localStorage.setItem(SIGNATURE_KEY, currentConfigSignature())
  } catch (error) {
    console.warn('Failed to persist page size:', error)
  }
}
