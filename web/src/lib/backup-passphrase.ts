export const BACKUP_PASSPHRASE_MIN_LENGTH = 12

export interface BackupPassphraseAssessment {
  length: number
  valid: boolean
  score: 0 | 1 | 2 | 3 | 4
  label: 'Not set' | 'Weak' | 'Fair' | 'Good' | 'Strong'
  percent: number
  hasMixedCase: boolean
  hasNumber: boolean
  hasSymbol: boolean
  isLong: boolean
}

export function assessBackupPassphrase(value: string): BackupPassphraseAssessment {
  const length = value.length
  const hasLower = /[a-z]/.test(value)
  const hasUpper = /[A-Z]/.test(value)
  const hasMixedCase = hasLower && hasUpper
  const hasNumber = /\d/.test(value)
  const hasSymbol = /[^A-Za-z0-9\s]/.test(value)
  const isLong = length >= 16

  if (length === 0) {
    return {
      length,
      valid: false,
      score: 0,
      label: 'Not set',
      percent: 0,
      hasMixedCase,
      hasNumber,
      hasSymbol,
      isLong,
    }
  }

  let points = 0
  if (length >= BACKUP_PASSPHRASE_MIN_LENGTH) points += 1
  if (isLong) points += 1

  const variety = Number(hasMixedCase) + Number(hasNumber) + Number(hasSymbol)
  if (variety >= 2) points += 1
  if (length >= 20 || variety === 3) points += 1

  const score = Math.min(4, Math.max(1, points || 1)) as 1 | 2 | 3 | 4
  const labels: Record<1 | 2 | 3 | 4, BackupPassphraseAssessment['label']> = {
    1: 'Weak',
    2: 'Fair',
    3: 'Good',
    4: 'Strong',
  }

  return {
    length,
    valid: length >= BACKUP_PASSPHRASE_MIN_LENGTH,
    score,
    label: labels[score],
    percent: score * 25,
    hasMixedCase,
    hasNumber,
    hasSymbol,
    isLong,
  }
}


export function backupPassphrasesMatch(passphrase: string, confirmation: string): boolean {
  return passphrase.length >= BACKUP_PASSPHRASE_MIN_LENGTH && confirmation === passphrase
}
