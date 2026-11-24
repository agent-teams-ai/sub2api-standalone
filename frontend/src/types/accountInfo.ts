export type AccountFactSource = 'provider_api' | 'verified_claim' | 'user_supplied' | 'inferred' | 'imported'

export type AccountFact<T> = {
  status: 'known'
  value: T
  source: AccountFactSource
  observedAt: string
  freshness: 'current' | 'stale'
} | {
  status: 'unknown' | 'unsupported' | 'unavailable'
  value?: never
  source?: never
  observedAt?: never
  freshness?: never
}

export interface AccountSubscriptionFact {
  code: string
  label: string
  scope: 'personal' | 'organization' | 'workspace' | 'unknown'
  endsAt: { kind: 'instant' | 'date'; value: string } | null
}

// The ordinary admin DTO and private v2 reader project the same bounded facts.
// Raw credentials/extra are deliberately absent from this display contract.
export interface AccountInfo {
  status: 'available' | 'unavailable'
  infoRevision?: string
  fields?: {
    email: AccountFact<string>
    accountLabel: AccountFact<string>
    subscription: AccountFact<AccountSubscriptionFact>
    credentialExpiresAt: AccountFact<string>
    trainingUsage: AccountFact<'enabled' | 'disabled'>
  }
}
