import { describe, expect, it } from 'bun:test'
import { DEFAULT_LOCALE, normalizeLocale, SUPPORTED_LOCALES } from './i18n'

describe('dashboard i18n locale handling', () => {
  it('supports every language exposed by Settings', () => {
    expect(SUPPORTED_LOCALES).toEqual(['en', 'es', 'de', 'ja', 'ko', 'zh-CN', 'zh-TW'])
  })

  it('normalizes Chinese and rejects unsupported locales safely', () => {
    expect(normalizeLocale('zh')).toBe('zh-CN')
    expect(normalizeLocale('zh-TW')).toBe('zh-TW')
    expect(normalizeLocale('es')).toBe('es')
    expect(normalizeLocale('xx')).toBe(DEFAULT_LOCALE)
    expect(normalizeLocale(undefined)).toBe(DEFAULT_LOCALE)
  })
})
