<script lang="ts">
  import { onMount } from 'svelte'
  import {
    AlertCircle,
    Check,
    Cloud,
    Database,
    Download,
    Eye,
    EyeOff,
    Globe,
    Key,
    Laptop,
    Loader2,
    Lock,
    RefreshCw,
    Save,
    Shield,
    Sliders,
    Upload,
    User,
    Zap
  } from 'lucide-svelte'
  import Card from '../lib/ui/Card.svelte'
  import Toggle from '../lib/ui/Toggle.svelte'
  import Modal from '../lib/ui/Modal.svelte'
  import Button from '../lib/ui/Button.svelte'
  import { api, getAuthHeaders, responseErrorMessage, type Settings } from '../api/client'
  import { clearCallback, dashboardCallbackURL, readCallback } from '../lib/oauth-handoff'
  import { assessBackupPassphrase, backupPassphrasesMatch, BACKUP_PASSPHRASE_MIN_LENGTH } from '../lib/backup-passphrase'
  import { setRuntimeLocale } from '../lib/i18n'

  interface Props {
    settings?: Settings
    onRefresh?: () => void
  }

  interface DriveBackupFile {
    id: string
    name: string
    createdTime: string
    size: string
  }

  let {
    settings = {},
    onRefresh
  }: Props = $props()

  // General Settings State
  let requireLogin = $state(false)
  let sessionTimeout = $state('24h')
  let selectedLanguage = $state('en')
  let isApplyingLanguage = $state(false)
  let enableObservability = $state(false)

  // Routing Strategy State
  let fallbackStrategy = $state('failover')
  let stickyRoundRobinLimit = $state(3)
  let comboStrategy = $state('first-model')

  // Password Management State
  let currentPassword = $state('')
  let newPassword = $state('')
  let confirmNewPassword = $state('')
  let isUpdatingPassword = $state(false)
  let passwordSuccessMessage = $state<string | null>(null)
  let passwordErrorMessage = $state<string | null>(null)
  let showPasswordFields = $state(false)

  // SSO State
  let authMode = $state<'password' | 'oidc' | 'saml'>('password')
  let oidcIssuerUrl = $state('')
  let oidcClientId = $state('')
  let oidcScopes = $state('openid profile email')
  let oidcLoginLabel = $state('Sign in with OIDC')

  let samlEntryPoint = $state('')
  let samlIssuer = $state('')
  let samlCert = $state('')
  let samlLoginLabel = $state('Sign in with SAML SSO')

  // Save states
  let isSavingSettings = $state(false)
  let saveSuccess = $state(false)

  // Database Backup / Import
  let isDownloadingBackup = $state(false)
  let isImportingBackup = $state(false)
  let isUploadingDriveBackup = $state(false)
  let isConnectingDrive = $state(false)
  let isLoadingDriveBackups = $state(false)
  let isRestoringDriveBackup = $state(false)
  let driveBackupConfigured = $state(false)
  let driveBackupConnected = $state(false)
  let driveBackupStatusLoaded = $state(false)
  let driveBackupFiles = $state<DriveBackupFile[]>([])
  let selectedDriveBackup = $state<DriveBackupFile | null>(null)
  let fileInput: HTMLInputElement | null = $state(null)
  let dbPassword = $state('')
  let backupPassphrase = $state('')
  let backupPassphraseConfirmation = $state('')
  let showDbPassword = $state(false)
  let showBackupPassphrase = $state(false)
  let showBackupPassphraseConfirmation = $state(false)
  let backupPassphraseInfo = $derived(assessBackupPassphrase(backupPassphrase))
  let backupPassphrasesMatchState = $derived(backupPassphrasesMatch(backupPassphrase, backupPassphraseConfirmation))
  let dbAuthOpen = $state(false)
  let pendingImportFile: File | null = $state(null)
  let backupAction = $state<'download' | 'drive' | 'drive-restore' | 'connect' | 'import' | null>(null)

  $effect(() => {
    if (settings) {
      requireLogin = !!settings.requireLogin
      enableObservability = !!settings.enableObservability
      if (typeof settings.sessionTimeout === 'string') sessionTimeout = settings.sessionTimeout
      if (typeof settings.language === 'string') selectedLanguage = settings.language
      if (typeof settings.fallbackStrategy === 'string') fallbackStrategy = settings.fallbackStrategy
      if (typeof settings.stickyRoundRobinLimit === 'number') stickyRoundRobinLimit = settings.stickyRoundRobinLimit
      if (typeof settings.comboStrategy === 'string') comboStrategy = settings.comboStrategy

      // SSO
      if (settings.authMode === 'oidc' || settings.authMode === 'saml') authMode = settings.authMode
      if (typeof settings.oidcIssuerUrl === 'string') oidcIssuerUrl = settings.oidcIssuerUrl
      if (typeof settings.oidcClientId === 'string') oidcClientId = settings.oidcClientId
      if (typeof settings.oidcScopes === 'string') oidcScopes = settings.oidcScopes
      if (typeof settings.oidcLoginLabel === 'string') oidcLoginLabel = settings.oidcLoginLabel

      if (typeof settings.samlEntryPoint === 'string') samlEntryPoint = settings.samlEntryPoint
      if (typeof settings.samlIssuer === 'string') samlIssuer = settings.samlIssuer
      if (typeof settings.samlCert === 'string') samlCert = settings.samlCert
      if (typeof settings.samlLoginLabel === 'string') samlLoginLabel = settings.samlLoginLabel
    }
  })

  async function loadSettings() {
    try {
      const s = await api.getSettings()
      if (s) {
        requireLogin = !!s.requireLogin
        enableObservability = !!s.enableObservability
        if (s.sessionTimeout) sessionTimeout = String(s.sessionTimeout)
        if (s.language) selectedLanguage = String(s.language)
        if (s.fallbackStrategy) fallbackStrategy = String(s.fallbackStrategy)
        if (typeof s.stickyRoundRobinLimit === 'number') stickyRoundRobinLimit = s.stickyRoundRobinLimit
        if (s.comboStrategy) comboStrategy = String(s.comboStrategy)

        if (s.authMode === 'oidc' || s.authMode === 'saml') authMode = s.authMode
        if (s.oidcIssuerUrl) oidcIssuerUrl = String(s.oidcIssuerUrl)
        if (s.oidcClientId) oidcClientId = String(s.oidcClientId)
        if (s.oidcScopes) oidcScopes = String(s.oidcScopes)
        if (s.oidcLoginLabel) oidcLoginLabel = String(s.oidcLoginLabel)

        if (s.samlEntryPoint) samlEntryPoint = String(s.samlEntryPoint)
        if (s.samlIssuer) samlIssuer = String(s.samlIssuer)
        if (s.samlCert) samlCert = String(s.samlCert)
        if (s.samlLoginLabel) samlLoginLabel = String(s.samlLoginLabel)
      }
    } catch {
      // silent
    }
  }

  async function loadDriveBackupStatus() {
    try {
      const res = await fetch('/api/settings/backup/google/status', { headers: getAuthHeaders() })
      if (res.ok) {
        const data = await res.json()
        driveBackupConfigured = data?.configured === true
        driveBackupConnected = data?.connected === true
      }
    } catch {
      driveBackupConfigured = false
      driveBackupConnected = false
    } finally {
      driveBackupStatusLoaded = true
    }
  }

  onMount(() => {
    loadSettings()
    loadDriveBackupStatus()
  })

  async function handleLanguageSelection(event: Event) {
    const next = (event.currentTarget as HTMLSelectElement).value
    const previous = selectedLanguage
    if (next === previous || isApplyingLanguage) return

    selectedLanguage = next
    isApplyingLanguage = true
    try {
      // Apply first so the choice feels immediate; persist right after.
      await setRuntimeLocale(next)
      await api.updateSettings({ language: next })
      onRefresh?.()
    } catch (err) {
      selectedLanguage = previous
      try { await setRuntimeLocale(previous) } catch {}
      alert(`Failed to change language: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isApplyingLanguage = false
    }
  }

  async function handleSaveAll() {
    isSavingSettings = true
    saveSuccess = false
    try {
      await api.updateSettings({
        requireLogin,
        sessionTimeout,
        language: selectedLanguage,
        enableObservability,
        fallbackStrategy,
        stickyRoundRobinLimit,
        comboStrategy,
        authMode,
        oidcIssuerUrl,
        oidcClientId,
        oidcScopes,
        oidcLoginLabel,
        samlEntryPoint,
        samlIssuer,
        samlCert,
        samlLoginLabel,
      })
      saveSuccess = true
      onRefresh?.()
      setTimeout(() => (saveSuccess = false), 3000)
    } catch (err) {
      alert(`Failed to save settings: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isSavingSettings = false
    }
  }

  async function handleUpdatePassword(e: SubmitEvent) {
    e.preventDefault()
    passwordErrorMessage = null
    passwordSuccessMessage = null

    if (newPassword !== confirmNewPassword) {
      passwordErrorMessage = 'Passwords do not match'
      return
    }
    if (newPassword.length < 6) {
      passwordErrorMessage = 'Password must be at least 6 characters'
      return
    }

    isUpdatingPassword = true
    try {
      await api.updateSettings({
        currentPassword,
        newPassword,
      })
      passwordSuccessMessage = 'Password updated successfully!'
      currentPassword = ''
      newPassword = ''
      confirmNewPassword = ''
      showPasswordFields = false
      onRefresh?.()
    } catch (err) {
      passwordErrorMessage = err instanceof Error ? err.message : 'Failed to update password'
    } finally {
      isUpdatingPassword = false
    }
  }

  // closeDbAuth is the single dismissal path for the password modal. Modal
  // wires Escape, the overlay and both close buttons to onClose.
  function backupBusy() {
    return isImportingBackup || isDownloadingBackup || isUploadingDriveBackup ||
      isConnectingDrive || isLoadingDriveBackups || isRestoringDriveBackup
  }

  function resetBackupAuthState() {
    dbPassword = ''
    backupPassphrase = ''
    backupPassphraseConfirmation = ''
    showDbPassword = false
    showBackupPassphrase = false
    showBackupPassphraseConfirmation = false
    pendingImportFile = null
    driveBackupFiles = []
    selectedDriveBackup = null
    backupAction = null
  }

  function closeDbAuth() {
    if (backupBusy()) return
    dbAuthOpen = false
    resetBackupAuthState()
  }

  function openBackupAuth(action: 'download' | 'drive' | 'drive-restore' | 'connect') {
    if (backupBusy()) return
    resetBackupAuthState()
    backupAction = action
    dbAuthOpen = true
  }

  function handleDownloadBackup() {
    openBackupAuth('download')
  }

  function handleConnectDrive() {
    if (!driveBackupConfigured) return
    openBackupAuth('connect')
  }

  function handleDriveBackup() {
    if (!driveBackupConnected) return
    openBackupAuth('drive')
  }

  function handleDriveRestore() {
    if (!driveBackupConnected) return
    openBackupAuth('drive-restore')
  }

  function encryptedRestoreSelected() {
    const file = pendingImportFile
    if (!file) return false
    const lowerName = file.name.toLowerCase()
    return lowerName.endsWith('.9rbak') || file.type === 'application/vnd.9router.backup'
  }

  function backupPassphraseRequired() {
    if (backupAction === 'connect') return false
    if (backupAction === 'drive-restore') return selectedDriveBackup !== null
    return backupAction !== 'import' || encryptedRestoreSelected()
  }

  function creatingEncryptedBackup() {
    return backupAction === 'download' || backupAction === 'drive'
  }

  function backupPassphraseValid() {
    if (!backupPassphraseRequired()) return true
    if (!backupPassphraseInfo.valid) return false
    if (backupAction === 'import' || backupAction === 'drive-restore') return true
    return backupPassphrasesMatchState
  }

  function pendingBackupActionValid() {
    if (!dbPassword.trim()) return false
    if (backupAction === 'drive-restore' && selectedDriveBackup === null) return true
    return backupPassphraseValid()
  }

  function backupFileName() {
    const stamp = new Date().toISOString().replace(/[.:]/g, '-')
    return `9router-backup-${stamp}.9rbak`
  }

  async function fetchEncryptedBackup(password: string, passphrase: string): Promise<{ blob: Blob; name: string }> {
    const res = await fetch('/api/settings/database', {
      headers: {
        ...getAuthHeaders(),
        'x-9r-password': password,
        'x-9r-backup-passphrase': passphrase,
        'Accept': 'application/vnd.9router.backup',
      },
    })
    if (!res.ok) {
      throw new Error(await responseErrorMessage(res, 'Failed to export database'))
    }
    return { blob: await res.blob(), name: backupFileName() }
  }

  function downloadBackupBlob(blob: Blob, name: string) {
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = name
    document.body.appendChild(a)
    a.click()
    setTimeout(() => {
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    }, 1000)
  }

  async function runDownloadBackup() {
    isDownloadingBackup = true
    const password = dbPassword
    const passphrase = backupPassphrase
    if (!backupPassphraseValid()) {
      isDownloadingBackup = false
      return
    }
    try {
      const { blob, name } = await fetchEncryptedBackup(password, passphrase)
      downloadBackupBlob(blob, name)
    } catch (err) {
      alert(`Failed to download backup: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isDownloadingBackup = false
      dbAuthOpen = false
      resetBackupAuthState()
    }
  }

  async function waitForDriveCallback(state: string): Promise<string> {
    const deadline = Date.now() + 10 * 60 * 1000
    while (Date.now() < deadline) {
      const cb = readCallback(window.localStorage)
      if (cb && cb.state === state) {
        clearCallback(window.localStorage)
        if (cb.error) {
          throw new Error(cb.errorDesc ? `${cb.error}: ${cb.errorDesc}` : cb.error)
        }
        if (cb.raw) return cb.raw
      }
      await new Promise((resolve) => setTimeout(resolve, 500))
    }
    throw new Error('Google Drive authorization timed out')
  }

  async function runConnectDrive() {
    const password = dbPassword
    const popup = window.open('about:blank', '_blank', 'width=600,height=700')
    isConnectingDrive = true
    try {
      clearCallback(window.localStorage)
      const redirectUri = dashboardCallbackURL(window.location.origin)
      const authRes = await fetch(
        `/api/settings/backup/google/authorize?redirect_uri=${encodeURIComponent(redirectUri)}`,
        { headers: { ...getAuthHeaders(), 'x-9r-password': password } },
      )
      if (!authRes.ok) {
        throw new Error(await responseErrorMessage(authRes, 'Google Drive is not configured'))
      }
      const auth = await authRes.json()
      if (!auth?.authUrl || !auth?.state) throw new Error('Google Drive authorization response was incomplete')

      if (popup && !popup.closed) popup.location.replace(auth.authUrl)
      const code = await waitForDriveCallback(auth.state)
      try { popup?.close() } catch {}

      const connectRes = await fetch('/api/settings/backup/google/connect', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'x-9r-password': password,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          code,
          redirectUri: auth.redirectUri || redirectUri,
          state: auth.state,
        }),
      })
      if (!connectRes.ok) {
        throw new Error(await responseErrorMessage(connectRes, 'Failed to connect Google Drive'))
      }
      driveBackupConnected = true
      await loadDriveBackupStatus()
    } catch (err) {
      try { popup?.close() } catch {}
      alert(`Google Drive connection failed: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isConnectingDrive = false
      dbAuthOpen = false
      resetBackupAuthState()
    }
  }

  async function runDriveBackup() {
    const password = dbPassword
    const passphrase = backupPassphrase
    if (!backupPassphraseValid()) return
    isUploadingDriveBackup = true
    try {
      const uploadRes = await fetch('/api/settings/backup/google/upload', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'x-9r-password': password,
          'x-9r-backup-passphrase': passphrase,
          'Content-Type': 'application/json',
        },
        body: '{}',
      })
      if (!uploadRes.ok) {
        throw new Error(await responseErrorMessage(uploadRes, 'Failed to upload encrypted backup to Google Drive'))
      }
      const uploaded = await uploadRes.json()
      alert(`Backup saved to Google Drive: ${uploaded?.name || 'backup'}`)
    } catch (err) {
      alert(`Google Drive backup failed: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isUploadingDriveBackup = false
      dbAuthOpen = false
      resetBackupAuthState()
    }
  }

  async function loadDriveBackupsForRestore() {
    if (!dbPassword.trim()) return
    isLoadingDriveBackups = true
    try {
      const res = await fetch('/api/settings/backup/google/files', {
        headers: { ...getAuthHeaders(), 'x-9r-password': dbPassword },
      })
      if (!res.ok) throw new Error(await responseErrorMessage(res, 'Failed to load Google Drive backups'))
      const data = await res.json()
      driveBackupFiles = Array.isArray(data?.files) ? data.files : []
      selectedDriveBackup = driveBackupFiles[0] || null
    } catch (err) {
      alert(`Failed to load Google Drive backups: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isLoadingDriveBackups = false
    }
  }

  async function runDriveRestore() {
    if (!selectedDriveBackup || !backupPassphraseInfo.valid) return
    isRestoringDriveBackup = true
    try {
      const res = await fetch('/api/settings/backup/google/restore', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'x-9r-password': dbPassword,
          'x-9r-backup-passphrase': backupPassphrase,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ fileId: selectedDriveBackup.id }),
      })
      if (!res.ok) throw new Error(await responseErrorMessage(res, 'Failed to restore Google Drive backup'))
      alert(`Backup restored from Google Drive: ${selectedDriveBackup.name}. Reloading page...`)
      window.location.reload()
    } catch (err) {
      alert(`Google Drive restore failed: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isRestoringDriveBackup = false
    }
  }

  function runPendingBackupAction() {
    if (backupAction === 'connect') {
      void runConnectDrive()
    } else if (backupAction === 'drive') {
      void runDriveBackup()
    } else if (backupAction === 'drive-restore') {
      if (selectedDriveBackup) {
        void runDriveRestore()
      } else {
        void loadDriveBackupsForRestore()
      }
    } else if (backupAction === 'import') {
      void runImportBackup()
    } else {
      void runDownloadBackup()
    }
  }

  function handleFileSelected(e: Event) {
    if (isImportingBackup || isDownloadingBackup) return
    const target = e.target as HTMLInputElement
    const file = target.files?.[0]
    if (!file) return

    target.value = ''
    pendingImportFile = file
    backupAction = 'import'
    dbPassword = ''
    backupPassphrase = ''
    backupPassphraseConfirmation = ''
    showDbPassword = false
    showBackupPassphrase = false
    showBackupPassphraseConfirmation = false
    dbAuthOpen = true
  }

  async function runImportBackup() {
    const file = pendingImportFile
    if (!file) return
    // Snapshot the password before the first await: file.text() and
    // JSON.parse() can both yield, and reading live state afterwards would let a
    // concurrent dismissal blank the credential the request is authorized with.
    const password = dbPassword
    const passphrase = backupPassphrase
    const lowerNameForPassphrase = file.name.toLowerCase()
    if (lowerNameForPassphrase.endsWith('.9rbak') && !backupPassphraseInfo.valid) return
    isImportingBackup = true
    try {
      let res: Response
      const lowerName = file.name.toLowerCase()
      const isEncrypted = lowerName.endsWith('.9rbak') || file.type === 'application/vnd.9router.backup'
      const isZip = lowerName.endsWith('.zip') || file.type === 'application/zip'
      if (isEncrypted || isZip) {
        const buffer = await file.arrayBuffer()
        res = await fetch('/api/settings/database', {
          method: 'POST',
          headers: {
            ...getAuthHeaders(),
            'x-9r-password': password,
            'x-9r-backup-passphrase': passphrase,
            'Content-Type': isEncrypted ? 'application/vnd.9router.backup' : 'application/zip',
          },
          body: buffer,
        })
      } else {
        const raw = await file.text()
        const payload = JSON.parse(raw)
        res = await fetch('/api/settings/database', {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'x-9r-password': password, 'x-9r-backup-passphrase': passphrase, 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        })
      }
      if (!res.ok) {
        throw new Error(await responseErrorMessage(res, 'Failed to import database'))
      }
      alert('Database backup imported successfully! Reloading page...')
      window.location.reload()
    } catch (err) {
      alert(`Failed to import database: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isImportingBackup = false
      pendingImportFile = null
      dbAuthOpen = false
      dbPassword = ''
      backupPassphrase = ''
      backupPassphraseConfirmation = ''
      backupAction = null
    }
  }
