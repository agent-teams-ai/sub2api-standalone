import { shallowRef } from 'vue'
import type { AddMethod, AuthInputMethod } from '@/composables/useAccountOAuth'
import builtinCatalog from './platformCatalog.builtin.json'

/**
 * 平台清单：后端的平台清单（domain/platforms.go）加上多协议 API Key 供应商 profile。
 *
 * 平台选项、展示名、多协议账号表单的默认端点与分流规则都从这里读取。清单文件
 * platformCatalog.builtin.json 由后端测试 TestFrontendBuiltinPlatformCatalogInSync
 * 生成并校验与后端一致；前端随后端一同构建发布，无需运行时拉取。
 */

export type PlatformGateway = 'anthropic' | 'openai' | 'gemini' | 'antigravity'
export type ProviderRouting = 'by_inbound' | 'by_model'
export type ProviderNativeProtocol = 'chat_completions' | 'anthropic' | 'responses'

export interface ProviderProtocolRule {
  pattern: string
  /** 首选协议；入站协议不在 protocols 中时使用。 */
  protocol: string
  /** 模型支持的协议集合（首项即 protocol）；入站协议在其中时同协议直通。仅一个协议时省略。 */
  protocols?: string[]
}

export interface ProviderModeInfo {
  mode: string
  /** 以上游协议为键的官方默认基址；缺少某协议即该模式不提供该原生端点。 */
  base_urls: Partial<Record<string, string>>
  protocol_rules?: ProviderProtocolRule[]
}

export interface ProviderProfileInfo {
  default_mode: string
  routing: ProviderRouting
  responses_path?: string
  /** 上游 /models 为每个模型给出 supported_endpoints，按模型分流时未命中规则的模型据此选协议。 */
  model_catalog?: boolean
  /** 默认模式在前。 */
  modes: ProviderModeInfo[]
}

export interface PlatformSpec {
  id: string
  display_name: string
  gateway: PlatformGateway
  cn_provider: boolean
  litellm_provider?: string
  multi_protocol?: ProviderProfileInfo
}

export interface PlatformCatalog {
  platforms: PlatformSpec[]
  /** 组合分组逐平台回退的顺序（先命中者生效）。 */
  composite_precedence: string[]
  /** Generated from native method declarations; UI adapters do not own this list. */
  admin_connection_methods?: readonly AdminConnectionMethod[]
}

export type AdminConnectionSurface = 'create' | 'reauth' | 'legacy_reauth' | 'flow'
export type AdminConnectionCategory = 'oauth-based' | 'apikey' | 'bedrock' | 'service_account'

export interface AdminConnectionMethod {
  profile_id: string
  method_id: string
  method_revision: number
  platform: string
  label: string
  provider: { id: string; label: string }
  product: { id: string; label: string }
  account_mode?: string
  category: AdminConnectionCategory
  add_method?: AddMethod
  oauth_type?: string
  input_method?: string
  credential_mode?: string
  surfaces: readonly AdminConnectionSurface[]
  configuration_requirements: readonly string[]
}

export interface AdminConnectionSelection {
  platform: string
  surface: AdminConnectionSurface
  category?: AdminConnectionCategory
  addMethod?: AddMethod
  oauthType?: string
  credentialMode?: string
  accountMode?: string
}

export const BUILTIN_PLATFORM_CATALOG: PlatformCatalog = builtinCatalog as PlatformCatalog

const catalogState = shallowRef<PlatformCatalog>(BUILTIN_PLATFORM_CATALOG)

/** 替换当前清单（测试用于模拟新登记的平台）。 */
export function setPlatformCatalog(catalog: PlatformCatalog): void {
  catalogState.value = catalog
}

/** 恢复内置清单（测试用）。 */
export function resetPlatformCatalog(): void {
  catalogState.value = BUILTIN_PLATFORM_CATALOG
}

/** 全部具体平台，按展示顺序。 */
export function listPlatforms(): PlatformSpec[] {
  return catalogState.value.platforms
}

