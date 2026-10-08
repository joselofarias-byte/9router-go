<script lang="ts">
  import { KeyRound, ChevronDown } from 'lucide-svelte'
  import { api, type ProviderConnection, type ProviderNode } from '../../api/client'
  import { QUICK_API_PRESETS, matchingQuickNode, quickConnectionName } from '../../lib/quick-api-presets'

  interface Props {
    connections: ProviderConnection[]
    providerNodes: ProviderNode[]
    onRefresh: () => void
    onSelectProvider: (id: string) => void
  }

  let { connections, providerNodes, onRefresh, onSelectProvider }: Props = $props()
  let expanded = $state(false)
  let presetId = $state('unorouter')
  let apiKey = $state('')
  let customModelId = $state('')
  let submitting = $state(false)
  let error = $state('')
  let result = $state('')

  let selected = $derived(QUICK_API_PRESETS.find((p) => p.id === presetId) ?? QUICK_API_PRESETS[0])
  let modelId = $derived(customModelId.trim() || selected.defaultModel || '')
  let savedCount = $derived(connections.filter((c) =>
    selected.providerId ? c.provider === selected.providerId
      : providerNodes.some((n) => n.id === c.provider && n.prefix === selected.prefix && n.baseUrl?.replace(/\/+$/, '') === selected.baseUrl)
  ).length)

  function selectPreset(id: string) {
    presetId = id
    customModelId = ''
    error = ''
    result = ''
    apiKey = ''
  }

  async function saveConnection(e: SubmitEvent) {
    e.preventDefault()
    if (submitting || !apiKey.trim()) return
    error = ''
    result = ''
    submitting = true
    try {
      const preset = selected
      let providerId = preset.providerId || ''
      if (!providerId && preset.baseUrl && preset.prefix) {
        const conflicting = providerNodes.find((n) =>
          n.prefix === preset.prefix &&
          (n.type !== 'openai-compatible' || n.baseUrl?.replace(/\/+$/, '') !== preset.baseUrl)
        )
        if (conflicting) throw new Error('Ya existe un nodo distinto con el mismo prefijo. Revisá sus ajustes.')
        const existing = matchingQuickNode(preset, providerNodes)
        if (existing) providerId = existing.id
        else {
          const node = await api.createProviderNode({
            name: preset.name, prefix: preset.prefix, baseUrl: preset.baseUrl,
            apiType: 'chat', type: 'openai-compatible',
          })
          providerId = node.id
        }
      }
      if (!providerId) throw new Error('Proveedor sin conexión API configurada.')

      const name = quickConnectionName(preset, providerId, connections)
      const chosenModel = modelId
      await api.createConnection({
        provider: providerId,
        authType: preset.providerId ? 'apikey' : 'compatible',
        name,
        apiKey: apiKey.trim(),
        priority: 1,
        testStatus: 'unknown',
        ...(preset.baseUrl && chosenModel ? {
          defaultModel: chosenModel,
          providerSpecificData: { assignedModel: chosenModel },
        } : {}),
      })

      // Compatible endpoints need an initial model in the custom model list.
      let modelError = ''
      if (preset.baseUrl && chosenModel) {
        try {
          await api.saveCustomModel(providerId + '|' + chosenModel + '|llm', {
            id: chosenModel,
            providerAlias: providerId,
            type: 'llm',
          })
        } catch {
          modelError = 'La clave se guardó, pero no se pudo agregar el modelo. Abrí el proveedor para configurarlo.'
        }
      }
      apiKey = ''
      onRefresh()
      result = modelError || 'Clave guardada. Falta validar la inferencia y el coste del modelo.'
      onSelectProvider(providerId)
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause)
    } finally {
      submitting = false
    }
  }
</script>

<div class="rounded-2xl border border-border bg-surface shadow-sm overflow-hidden">
  <button
    type="button"
    aria-expanded={expanded}
    onclick={() => (expanded = !expanded)}
    class="w-full flex items-center justify-between gap-3 p-4 text-left hover:bg-surface-2 transition-colors"
  >
    <span class="flex items-center gap-2">
      <KeyRound class="h-5 w-5 text-brand-500" />
      <span class="font-semibold text-text-main">Mis APIs · conexión rápida</span>
    </span>
    <ChevronDown class="h-4 w-4 text-text-muted transition-transform {expanded ? 'rotate-180' : ''}" />
  </button>
  {#if expanded}
    <form onsubmit={saveConnection} class="border-t border-border px-4 py-4 flex flex-col gap-3">
      <p class="text-xs text-text-muted">
        Elegí un proveedor, pegá la API key y guardá. El endpoint se configura solo; no se realiza ninguna consulta
        de pago durante el alta. Las suscripciones ChatGPT/Gemini no son API keys.
      </p>
      <div class="flex flex-col gap-1">
        <label class="text-sm font-medium text-text-main" for="quick-api-provider">Proveedor</label>
        <select
          id="quick-api-provider"
          value={presetId}
          onchange={(e) => selectPreset(e.currentTarget.value)}
          class="w-full rounded-lg bg-surface-2 text-text-main p-2 border border-border"
        >
          {#each QUICK_API_PRESETS as p (p.id)}
            <option value={p.id}>{p.name}{p.tier === 'experimental' ? ' (experimental)' : ''}</option>
          {/each}
        </select>
      </div>
      <div class="flex flex-col gap-1">
        <label class="text-sm font-medium text-text-main" for="quick-api-key">API key</label>
        <input id="quick-api-key" type="password" autocomplete="new-password" spellcheck="false"
          bind:value={apiKey} placeholder="Pegar clave" required
          class="w-full rounded-lg bg-surface-2 text-text-main p-2 border border-border font-mono"
        />
      </div>
      {#if selected.baseUrl}
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-text-main" for="quick-api-model">Modelo inicial (editable)</label>
          <input id="quick-api-model" type="text" bind:value={customModelId}
            placeholder={selected.defaultModel || 'model-id'}
            class="w-full rounded-lg bg-surface-2 text-text-main p-2 border border-border font-mono"
          />
          <p class="text-xs text-text-muted">Usaremos {modelId || '(ninguno)'}. Verificá que sea gratis antes de hacer inferencia.</p>
        </div>
        <p class="text-xs text-text-muted">Endpoint: {selected.baseUrl}</p>
      {/if}
      <p class="text-xs text-text-muted">
        {selected.note}
        {#if savedCount > 0} · {savedCount} conexiones existentes{/if}
      </p>
      <a href={selected.website} target="_blank" rel="noopener noreferrer" class="text-brand-500 underline text-xs">
        Sitio y claves del proveedor
      </a>
      {#if error}<p role="alert" class="text-xs text-red-500">{error}</p>{/if}
      {#if result}<p role="status" class="text-xs text-text-muted">{result}</p>{/if}
      <button
        type="submit"
        disabled={submitting || !apiKey.trim()}
        class="bg-brand-500 hover:bg-brand-600 text-white font-medium rounded-lg p-2 disabled:opacity-50"
      >
        {submitting ? 'Guardando...' : 'Guardar API'}
      </button>
      <p class="text-xs text-text-muted">
        La clave se almacena en 9router-go, no en GitHub. Guardar no garantiza que un modelo esté disponible.
      </p>
    </form>
  {/if}
</div>
