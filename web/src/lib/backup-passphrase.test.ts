import { describe, expect, it } from 'bun:test'
import { assessBackupPassphrase, backupPassphrasesMatch, BACKUP_PASSPHRASE_MIN_LENGTH } from './backup-passphrase'

describe('backup passphrase guidance', () => {
  it('requires at least 12 characters', () => {
    expect(BACKUP_PASSPHRASE_MIN_LENGTH).toBe(12)
    expect(assessBackupPassphrase('12345678901').valid).toBe(false)
    expect(assessBackupPassphrase('123456789012').valid).toBe(true)
  })

  it('reports character count and a strength estimate', () => {
    const weak = assessBackupPassphrase('123456789012')
    expect(weak.length).toBe(12)
    expect(weak.label).toBe('Weak')

    const strong = assessBackupPassphrase('MiRespaldo-2026!Seguro')
    expect(strong.valid).toBe(true)
    expect(strong.label).toBe('Strong')
    expect(strong.percent).toBe(100)
  })

  it('recognizes the optional strength hints independently', () => {
    const assessment = assessBackupPassphrase('LongPhrase2026!')
    expect(assessment.hasMixedCase).toBe(true)
    expect(assessment.hasNumber).toBe(true)
    expect(assessment.hasSymbol).toBe(true)
    expect(assessment.isLong).toBe(false)
  })

  it('requires the confirmation to exactly match a valid passphrase', () => {
    expect(backupPassphrasesMatch('MiRespaldo2026!', 'MiRespaldo2026!')).toBe(true)
    expect(backupPassphrasesMatch('MiRespaldo2026!', 'MiRespaldo2026?')).toBe(false)
    expect(backupPassphrasesMatch('12345678901', '12345678901')).toBe(false)
    expect(backupPassphrasesMatch('MiRespaldo2026!', '')).toBe(false)
  })
})
