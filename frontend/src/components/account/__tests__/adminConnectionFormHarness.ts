import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { expect, vi } from 'vitest'
import type { Account } from '@/types'
import type { AddMethod, AuthInputMethod } from '@/composables/useAccountOAuth'

// Only the external admin API and unrelated dialog controls are replaced. The
// actual modal, OAuth form, capability readers and auth composables run together.
const apiMocks = vi.hoisted(() => ({
  accounts: {
    create: vi.fn(), update: vi.fn(), clearError: vi.fn(), applyOAuthCredentials: vi.fn(),
    generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), refreshOpenAIToken: vi.fn(),
    importCodexSession: vi.fn(), createOpenAICodexPAT: vi.fn(),
    checkMixedChannelRisk: vi.fn(), syncUpstreamModels: vi.fn(), probeUpstreamBilling: vi.fn()
  },
  gemini: { generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), getCapabilities: vi.fn() },
  antigravity: { generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), refreshAntigravityToken: vi.fn() },
  grok: {
    generateAuthUrl: vi.fn(), exchangeCode: vi.fn(), refreshGrokToken: vi.fn(),
    validateSSOToken: vi.fn(), createFromSSO: vi.fn(), authorizePassword: vi.fn(), getCapabilities: vi.fn()
  },
  settings: { getWebSearchEmulationConfig: vi.fn(), getSettings: vi.fn() },
  tlsFingerprintProfiles: { list: vi.fn() }
}))
const errorMock = vi.hoisted(() => vi.fn())
export const formAPI = apiMocks
export const formErrors = errorMock

vi.mock('@/api/admin', () => ({ adminAPI: apiMocks }))
vi.mock('@/api/admin/accounts', () => ({ getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([]) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: errorMock, showSuccess: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

// These same unrelated controls are stubbed at mount below. Replacing their
// modules also avoids compiling their legacy imported-type macros; this suite
// qualifies the real connection forms, not the whole frontend build toolchain.
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { name: 'BaseDialog', props: ['show'], template: '<div/>' } }))
vi.mock('@/components/common/ConfirmDialog.vue', () => ({ default: { name: 'ConfirmDialog', template: '<div/>' } }))
vi.mock('@/components/common/Select.vue', () => ({ default: { name: 'Select', props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], template: '<div/>' } }))
vi.mock('@/components/icons/Icon.vue', () => ({ default: { name: 'Icon', props: ['name', 'size'], template: '<span/>' } }))
vi.mock('@/components/common/PlatformIcon.vue', () => ({ default: { name: 'PlatformIcon', props: ['platform', 'size'], template: '<span/>' } }))
vi.mock('@/components/common/ProxySelector.vue', () => ({ default: { name: 'ProxySelector', props: ['modelValue'], template: '<div/>' } }))
vi.mock('@/components/common/ProxyAdBanner.vue', () => ({ default: { name: 'ProxyAdBanner', template: '<div/>' } }))
vi.mock('@/components/common/GroupSelector.vue', () => ({ default: { name: 'GroupSelector', props: ['modelValue'], template: '<div/>' } }))
vi.mock('@/components/account/ModelWhitelistSelector.vue', () => ({ default: { name: 'ModelWhitelistSelector', props: ['modelValue'], template: '<div/>' } }))
vi.mock('@/components/account/QuotaLimitCard.vue', () => ({ default: { name: 'QuotaLimitCard', template: '<div/>' } }))

import CreateAccountModal from '../CreateAccountModal.vue'
import ReAuthAccountModal from '@/components/admin/account/ReAuthAccountModal.vue'
import LegacyReAuthAccountModal from '../ReAuthAccountModal.vue'
import RealOAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'
export const OAuthAuthorizationFlow = RealOAuthAuthorizationFlow

