import { describe, expect, it } from 'vitest'
import { supportsUpstreamBillingProbe } from '../upstreamBillingProbe'

describe('upstream billing probe eligibility', () => {
  it.each([
    'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek',
    'minimax', 'opencode_go', 'typesafe', 'command_code', 'cline'
  ])('allows the supported %s API key identity', platform => {
    expect(supportsUpstreamBillingProbe(platform, 'apikey')).toBe(true)
    expect(supportsUpstreamBillingProbe(platform, 'oauth')).toBe(false)
  })

  it.each(['kiro', 'bytedance', 'fal', 'leonardo', 'atlascloud', 'apiz', 'higgsfield', 'composite', 'unknown', ''])('does not enable relay probes for %s', platform => {
    expect(supportsUpstreamBillingProbe(platform, 'apikey')).toBe(false)
  })
})
