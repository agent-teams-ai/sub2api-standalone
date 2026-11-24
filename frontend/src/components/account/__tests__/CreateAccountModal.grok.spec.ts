import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { resetPlatformCatalog } from '@/constants/platformCatalog'
import {
  OAuthAuthorizationFlow, completeManual, formAPI, formErrors, mountCreateForm,
  mountedForms, renderedMethods, resetFormAPI, selectPlatform, submitInput
} from './adminConnectionFormHarness'

beforeEach(resetFormAPI)
afterEach(() => {
  for (const wrapper of mountedForms.splice(0)) wrapper.unmount()
  resetPlatformCatalog()
})

describe('Grok native-declared create forms', () => {
  it('creates an API-key account with the official xAI default', async () => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'grok')
    await wrapper.get('[data-testid="grok-account-type-api-key"]').trigger('click')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Grok API')
    await wrapper.get('#create-account-form input[type="password"]').setValue('xai-key')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({
      platform: 'grok', type: 'apikey', credentials: { api_key: 'xai-key', base_url: 'https://api.x.ai/v1' }
    })
  })

  it.each(['manual', 'refresh_token', 'sso_cookie'] as const)('preserves upstream validation and submission for %s', async (method) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'grok')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Grok OAuth')
    await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
    await wrapper.get('[data-testid="grok-custom-base-url-input"]').setValue('https://grok-relay.example/v1')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    const flow = wrapper.getComponent(OAuthAuthorizationFlow)
    expect(renderedMethods(flow)).toEqual(['manual', 'refresh_token', 'sso_cookie'])
    expect(formAPI.grok.getCapabilities).not.toHaveBeenCalled()
    if (method === 'manual') await completeManual(wrapper)
    else await submitInput(flow, method, 'input-token')
    const call = method === 'sso_cookie' ? formAPI.grok.createFromSSO : formAPI.accounts.create
    expect(call).toHaveBeenCalledTimes(1)
    expect(call.mock.calls[0]?.[0]?.credentials).toMatchObject({ base_url: 'https://grok-relay.example/v1' })
    if (method === 'sso_cookie') expect(formAPI.grok.createFromSSO.mock.calls[0]?.[0]?.sso_tokens).toEqual(['input-token'])
    expect(formAPI.grok.authorizePassword).not.toHaveBeenCalled()
  })

  it.each(['manual', 'refresh_token', 'sso_cookie'] as const)('blocks invalid upstream configuration before %s credential exchange', async (method) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'grok')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Invalid Grok')
    await wrapper.get('[data-testid="grok-custom-base-url-toggle"]').trigger('click')
    await wrapper.get('[data-testid="grok-custom-base-url-input"]').setValue('invalid-url')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    if (method === 'manual') await completeManual(wrapper)
    else await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), method, 'input-token')
    expect(formErrors).toHaveBeenCalledWith('admin.accounts.grokCustomBaseUrl.invalid')
    expect(formAPI.grok.exchangeCode).not.toHaveBeenCalled()
    expect(formAPI.grok.refreshGrokToken).not.toHaveBeenCalled()
    expect(formAPI.grok.createFromSSO).not.toHaveBeenCalled()
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
  })
})
