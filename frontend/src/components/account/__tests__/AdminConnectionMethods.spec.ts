import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  BUILTIN_PLATFORM_CATALOG, adminConnectionMethods, adminOAuthInputMethods, adminOAuthInputLabels,
  resetPlatformCatalog, setPlatformCatalog, type PlatformCatalog
} from '@/constants/platformCatalog'
import {
  OAuthAuthorizationFlow, chooseInput, clickText, completeManual, formAPI,
  mountCreateForm, mountedForms, mountFlow, mountReauthForm, openCreateOAuth,
  renderedMethods, resetFormAPI, selectPlatform, submitInput
} from './adminConnectionFormHarness'

beforeEach(resetFormAPI)
afterEach(() => {
  for (const wrapper of mountedForms.splice(0)) wrapper.unmount()
  resetPlatformCatalog()
})

// Independently maintained acceptance oracle: neither this table nor its
// expected DOM/actions is produced by the catalog builder/selector under test.
const baseline = [
  { platform: 'anthropic', addMethod: 'oauth', oauthType: '', create: ['manual', 'cookie'], reauth: ['manual', 'cookie'], legacy: ['manual', 'cookie'] },
  { platform: 'anthropic', addMethod: 'setup-token', oauthType: '', create: ['manual', 'cookie'], reauth: ['manual', 'cookie'], legacy: ['manual', 'cookie'] },
  { platform: 'openai', addMethod: 'oauth', oauthType: '', create: ['manual', 'refresh_token', 'mobile_refresh_token', 'codex_session', 'agent_identity', 'codex_pat'], reauth: ['manual', 'refresh_token'], legacy: ['manual'] },
  { platform: 'gemini', addMethod: 'oauth', oauthType: 'google_one', create: ['manual'], reauth: ['manual'], legacy: ['manual'] },
  { platform: 'gemini', addMethod: 'oauth', oauthType: 'code_assist', create: ['manual'], reauth: ['manual'], legacy: ['manual'] },
  { platform: 'antigravity', addMethod: 'oauth', oauthType: '', create: ['manual', 'refresh_token'], reauth: ['manual', 'refresh_token'], legacy: ['manual'] },
  { platform: 'grok', addMethod: 'oauth', oauthType: '', create: ['manual', 'refresh_token', 'sso_cookie'], reauth: ['manual', 'refresh_token', 'sso_cookie'], legacy: [] }
] as const