const stubs = {
  BaseDialog: defineComponent({ props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' }),
  ConfirmDialog: true, Select: true, Icon: true, PlatformIcon: true,
  ProxySelector: true, ProxyAdBanner: true, GroupSelector: true,
  ModelWhitelistSelector: true, QuotaLimitCard: true
}
const globalMocks = { $t: (key: string) => key }

export function resetFormAPI(): void {
  vi.resetAllMocks()
  const auth = { auth_url: 'https://auth.example/authorize?state=form-state', session_id: 'form-session', state: 'form-state' }
  const token = {
    access_token: 'access-result', refresh_token: 'refresh-result', expires_at: 1900000000,
    token_type: 'Bearer', email: 'operator@example.test', project_id: 'project-result'
  }
  for (const generate of [formAPI.accounts.generateAuthUrl, formAPI.gemini.generateAuthUrl, formAPI.antigravity.generateAuthUrl, formAPI.grok.generateAuthUrl]) generate.mockResolvedValue(auth)
  for (const exchange of [formAPI.accounts.exchangeCode, formAPI.accounts.refreshOpenAIToken, formAPI.gemini.exchangeCode, formAPI.antigravity.exchangeCode, formAPI.antigravity.refreshAntigravityToken, formAPI.grok.exchangeCode, formAPI.grok.refreshGrokToken, formAPI.grok.validateSSOToken, formAPI.grok.authorizePassword]) exchange.mockResolvedValue(token)
  for (const write of [formAPI.accounts.create, formAPI.accounts.update, formAPI.accounts.clearError, formAPI.accounts.applyOAuthCredentials]) write.mockResolvedValue({ id: 41 })
  formAPI.accounts.importCodexSession.mockResolvedValue({ created: 1, updated: 0, skipped: 0, failed: 0, errors: [], warnings: [] })
  formAPI.accounts.createOpenAICodexPAT.mockResolvedValue({ id: 41 })
  formAPI.accounts.checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
  formAPI.accounts.syncUpstreamModels.mockResolvedValue({ models: [], metadata: {} })
  formAPI.accounts.probeUpstreamBilling.mockResolvedValue({})
  formAPI.grok.createFromSSO.mockResolvedValue({ created: [{ id: 41 }], failed: [] })
  formAPI.grok.getCapabilities.mockResolvedValue({ password_auth_enabled: false })
  formAPI.gemini.getCapabilities.mockResolvedValue({ ai_studio_oauth_enabled: false })
  formAPI.settings.getWebSearchEmulationConfig.mockResolvedValue({ enabled: false, providers: [] })
  formAPI.settings.getSettings.mockResolvedValue({})
  formAPI.tlsFingerprintProfiles.list.mockResolvedValue([])
}

export const mountedForms: VueWrapper[] = []
export function mountCreateForm() {
  const wrapper = mount(CreateAccountModal, { props: { show: true, proxies: [], groups: [] }, global: { stubs, mocks: globalMocks } })
  mountedForms.push(wrapper)
  return wrapper
}

export function mountReauthForm(platform: string, options: { legacy?: boolean; addMethod?: AddMethod; oauthType?: string; hasRefreshToken?: boolean } = {}) {
  const account: Account = {
    id: 41, name: 'Existing account', platform, type: options.addMethod ?? 'oauth', proxy_id: 17,
    credentials: { oauth_type: options.oauthType ?? 'code_assist' },
    credentials_status: { has_refresh_token: options.hasRefreshToken ?? false }, extra: {},
    concurrency: 1, priority: 1, status: 'active', error_message: null, last_used_at: null,
    expires_at: null, auto_pause_on_expired: false,
    created_at: '2026-11-23T00:00:00Z', updated_at: '2026-11-23T00:00:00Z',
    schedulable: true, rate_limited_at: null, rate_limit_reset_at: null, overload_until: null,
    temp_unschedulable_until: null, temp_unschedulable_reason: null,
    session_window_start: null, session_window_end: null, session_window_status: null
  }
  const wrapper = mount(options.legacy ? LegacyReAuthAccountModal : ReAuthAccountModal, {
    props: { show: true, account }, global: { stubs, mocks: globalMocks }
  })
  mountedForms.push(wrapper)
  return wrapper
}

export function mountFlow(inputMethods: readonly AuthInputMethod[], platform = 'grok', initialInputMethod: AuthInputMethod = 'manual') {
  const wrapper = mount(OAuthAuthorizationFlow, {
    props: { addMethod: 'oauth', inputMethods, platform, initialInputMethod }, global: { stubs, mocks: globalMocks }
  })
  mountedForms.push(wrapper)
  return wrapper
}

export async function clickText(wrapper: Pick<VueWrapper, 'findAll'>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().trim() === text)
  expect(button, text).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

export async function selectPlatform(wrapper: VueWrapper, platform: string) {
  const genericButton = wrapper.find(`[data-testid="platform-button-${platform}"]`)
  if (genericButton.exists()) await genericButton.trigger('click')
  else await clickText(wrapper, { anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Gemini', antigravity: 'Antigravity', grok: 'Grok', kimi: 'Kimi', zhipu: 'Zhipu GLM', deepseek: 'DeepSeek', minimax: 'MiniMax' }[platform] ?? platform)
  await flushPromises()
}

export async function openCreateOAuth(platform: string, addMethod: AddMethod = 'oauth', oauthType = 'code_assist') {
  const wrapper = mountCreateForm()
  await selectPlatform(wrapper, platform)
  const category = wrapper.get('[data-admin-category="oauth-based"]')
  await category.trigger('click')
  if (platform === 'anthropic') await wrapper.get(`input[type="radio"][value="${addMethod}"]`).setValue(true)
  if (platform === 'gemini') {
    if (oauthType === 'ai_studio') await clickText(wrapper, 'admin.accounts.gemini.oauthType.showAdvanced')
    await wrapper.get(`[data-admin-oauth-type="${oauthType}"]`).trigger('click')
  }
  await wrapper.get('[data-tour="account-form-name"]').setValue('Form qualification')
  await wrapper.get('#create-account-form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

export function renderedMethods(flow: Pick<VueWrapper, 'findAll'>): string[] {
  const choices = flow.findAll('input[type="radio"]').map((input) => input.attributes('value'))
  const methods = choices.length ? choices : flow.findAll('[data-admin-input-method]').map((form) => form.attributes('data-admin-input-method'))
  return methods.map((method) => {
    if (method === undefined) throw new Error('Rendered authorization choice has no method identifier')
    return method
  }).sort()
}

export async function chooseInput(flow: Pick<VueWrapper, 'find'>, method: AuthInputMethod) {
  const radio = flow.find(`input[type="radio"][value="${method}"]`)
  if (radio.exists()) await radio.setValue(true)
  expect(flow.find(`[data-admin-input-method="${method}"]`).exists()).toBe(true)
}

export async function submitInput(flow: Pick<VueWrapper, 'find' | 'get'>, method: AuthInputMethod, value: string) {
  await chooseInput(flow, method)
  const panel = flow.get(`[data-admin-input-method="${method}"]`)
  await panel.get('textarea').setValue(value)
  await panel.get('button.btn-primary').trigger('click')
  await flushPromises()
}

export async function completeManual(wrapper: VueWrapper) {
  const flow = wrapper.getComponent(OAuthAuthorizationFlow)
  await chooseInput(flow, 'manual')
  await flow.get('[data-admin-input-method="manual"] button.btn-primary').trigger('click')
  await flushPromises()
  await flow.get('[data-admin-input-method="manual"] textarea').setValue(
    flow.props('platform') === 'anthropic' ? 'code-value' : 'https://callback.example/?code=code-value&state=form-state'
  )
  await clickText(wrapper, 'admin.accounts.oauth.completeAuth')
  await flushPromises()
}
