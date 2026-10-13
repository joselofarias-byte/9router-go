<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { api, type MultiagentPolicy, type MultiagentResult } from '../api/client'

  let prompt = $state('')
  let models = $state<string[]>([])
  let policy = $state<MultiagentPolicy | null>(null)
  let policyLoading = $state(true)
  let allowExternal = $state(false)
  let dryRun = $state(true)
  let preview = $state(false)
  let loading = $state(false)
  let error = $state('')
  let results = $state<MultiagentResult[]>([])
  let controller: AbortController | null = null
  const policyController = new AbortController()
  const canRun = $derived(!loading && !!policy && !!prompt.trim() && models.length > 0 &&
    models.length <= policy.max_models && (dryRun || (allowExternal && policy.external_enabled)))

  async function loadPolicy() {
    policyLoading = true
    error = ''
    try {
      const loaded = await api.getMultiagentPolicy(policyController.signal)
      const firstLoad = policy === null
      policy = loaded
      models = firstLoad ? loaded.models.slice(0, loaded.max_models) : models.filter(model => loaded.models.includes(model))
    } catch (e) {
      if (!(e instanceof Error && e.name === 'AbortError')) {
        error = e instanceof Error ? e.message : 'No se pudieron cargar los modelos.'
      }
    } finally { policyLoading = false }
  }
  onMount(() => { void loadPolicy() })
  onDestroy(() => { controller?.abort(); policyController.abort() })

  function toggleModel(model: string, checked: boolean) {
    models = checked ? [...models, model] : models.filter(m => m !== model)
  }

  async function run() {
    if (!canRun) return
    const simulated = dryRun
    error = ''
    results = []
    preview = false
    loading = true
    controller = new AbortController()
    try {
      const data = await api.runMultiagent({
        prompt, models: [...models], concurrency: 3, timeout_ms: 60000,
        allow_external: !simulated && allowExternal, dry_run: simulated
      }, controller.signal)
      results = data.results
      preview = data.dry_run === true
    } catch (e) {
      if (e instanceof Error && e.name === 'AbortError') error = 'Solicitud cancelada.'
      else error = e instanceof Error ? e.message : 'No se pudo completar la solicitud.'
    } finally {
      loading = false
      controller = null
    }
  }

  function resultError(message: string) {
    const labels: Record<string, string> = {
      'cancelled': 'Cancelado',
      'timed out': 'El modelo superó el tiempo de espera.',
      'response too large': 'La respuesta superó el tamaño permitido.',
      'empty model response': 'El modelo devolvió una respuesta vacía.',
      'model request failed': 'No se pudo obtener la respuesta de este modelo.'
    }
    return labels[message] || message
  }
</script>

<section class="space-y-6">
  <div class="space-y-2">
    <h2 class="text-2xl font-semibold text-text-main">Comparador multimodelo</h2>
    <p class="text-sm text-text-muted">Escribí una tarea, elegí los modelos y compará sus respuestas.</p>
  </div>
  <form onsubmit={(event) => { event.preventDefault(); void run() }} class="space-y-4 rounded-xl border border-border-subtle bg-surface p-5">
    <label class="block text-sm font-medium text-text-main" for="multiagent-prompt">Prompt</label>
    <textarea id="multiagent-prompt" bind:value={prompt} disabled={loading} rows="6" maxlength="20000" placeholder="Escribí la tarea para todos los modelos…" class="w-full rounded-lg border border-border-subtle bg-surface-2 p-3 text-text-main" required></textarea>
    <fieldset disabled={loading || policyLoading} class="space-y-3">
      <legend class="text-sm font-medium text-text-main">Modelos</legend>
      {#if policyLoading}
        <p role="status" class="text-sm text-text-muted">Cargando modelos…</p>
      {:else if policy && policy.models.length > 0}
        <div class="grid gap-2 sm:grid-cols-2">
          {#each policy.models as model (model)}
            <label class="flex items-center gap-3 rounded-lg border border-border-subtle p-3 text-sm text-text-main">
              <input type="checkbox" checked={models.includes(model)}
                disabled={!models.includes(model) && models.length >= policy.max_models}
                onchange={(event) => toggleModel(model, event.currentTarget.checked)} />
              <span class="break-all">{model}</span>
            </label>
          {/each}
        </div>
        <p class="text-xs text-text-muted">{models.length} de {policy.max_models} modelos seleccionados.</p>
      {:else if policy}
        <p class="text-sm text-text-muted">Todavía no hay modelos habilitados para el comparador.</p>
      {/if}
    </fieldset>
    {#if !policyLoading && !policy}
      <button type="button" onclick={() => void loadPolicy()} class="rounded-lg border border-border-subtle px-3 py-2 text-sm text-text-main">Reintentar carga</button>
    {/if}
    <label class="flex items-center gap-3 text-sm text-text-main">
      <input type="checkbox" bind:checked={dryRun} disabled={loading} />
      <span>Validar selección sin enviar el prompt</span>
    </label>
    {#if !dryRun}
      {#if policy && !policy.external_enabled}
        <p role="status" class="text-sm text-text-muted">El servidor tiene habilitada únicamente la simulación.</p>
      {:else}
        <label class="flex items-center gap-3 text-sm text-text-main">
          <input type="checkbox" bind:checked={allowExternal} disabled={loading} />
          <span>Enviar a los proveedores seleccionados usando sus cuotas o créditos</span>
        </label>
      {/if}
    {/if}
    <div class="flex gap-3">
      <button type="submit" disabled={!canRun} class="rounded-lg bg-primary px-5 py-2 text-white disabled:opacity-40">{dryRun ? 'Validar selección' : 'Ejecutar en paralelo'}</button>
      {#if loading}<button type="button" onclick={() => controller?.abort()} class="rounded-lg border border-border-subtle px-4 py-2 text-text-main">Cancelar</button>{/if}
    </div>
  </form>
  {#if error}<p role="alert" class="text-sm text-red-500">{error}</p>{/if}
  {#if loading}<p role="status" class="text-sm text-text-muted">Consultando modelos…</p>{/if}
  {#if preview}<p role="status" class="text-sm text-text-muted">Selección validada. No se enviaron solicitudes a proveedores.</p>{/if}
  {#if results.length > 0}
    <div class="grid gap-4 md:grid-cols-2">
      {#each results as result (result.model)}
        <article class="space-y-3 rounded-xl border border-border-subtle bg-surface p-5">
          <div class="flex flex-wrap justify-between gap-2">
            <h3 class="break-all font-semibold text-text-main">{result.model}</h3>
            {#if !preview}<span class="text-xs text-text-muted">{result.duration_ms} ms</span>{/if}
          </div>
          {#if preview}<p class="text-sm text-text-muted">Modelo habilitado.</p>
          {:else if result.error}<p class="text-sm text-red-500">{resultError(result.error)}</p>
          {:else}<pre class="whitespace-pre-wrap break-words font-sans text-sm text-text-main">{result.output}</pre>{/if}
        </article>
      {/each}
    </div>
  {/if}
</section>
