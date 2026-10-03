<script lang="ts">
  import { onMount } from 'svelte'
  import {
    OAUTH_CHANNEL,
    parseCallbackURL,
    writeCallback,
  } from '../../lib/oauth-handoff'

  let status = $state<'working' | 'ok' | 'error'>('working')
  let message = $state('Processing callback…')
  let raw = $state('')
  let copied = $state(false)

  function store() {
    return typeof window !== 'undefined' ? window.localStorage : null
  }

  onMount(() => {
    const ls = store()
    const parsed = parseCallbackURL(window.location.href)
    if (parsed.error) {
      status = 'error'
      message = `Login failed: ${parsed.error}${parsed.errorDesc ? ` — ${parsed.errorDesc}` : ''}`
      // Still hand the error to the dashboard so the modal can show it.
      if (ls) writeCallback(ls, { state: parsed.state, raw: '', error: parsed.error, errorDesc: parsed.errorDesc })
    } else if (parsed.raw) {
      status = 'ok'
      message = 'Login successful. The code was sent to the dashboard.'
      raw = parsed.raw
      const payload = { state: parsed.state, raw: parsed.raw }
      if (ls) writeCallback(ls, payload)
      try {
        const bc = new BroadcastChannel(OAUTH_CHANNEL)
        bc.postMessage({ ...payload, at: Date.now() })
        bc.close()
      } catch {
        /* BroadcastChannel unavailable — the dashboard falls back to polling/storage events */
      }
      if (window.opener) {
        const origins = [window.location.origin, 'http://localhost:1455']
        for (const origin of origins) {
          try {
            window.opener.postMessage({
              type: 'oauth_callback',
              data: { ...payload, error: '', errorDescription: '' },
            }, origin)
          } catch {
            /* opener may have navigated away */
          }
        }
      }
      // Keep this page visible on mobile so the authorization code remains
      // recoverable when the automatic handoff is blocked or delayed.
    } else {
      status = 'error'
      message = 'No code in this URL. Repeat the login from the dashboard.'
    }
  })

  async function copyRaw() {
    if (!raw) return
    try {
      await navigator.clipboard.writeText(raw)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {
      copied = false
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-bg p-4">
  <div class="max-w-xl w-full rounded-xl border border-border bg-surface-1 p-6 text-center">
    {#if status === 'working'}
      <p class="text-text-muted">Processing callback…</p>
    {:else if status === 'ok'}
      <p class="text-lg font-semibold text-green-500">Login successful!</p>
      <p class="text-sm text-text-muted mt-2">{message}</p>
      <p class="text-xs text-text-muted mt-1">
        Keep this page open until the dashboard confirms the connection. If automatic handoff fails, copy the authorization code below and paste it into <b>Code or callback URL</b>.
      </p>
      {#if raw}
        <textarea readonly class="mt-4 w-full min-h-24 rounded-lg border border-border bg-surface-2 p-3 text-xs font-mono text-left">{raw}</textarea>
        <button
          type="button"
          onclick={copyRaw}
          class="mt-3 px-4 py-2 text-xs font-semibold rounded-lg bg-surface-2 hover:bg-surface-3 border border-border cursor-pointer"
        >
          {copied ? 'Copied!' : 'Copy code'}
        </button>
      {/if}
    {:else}
      <p class="text-lg font-semibold text-red-500">Callback failed</p>
      <p class="text-sm text-text-muted mt-2">{message}</p>
      <p class="text-xs text-text-muted mt-1">Close this tab and repeat the login from the dashboard.</p>
    {/if}
  </div>
</div>
