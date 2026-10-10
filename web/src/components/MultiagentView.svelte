<script lang="ts">
  type ModelResult = { model: string; output?: string; error?: string; duration_ms: number }
  let prompt = $state('')
  let modelsText = $state('')
  let allowExternal = $state(false)
  let loading = $state(false)
  let error = $state('')
  let results = $state<ModelResult[]>([])
  let controller: AbortController | null = null
  const allModels = $derived([...new Set(modelsText.split(/[\n,]+/).map(m => m.trim()).filter(Boolean))])
  const models = $derived(allModels.slice(0, 8))

  async function run() {
    if (!prompt.trim() || models.length === 0 || allModels.length > 8 || !allowExternal || loading) return
    error = ''
    results = []
    loading = true
    controller = new AbortController()
    try {
      const response = await fetch('/api/multiagent/run', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt,
          models,
          concurrency: 3,
          timeout_ms: 60000,
          allow_external: true
        }),
        signal: controller.signal
      })
      if (!response.ok) {
        const detail = (await response.text()).trim().slice(0, 300)
        const descriptions: Record<number, string> = {
          400: 'Solicitud inválida: revisá los modelos y el prompt.',
          401: 'Iniciá sesión para usar el comparador.',
          403: 'El servidor no autorizó esta ejecución o alguno de los modelos.',
          429: 'Hay demasiadas ejecuciones simultáneas. Intentá nuevamente más tarde.'
        }
        throw new Error(descriptions[response.status] || `Error HTTP ${response.status}${detail ? ': ' + detail : ''}`)
      }
      const data = await response.json() as { results: ModelResult[] }
      results = data.results || []
    } catch (e) {
      if (e instanceof Error && e.name === 'AbortError') error = 'Solicitud cancelada. La cancelación no garantiza que el proveedor detenga el cobro.'
      else error = e instanceof Error ? e.message : 'No se pudo completar la solicitud'
    } finally {
      loading = false
      controller = null
    }
  }

  function cancel() { controller?.abort() }
</script>

<section class="space-y-6">
  <div class="space-y-2">
    <h2 class="text-2xl font-semibold text-text-main">Comparador multimodelo</h2>
    <p class="text-sm text-text-muted">Un prompt, varios modelos, respuestas independientes. Máximo 8 modelos por ejecución.</p>
  </div>
  <form onsubmit={(event) => { event.preventDefault(); void run() }} class="space-y-4 rounded-xl border border-border-subtle bg-surface p-5">
    <label class="block text-sm font-medium text-text-main" for="multiagent-prompt">Prompt</label>
    <textarea id="multiagent-prompt" bind:value={prompt} rows="6" maxlength="20000" placeholder="Escribí la tarea para todos los modelos…" class="w-full rounded-lg border border-border-subtle bg-bg p-3 text-text-main" required></textarea>
    <label class="block text-sm font-medium text-text-main" for="multiagent-models">Modelos (uno por línea o separados por coma)</label>
    <textarea id="multiagent-models" bind:value={modelsText} rows="4" placeholder="provider/model-a&#10;provider/model-b" class="w-full rounded-lg border border-border-subtle bg-bg p-3 font-mono text-sm text-text-main" required></textarea>
    <p class="text-xs text-text-muted">{allModels.length} modelos seleccionados. Usá identificadores disponibles en Capimux.</p>
    {#if allModels.length > 8}<p role="alert" class="text-sm text-red-500">El máximo es 8 modelos. Quitá {allModels.length - 8} para continuar.</p>{/if}
    <p class="text-xs text-text-muted">La ejecución requiere habilitación del servidor y una lista de modelos autorizados. No hay presupuesto monetario automático.</p>
    <label class="flex items-start gap-3 text-sm text-text-main">
      <input type="checkbox" bind:checked={allowExternal} class="mt-1" />
      <span>Autorizo el envío de este prompt a los proveedores seleccionados. Pueden aplicarse cargos o cuotas de cada proveedor.</span>
    </label>
    <div class="flex gap-3">
      <button type="submit" disabled={loading || !allowExternal || !prompt.trim() || models.length === 0 || allModels.length > 8} class="rounded-lg bg-primary px-5 py-2 text-white disabled:opacity-40">Ejecutar en paralelo</button>
      {#if loading}<button type="button" onclick={cancel} class="rounded-lg border border-border-subtle px-4 py-2">Cancelar</button>{/if}
    </div>
  </form>
  {#if error}<p role="alert" class="text-sm text-red-500">{error}</p>{/if}
  {#if loading}<p role="status" class="text-sm text-text-muted">Consultando modelos…</p>{/if}
  {#if results.length > 0}
    <div class="grid gap-4 md:grid-cols-2">
      {#each results as result, i (i)}
        <article class="rounded-xl border border-border-subtle bg-surface p-5 space-y-3">
          <div class="flex flex-wrap justify-between gap-2">
            <h3 class="font-semibold text-text-main break-all">{result.model}</h3>
            <span class="text-xs text-text-muted">{result.duration_ms} ms</span>
          </div>
          {#if result.error}<p class="text-sm text-red-500">{result.error}</p>
          {:else}<pre class="whitespace-pre-wrap break-words text-sm text-text-main font-sans">{result.output || '(sin contenido)'}</pre>{/if}
        </article>
      {/each}
    </div>
  {/if}
</section>
