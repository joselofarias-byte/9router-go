<script lang="ts">
  import { fmt, fmtCost, type StatsData } from './types'

  let { stats = {} }: { stats?: StatsData } = $props()
</script>

<div class="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-5 sm:gap-4">
  <div class="bg-surface border border-border-subtle rounded-[14px] shadow-[var(--shadow-soft)] flex min-w-0 flex-col gap-1 px-3 py-2.5 sm:px-4 sm:py-3">
    <span class="text-text-muted text-[10px] sm:text-sm uppercase font-semibold leading-tight">Total Requests</span>
    <span class="truncate text-xl sm:text-2xl font-bold text-text-main">{fmt(stats.totalRequests)}</span>
  </div>

  <div class="bg-surface border border-border-subtle rounded-[14px] shadow-[var(--shadow-soft)] flex min-w-0 flex-col gap-1 px-3 py-2.5 sm:px-4 sm:py-3">
    <span class="text-text-muted text-[10px] sm:text-sm uppercase font-semibold leading-tight">Total Input Tokens</span>
    <span class="truncate text-xl sm:text-2xl font-bold text-primary">{fmt(stats.totalPromptTokens)}</span>
  </div>

  <div class="bg-surface border border-border-subtle rounded-[14px] shadow-[var(--shadow-soft)] flex min-w-0 flex-col gap-1 px-3 py-2.5 sm:px-4 sm:py-3">
    <span class="text-text-muted text-[10px] sm:text-sm uppercase font-semibold leading-tight">Cached Tokens</span>
    <span class="truncate text-xl sm:text-2xl font-bold text-info">{fmt(stats.totalCachedTokens)}</span>
  </div>

  <div class="bg-surface border border-border-subtle rounded-[14px] shadow-[var(--shadow-soft)] flex min-w-0 flex-col gap-1 px-3 py-2.5 sm:px-4 sm:py-3">
    <span class="text-text-muted text-[10px] sm:text-sm uppercase font-semibold leading-tight">Output Tokens</span>
    <span class="truncate text-xl sm:text-2xl font-bold text-success">{fmt(stats.totalCompletionTokens)}</span>
  </div>

  <div class="col-span-2 bg-surface border border-border-subtle rounded-[14px] shadow-[var(--shadow-soft)] flex min-w-0 flex-col gap-1 px-3 py-2.5 sm:col-span-1 sm:px-4 sm:py-3">
    <span class="text-text-muted text-[10px] sm:text-sm uppercase font-semibold leading-tight">API-equivalent estimate</span>
    <span class="truncate text-xl sm:text-2xl font-bold text-warning">~{fmtCost(stats.totalCost)}</span>
    <span class="text-[10px] text-text-muted">Reference estimate only — not your subscription bill</span>
  </div>
</div>