/** Select existing form actions without duplicating native provider/method policy. */
export function adminConnectionMethods(selection: AdminConnectionSelection): readonly AdminConnectionMethod[] {
  return (catalogState.value.admin_connection_methods ?? []).filter((method) =>
    method.platform === selection.platform && method.surfaces.includes(selection.surface) &&
    (!selection.category || method.category === selection.category) &&
    (!selection.addMethod || !method.add_method || method.add_method === selection.addMethod) &&
    (!selection.oauthType || !method.oauth_type || method.oauth_type === selection.oauthType) &&
    (!selection.accountMode || method.account_mode === selection.accountMode) &&
    (!selection.credentialMode || !method.credential_mode || method.credential_mode === selection.credentialMode)
  )
}

export function adminConnectionMethodAvailable(method: AdminConnectionMethod, enabledRequirements: readonly string[] = []): boolean {
  return method.configuration_requirements.every((requirement) => enabledRequirements.includes(requirement))
}

// Finite renderer primitives, not a provider allowlist. Unknown native input
// kinds remain unavailable until this renderer has an implementation for them.
const AUTH_INPUT_METHODS: readonly AuthInputMethod[] = [
  'manual', 'cookie', 'refresh_token', 'mobile_refresh_token', 'codex_session',
  'agent_identity', 'codex_pat', 'sso_cookie', 'email_password'
]

export function adminOAuthInputMethods(
  selection: AdminConnectionSelection,
  enabledRequirements: readonly string[] = []
): readonly AuthInputMethod[] {
  const declared = adminConnectionMethods(selection).filter((method) =>
    adminConnectionMethodAvailable(method, enabledRequirements)
  )
  return AUTH_INPUT_METHODS.filter((input) => declared.some((method) => method.input_method === input))
}

export function adminOAuthInputLabels(
  selection: AdminConnectionSelection,
  enabledRequirements: readonly string[] = []
): Partial<Record<AuthInputMethod, string>> {
  const declared = adminConnectionMethods(selection).filter((method) =>
    adminConnectionMethodAvailable(method, enabledRequirements)
  )
  const labels: Partial<Record<AuthInputMethod, string>> = {}
  for (const input of AUTH_INPUT_METHODS) {
    const method = declared.find((entry) => entry.input_method === input)
    if (method) labels[input] = method.label
  }
  return labels
}

export function listPlatformIds(): string[] {
  return listPlatforms().map(spec => spec.id)
}

export function getPlatformSpec(platform: string | null | undefined): PlatformSpec | undefined {
  if (!platform) return undefined
  return catalogState.value.platforms.find(spec => spec.id === platform)
}

export function isKnownPlatform(platform: string | null | undefined): boolean {
  return !!getPlatformSpec(platform)
}

/** 与后端 buildUpstreamModelsRequest 支持的平台保持一致。 */
export function supportsUpstreamModelSync(platform: string | null | undefined): boolean {
  const spec = getPlatformSpec(platform?.trim().toLowerCase())
  if (!spec) return false
  return !!spec.multi_protocol || ['anthropic', 'openai', 'gemini', 'antigravity', 'grok'].includes(spec.id)
}

/** 清单中声明了定价目录 provider 的平台才支持同步定价模型。 */
export function supportsPricingModelSync(platform: string | null | undefined): boolean {
  return !!getPlatformSpec(platform?.trim().toLowerCase())?.litellm_provider?.trim()
}

/** 平台展示名；未登记的值原样返回。 */
export function platformDisplayName(platform: string | null | undefined): string {
  return getPlatformSpec(platform)?.display_name ?? platform ?? ''
}

export function compositePrecedencePlatformIds(): string[] {
  return catalogState.value.composite_precedence
}

export function platformUsesGateway(platform: string | null | undefined, gateway: PlatformGateway): boolean {
  return getPlatformSpec(platform)?.gateway === gateway
}

export function getProviderProfile(platform: string | null | undefined): ProviderProfileInfo | undefined {
  return getPlatformSpec(platform)?.multi_protocol ?? undefined
}

/** 指定接入模式的端点；未知模式回落默认模式（与后端 ProviderProfile.Endpoints 一致）。 */
export function getProviderMode(platform: string | null | undefined, mode: string | null | undefined): ProviderModeInfo | undefined {
  const profile = getProviderProfile(platform)
  if (!profile) return undefined
  const trimmed = (mode ?? '').trim()
  return (
    profile.modes.find(item => item.mode === trimmed) ??
    profile.modes.find(item => item.mode === profile.default_mode) ??
    profile.modes[0]
  )
}
