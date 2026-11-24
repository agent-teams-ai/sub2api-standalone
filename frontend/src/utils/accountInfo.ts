import type { AccountFact, AccountFactSource, AccountInfo, AccountSubscriptionFact } from '@/types/accountInfo'

const sourceLabels: Record<AccountFactSource, string> = {
  provider_api: 'admin.accounts.factProviderConfirmed',
  verified_claim: 'admin.accounts.factVerifiedClaim',
  user_supplied: 'admin.accounts.factDeclared',
  inferred: 'admin.accounts.factInferred',
  imported: 'admin.accounts.factImportedUnverified',
}

export function accountFactSourceLabel(fact: AccountFact<unknown> | undefined, t: (key: string) => string): string {
  if (fact?.status !== 'known') return ''
  const source = t(sourceLabels[fact.source])
  return fact.freshness === 'stale' ? `${source} · ${t('admin.accounts.factStale')}` : source
}

export function accountFactTitle(fact: AccountFact<unknown> | undefined, t: (key: string) => string): string {
  if (fact?.status !== 'known') return t('admin.accounts.factUnknown')
  return `${accountFactSourceLabel(fact, t)} · ${fact.observedAt}`
}

interface AccountEmailInput {
  account_info?: AccountInfo
  parent_account_info?: AccountInfo
  parent_account_id?: number | null
  extra?: Record<string, unknown> | null
  credentials?: Record<string, unknown> | null
  parent_email?: string | null
}

export function accountEmailPresentation(account: AccountEmailInput, t: (key: string) => string): { value: string; source: string; title: string } | null {
  const linked = account.parent_account_id != null
  const info = linked ? account.parent_account_info : account.account_info
  const fact = info?.status === 'available' ? info.fields?.email : undefined
  if (fact?.status === 'known') {
    const prefix = linked ? `${t('admin.accounts.factLinkedAccount')} · ` : ''
    return { value: fact.value, source: prefix + accountFactSourceLabel(fact, t), title: prefix + accountFactTitle(fact, t) }
  }
  // Legacy labels remain useful for identifying an imported or linked account,
  // but do not acquire provider provenance or an invented observation time.
  const legacy = [account.extra?.email_address, account.extra?.email, account.credentials?.email, account.parent_email]
    .find((value): value is string => typeof value === 'string' && value.trim().length > 0)
  if (!legacy) return null
  const source = t('admin.accounts.factImportedUnverified')
  return { value: legacy, source, title: source }
}

// A provider calendar date has no timezone. Only instants use the existing
// account UI policy of displaying the user's local calendar date.
export function accountSubscriptionExpiryDate(end: AccountSubscriptionFact['endsAt'] | undefined): string {
  if (!end) return ''
  if (end.kind === 'date') return /^\d{4}-\d{2}-\d{2}$/.test(end.value) ? end.value : ''
  const date = new Date(end.value)
  if (!Number.isFinite(date.getTime())) return ''
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
