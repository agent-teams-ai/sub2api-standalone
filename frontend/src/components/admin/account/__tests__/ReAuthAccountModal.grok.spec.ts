import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { resetPlatformCatalog } from '@/constants/platformCatalog'
import {
  OAuthAuthorizationFlow, formAPI, mountedForms, mountReauthForm,
  renderedMethods, resetFormAPI, submitInput
} from '@/components/account/__tests__/adminConnectionFormHarness'

beforeEach(resetFormAPI)
afterEach(() => {
  for (const wrapper of mountedForms.splice(0)) wrapper.unmount()
  resetPlatformCatalog()
})

describe('Grok native-declared reauth forms', () => {
  it.each([false, true])('chooses a non-password default from saved credentials (hasRefreshToken=%s)', async (hasRefreshToken) => {
    const wrapper = mountReauthForm('grok', { hasRefreshToken })
    await flushPromises()
    const flow = wrapper.getComponent(OAuthAuthorizationFlow)
    expect(renderedMethods(flow)).toEqual(['manual', 'refresh_token', 'sso_cookie'])
    expect(flow.get('[data-admin-input-method]').attributes('data-admin-input-method')).toBe(hasRefreshToken ? 'refresh_token' : 'sso_cookie')
    expect(wrapper.text()).not.toContain('admin.accounts.oauth.completeAuth')
    expect(formAPI.grok.getCapabilities).not.toHaveBeenCalled()
  })

  it.each([
    [undefined, 'refresh_token'],
    [{ has_refresh_token: false }, 'sso_cookie'],
  ] as const)('prefers the canonical credential presence flag and accepts only an absent legacy fallback (%j)', async (credentialsStatus, expectedMethod) => {
    const wrapper = mountReauthForm('grok')
    const account = wrapper.props('account')
    if (!account) throw new Error('Reauth fixture requires an existing account')
    await wrapper.setProps({ account: { ...account, credentials_status: credentialsStatus, credentials: { has_refresh_token: true } } })
    await flushPromises()
    const flow = wrapper.getComponent(OAuthAuthorizationFlow)
    expect(flow.get('[data-admin-input-method]').attributes('data-admin-input-method')).toBe(expectedMethod)
    expect(formAPI.grok.getCapabilities).not.toHaveBeenCalled()
  })

  it.each(['refresh_token', 'sso_cookie'] as const)('reauthenticates the existing account with %s without batch create or storing the import secret', async (method) => {
    const wrapper = mountReauthForm('grok')
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), method, 'import-secret')
    const endpoint = method === 'refresh_token' ? formAPI.grok.refreshGrokToken : formAPI.grok.validateSSOToken
    expect(endpoint).toHaveBeenCalledTimes(1)
    expect(endpoint).toHaveBeenCalledWith('import-secret', 17)
    expect(formAPI.accounts.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.applyOAuthCredentials).toHaveBeenCalledWith(41, expect.objectContaining({ type: 'oauth', credentials: expect.objectContaining({ access_token: 'access-result', refresh_token: 'refresh-result' }) }))
    expect(JSON.stringify(formAPI.accounts.applyOAuthCredentials.mock.calls)).not.toContain('import-secret')
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
    expect(formAPI.grok.createFromSSO).not.toHaveBeenCalled()
    expect(formAPI.grok.authorizePassword).not.toHaveBeenCalled()
  })
})
