import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { AccountInfo } from '@/types/accountInfo'
import PlatformTypeBadge from '../PlatformTypeBadge.vue'

// Icon rendering is outside the fact-display contract. Keep the actual badge,
// props, normalization and visible fact text under the guarded Vue compiler.
vi.mock('@/components/common/PlatformIcon.vue', () => ({ default: { template: '<svg />' } }))
vi.mock('@/components/common/GrokFreeIcon.vue', () => ({ default: { template: '<svg />' } }))
vi.mock('@/components/icons/Icon.vue', () => ({ default: { template: '<svg />' } }))

vi.mock('vue-i18n', () => {
  const labels: Record<string, string> = {
    'admin.accounts.factTrainingUsage': 'Data use for training',
    'admin.accounts.factTrainingDisabled': 'disabled',
    'admin.accounts.factTrainingEnabled': 'enabled',
    'admin.accounts.factUnknown': 'unknown',
    'admin.accounts.factPlanUnknown': 'Plan unknown',
    'admin.accounts.factProviderConfirmed': 'provider confirmed',
    'admin.accounts.factDeclared': 'declared',
    'admin.accounts.factInferred': 'inferred',
    'admin.accounts.factStale': 'stale',
  }
  return { useI18n: () => ({ t: (key: string) => labels[key] || key }) }
})

const observedAt = '2026-11-23T10:00:00Z'
function unknownInfo(): AccountInfo {
  return { status: 'available', infoRevision: '0', fields: {
    email: { status: 'unknown' }, accountLabel: { status: 'unknown' },
    subscription: { status: 'unknown' }, credentialExpiresAt: { status: 'unknown' },
    trainingUsage: { status: 'unknown' },
  } }
}

describe('PlatformTypeBadge normalized provider facts', () => {
  it('does not turn legacy training, plan, or expiry markers into confirmed properties', () => {
    const wrapper = mount(PlatformTypeBadge, { props: {
      platform: 'openai', type: 'oauth', accountInfo: unknownInfo(),
      privacyMode: 'training_off', planType: 'pro', subscriptionExpiresAt: '2099-01-01T00:00:00Z',
    } })
    expect(wrapper.text()).toContain('Data use for training: unknown')
    expect(wrapper.text()).toContain('Plan unknown')
    expect(wrapper.text()).not.toMatch(/Private|Public|Pro 200|2099-01-01/)
  })

  it('shows the bounded training meaning, source, time and staleness', () => {
    const info = unknownInfo()
    info.fields!.trainingUsage = { status: 'known', value: 'disabled', source: 'provider_api', observedAt, freshness: 'stale' }
    const wrapper = mount(PlatformTypeBadge, { props: { platform: 'openai', type: 'oauth', accountInfo: info } })
    const training = wrapper.get('[data-testid="training-usage"]')
    expect(training.text()).toBe('Data use for training: disabled · stale')
    expect(training.attributes('title')).toContain(`provider confirmed · stale · ${observedAt}`)
    expect(wrapper.text()).not.toMatch(/Private|Public/)
  })

  it.each([
    ['grok', 'inferred', 'inferred'],
    ['gemini', 'user_supplied', 'declared'],
  ] as const)('keeps %s subscription provenance visible', (platform, source, label) => {
    const info = unknownInfo()
    info.fields!.subscription = { status: 'known', value: { code: 'pro', label: 'Pro', scope: 'unknown', endsAt: null }, source, observedAt, freshness: 'current' }
    info.fields!.trainingUsage = { status: 'unsupported' }
    const wrapper = mount(PlatformTypeBadge, { props: { platform, type: 'oauth', accountInfo: info } })
    expect(wrapper.get('[data-testid="subscription-provenance"]').text()).toBe(`(${label})`)
    expect(wrapper.find('[data-testid="training-usage"]').exists()).toBe(false)
  })

  it('keeps a failed PATCH as an operation result while preserving a prior confirmed fact', () => {
    const info = unknownInfo()
    info.fields!.trainingUsage = { status: 'known', value: 'disabled', source: 'provider_api', observedAt, freshness: 'current' }
    const wrapper = mount(PlatformTypeBadge, { props: { platform: 'openai', type: 'oauth', accountInfo: info, privacyMode: 'training_set_cf_blocked' } })
    expect(wrapper.text()).toContain('CF')
    expect(wrapper.get('[data-testid="training-usage"]').text()).toBe('Data use for training: disabled')
    expect(wrapper.text()).not.toMatch(/Private|Public/)
  })

  it('uses the canonical provider label without guessing a plan from an opaque tier ID', () => {
    const info = unknownInfo()
    info.fields!.subscription = { status: 'known', value: { code: 'g1-pro-tier', label: 'Pro', scope: 'unknown', endsAt: null }, source: 'provider_api', observedAt, freshness: 'current' }
    info.fields!.trainingUsage = { status: 'unsupported' }
    const wrapper = mount(PlatformTypeBadge, { props: { platform: 'antigravity', type: 'oauth', accountInfo: info } })
    expect(wrapper.text()).toContain('Pro')
    expect(wrapper.text()).not.toContain('g1-pro-tier')
    expect(wrapper.get('[data-testid="subscription-provenance"]').text()).toBe('(provider confirmed)')
  })
})
