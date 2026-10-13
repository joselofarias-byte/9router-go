import { describe, expect, it } from 'bun:test'
import { api } from './client'

describe('multiagent API client', () => {
  it('loads policy and sends selection and cancellation signal through the shared client', async () => {
    const originalFetch = globalThis.fetch
    const requests: Array<{ url: string; init?: RequestInit }> = []
    globalThis.fetch = async (input, init) => {
      requests.push({ url: String(input), init })
      return Response.json(String(input).endsWith('/policy')
        ? { models: ['provider/a'], external_enabled: true, max_models: 8, max_output_tokens: 4096 }
        : { results: [{ model: 'provider/a', output: 'answer', duration_ms: 12 }] })
    }
    const controller = new AbortController()
    try {
      const policy = await api.getMultiagentPolicy(controller.signal)
      const payload = { prompt: 'task', models: policy.models, concurrency: 3, timeout_ms: 60000, allow_external: true, dry_run: false }
      const result = await api.runMultiagent(payload, controller.signal)
      expect(requests.map(r => r.url)).toEqual(['/api/multiagent/policy', '/api/multiagent/run'])
      expect(JSON.parse(String(requests[1].init?.body))).toEqual(payload)
      expect(requests[1].init?.signal).toBe(controller.signal)
      expect(result.results[0].output).toBe('answer')
    } finally { globalThis.fetch = originalFetch }
  })

  it('surfaces server refusal instead of showing an empty successful comparison', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = async () => Response.json({ error: 'model not allowed by server policy' }, { status: 403 })
    try {
      await expect(api.runMultiagent({ prompt: 'task', models: ['unknown'], concurrency: 1, timeout_ms: 1000, allow_external: true, dry_run: false })).rejects.toThrow('model not allowed by server policy')
    } finally { globalThis.fetch = originalFetch }
  })
})