describe('native-declared admin connection forms', () => {
  it.each(baseline)('renders and submits create $platform/$addMethod/$oauthType', async (row) => {
    const wrapper = await openCreateOAuth(row.platform, row.addMethod, row.oauthType || 'code_assist')
    const flow = wrapper.getComponent(OAuthAuthorizationFlow)
    const expected = [...row.create].sort()
    expect(renderedMethods(flow)).toEqual(expected)
    expect(adminOAuthInputMethods({ platform: row.platform, surface: 'create', category: 'oauth-based', addMethod: row.addMethod, oauthType: row.oauthType })).toEqual(expect.arrayContaining([...row.create]))
    expect(formAPI.grok.getCapabilities).not.toHaveBeenCalled()
    await completeManual(wrapper)
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({ platform: row.platform, type: row.addMethod })
    if (row.platform === 'anthropic') expect(formAPI.accounts.exchangeCode).toHaveBeenCalledWith(
      row.addMethod === 'oauth' ? '/admin/accounts/exchange-code' : '/admin/accounts/exchange-setup-token-code',
      expect.objectContaining({ code: 'code-value', session_id: 'form-session' })
    )
    if (row.platform === 'openai') expect(formAPI.accounts.exchangeCode).toHaveBeenCalledWith('/admin/openai/exchange-code', expect.objectContaining({ code: 'code-value', state: 'form-state' }))
    if (row.platform === 'gemini') expect(formAPI.gemini.exchangeCode).toHaveBeenCalledWith(expect.objectContaining({ code: 'code-value', oauth_type: row.oauthType }))
    if (row.platform === 'antigravity') expect(formAPI.antigravity.exchangeCode).toHaveBeenCalledTimes(1)
    if (row.platform === 'grok') expect(formAPI.grok.exchangeCode).toHaveBeenCalledTimes(1)
  })

  for (const legacy of [false, true]) {
    it.each(baseline)(`renders ${legacy ? 'legacy' : 'admin'} reauth $platform/$addMethod/$oauthType`, async (row) => {
      const wrapper = mountReauthForm(row.platform, { legacy, addMethod: row.addMethod, oauthType: row.oauthType })
      await flushPromises()
      const flow = wrapper.getComponent(OAuthAuthorizationFlow)
      expect(renderedMethods(flow)).toEqual([...(legacy ? row.legacy : row.reauth)].sort())
      expect(formAPI.grok.getCapabilities).not.toHaveBeenCalled()
      if (legacy && row.platform === 'grok') {
        expect(flow.findAll('textarea')).toHaveLength(0)
        expect(wrapper.text()).not.toContain('admin.accounts.oauth.completeAuth')
        return
      }
      await completeManual(wrapper)
      expect(formAPI.accounts.create).not.toHaveBeenCalled()
      const writes = formAPI.accounts.applyOAuthCredentials.mock.calls.length + formAPI.accounts.update.mock.calls.length
      expect(writes).toBe(1)
      expect([...formAPI.accounts.applyOAuthCredentials.mock.calls, ...formAPI.accounts.update.mock.calls][0]?.[0]).toBe(41)
    })
  }

  it.each([
    ['anthropic', ['oauth-based', 'apikey', 'bedrock', 'service_account']],
    ['openai', ['oauth-based', 'apikey']],
    ['gemini', ['oauth-based', 'apikey', 'service_account']],
    ['antigravity', ['oauth-based', 'apikey']],
    ['grok', ['oauth-based', 'apikey']]
  ])('renders the declared account categories for %s', async (platform, categories) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, platform as string)
    expect(wrapper.findAll('[data-admin-category]').map((button) => button.attributes('data-admin-category')).sort()).toEqual([...(categories as string[])].sort())
  })

  it.each([
    ['kimi', 'payg'], ['kimi', 'coding'], ['zhipu', 'payg'], ['zhipu', 'coding'],
    ['deepseek', 'payg'], ['minimax', 'payg'], ['minimax', 'coding'],
    ['opencode_go', 'go'], ['opencode_go', 'zen'], ['command_code', 'payg'], ['cline', 'payg']
  ] as const)('binds the %s/%s API-key form to its exact declared product mode', async (platform, accountMode) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, platform)
    const choice = wrapper.find(`[data-admin-account-mode="${accountMode}"]`)
    if (choice.exists()) await choice.trigger('click')
    else expect(['command_code', 'cline']).toContain(platform)
    await wrapper.get('[data-tour="account-form-name"]').setValue('Mode qualification')
    await wrapper.get('#create-account-form input[type="password"]').setValue('mode-api-key')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({ platform, type: 'apikey', credentials: { api_key: 'mode-api-key', account_mode: accountMode } })
  })

  it.each(['oauth', 'setup-token'] as const)('submits the Claude %s cookie form through the existing endpoint', async (addMethod) => {
    const wrapper = await openCreateOAuth('anthropic', addMethod)
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), 'cookie', 'sessionKey-test')
    expect(formAPI.accounts.exchangeCode).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.exchangeCode).toHaveBeenCalledWith(
      addMethod === 'oauth' ? '/admin/accounts/cookie-auth' : '/admin/accounts/setup-token-cookie-auth',
      expect.objectContaining({ code: 'sessionKey-test', session_id: '' })
    )
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({ platform: 'anthropic', type: addMethod })
  })

  it.each([
    ['openai', 'refresh_token'], ['openai', 'mobile_refresh_token'],
    ['antigravity', 'refresh_token'], ['grok', 'refresh_token']
  ] as const)('submits %s %s through the real form and token adapter', async (platform, method) => {
    const wrapper = await openCreateOAuth(platform)
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), method, ' refresh-input ')
    const target = platform === 'openai' ? formAPI.accounts.refreshOpenAIToken : platform === 'grok' ? formAPI.grok.refreshGrokToken : formAPI.antigravity.refreshAntigravityToken
    expect(target).toHaveBeenCalledTimes(1)
    expect(target.mock.calls[0]?.[0]).toBe('refresh-input')
    if (method === 'mobile_refresh_token') expect(formAPI.accounts.refreshOpenAIToken.mock.calls[0]?.[3]).toBe('app_LlGpXReQgckcGGUo2JrYvtJK')
    else if (platform === 'openai') expect(formAPI.accounts.refreshOpenAIToken.mock.calls[0]?.[3]).toBeUndefined()
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({ platform, type: 'oauth', credentials: { access_token: 'access-result' } })
  })

  it.each([
    ['codex_session', '{"access_token":"session-input"}'],
    ['agent_identity', '{"auth_mode":"agentIdentity","agent_identity":{"agent_runtime_id":"runtime"}}']
  ] as const)('submits the %s import form with the original payload', async (method, content) => {
    const wrapper = await openCreateOAuth('openai')
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), method, content)
    expect(formAPI.accounts.importCodexSession).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.importCodexSession).toHaveBeenCalledWith(expect.objectContaining({ content, update_existing: true }))
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
  })

  it('submits the PAT form to its distinct admission endpoint', async () => {
    const wrapper = await openCreateOAuth('openai')
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), 'codex_pat', ' pat-input ')
    expect(formAPI.accounts.createOpenAICodexPAT).toHaveBeenCalledWith(expect.objectContaining({ access_token: 'pat-input' }))
    expect(formAPI.accounts.importCodexSession).not.toHaveBeenCalled()
  })

  it.each(['openai', 'antigravity'] as const)('reauth %s refresh updates the same account', async (platform) => {
    const wrapper = mountReauthForm(platform)
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), 'refresh_token', 'refresh-input')
    expect(formAPI.accounts.applyOAuthCredentials).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.applyOAuthCredentials).toHaveBeenCalledWith(41, expect.objectContaining({ credentials: expect.objectContaining({ access_token: 'access-result' }) }))
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
  })

  it.each([false, true])('uses the declared Claude setup-token cookie reauth (legacy=%s)', async (legacy) => {
    const wrapper = mountReauthForm('anthropic', { legacy, addMethod: 'setup-token' })
    await submitInput(wrapper.getComponent(OAuthAuthorizationFlow), 'cookie', 'sessionKey-input')
    expect(formAPI.accounts.exchangeCode).toHaveBeenCalledWith('/admin/accounts/setup-token-cookie-auth', expect.objectContaining({ code: 'sessionKey-input', proxy_id: 17 }))
    const write = legacy ? formAPI.accounts.update : formAPI.accounts.applyOAuthCredentials
    expect(write).toHaveBeenCalledWith(41, expect.objectContaining({ type: 'setup-token' }))
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
  })

  it.each(['sigv4', 'apikey'] as const)('submits the Bedrock %s credential form unchanged', async (mode) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'anthropic')
    await wrapper.get('[data-admin-category="bedrock"]').trigger('click')
    expect(wrapper.findAll('input[type="radio"]').map((input) => input.attributes('value')).sort()).toEqual(['apikey', 'sigv4'])
    await wrapper.get(`input[type="radio"][value="${mode}"]`).setValue(true)
    await wrapper.get('[data-tour="account-form-name"]').setValue('Bedrock form')
    if (mode === 'sigv4') await wrapper.get('input[placeholder="AKIA..."]').setValue('AKIAFORMTEST')
    await wrapper.get('#create-account-form input[type="password"]').setValue('cloud-secret')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({
      platform: 'anthropic', type: 'bedrock', credentials: {
        auth_mode: mode, aws_region: 'us-east-1',
        ...(mode === 'sigv4' ? { aws_access_key_id: 'AKIAFORMTEST', aws_secret_access_key: 'cloud-secret' } : { api_key: 'cloud-secret' })
      }
    })
  })

  it.each(['anthropic', 'gemini'] as const)('submits the %s Vertex service-account upload form', async (platform) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, platform)
    await wrapper.get('[data-admin-category="service_account"]').trigger('click')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Vertex upload')
    const contents = JSON.stringify({ project_id: 'form-project', client_email: 'form@project.example', private_key: 'fixture-private-key' })
    const file = new File([contents], 'credentials.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', { value: async () => contents })
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    await flushPromises()
    expect(wrapper.text()).toContain('form@project.example')
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]).toMatchObject({
      platform, type: 'service_account', credentials: {
        service_account_json: contents, project_id: 'form-project', client_email: 'form@project.example', tier_id: 'vertex'
      }
    })
  })

  it.each([
    { selector: '[data-testid="create-openai-ws-mode"] select-stub', category: 'oauth-based', values: ['off', 'ctx_pool', 'passthrough', 'http_bridge'] },
    { selector: '[data-testid="create-codex-fingerprint-mode-select"]', category: 'oauth-based', values: ['off', 'device', 'session', 'full'] },
    { selector: '[data-testid="create-openai-compact-mode-select"]', category: 'oauth-based', values: ['auto', 'force_on', 'force_off'] },
    { selector: '[data-testid="openai-responses-mode-select"]', category: 'apikey', values: ['auto', 'force_responses', 'force_chat_completions'] }
  ])('validates broad Select events at the $selector typed form boundary', async ({ selector, category, values }) => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'openai')
    await wrapper.get(`[data-admin-category="${category}"]`).trigger('click')
    const selectElement = wrapper.get(selector).element
    const select = wrapper.findAllComponents({ name: 'Select' }).find((candidate) => candidate.element === selectElement)
    if (!select) throw new Error(`No Select component at ${selector}`)
    for (const value of values) {
      select.vm.$emit('update:modelValue', value)
      await flushPromises()
      expect(select.props('modelValue')).toBe(value)
    }
    for (const value of [null, undefined, true, 17, 'unsupported-mode']) {
      select.vm.$emit('update:modelValue', value)
      await flushPromises()
      expect(select.props('modelValue')).toBe(values[values.length - 1])
    }
  })
})

