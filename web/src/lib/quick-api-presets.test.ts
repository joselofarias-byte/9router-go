import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { QUICK_API_PRESETS, getQuickApiPreset, matchingQuickNode, quickConnectionName } from './quick-api-presets'

describe('personal quick API keys', () => {
  it('includes known API gateways, not the kiraai.ai consumer app as an API', () => {
    assert.equal(getQuickApiPreset('unorouter')?.baseUrl, 'https://api.unorouter.com/v1')
    assert.equal(getQuickApiPreset('aihubmix')?.baseUrl, 'https://aihubmix.com/v1')
    assert.equal(getQuickApiPreset('apinex')?.tier, 'experimental')
    assert.equal(getQuickApiPreset('kira-vn')?.tier, 'experimental')
    assert.equal(QUICK_API_PRESETS.some((p) => p.id === 'kiraai-app'), false)
    assert.equal(QUICK_API_PRESETS.some((p) => p.baseUrl?.includes('kiraai.ai')), false)
  })

  it('reuses only matching compatible nodes, never another URL with same prefix', () => {
    const preset = getQuickApiPreset('unorouter')!
    assert.equal(matchingQuickNode(preset, [
      { id: 'bad', type: 'openai-compatible', prefix: 'unorouter-personal', name: 'Wrong', baseUrl: 'https://evil.example/v1' },
      { id: 'good', type: 'openai-compatible', prefix: 'unorouter-personal', name: 'UnoRouter', baseUrl: 'https://api.unorouter.com/v1/' },
    ])?.id, 'good')
    assert.equal(matchingQuickNode(preset, []), null)
  })

  it('picks new connection labels instead of silently replacing an old key', () => {
    const preset = getQuickApiPreset('openrouter')!
    const list = [
      { provider: 'openrouter', name: 'OpenRouter API 1' },
      { provider: 'openrouter', name: 'OpenRouter API 2' },
      { provider: 'groq', name: 'OpenRouter API 3' },
    ]
    assert.equal(quickConnectionName(preset, 'openrouter', list), 'OpenRouter API 3')
  })
})
