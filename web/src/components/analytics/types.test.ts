import { describe, expect, it } from 'bun:test'
import { cachedTokensFor, cachePercent, cachePercentFor, outputRateFor, fmtPercent, fmtRate, fmtRequestCost, fmt, type RequestDetailItem } from './types'

describe('request detail token formatting', () => {
  it('prefers canonical cached_tokens', () => {
    const detail: RequestDetailItem = {
      tokens: { cached_tokens: 120, cache_read_input_tokens: 90 },
    }

    expect(cachedTokensFor(detail)).toBe(120)
    expect(fmt(cachedTokensFor(detail))).toBe('120')
  })

  it('falls back to legacy cache_read_input_tokens', () => {
    expect(cachedTokensFor({ tokens: { cache_read_input_tokens: 80 } })).toBe(80)
  })

  it('renders missing cache usage as zero', () => {
    expect(fmt(cachedTokensFor({}))).toBe('0')
    expect(fmt(cachedTokensFor({ tokens: { cached_tokens: 0, cache_read_input_tokens: 25 } }))).toBe('0')
  })
})

describe('request performance and cost', () => {
  it.each([
    [100, 99, 99], [100, 0, 0], [0, 0, null],
    [undefined, undefined, null], [100, undefined, null],
    [100, 101, null], [100, -1, null], [NaN, 1, null],
  ])('cache percent for %s input / %s cached', (prompt, cached, expected) => {
    expect(cachePercent(prompt as number, cached as number)).toBe(expected)
  })

  it('supports legacy cache counts and does not treat missing cache as a measured zero', () => {
    expect(cachePercentFor({ tokens: { prompt_tokens: 100, cache_read_input_tokens: 80 } })).toBe(80)
    expect(cachePercentFor({ tokens: { prompt_tokens: 100 } })).toBeNull()
    expect(fmtPercent(null)).toBe('—')
  })

  it('uses total duration rather than claiming decoding speed', () => {
    const detail = { tokens: { completion_tokens: 400 }, latency: { total: 2000, ttft: 1000 } }
    expect(outputRateFor(detail)).toBe(200)
    expect(fmtRate({ ...detail, tokensEstimated: true })).toBe('~200.0')
  })

  it.each([{}, { latency: { total: 0 } }, { latency: { total: -1 } }, { latency: { total: Infinity } }])('keeps unavailable throughput empty', detail => {
    expect(outputRateFor({ tokens: { completion_tokens: 100 }, ...detail })).toBeNull()
  })

  it('keeps unknown prices distinct from free and preserves small charges', () => {
    expect(fmtRequestCost({})).toBe('—')
    expect(fmtRequestCost({ cost: null })).toBe('—')
    expect(fmtRequestCost({ cost: 0, costEstimated: true })).toBe('~$0.000000')
    expect(fmtRequestCost({ cost: 0.000123, costEstimated: true })).toBe('~$0.000123')
    expect(fmtRequestCost({ cost: 0.0000001 })).toBe('<$0.000001')
    expect(fmtRequestCost({ cost: NaN })).toBe('—')
  })
})
