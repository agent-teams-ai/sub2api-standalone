import { describe, expect, it, vi } from 'vitest'
import { accountEmailPresentation, accountSubscriptionExpiryDate } from '../accountInfo'
import type { AccountInfo } from '@/types/accountInfo'

describe('account subscription expiry display', () => {
  it('preserves calendar dates and applies local conversion only to instants', () => {
    // Simulate an instant whose local day is earlier than its ISO UTC day.
    const year = vi.spyOn(Date.prototype, 'getFullYear').mockReturnValue(2028)
    const month = vi.spyOn(Date.prototype, 'getMonth').mockReturnValue(0)
    const day = vi.spyOn(Date.prototype, 'getDate').mockReturnValue(1)
    try {
      expect(accountSubscriptionExpiryDate({ kind: 'date', value: '2028-01-02' })).toBe('2028-01-02')
      expect(day).not.toHaveBeenCalled()
      expect(accountSubscriptionExpiryDate({ kind: 'instant', value: '2028-01-02T00:00:00Z' })).toBe('2028-01-01')
      expect(day).toHaveBeenCalledOnce()
    } finally {
      year.mockRestore()
      month.mockRestore()
      day.mockRestore()
    }
  })
})

describe('account email provenance', () => {
  const t = (key: string) => key
  it('prefers the canonical known email and retains source, age and observation time', () => {
    const info: AccountInfo = { status: 'available', fields: {
      email: { status: 'known', value: 'verified@example.test', source: 'provider_api', observedAt: '2026-11-23T10:00:00Z', freshness: 'stale' },
      accountLabel: { status: 'unknown' }, subscription: { status: 'unknown' },
      credentialExpiresAt: { status: 'unknown' }, trainingUsage: { status: 'unknown' },
    } }
    const email = accountEmailPresentation({ account_info: info, extra: { email: 'legacy@example.test' } }, t)
    expect(email?.value).toBe('verified@example.test')
    expect(email?.source).toContain('factProviderConfirmed')
    expect(email?.source).toContain('factStale')
    expect(email?.title).toContain('2026-11-23T10:00:00Z')
  })
  it('keeps historical and linked labels explicitly unverified without a synthetic time', () => {
    const email = accountEmailPresentation({ account_info: { status: 'unavailable' }, parent_email: 'parent@example.test' }, t)
    expect(email).toEqual({ value: 'parent@example.test', source: 'admin.accounts.factImportedUnverified', title: 'admin.accounts.factImportedUnverified' })
    expect(accountEmailPresentation({}, t)).toBeNull()
  })

  it('labels confirmed parent facts as linked and never treats them as the child subject', () => {
    const parent: AccountInfo = { status: 'available', fields: {
      email: { status: 'known', value: 'parent-confirmed@example.test', source: 'provider_api', observedAt: '2026-11-23T10:00:00Z', freshness: 'current' },
      accountLabel: { status: 'unknown' }, subscription: { status: 'unknown' },
      credentialExpiresAt: { status: 'unknown' }, trainingUsage: { status: 'unknown' },
    } }
    const email = accountEmailPresentation({ parent_account_id: 10, account_info: { status: 'unavailable' }, parent_account_info: parent }, t)
    expect(email?.value).toBe('parent-confirmed@example.test')
    expect(email?.source).toContain('factLinkedAccount')
    expect(email?.source).toContain('factProviderConfirmed')
    expect(accountEmailPresentation({ parent_account_info: parent }, t)).toBeNull()
  })
})
