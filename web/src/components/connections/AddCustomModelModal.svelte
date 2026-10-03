<script lang="ts">
  // Port of upstream AddCustomModelModal.js (dashboard/providers/[id]).
  import Button from '../../lib/ui/Button.svelte'
  import Modal from '../../lib/ui/Modal.svelte'
  import Toggle from '../../lib/ui/Toggle.svelte'
  import { api } from '../../api/client'

  export interface ModelCaps {
    vision: boolean
    reasoning: boolean
  }

  interface Props {
    isOpen: boolean
    /** Storage alias of the provider (e.g. "oc") — used to strip the alias prefix and to test. */
    providerAlias: string
    onSave: (modelId: string, caps: ModelCaps) => Promise<void> | void
    onClose: () => void
  }

  let { isOpen, providerAlias, onSave, onClose }: Props = $props()

  // Port of CAPACITY_META (upstream shared/constants/models.js).
  const CAPACITY_META: Record<string, { label: string; desc: string }> = {
    vision: { label: 'Vision', desc: 'Supports image input' },
    reasoning: { label: 'Reasoning', desc: 'Supports reasoning / thinking' },
  }

  const defaultCaps = (): ModelCaps => ({ vision: false, reasoning: false })

  let modelId = $state('')
  let caps = $state<ModelCaps>(defaultCaps())
  let testStatus = $state<'testing' | 'ok' | 'error' | null>(null)
  let testError = $state('')
  let saving = $state(false)

  // Reset state when modal opens (upstream parity).
  $effect(() => {
    if (isOpen) {
      modelId = ''
      caps = defaultCaps()
      testStatus = null
      testError = ''
    }
  })

  /** Strip provider's own alias prefix (e.g. "cc/model" -> "model" for cc provider). */
  function stripAlias(id: string): string {
    const prefix = `${providerAlias}/`
    return id.startsWith(prefix) ? id.slice(prefix.length) : id
  }

  let cleanId = $derived(stripAlias(modelId.trim()))

  async function handleTest() {
    if (!cleanId || testStatus === 'testing') return
    testStatus = 'testing'
    testError = ''
    try {
      const data = await api.testModel(`${providerAlias}/${cleanId}`)
      testStatus = data.ok ? 'ok' : 'error'
      testError = data.error || ''
    } catch (err) {
      testStatus = 'error'
      testError = err instanceof Error ? err.message : String(err)
    }
  }

  async function handleSave() {
    if (!cleanId || saving) return
    saving = true
    try {
      await onSave(cleanId, { ...caps })
    } finally {
      saving = false
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleTest()
    }
  }
</script>

<Modal {isOpen} {onClose} title="Add Custom Model">
  <div class="flex flex-col gap-4">
    <div>
      <label class="text-sm font-medium mb-1.5 block" for="add-custom-model-id">Model ID</label>
      <div class="flex gap-2">
        <input
          id="add-custom-model-id"
          type="text"
          value={modelId}
          oninput={(e) => {
            modelId = (e.target as HTMLInputElement).value
            testStatus = null
            testError = ''
          }}
          onkeydown={handleKeyDown}
          placeholder="e.g. claude-opus-4-5"
          class="flex-1 px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:border-primary"
        />
        <Button
          variant="secondary"
          loading={testStatus === 'testing'}
          onclick={handleTest}
          disabled={!modelId.trim() || testStatus === 'testing'}
        >
          {#if testStatus !== 'testing'}
            <span class="material-symbols-outlined text-[18px]">science</span>
          {/if}
          {testStatus === 'testing' ? 'Testing...' : 'Test'}
        </Button>
      </div>
      <p class="text-xs text-text-muted mt-1">
        Sent to provider as:
        <code class="font-mono bg-sidebar px-1 rounded">{cleanId || 'model-id'}</code>
      </p>
    </div>

    <div>
      <label class="text-sm font-medium mb-1.5 block">Capabilities</label>
      <div class="flex flex-wrap gap-4">
        {#each Object.entries(CAPACITY_META) as [key, meta]}
          <div class="flex items-center gap-2">
            <Toggle
              checked={caps[key as keyof ModelCaps]}
              onchange={(v: boolean) => {
                caps = { ...caps, [key]: v }
              }}
              label={meta.label}
              size="sm"
            />
            <div class="leading-tight">
              <div class="text-sm">{meta.label}</div>
              <div class="text-xs text-text-muted">{meta.desc}</div>
            </div>
          </div>
        {/each}
      </div>
    </div>

    {#if testStatus === 'ok'}
      <div class="flex items-center gap-2 text-sm text-green-600">
        <span class="material-symbols-outlined text-base">check_circle</span>
        Model is reachable
      </div>
    {/if}
    {#if testStatus === 'error'}
      <div class="flex items-start gap-2 text-sm text-red-500">
        <span class="material-symbols-outlined text-base shrink-0">cancel</span>
        <span>{testError || 'Model not reachable'}</span>
      </div>
    {/if}

    <div class="flex gap-2 pt-1">
      <Button onclick={onClose} variant="ghost" fullWidth size="sm">Cancel</Button>
      <Button onclick={handleSave} fullWidth size="sm" disabled={!modelId.trim() || saving}>
        {saving ? 'Adding...' : 'Add Model'}
      </Button>
    </div>
  </div>
</Modal>
