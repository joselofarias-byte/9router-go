/**
 * Convenience shortcuts for known API gateways, not verified free quotas.
 * Free model IDs are only initial candidates, subject to live validation.
 */
export interface QuickApiPreset {
  id: string
  name: string
  baseUrl?: string
  prefix?: string
  defaultModel?: string
  providerId?: string
  website: string
  tier: 'documented' | 'experimental' | 'existing'
  note: string
}

export const QUICK_API_PRESETS: readonly QuickApiPreset[] = [
  {
    id: 'unorouter', name: 'UnoRouter', baseUrl: 'https://api.unorouter.com/v1',
    prefix: 'unorouter-personal', defaultModel: 'qwen3.8-27b:free',
    website: 'https://unorouter.com/en/models?q=Free',
    tier: 'documented', note: 'Usar solo IDs :free. API aún sin validar con tu cuenta.',
  },
  {
    id: 'aihubmix', name: 'AIHubMix', baseUrl: 'https://aihubmix.com/v1',
    prefix: 'aihubmix-personal', defaultModel: 'coding-glm-5.3-flash-free',
    website: 'https://aihubmix.com/models/free',
    tier: 'documented', note: 'Sin pago inicial: pruebas limitadas; cuota diaria ampliada requiere recarga.',
  },
  {
    id: 'apinex', name: 'APInex', baseUrl: 'https://api.apinex.bond/v1',
    prefix: 'apinex-personal', defaultModel: 'free/deepseek-v4.1-flash',
    website: 'https://apinex.bond/models',
    tier: 'experimental', note: 'En cuarentena. No mandar repositorios privados ni información sensible.',
  },
  {
    id: 'kira-vn', name: 'Kira AI API (Vietnam)', baseUrl: 'https://kiraai.vn/api/v1',
    prefix: 'kira-vn-personal', defaultModel: 'qwen3.8-flash-free',
    website: 'https://kiraai.vn/models/',
    tier: 'experimental', note: 'Es Kira.vn, NO kiraai.ai. Free temporal, consultar cuota y privacidad.',
  },
  {
    id: 'openrouter', name: 'OpenRouter', providerId: 'openrouter',
    website: 'https://openrouter.ai/settings/keys',
    tier: 'existing', note: 'Elegir modelo free al usarlo; otros modelos son de pago.',
  },
  {
    id: 'groq', name: 'Groq', providerId: 'groq',
    website: 'https://console.groq.com/keys',
    tier: 'existing', note: 'API key propia, cuotas según el plan.',
  },
  {
    id: 'opencode-zen', name: 'OpenCode Zen', providerId: 'opencode-zen',
    website: 'https://opencode.ai/docs/zen',
    tier: 'existing', note: 'Seleccionar un modelo free para evitar gastos.',
  },
  {
    id: 'gemini', name: 'Google Gemini API', providerId: 'gemini',
    website: 'https://aistudio.google.com/apikey',
    tier: 'existing', note: 'La clave de API es distinta de una suscripción Gemini.',
  },
  {
    id: 'openai', name: 'OpenAI API', providerId: 'openai',
    website: 'https://platform.openai.com/api-keys',
    tier: 'existing', note: 'La API se factura independientemente de ChatGPT.',
  },
]

export function getQuickApiPreset(id: string): QuickApiPreset | undefined {
  return QUICK_API_PRESETS.find((entry) => entry.id === id)
}

export function matchingQuickNode(
  preset: QuickApiPreset,
  nodes: Array<{ id: string; type: string; name: string; prefix?: string; baseUrl?: string }>
): { id: string } | null {
  if (!preset.baseUrl || !preset.prefix) return null
  const normalized = preset.baseUrl.replace(/\/+$/, '')
  return nodes.find((n) =>
    n.type === 'openai-compatible' &&
    n.prefix === preset.prefix &&
    n.baseUrl?.replace(/\/+$/, '') === normalized
  ) ?? null
}

export function quickConnectionName(
  preset: Pick<QuickApiPreset, 'name'>,
  providerId: string,
  connections: Array<{ name?: string | null; provider: string }>
): string {
  const used = new Set(connections.filter((c) => c.provider === providerId).map((c) => c.name))
  for (let i = 1; i < 10000; i++) {
    const candidate = preset.name + ' API ' + i
    if (!used.has(candidate)) return candidate
  }
  throw new Error('No hay nombres libres para nuevas conexiones')
}