describe('conditional configurations and catalog drift', () => {
  it.each([false, true])('AI Studio create and reauth respect the existing capability (configured=%s)', async (configured) => {
    formAPI.gemini.getCapabilities.mockResolvedValue({ ai_studio_oauth_enabled: configured })
    const create = mountCreateForm()
    await selectPlatform(create, 'gemini')
    await clickText(create, 'admin.accounts.gemini.oauthType.showAdvanced')
    expect(create.get('[data-admin-oauth-type="ai_studio"]').attributes('disabled') !== undefined).toBe(!configured)
    const reauth = mountReauthForm('gemini', { oauthType: 'ai_studio' })
    await flushPromises()
    expect(renderedMethods(reauth.getComponent(OAuthAuthorizationFlow))).toEqual(configured ? ['manual'] : [])
    if (configured) {
      await completeManual(reauth)
      expect(formAPI.gemini.generateAuthUrl).toHaveBeenCalledWith(expect.objectContaining({ oauth_type: 'ai_studio' }))
      const enabledCreate = await openCreateOAuth('gemini', 'oauth', 'ai_studio')
      await completeManual(enabledCreate)
      expect(formAPI.gemini.exchangeCode.mock.calls.map((call) => call[0]?.oauth_type)).toEqual(['ai_studio', 'ai_studio'])
    } else {
      expect(reauth.text()).not.toContain('admin.accounts.oauth.completeAuth')
      expect(formAPI.gemini.generateAuthUrl).not.toHaveBeenCalled()
    }
  })

  it.each([false, true])('the standalone password form additionally requires its helper (configured=%s)', async (configured) => {
    formAPI.grok.getCapabilities.mockResolvedValue({ password_auth_enabled: configured })
    const selection = { platform: 'grok', surface: 'flow', category: 'oauth-based' } as const
    const declared = adminOAuthInputMethods(selection, ['grok_password_login'])
    expect(declared).toContain('email_password')
    const flow = mountFlow(declared)
    await flow.setProps({ inputLabels: adminOAuthInputLabels(selection, ['grok_password_login']) })
    await flushPromises()
    expect(flow.get('input[value="sso_cookie"]').element.closest('label')?.textContent?.trim()).toBe('Grok SSO')
    expect(renderedMethods(flow).includes('email_password')).toBe(configured)
    expect(formAPI.grok.getCapabilities).toHaveBeenCalledTimes(1)
    if (configured) {
      expect(flow.get('input[value="email_password"]').element.closest('label')?.textContent?.trim()).toBe('Email and password')
      await submitInput(flow, 'email_password', 'operator@example.test----form-password')
      expect(flow.emitted('authorize-password')).toEqual([['operator@example.test----form-password']])
    } else expect(flow.emitted('authorize-password')).toBeUndefined()
  })

  it('ignores a late password capability reply after the method is withdrawn', async () => {
    let resolveCapability!: (value: { password_auth_enabled: boolean }) => void
    formAPI.grok.getCapabilities.mockReturnValue(new Promise((resolve) => { resolveCapability = resolve }))
    const flow = mountFlow(['manual', 'email_password'])
    await flow.setProps({ inputMethods: ['manual'] })
    resolveCapability({ password_auth_enabled: true })
    await flushPromises()
    expect(renderedMethods(flow)).toEqual(['manual'])
    expect(flow.emitted('authorize-password')).toBeUndefined()
  })

  it('removes a declared choice, clears its secret and rejects its stale submit', async () => {
    const wrapper = await openCreateOAuth('openai')
    const flow = wrapper.getComponent(OAuthAuthorizationFlow)
    await chooseInput(flow, 'refresh_token')
    await flow.get('textarea').setValue('retired-secret')
    const staleSubmit = flow.get('button.btn-primary')
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      admin_connection_methods: BUILTIN_PLATFORM_CATALOG.admin_connection_methods!.filter((method) => method.platform !== 'openai' || method.input_method !== 'refresh_token')
    })
    await flushPromises()
    expect(renderedMethods(flow)).not.toContain('refresh_token')
    await staleSubmit.trigger('click')
    expect(formAPI.accounts.refreshOpenAIToken).not.toHaveBeenCalled()
    setPlatformCatalog(BUILTIN_PLATFORM_CATALOG)
    await flushPromises()
    await chooseInput(flow, 'refresh_token')
    expect(flow.get('textarea').element.value).toBe('')
  })

  it('unknown configuration requirements deny invocation without a fallback', async () => {
    setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, admin_connection_methods: BUILTIN_PLATFORM_CATALOG.admin_connection_methods!.map((method) => ({ ...method, configuration_requirements: ['unimplemented_requirement'] })) })
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'openai')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Unavailable')
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    expect(wrapper.findComponent(OAuthAuthorizationFlow).exists()).toBe(false)
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
    expect(formAPI.accounts.generateAuthUrl).not.toHaveBeenCalled()
  })

  it('an unknown input renderer cannot advance the OAuth form or invoke another method', async () => {
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      admin_connection_methods: BUILTIN_PLATFORM_CATALOG.admin_connection_methods!.map((method) =>
        method.platform === 'openai' && method.category === 'oauth-based' ? { ...method, input_method: 'future_input_renderer' } : method
      )
    })
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'openai')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Unsupported renderer')
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    expect(wrapper.findComponent(OAuthAuthorizationFlow).exists()).toBe(false)
    expect(formAPI.accounts.generateAuthUrl).not.toHaveBeenCalled()
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
  })

  it('withdrawing only Kimi Coding blocks its selected form and preserves PAYG', async () => {
    const wrapper = mountCreateForm()
    await selectPlatform(wrapper, 'kimi')
    await wrapper.get('[data-admin-account-mode="coding"]').trigger('click')
    await wrapper.get('[data-tour="account-form-name"]').setValue('Retired coding')
    await wrapper.get('#create-account-form input[type="password"]').setValue('mode-api-key')
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      admin_connection_methods: BUILTIN_PLATFORM_CATALOG.admin_connection_methods!.filter((method) => method.platform !== 'kimi' || method.account_mode !== 'coding')
    })
    await flushPromises()
    expect(wrapper.find('[data-admin-account-mode="coding"]').exists()).toBe(false)
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    expect(formAPI.accounts.create).not.toHaveBeenCalled()
    await wrapper.get('[data-admin-account-mode="payg"]').trigger('click')
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create.mock.calls[0]?.[0]?.credentials.account_mode).toBe('payg')
  })

  it('uses the cross-language native method fixture without another frontend method registry', async () => {
    const fixture = JSON.parse(readFileSync(resolve(process.cwd(), '../backend/internal/service/testdata/native_connection_catalog_contract.json'), 'utf8')) as PlatformCatalog
    setPlatformCatalog(fixture)
    expect(adminConnectionMethods({ platform: 'openai', surface: 'create' }).map((method) => method.method_id)).toEqual(['fixture_browser_grant'])
    const wrapper = await openCreateOAuth('openai')
    expect(renderedMethods(wrapper.getComponent(OAuthAuthorizationFlow))).toEqual(['manual'])
    expect(wrapper.getComponent(OAuthAuthorizationFlow).text()).toContain('Fixture authorization')
    await completeManual(wrapper)
    expect(formAPI.accounts.generateAuthUrl).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.exchangeCode).toHaveBeenCalledTimes(1)
    expect(formAPI.accounts.create).toHaveBeenCalledTimes(1)
  })
})