</script>

<div class="flex flex-col gap-6">
  <!-- PAGE HEADER & SAVE BUTTON -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
    <div class="space-y-1">
      <div class="flex items-center gap-2">
        <div class="p-2 rounded-lg bg-brand-500/10 text-brand-500">
          <User class="w-5 h-5" />
        </div>
        <div>
          <h1 class="font-headline text-2xl sm:text-3xl font-bold text-text-main tracking-tight flex items-center gap-2">
            Settings & Profile
          </h1>
          <p class="font-body text-xs sm:text-sm text-text-muted">
            System configuration, authentication credentials, and local data storage
          </p>
        </div>
      </div>
    </div>

    <button
      type="button"
      onclick={handleSaveAll}
      disabled={isSavingSettings}
      class="flex items-center gap-1.5 px-4 py-2 rounded-lg bg-brand-500 hover:bg-brand-600 disabled:opacity-50 text-white font-semibold text-xs transition cursor-pointer shadow-md shadow-brand-500/20"
    >
      {#if isSavingSettings}
        <Loader2 class="w-3.5 h-3.5 animate-spin" />
        <span>Saving...</span>
      {:else if saveSuccess}
        <Check class="w-3.5 h-3.5" />
        <span>Settings Saved!</span>
      {:else}
        <Save class="w-3.5 h-3.5" />
        <span>Save Changes</span>
      {/if}
    </button>
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-2 gap-5">
    <!-- SECTION 1: Backup & Recovery -->
    <Card padding="md" class="space-y-4">
      <div class="flex items-center justify-between pb-2 border-b border-border">
        <div class="flex items-center gap-2">
          <Cloud class="w-4 h-4 text-brand-500" />
          <h2 class="text-sm font-bold text-text-main">Google Drive Backup</h2>
        </div>
        {#if !driveBackupStatusLoaded}
          <span class="text-[10px] font-semibold text-text-muted">Checking…</span>
        {:else if driveBackupConnected}
          <span class="text-[10px] font-bold px-2 py-0.5 rounded bg-success/10 text-success border border-success/20">Connected</span>
        {:else}
          <span class="text-[10px] font-bold px-2 py-0.5 rounded bg-surface-2 text-text-muted border border-border">Not connected</span>
        {/if}
      </div>

      <div class="space-y-3 text-xs">
        <p class="text-[11px] text-text-muted leading-relaxed">
          Encrypted backups are stored in <strong class="text-text-main">9router Backups</strong> on Google Drive. Only the newest 5 app-created backups are kept.
        </p>

        {#if !driveBackupStatusLoaded}
          <div class="flex items-center gap-2 text-text-muted">
            <Loader2 class="w-4 h-4 animate-spin" />
            <span>Checking Google Drive…</span>
          </div>
        {:else if !driveBackupConfigured}
          <div class="p-3 rounded-xl border border-warning/30 bg-warning/5 text-warning">
            Google Drive OAuth is not configured in this build yet.
          </div>
        {:else if !driveBackupConnected}
          <button
            type="button"
            onclick={handleConnectDrive}
            disabled={backupBusy()}
            class="w-full py-2.5 px-3 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-xs font-semibold transition cursor-pointer flex items-center justify-center gap-2 disabled:opacity-50"
          >
            <Cloud class="w-4 h-4" />
            <span>Connect Google Drive</span>
          </button>
          <p class="text-[10px] text-text-subtle text-center">
            You authorize Google once. 9router-go stores a refresh token locally so later backups are one tap.
          </p>
        {:else}
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <button
              type="button"
              onclick={handleDriveBackup}
              disabled={backupBusy()}
              class="py-2.5 px-3 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-xs font-semibold transition cursor-pointer flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              <Cloud class="w-3.5 h-3.5" />
              <span>Create backup now</span>
            </button>
            <button
              type="button"
              onclick={handleDriveRestore}
              disabled={backupBusy()}
              class="py-2.5 px-3 rounded-lg bg-surface-2 hover:bg-surface-3 border border-border text-xs font-semibold text-text-main transition cursor-pointer flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              <RefreshCw class="w-3.5 h-3.5 text-info" />
              <span>Restore from Google Drive</span>
            </button>
          </div>
        {/if}

        <details class="pt-2 border-t border-border/60">
          <summary class="cursor-pointer text-[11px] font-semibold text-text-muted hover:text-text-main">Advanced: local file export/import</summary>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 pt-3">
            <button
              type="button"
              onclick={handleDownloadBackup}
              disabled={backupBusy()}
              class="py-2 px-3 rounded-lg bg-surface-2 hover:bg-surface-3 border border-border text-xs font-semibold text-text-main transition cursor-pointer flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              <Download class="w-3.5 h-3.5 text-brand-500" />
              <span>Export encrypted file</span>
            </button>

            <input
              type="file"
              accept=".9rbak,.zip,.json,application/vnd.9router.backup,application/zip,application/json"
              bind:this={fileInput}
              onchange={handleFileSelected}
              class="hidden"
            />

            <button
              type="button"
              onclick={() => fileInput?.click()}
              disabled={backupBusy()}
              class="py-2 px-3 rounded-lg bg-surface-2 hover:bg-surface-3 border border-border text-xs font-semibold text-text-main transition cursor-pointer flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              <Upload class="w-3.5 h-3.5 text-text-muted" />
              <span>Import backup file</span>
            </button>
          </div>
        </details>
      </div>
    </Card>

    <!-- SECTION 2: Language & Region -->
    <Card padding="md" class="space-y-4">
      <div class="flex items-center gap-2 pb-2 border-b border-border">
        <Globe class="w-4 h-4 text-brand-500" />
        <h2 class="text-sm font-bold text-text-main">Language & Display</h2>
      </div>

      <div class="space-y-3 text-xs">
        <div class="space-y-1">
          <label for="lang-select" class="block font-semibold text-text-main">Dashboard Language</label>
          <select
            id="lang-select"
            value={selectedLanguage}
            onchange={handleLanguageSelection}
            disabled={isApplyingLanguage}
            data-i18n-skip="true"
            class="w-full px-3 py-2 rounded-lg bg-bg border border-border text-xs text-text-main focus:outline-none focus:border-brand-500 disabled:opacity-60"
          >
            <option value="en">English (US)</option>
            <option value="zh-CN">简体中文 (Simplified Chinese)</option>
            <option value="zh-TW">繁體中文 (Traditional Chinese)</option>
            <option value="ja">日本語 (Japanese)</option>
            <option value="ko">한국어 (Korean)</option>
            <option value="es">Español</option>
            <option value="de">Deutsch</option>
          </select>
          <p class="text-[11px] text-text-subtle flex items-center gap-1.5">
            <span>Applies immediately and is saved automatically.</span>
            {#if isApplyingLanguage}<Loader2 class="w-3 h-3 animate-spin" />{/if}
          </p>
        </div>
      </div>
    </Card>

    <!-- SECTION 3: Security & Master Password -->
    <Card padding="md" class="space-y-4">
      <div class="flex items-center justify-between pb-2 border-b border-border">
        <div class="flex items-center gap-2">
          <Shield class="w-4 h-4 text-brand-500" />
          <h2 class="text-sm font-bold text-text-main">Security & Password</h2>
        </div>
        <Toggle
          checked={requireLogin}
          size="sm"
          label="Require login"
          onChange={(val) => (requireLogin = val)}
        />
      </div>

      <div class="space-y-3 text-xs">
        <div class="flex items-center justify-between">
          <div>
            <p class="font-semibold text-text-main">Require Password on Localhost</p>
            <p class="text-[11px] text-text-subtle">Default password is <code class="font-mono text-brand-500">123456</code></p>
          </div>
        </div>

        <div class="space-y-1 pt-1">
          <label for="session-timeout" class="block font-semibold text-text-main">Session Timeout</label>
          <select
            id="session-timeout"
            bind:value={sessionTimeout}
            class="w-full px-3 py-2 rounded-lg bg-bg border border-border text-xs text-text-main focus:outline-none focus:border-brand-500"
          >
            <option value="15m">15 Minutes</option>
            <option value="1h">1 Hour</option>
            <option value="24h">24 Hours</option>
            <option value="7d">7 Days</option>
            <option value="never">Never (Stay signed in)</option>
          </select>
        </div>

        <!-- Update Password Toggle & Form -->
        <div class="pt-2 border-t border-border/60">
          <button
            type="button"
            onclick={() => (showPasswordFields = !showPasswordFields)}
            class="text-xs font-semibold text-brand-500 hover:opacity-80 cursor-pointer"
          >
            {showPasswordFields ? 'Hide Change Password' : 'Change Master Password'}
          </button>

          {#if showPasswordFields}
            <form onsubmit={handleUpdatePassword} class="space-y-2.5 pt-3">
              {#if passwordErrorMessage}
                <div class="p-2.5 rounded-lg bg-danger/10 border border-danger/20 text-danger text-[11px]">
                  {passwordErrorMessage}
                </div>
              {/if}
              {#if passwordSuccessMessage}
                <div class="p-2.5 rounded-lg bg-success/10 border border-success/20 text-success text-[11px]">
                  {passwordSuccessMessage}
                </div>
              {/if}

              <div class="space-y-1">
                <label for="curr-pwd" class="block text-[11px] font-semibold text-text-muted">Current Password</label>
                <input
                  id="curr-pwd"
                  type="password"
                  bind:value={currentPassword}
                  placeholder="123456"
                  class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main focus:outline-none focus:border-brand-500"
                  required
                />
              </div>

              <div class="space-y-1">
                <label for="new-pwd" class="block text-[11px] font-semibold text-text-muted">New Password</label>
                <input
                  id="new-pwd"
                  type="password"
                  bind:value={newPassword}
                  placeholder="At least 6 characters"
                  class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main focus:outline-none focus:border-brand-500"
                  required
                />
              </div>

              <div class="space-y-1">
                <label for="confirm-pwd" class="block text-[11px] font-semibold text-text-muted">Confirm New Password</label>
                <input
                  id="confirm-pwd"
                  type="password"
                  bind:value={confirmNewPassword}
                  placeholder="Re-enter new password"
                  class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main focus:outline-none focus:border-brand-500"
                  required
                />
              </div>

              <button
                type="submit"
                disabled={isUpdatingPassword}
                class="w-full py-2 px-3 rounded-lg bg-brand-500 hover:bg-brand-600 text-white font-semibold text-xs transition cursor-pointer"
              >
                {isUpdatingPassword ? 'Updating...' : 'Update Password'}
              </button>
            </form>
          {/if}
        </div>
      </div>
    </Card>

    <!-- SECTION 4: Single Sign-On (SSO) -->
    <Card padding="md" class="space-y-4">
      <div class="flex items-center justify-between pb-2 border-b border-border">
        <div class="flex items-center gap-2">
          <Key class="w-4 h-4 text-brand-500" />
          <h2 class="text-sm font-bold text-text-main">Single Sign-On (SSO)</h2>
        </div>
      </div>

      <div class="space-y-3 text-xs">
        <div class="space-y-1">
          <label for="auth-mode-select" class="block font-semibold text-text-main">Authentication Mode</label>
          <select
            id="auth-mode-select"
            bind:value={authMode}
            class="w-full px-3 py-2 rounded-lg bg-bg border border-border text-xs text-text-main focus:outline-none focus:border-brand-500"
          >
            <option value="password">Password Only</option>
            <option value="oidc">OpenID Connect (OIDC)</option>
            <option value="saml">SAML 2.0 SSO</option>
          </select>
        </div>

        {#if authMode === 'oidc'}
          <div class="space-y-2 pt-2 border-t border-border/60">
            <div class="space-y-1">
              <label for="oidc-issuer" class="block font-semibold text-text-muted">Issuer URL</label>
              <input
                id="oidc-issuer"
                type="text"
                bind:value={oidcIssuerUrl}
                placeholder="https://accounts.google.com or Okta URL"
                class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main"
              />
            </div>
            <div class="space-y-1">
              <label for="oidc-client-id" class="block font-semibold text-text-muted">Client ID</label>
              <input
                id="oidc-client-id"
                type="text"
                bind:value={oidcClientId}
                placeholder="client-id"
                class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main"
              />
            </div>
            <div class="space-y-1">
              <label for="oidc-scopes" class="block font-semibold text-text-muted">Scopes</label>
              <input
                id="oidc-scopes"
                type="text"
                bind:value={oidcScopes}
                placeholder="openid profile email"
                class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main"
              />
            </div>
          </div>
        {:else if authMode === 'saml'}
          <div class="space-y-2 pt-2 border-t border-border/60">
            <div class="space-y-1">
              <label for="saml-entrypoint" class="block font-semibold text-text-muted">SAML EntryPoint (SSO URL)</label>
              <input
                id="saml-entrypoint"
                type="text"
                bind:value={samlEntryPoint}
                placeholder="https://idp.example.com/sso"
                class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main"
              />
            </div>
            <div class="space-y-1">
              <label for="saml-issuer" class="block font-semibold text-text-muted">SP Entity ID / Issuer</label>
              <input
                id="saml-issuer"
                type="text"
                bind:value={samlIssuer}
                placeholder="https://9router.local"
                class="w-full px-3 py-1.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main"
              />
            </div>
            <div class="space-y-1">
              <label for="saml-cert" class="block font-semibold text-text-muted">X.509 Certificate (PEM)</label>
              <textarea
                id="saml-cert"
                bind:value={samlCert}
                rows={3}
                placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                class="w-full p-2.5 rounded-lg bg-bg border border-border text-xs font-mono text-text-main"
              ></textarea>
            </div>
          </div>
        {/if}
      </div>
    </Card>

    <!-- SECTION 5: Routing Strategy & Limits -->
    <Card padding="md" class="space-y-4">
      <div class="flex items-center gap-2 pb-2 border-b border-border">
        <Sliders class="w-4 h-4 text-brand-500" />
        <h2 class="text-sm font-bold text-text-main">Default Routing Strategy</h2>
      </div>

      <div class="space-y-3 text-xs">
        <div class="space-y-1">
          <label for="fallback-strat" class="block font-semibold text-text-main">Connection Fallback Strategy</label>
          <select
            id="fallback-strat"
            bind:value={fallbackStrategy}
            class="w-full px-3 py-2 rounded-lg bg-bg border border-border text-xs text-text-main focus:outline-none focus:border-brand-500"
          >
            <option value="failover">Failover (Try next on error/rate-limit)</option>
            <option value="round-robin">Round Robin (Distribute requests across all connections)</option>
            <option value="sticky-round-robin">Sticky Round-Robin (Keep active connection up to limit)</option>
          </select>
        </div>

        {#if fallbackStrategy === 'sticky-round-robin'}
          <div class="space-y-1">
            <label for="sticky-limit" class="block font-semibold text-text-main">Sticky Request Limit</label>
            <input
              id="sticky-limit"
              type="number"
              bind:value={stickyRoundRobinLimit}
              min="1"
              max="100"
              class="w-full px-3 py-2 rounded-lg bg-bg border border-border text-xs font-mono text-text-main focus:outline-none focus:border-brand-500"
            />
            <p class="text-[11px] text-text-subtle">
              Number of consecutive requests routed to the same credential before rotating.
            </p>
          </div>
        {/if}

        <div class="space-y-1">
          <label for="combo-strat" class="block font-semibold text-text-main">Combo Routing Mode</label>
          <select
            id="combo-strat"
            bind:value={comboStrategy}
            class="w-full px-3 py-2 rounded-lg bg-bg border border-border text-xs text-text-main focus:outline-none focus:border-brand-500"
          >
            <option value="first-model">First Model (Always try primary model first)</option>
            <option value="round-robin">Round Robin (Rotate starting model)</option>
          </select>
        </div>
      </div>
    </Card>

    <!-- SECTION 6: Observability & Tracing -->
    <Card padding="md" class="space-y-4">
      <div class="flex items-center justify-between pb-2 border-b border-border">
        <div class="flex items-center gap-2">
          <Zap class="w-4 h-4 text-brand-500" />
          <h2 class="text-sm font-bold text-text-main">Observability</h2>
        </div>
        <Toggle
          checked={enableObservability}
          size="sm"
          label="Enable Observability"
          onChange={(val) => (enableObservability = val)}
        />
      </div>

      <div class="space-y-2 text-xs">
        <p class="text-text-muted leading-relaxed">
          Collect real-time telemetry, TTFT (Time to First Token), round-trip latency, and token throughput for all routed LLM requests.
        </p>
        <div class="p-3 rounded-xl bg-bg border border-border text-[11px] text-text-subtle space-y-1">
          <div>Telemetry destination: In-memory ring buffer & SQLite <code class="font-mono text-brand-500">requestDetails</code></div>
          <div>Real-time stream: <code class="font-mono text-info">/api/usage/stream</code> (SSE)</div>
        </div>
      </div>
    </Card>

    <Modal
      isOpen={dbAuthOpen}
      onClose={closeDbAuth}
      title={backupAction === 'import'
        ? 'Restore backup file'
        : backupAction === 'drive'
          ? 'Create Google Drive backup'
          : backupAction === 'drive-restore'
            ? 'Restore from Google Drive'
            : backupAction === 'connect'
              ? 'Connect Google Drive'
              : 'Export encrypted backup'}
      size="sm"
    >
      <div class="space-y-3">
        <p class="text-text-muted">
          {backupAction === 'import'
            ? `Restore "${pendingImportFile?.name || 'backup'}"? This will overwrite existing server data.`
            : backupAction === 'drive'
              ? 'Create an encrypted recovery backup in your Google Drive?'
              : backupAction === 'drive-restore'
                ? 'Choose a Google Drive backup and restore it on this device.'
                : backupAction === 'connect'
                  ? 'Authorize this device to use your Google Drive for encrypted 9router backups.'
                  : 'Export an encrypted recovery backup (.9rbak) to this device?' }
        </p>
        <div class="rounded-xl border border-border bg-surface-2/40 p-3 space-y-2">
          <div>
            <p class="text-xs font-bold text-text-main">1. Dashboard authorization</p>
            <p class="text-[10px] text-text-subtle">This is your current dashboard password. It only authorizes the backup operation.</p>
          </div>
          <div class="space-y-1.5">
            <label for="backup-dashboard-password" class="text-sm font-medium text-text-main">Dashboard password</label>
            <div class="relative">
              <input
                id="backup-dashboard-password"
                type={showDbPassword ? 'text' : 'password'}
                placeholder="Current dashboard password"
                bind:value={dbPassword}
                autocomplete="current-password"
                class="w-full py-2.5 pl-3 pr-11 text-[16px] sm:text-sm text-text-main bg-surface-2 rounded-[10px] border border-transparent placeholder-text-muted/70 focus:outline-none focus:ring-2 focus:ring-brand-500/30 focus:border-brand-500/40"
              />
              <button
                type="button"
                onclick={() => (showDbPassword = !showDbPassword)}
                class="absolute inset-y-0 right-0 px-3 flex items-center text-text-muted hover:text-text-main"
                aria-label={showDbPassword ? 'Hide password' : 'Show password'}
              >
                {#if showDbPassword}<EyeOff class="w-4 h-4" />{:else}<Eye class="w-4 h-4" />{/if}
              </button>
            </div>
          </div>
        </div>

        {#if backupAction === 'drive-restore'}
          <div class="rounded-xl border border-border bg-surface-2/40 p-3 space-y-2">
            <div class="flex items-center justify-between">
              <p class="text-xs font-bold text-text-main">2. Choose backup</p>
              {#if isLoadingDriveBackups}<Loader2 class="w-3.5 h-3.5 animate-spin text-brand-500" />{/if}
            </div>
            {#if driveBackupFiles.length === 0}
              <p class="text-[10px] text-text-subtle">Enter your dashboard password, then load the backups stored in Drive.</p>
            {:else}
              <div class="space-y-1.5 max-h-40 overflow-y-auto">
                {#each driveBackupFiles as file}
                  <button
                    type="button"
                    onclick={() => (selectedDriveBackup = file)}
                    class="w-full text-left p-2.5 rounded-lg border transition {selectedDriveBackup?.id === file.id ? 'border-brand-500 bg-brand-500/10' : 'border-border bg-bg hover:bg-surface-2'}"
                  >
                    <div class="text-[11px] font-semibold text-text-main truncate">{file.name}</div>
                    <div class="text-[10px] text-text-subtle">{file.createdTime ? new Date(file.createdTime).toLocaleString() : 'Unknown date'}</div>
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        {/if}

        {#if backupPassphraseRequired()}
          <div class="rounded-xl border border-border bg-surface-2/40 p-3 space-y-3">
            <div>
              <p class="text-xs font-bold text-text-main">{backupAction === 'import' || backupAction === 'drive-restore' ? '3. Backup decryption' : '2. Backup encryption'}</p>
              <p class="text-[10px] text-text-subtle">
                {backupAction === 'import' || backupAction === 'drive-restore'
                  ? 'Enter the recovery passphrase that was used when this encrypted backup was created.'
                  : 'Create a separate recovery passphrase for this .9rbak file. You will need it to restore the backup later.'}
              </p>
            </div>

            <div class="space-y-1.5">
              <label for="backup-recovery-passphrase" class="text-sm font-medium text-text-main">Recovery passphrase</label>
              <div class="relative">
                <input
                  id="backup-recovery-passphrase"
                  type={showBackupPassphrase ? 'text' : 'password'}
                  placeholder={backupAction === 'import' || backupAction === 'drive-restore' ? 'Passphrase used when this .9rbak was created' : 'Choose 12+ characters and keep it safe'}
                  bind:value={backupPassphrase}
                  autocomplete="new-password"
                  aria-invalid={backupPassphrase.length > 0 && !backupPassphraseInfo.valid}
                  class="w-full py-2.5 pl-3 pr-11 text-[16px] sm:text-sm text-text-main bg-surface-2 rounded-[10px] border placeholder-text-muted/70 focus:outline-none focus:ring-2 transition {backupPassphrase.length > 0 && !backupPassphraseInfo.valid ? 'border-danger/60 focus:ring-danger/30' : 'border-transparent focus:ring-brand-500/30 focus:border-brand-500/40'}"
                />
                <button
                  type="button"
                  onclick={() => (showBackupPassphrase = !showBackupPassphrase)}
                  class="absolute inset-y-0 right-0 px-3 flex items-center text-text-muted hover:text-text-main"
                  aria-label={showBackupPassphrase ? 'Hide password' : 'Show password'}
                >
                  {#if showBackupPassphrase}<EyeOff class="w-4 h-4" />{:else}<Eye class="w-4 h-4" />{/if}
                </button>
              </div>

              <div class="flex items-center justify-between gap-3 text-[11px]">
                <span class={backupPassphraseInfo.valid ? 'text-success font-semibold' : 'text-text-muted'}>
                  {backupPassphraseInfo.length} / {BACKUP_PASSPHRASE_MIN_LENGTH} <span>minimum</span>
                  {#if backupPassphraseInfo.valid} ✓{/if}
                </span>
                {#if creatingEncryptedBackup()}
                  <span class="text-text-muted">
                    Password strength:
                    <strong class={backupPassphraseInfo.score >= 4 ? 'text-success' : backupPassphraseInfo.score >= 3 ? 'text-info' : backupPassphraseInfo.score >= 2 ? 'text-warning' : 'text-danger'}>
                      {backupPassphraseInfo.label}
                    </strong>
                  </span>
                {/if}
              </div>

              {#if creatingEncryptedBackup()}
                <div class="h-1.5 rounded-full bg-surface-3 overflow-hidden" aria-hidden="true">
                  <div
                    class="h-full rounded-full transition-all duration-200 {backupPassphraseInfo.score >= 4 ? 'bg-success' : backupPassphraseInfo.score >= 3 ? 'bg-info' : backupPassphraseInfo.score >= 2 ? 'bg-warning' : 'bg-danger'}"
                    style:width={`${backupPassphraseInfo.percent}%`}
                  ></div>
                </div>
                <p class="text-[10px] text-text-subtle">Use a mix of upper/lowercase, numbers, and symbols.</p>
              {/if}

              {#if backupPassphrase.length > 0 && !backupPassphraseInfo.valid}
                <p class="text-[11px] text-danger flex items-center gap-1">
                  <AlertCircle class="w-3.5 h-3.5" />
                  <span>At least 12 characters</span>
                </p>
              {/if}
            </div>

            {#if creatingEncryptedBackup()}
              <div class="space-y-1.5 pt-1">
                <label for="backup-recovery-passphrase-confirm" class="text-sm font-medium text-text-main">Confirm recovery passphrase</label>
                <div class="relative">
                  <input
                    id="backup-recovery-passphrase-confirm"
                    type={showBackupPassphraseConfirmation ? 'text' : 'password'}
                    placeholder="Type the same recovery passphrase again"
                    bind:value={backupPassphraseConfirmation}
                    autocomplete="new-password"
                    aria-invalid={backupPassphraseConfirmation.length > 0 && !backupPassphrasesMatchState}
                    class="w-full py-2.5 pl-3 pr-11 text-[16px] sm:text-sm text-text-main bg-surface-2 rounded-[10px] border placeholder-text-muted/70 focus:outline-none focus:ring-2 transition {backupPassphraseConfirmation.length > 0 && !backupPassphrasesMatchState ? 'border-danger/60 focus:ring-danger/30' : backupPassphrasesMatchState ? 'border-success/50 focus:ring-success/30' : 'border-transparent focus:ring-brand-500/30 focus:border-brand-500/40'}"
                  />
                  <button
                    type="button"
                    onclick={() => (showBackupPassphraseConfirmation = !showBackupPassphraseConfirmation)}
                    class="absolute inset-y-0 right-0 px-3 flex items-center text-text-muted hover:text-text-main"
                    aria-label={showBackupPassphraseConfirmation ? 'Hide password' : 'Show password'}
                  >
                    {#if showBackupPassphraseConfirmation}<EyeOff class="w-4 h-4" />{:else}<Eye class="w-4 h-4" />{/if}
                  </button>
                </div>

                <div class="flex items-center justify-between gap-3 text-[11px]">
                  <span class={backupPassphraseConfirmation.length >= BACKUP_PASSPHRASE_MIN_LENGTH ? 'text-success font-semibold' : 'text-text-muted'}>
                    {backupPassphraseConfirmation.length} / {BACKUP_PASSPHRASE_MIN_LENGTH} <span>minimum</span>
                    {#if backupPassphraseConfirmation.length >= BACKUP_PASSPHRASE_MIN_LENGTH} ✓{/if}
                  </span>
                  {#if backupPassphraseConfirmation.length > 0}
                    <span class={backupPassphrasesMatchState ? 'text-success font-semibold' : 'text-danger font-semibold'}>
                      {backupPassphrasesMatchState ? 'Passphrases match ✓' : 'Passphrases do not match'}
                    </span>
                  {/if}
                </div>
              </div>
            {/if}

            <p class="text-[10px] text-text-subtle leading-relaxed">
              {backupAction === 'import' || backupAction === 'drive-restore'
                ? 'Required to decrypt this encrypted .9rbak backup.'
                : 'This passphrase encrypts the provider credentials inside the backup. It is not stored by 9Router or Google Drive.'}
            </p>
          </div>
        {/if}
      </div>
      {#snippet footer()}
        <Button variant="ghost" onclick={closeDbAuth} disabled={backupBusy()}>
          Cancel
        </Button>
        <Button
          variant="primary"
          onclick={runPendingBackupAction}
          loading={backupBusy()}
          disabled={backupBusy() || !pendingBackupActionValid()}
        >
          {backupAction === 'import'
            ? 'Restore'
            : backupAction === 'drive'
              ? 'Create backup'
              : backupAction === 'drive-restore'
                ? selectedDriveBackup ? 'Restore selected backup' : 'Load backups'
                : backupAction === 'connect'
                  ? 'Connect Google Drive'
                  : 'Export file'}
        </Button>
      {/snippet}
    </Modal>
  </div>
</div>
