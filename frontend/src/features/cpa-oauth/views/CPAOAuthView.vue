<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NInput, NModal, NSpin, useMessage } from 'naive-ui'
import {
  CheckCircle2,
  ExternalLink,
  KeyRound,
  Loader2,
  LogIn,
  RefreshCw,
  ShieldCheck,
} from 'lucide-vue-next'

import {
  createCPAOAuthURL,
  getCPAOAuthStatus,
  listCPAOAuthProviders,
  submitCPAOAuthCallback,
} from '@/features/cpa-oauth/api/cpaOAuthApi'
import { useI18n } from '@/shared/i18n'
import type { CPAOAuthAuthURLResponse, CPAOAuthProvider, CPAOAuthStatusResponse } from '@/shared/types/api'

interface ProviderView {
  id: string
  label: string
  shortName: string
  tag: string
  descriptionZh: string
  descriptionEn: string
  methodZh: string
  methodEn: string
  logoPath: string
  iconText: string
  sequence: string
  tone: 'codex' | 'claude' | 'gemini' | 'antigravity' | 'grok' | 'kimi'
}

type ProviderMeta = Omit<ProviderView, 'id' | 'label' | 'sequence'>

type FlowStage = 'waiting' | 'validating' | 'success' | 'error'

const message = useMessage()
const { errorText, t } = useI18n()
const assetCacheKey = encodeURIComponent(import.meta.env.VITE_APP_VERSION || 'dev')

function providerLogo(name: string) {
  return `/providers/${name}.svg?v=${assetCacheKey}`
}

const defaultProviders: CPAOAuthProvider[] = [
  { id: 'codex', label: 'Codex / OpenAI' },
  { id: 'anthropic', label: 'Claude' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'antigravity', label: 'Antigravity' },
  { id: 'xai', label: 'Grok / xAI' },
  { id: 'kimi', label: 'Kimi' },
]

const providerMeta: Record<string, ProviderMeta> = {
  codex: {
    shortName: 'Codex',
    tag: 'OpenAI',
    descriptionZh: '连接 Codex CLI 与 OpenAI 账号，凭据由 CPA 统一保存。',
    descriptionEn: 'Connect Codex CLI and OpenAI accounts with credentials managed by CPA.',
    methodZh: '浏览器 OAuth',
    methodEn: 'Browser OAuth',
    logoPath: providerLogo('openai'),
    iconText: 'C',
    tone: 'codex',
  },
  anthropic: {
    shortName: 'Claude',
    tag: 'Anthropic',
    descriptionZh: '为 Claude Code 与 Claude CLI 添加 Anthropic 授权账号。',
    descriptionEn: 'Add an Anthropic account for Claude Code and Claude CLI.',
    methodZh: '浏览器 OAuth',
    methodEn: 'Browser OAuth',
    logoPath: providerLogo('claude'),
    iconText: '\u2726',
    tone: 'claude',
  },
  gemini: {
    shortName: 'Gemini',
    tag: 'Google',
    descriptionZh: '连接 Google 账号，为 Gemini CLI 写入 OAuth 凭据。',
    descriptionEn: 'Connect a Google account and add OAuth credentials for Gemini CLI.',
    methodZh: '浏览器 OAuth',
    methodEn: 'Browser OAuth',
    logoPath: providerLogo('gemini'),
    iconText: 'G',
    tone: 'gemini',
  },
  antigravity: {
    shortName: 'Antigravity',
    tag: 'Google',
    descriptionZh: '通过 Google 授权接入 Antigravity 账号与项目凭据。',
    descriptionEn: 'Connect Antigravity account and project credentials through Google.',
    methodZh: '浏览器 OAuth',
    methodEn: 'Browser OAuth',
    logoPath: providerLogo('antigravity'),
    iconText: 'A',
    tone: 'antigravity',
  },
  xai: {
    shortName: 'Grok',
    tag: 'xAI',
    descriptionZh: '使用 xAI 设备授权登录，由 CPA 跟踪认证状态。',
    descriptionEn: 'Sign in with xAI device authorization while CPA tracks the session.',
    methodZh: '设备授权',
    methodEn: 'Device flow',
    logoPath: providerLogo('xai'),
    iconText: 'X',
    tone: 'grok',
  },
  kimi: {
    shortName: 'Kimi',
    tag: 'Moonshot',
    descriptionZh: '使用 Moonshot 设备授权添加 Kimi 服务账号。',
    descriptionEn: 'Add a Kimi service account through Moonshot device authorization.',
    methodZh: '设备授权',
    methodEn: 'Device flow',
    logoPath: providerLogo('kimi'),
    iconText: 'K',
    tone: 'kimi',
  },
}

const providers = ref<CPAOAuthProvider[]>(defaultProviders)
const selectedProvider = ref('codex')
const redirectURL = ref('')
const isLoadingProviders = ref(false)
const startingProvider = ref<string | null>(null)
const isChecking = ref(false)
const isSubmittingCallback = ref(false)
const pageError = ref<string | null>(null)
const flowError = ref<string | null>(null)
const authURLResponse = ref<CPAOAuthAuthURLResponse | null>(null)
const statusResponse = ref<CPAOAuthStatusResponse | null>(null)
const flowModalVisible = ref(false)
const flowStage = ref<FlowStage>('waiting')
const callbackAccepted = ref(false)

const providerCards = computed(() => providers.value.map((provider, index) => toProviderView(provider, index)))
const activeProvider = computed(() => {
  return providerCards.value.find((provider) => provider.id === selectedProvider.value)
    ?? toProviderView({ id: selectedProvider.value, label: selectedProvider.value }, 0)
})
const authURL = computed(() => getFirstString(authURLResponse.value, [
  'url',
  'auth_url',
  'authUrl',
  'authorization_url',
  'authorizationUrl',
  'login_url',
  'loginUrl',
]))
const authState = computed(() => getFirstString(authURLResponse.value, ['state', 'oauth_state', 'oauthState']))
const deviceCode = computed(() => getFirstString(authURLResponse.value, ['user_code', 'userCode', 'device_code', 'deviceCode']))
const isDeviceFlow = computed(() => {
  return getFirstString(authURLResponse.value, ['flow']).toLowerCase() === 'device' || Boolean(deviceCode.value)
})
const canSubmitCallback = computed(() => redirectURL.value.trim().length > 0 && !isSubmittingCallback.value)

onMounted(loadProviders)

async function loadProviders() {
  isLoadingProviders.value = true
  pageError.value = null
  try {
    const response = await listCPAOAuthProviders()
    if (response.providers.length) providers.value = response.providers
  } catch (error) {
    pageError.value = errorText(error, '加载登录服务失败', 'Failed to load sign-in services')
  } finally {
    isLoadingProviders.value = false
  }
}

function getFirstString(source: Record<string, unknown> | null | undefined, keys: string[]) {
  if (!source) return ''
  for (const key of keys) {
    const value = source[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function responseStatus(source: Record<string, unknown> | null | undefined) {
  return getFirstString(source, ['status']).toLowerCase()
}

function isSuccessfulResponse(source: Record<string, unknown> | null | undefined) {
  return ['ok', 'success', 'complete', 'completed'].includes(responseStatus(source))
}

function responseError(source: Record<string, unknown> | null | undefined) {
  return getFirstString(source, ['error', 'message', 'detail'])
}

function toProviderView(provider: CPAOAuthProvider, index: number): ProviderView {
  const meta = providerMeta[provider.id] ?? providerMeta[provider.id.toLowerCase()]
  return {
    id: provider.id,
    label: provider.label,
    shortName: meta?.shortName ?? provider.label,
    tag: meta?.tag ?? 'OAuth',
    descriptionZh: meta?.descriptionZh ?? `通过 CPA 添加 ${provider.label} 授权账号。`,
    descriptionEn: meta?.descriptionEn ?? `Add a ${provider.label} account through CPA.`,
    methodZh: meta?.methodZh ?? 'OAuth 授权',
    methodEn: meta?.methodEn ?? 'OAuth authorization',
    logoPath: meta?.logoPath ?? '',
    iconText: meta?.iconText ?? provider.label.slice(0, 1).toUpperCase(),
    sequence: String(index + 1).padStart(2, '0'),
    tone: meta?.tone ?? 'codex',
  }
}

function openPendingOAuthWindow(providerLabel: string) {
  const popup = window.open('about:blank', '_blank')
  if (!popup) return null

  try {
    popup.opener = null
    popup.document.title = 'CPA OAuth'
    popup.document.body.style.margin = '0'
    popup.document.body.style.fontFamily = 'system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif'
    popup.document.body.innerHTML = `
      <main style="min-height:100vh;display:grid;place-items:center;background:#f7f8fa;color:#1f2937">
        <section style="max-width:420px;padding:28px;text-align:center">
          <h1 id="oauth-pending-title" style="margin:0 0 10px;font-size:20px"></h1>
          <p style="margin:0;color:#64748b;line-height:1.6">CPA-Helper is creating the OAuth URL.</p>
        </section>
      </main>
    `
    const title = popup.document.getElementById('oauth-pending-title')
    if (title) title.textContent = `Opening ${providerLabel}...`
  } catch {
    // The placeholder is optional; some browsers restrict writing to it.
  }
  return popup
}

function closeOAuthWindow(popup: Window | null) {
  if (popup && !popup.closed) popup.close()
}

function navigateOAuthWindow(popup: Window | null, url: string) {
  if (popup && !popup.closed) {
    popup.location.href = url
    return true
  }
  return Boolean(window.open(url, '_blank', 'noopener'))
}

function resetFlow() {
  redirectURL.value = ''
  authURLResponse.value = null
  statusResponse.value = null
  flowError.value = null
  flowStage.value = 'waiting'
  callbackAccepted.value = false
}

function continueAddingAccount() {
  flowModalVisible.value = false
  resetFlow()
}

async function startOAuth(providerID: string) {
  if (startingProvider.value) return

  selectedProvider.value = providerID
  resetFlow()
  const provider = providerCards.value.find((item) => item.id === providerID) ?? activeProvider.value
  const pendingWindow = openPendingOAuthWindow(provider.shortName)
  startingProvider.value = providerID
  try {
    const response = await createCPAOAuthURL({ provider: providerID })
    authURLResponse.value = response
    if (!isSuccessfulResponse(response)) {
      throw new Error(responseError(response) || t('CPA 创建登录会话失败', 'CPA failed to create the sign-in session'))
    }
    if (!authURL.value) {
      throw new Error(t('CPA 没有返回登录地址', 'CPA did not return a sign-in URL'))
    }

    const opened = navigateOAuthWindow(pendingWindow, authURL.value)
    flowModalVisible.value = true
    if (!opened) {
      message.warning(t('浏览器拦截了登录页，请在弹窗中重新打开', 'The browser blocked the sign-in page; reopen it from the dialog'))
    }
  } catch (error) {
    closeOAuthWindow(pendingWindow)
    flowStage.value = 'error'
    flowError.value = errorText(error, '打开 CPA 登录页失败', 'Failed to open the CPA sign-in page')
    flowModalVisible.value = true
  } finally {
    startingProvider.value = null
  }
}

function reopenAuthURL() {
  if (!authURL.value || !window.open(authURL.value, '_blank', 'noopener')) {
    message.warning(t('浏览器拦截了登录页', 'The browser blocked the sign-in page'))
  }
}

function validateCallbackURL(value: string) {
  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    return t('请输入浏览器地址栏中的完整回调 URL', 'Enter the complete callback URL from the browser address bar')
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) {
    return t('回调 URL 必须使用 http 或 https', 'The callback URL must use http or https')
  }
  const state = parsed.searchParams.get('state')?.trim() ?? ''
  const code = parsed.searchParams.get('code')?.trim() ?? ''
  const callbackError = parsed.searchParams.get('error')?.trim() ?? parsed.searchParams.get('error_description')?.trim() ?? ''
  if (!state) return t('回调 URL 缺少 state 参数', 'The callback URL is missing the state parameter')
  if (!code && !callbackError) return t('回调 URL 缺少 code 或 error 参数', 'The callback URL is missing a code or error parameter')
  if (authState.value && state !== authState.value) {
    return t('回调 URL 的 state 与当前登录会话不匹配', 'The callback state does not match the current sign-in session')
  }
  return ''
}

async function submitCallback() {
  const value = redirectURL.value.trim()
  const validationMessage = validateCallbackURL(value)
  if (validationMessage) {
    flowStage.value = 'error'
    flowError.value = validationMessage
    return
  }

  isSubmittingCallback.value = true
  flowStage.value = 'validating'
  flowError.value = null
  try {
    const response = await submitCPAOAuthCallback({
      provider: selectedProvider.value,
      redirect_url: value,
    })
    statusResponse.value = response
    if (!isSuccessfulResponse(response)) {
      throw new Error(responseError(response) || t('CPA 回调验证失败', 'CPA callback validation failed'))
    }
    callbackAccepted.value = true
    await waitForCompletion()
  } catch (error) {
    flowStage.value = 'error'
    flowError.value = errorText(error, 'CPA 回调验证失败', 'CPA callback validation failed')
  } finally {
    isSubmittingCallback.value = false
  }
}

function delay(milliseconds: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds))
}

async function waitForCompletion() {
  if (!authState.value) {
    throw new Error(t('CPA 回调响应缺少可验证的 state', 'The CPA callback response has no state to verify'))
  }

  for (let attempt = 0; attempt < 12; attempt += 1) {
    await delay(attempt === 0 ? 400 : 750)
    const response = await getCPAOAuthStatus(authState.value)
    statusResponse.value = response
    if (isSuccessfulResponse(response)) {
      flowStage.value = 'success'
      message.success(t('回调成功', 'Callback succeeded'))
      return
    }
    if (responseStatus(response) === 'error') {
      throw new Error(responseError(response) || t('CPA 保存账号失败', 'CPA failed to save the account'))
    }
  }

  flowStage.value = 'waiting'
  message.info(t('CPA 已接收回调，账号仍在保存中', 'CPA accepted the callback and is still saving the account'))
}

async function checkStatus() {
  if (!authState.value) {
    flowStage.value = 'error'
    flowError.value = t('当前登录会话缺少 state', 'The current sign-in session has no state')
    return
  }

  isChecking.value = true
  flowStage.value = 'validating'
  flowError.value = null
  try {
    const response = await getCPAOAuthStatus(authState.value)
    statusResponse.value = response
    if (isSuccessfulResponse(response)) {
      flowStage.value = 'success'
      message.success(t('登录成功', 'Sign-in succeeded'))
      return
    }
    if (responseStatus(response) === 'error') {
      throw new Error(responseError(response) || t('CPA 登录失败', 'CPA sign-in failed'))
    }
    flowStage.value = 'waiting'
    message.info(t('CPA 仍在等待登录完成', 'CPA is still waiting for sign-in to finish'))
  } catch (error) {
    flowStage.value = 'error'
    flowError.value = errorText(error, '查询 CPA 登录状态失败', 'Failed to check CPA sign-in status')
  } finally {
    isChecking.value = false
  }
}
</script>

<template>
  <section class="page cpa-oauth-page">
    <div class="page-heading">
      <div>
        <span class="page-kicker">CPA / ACCOUNT ACCESS</span>
        <h1 class="page-title">{{ t('登录服务', 'Sign-in services') }}</h1>
        <p class="page-subtitle">{{ t('选择对应渠道，将新的认证账号安全写入 CPA。', 'Choose a provider and add a new authenticated account to CPA.') }}</p>
      </div>
      <NButton quaternary circle :loading="isLoadingProviders" :title="t('刷新登录服务', 'Refresh sign-in services')" @click="loadProviders">
        <template #icon><RefreshCw :size="17" /></template>
      </NButton>
    </div>

    <NAlert v-if="pageError" type="error" :bordered="false" closable @close="pageError = null">
      {{ pageError }}
    </NAlert>

    <NSpin :show="isLoadingProviders">
      <div class="provider-grid">
        <button
          v-for="provider in providerCards"
          :key="provider.id"
          type="button"
          class="provider-card"
          :class="`tone-${provider.tone}`"
          :disabled="Boolean(startingProvider)"
          @click="startOAuth(provider.id)"
        >
          <div class="provider-art">
            <div class="provider-art-toolbar">
              <span class="provider-index">{{ provider.sequence }}</span>
              <span class="provider-method-label">{{ t(provider.methodZh, provider.methodEn) }}</span>
            </div>
            <span class="provider-logo-shell">
              <img v-if="provider.logoPath" class="provider-logo-image" :src="provider.logoPath" :alt="`${provider.label} logo`">
              <span v-else class="provider-logo-fallback">{{ provider.iconText }}</span>
            </span>
            <strong class="provider-wordmark">{{ provider.shortName }}</strong>
          </div>
          <div class="provider-body">
            <span class="provider-owner">{{ provider.tag }}</span>
            <p class="provider-description">{{ t(provider.descriptionZh, provider.descriptionEn) }}</p>
            <span class="provider-action">
              <span>{{ t('连接账号', 'Connect account') }}</span>
              <span class="provider-action-icon">
                <Loader2 v-if="startingProvider === provider.id" :size="16" class="spin-icon" />
                <LogIn v-else :size="16" />
              </span>
            </span>
          </div>
        </button>
      </div>
    </NSpin>

    <NModal
      v-model:show="flowModalVisible"
      preset="card"
      :title="`${activeProvider.shortName} ${t('登录', 'sign-in')}`"
      :style="{ width: 'min(560px, calc(100vw - 32px))' }"
      :mask-closable="!isSubmittingCallback && !isChecking"
    >
      <div class="oauth-dialog">
        <div v-if="flowStage === 'success'" class="result-state success-state">
          <CheckCircle2 :size="34" />
          <div>
            <h3>{{ isDeviceFlow ? t('登录成功', 'Sign-in succeeded') : t('回调成功', 'Callback succeeded') }}</h3>
            <p>{{ t('CPA 已确认本次授权，可以继续添加账号。', 'CPA confirmed this authorization; you can continue adding accounts.') }}</p>
          </div>
        </div>

        <template v-else>
          <div class="dialog-provider">
            <div class="brand-mark" :class="`tone-${activeProvider.tone}`">
              <img v-if="activeProvider.logoPath" :src="activeProvider.logoPath" :alt="`${activeProvider.label} logo`">
              <span v-else>{{ activeProvider.iconText }}</span>
            </div>
            <div>
              <strong>{{ activeProvider.label }}</strong>
              <span v-if="authState">State: {{ authState }}</span>
            </div>
          </div>

          <NAlert v-if="flowError" type="error" :bordered="false" closable @close="flowError = null">
            {{ flowError }}
          </NAlert>

          <template v-if="authURLResponse && isDeviceFlow">
            <div v-if="deviceCode" class="device-code">
              <span>{{ t('设备码', 'Device code') }}</span>
              <strong>{{ deviceCode }}</strong>
            </div>
            <p class="dialog-copy">{{ t('在登录页完成确认后，返回这里验证登录结果。', 'Complete confirmation on the sign-in page, then verify the result here.') }}</p>
          </template>

          <template v-else-if="authURLResponse">
            <div class="callback-heading">
              <ShieldCheck :size="20" />
              <div>
                <h3>{{ callbackAccepted ? t('CPA 正在保存账号', 'CPA is saving the account') : t('输入回调 URL', 'Enter callback URL') }}</h3>
                <p>{{ callbackAccepted ? t('回调已接收，请验证账号保存结果。', 'The callback was accepted; verify that the account was saved.') : t('粘贴授权完成后浏览器地址栏中的完整 URL。', 'Paste the complete browser address after authorization.') }}</p>
              </div>
            </div>
            <NInput
              v-model:value="redirectURL"
              type="textarea"
              :autosize="{ minRows: 3, maxRows: 5 }"
              :disabled="isSubmittingCallback || callbackAccepted"
              placeholder="http://127.0.0.1:8317/codex/callback?code=...&state=..."
              @keydown.ctrl.enter.prevent="submitCallback"
            />
          </template>

          <div v-if="flowStage === 'validating'" class="validating-row">
            <Loader2 :size="17" class="spin-icon" />
            <span>{{ t('正在等待 CPA 验证', 'Waiting for CPA validation') }}</span>
          </div>
        </template>

        <div class="dialog-actions">
          <template v-if="flowStage === 'success'">
            <NButton type="primary" @click="continueAddingAccount">
              <template #icon><KeyRound :size="16" /></template>
              {{ t('继续添加账号', 'Add another account') }}
            </NButton>
          </template>
          <template v-else>
            <NButton v-if="authURL" secondary :disabled="isSubmittingCallback || isChecking" @click="reopenAuthURL">
              <template #icon><ExternalLink :size="16" /></template>
              {{ t('重新打开登录页', 'Reopen sign-in page') }}
            </NButton>
            <NButton
              v-if="isDeviceFlow"
              type="primary"
              :loading="isChecking"
              :disabled="!authState || isSubmittingCallback"
              @click="checkStatus"
            >
              {{ t('验证登录结果', 'Verify sign-in') }}
            </NButton>
            <NButton
              v-else-if="authURLResponse && callbackAccepted"
              type="primary"
              :loading="isChecking"
              :disabled="!authState || isSubmittingCallback"
              @click="checkStatus"
            >
              {{ t('验证保存结果', 'Verify saved account') }}
            </NButton>
            <NButton
              v-else-if="authURLResponse"
              type="primary"
              :loading="isSubmittingCallback"
              :disabled="!canSubmitCallback"
              @click="submitCallback"
            >
              {{ t('提交并验证', 'Submit and verify') }}
            </NButton>
            <NButton v-else secondary @click="flowModalVisible = false">{{ t('关闭', 'Close') }}</NButton>
          </template>
        </div>
      </div>
    </NModal>
  </section>
</template>

<style scoped>
.cpa-oauth-page {
  display: grid;
  gap: 22px;
}

.page-heading {
  display: flex;
  width: 100%;
  max-width: 1360px;
  margin: 0 auto;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.page-heading h1,
.page-heading p {
  margin: 0;
}

.page-heading p {
  max-width: 640px;
  margin-top: 7px;
  color: var(--cpa-text-muted);
  font-size: 14px;
  line-height: 1.6;
}

.page-kicker {
  display: block;
  margin-bottom: 6px;
  color: var(--cpa-primary);
  font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0;
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  width: 100%;
  max-width: 1360px;
  margin: 0 auto;
  gap: 18px;
}

.provider-card {
  --provider-color: #168570;
  --provider-soft: #e9f6f2;
  --provider-art-bg: #dceee8;
  --provider-art-ink: #12362d;
  display: grid;
  grid-template-rows: 142px minmax(156px, 1fr);
  min-width: 0;
  min-height: 298px;
  padding: 0;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--provider-color) 16%, var(--cpa-border));
  border-radius: 8px;
  background: var(--cpa-surface);
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
  box-shadow: 0 3px 10px color-mix(in srgb, var(--provider-color) 7%, transparent), var(--cpa-shadow-hairline);
  transition: border-color 220ms ease, box-shadow 220ms ease, transform 220ms ease;
}

.provider-card:hover,
.provider-card:focus-visible {
  border-color: color-mix(in srgb, var(--provider-color) 46%, var(--cpa-border));
  box-shadow: 0 18px 36px color-mix(in srgb, var(--provider-color) 15%, transparent), var(--cpa-shadow-hairline);
  transform: translateY(-4px);
  outline: none;
}

.provider-card:focus-visible {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--provider-color) 24%, transparent), 0 18px 36px color-mix(in srgb, var(--provider-color) 15%, transparent);
}

.provider-card:active {
  transform: translateY(-1px) scale(0.995);
}

.provider-card:disabled {
  cursor: wait;
}

.provider-art {
  position: relative;
  display: grid;
  grid-template-rows: auto 1fr auto;
  min-width: 0;
  padding: 15px 18px 17px;
  overflow: hidden;
  border-bottom: 1px solid color-mix(in srgb, var(--provider-color) 18%, transparent);
  background: var(--provider-art-bg);
  color: var(--provider-art-ink);
}

.provider-art::after {
  position: absolute;
  inset: 0 0 0 52%;
  background: repeating-linear-gradient(135deg, transparent 0 12px, color-mix(in srgb, var(--provider-art-ink) 8%, transparent) 12px 13px);
  content: '';
  pointer-events: none;
}

.provider-art-toolbar {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.provider-index,
.provider-method-label {
  font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0;
}

.provider-index {
  opacity: 0.62;
}

.provider-method-label {
  padding: 4px 7px;
  border: 1px solid color-mix(in srgb, var(--provider-art-ink) 16%, transparent);
  border-radius: 4px;
  background: color-mix(in srgb, white 42%, transparent);
}

.provider-logo-shell {
  position: absolute;
  z-index: 1;
  top: 48px;
  left: 18px;
  display: grid;
  width: 58px;
  height: 58px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--provider-art-ink) 12%, transparent);
  border-radius: 8px;
  background: rgb(255 255 255 / 78%);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 86%), 0 8px 20px color-mix(in srgb, var(--provider-art-ink) 10%, transparent);
  transition: transform 220ms ease;
}

.provider-card:hover .provider-logo-shell {
  transform: translateY(-2px) rotate(-2deg);
}

.provider-logo-image {
  display: block;
  width: 34px;
  height: 34px;
  object-fit: contain;
}

.tone-kimi .provider-logo-image {
  width: 42px;
  height: 42px;
  border-radius: 8px;
}

.provider-logo-fallback {
  font-size: 21px;
  font-weight: 800;
}

.provider-wordmark {
  position: relative;
  z-index: 1;
  align-self: end;
  justify-self: end;
  max-width: calc(100% - 78px);
  overflow: hidden;
  font-size: 24px;
  font-weight: 760;
  line-height: 1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.provider-body {
  display: grid;
  grid-template-rows: auto minmax(42px, 1fr) auto;
  min-width: 0;
  gap: 11px;
  padding: 17px 18px 15px;
}

.provider-owner {
  color: var(--provider-color);
  font-size: 12px;
  font-weight: 750;
}

.provider-description {
  display: -webkit-box;
  margin: 0;
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-size: 13px;
  line-height: 1.6;
  text-wrap: pretty;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.provider-action {
  display: flex;
  min-width: 0;
  min-height: 38px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 11px;
  border-top: 1px solid color-mix(in srgb, var(--provider-color) 15%, var(--cpa-border));
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 700;
}

.provider-action-icon {
  display: grid;
  width: 29px;
  height: 29px;
  flex: 0 0 29px;
  place-items: center;
  border-radius: 6px;
  background: var(--provider-soft);
  color: var(--provider-color);
  transition: background 180ms ease, color 180ms ease, transform 180ms ease;
}

.provider-card:hover .provider-action-icon {
  background: var(--provider-color);
  color: white;
  transform: translateX(2px);
}

.tone-codex {
  --provider-color: #168570;
  --provider-soft: #e9f6f2;
  --provider-art-bg: #dceee8;
  --provider-art-ink: #12362d;
}

.tone-claude {
  --provider-color: #c45f3d;
  --provider-soft: #fcf1ea;
  --provider-art-bg: #f5e5da;
  --provider-art-ink: #603321;
}

.tone-gemini {
  --provider-color: #6d5eaa;
  --provider-soft: #f2f0fa;
  --provider-art-bg: #e8e6f4;
  --provider-art-ink: #40386a;
}

.tone-antigravity {
  --provider-color: #7846b8;
  --provider-soft: #f5effb;
  --provider-art-bg: #eee5f7;
  --provider-art-ink: #4c2f71;
}

.tone-grok {
  --provider-color: #263746;
  --provider-soft: #edf0f2;
  --provider-art-bg: #e0e5e8;
  --provider-art-ink: #111c25;
}

.tone-kimi {
  --provider-color: #1778d3;
  --provider-soft: #edf6ff;
  --provider-art-bg: #dfedfb;
  --provider-art-ink: #153f69;
}

:root.dark .tone-codex { --provider-soft: #183d34; --provider-art-bg: #173229; --provider-art-ink: #d9f5eb; }
:root.dark .tone-claude { --provider-soft: #4b2b20; --provider-art-bg: #45271d; --provider-art-ink: #ffe8db; }
:root.dark .tone-gemini { --provider-soft: #36304f; --provider-art-bg: #2d2945; --provider-art-ink: #ece8ff; }
:root.dark .tone-antigravity { --provider-soft: #3d2a53; --provider-art-bg: #342547; --provider-art-ink: #f1e7ff; }
:root.dark .tone-grok { --provider-color: #9eb7c8; --provider-soft: #28353d; --provider-art-bg: #1e282f; --provider-art-ink: #f2f5f7; }
:root.dark .tone-kimi { --provider-soft: #1b3c5b; --provider-art-bg: #17324d; --provider-art-ink: #e3f0ff; }

:root.dark .provider-card.tone-grok:hover .provider-action-icon {
  color: #111c25;
}

.brand-mark,
.dialog-provider,
.callback-heading,
.result-state,
.validating-row {
  display: flex;
  align-items: center;
}

.brand-mark {
  --provider-color: var(--cpa-primary);
  --provider-soft: var(--cpa-primary-weak);
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex: 0 0 44px;
  border: 1px solid color-mix(in srgb, var(--provider-color) 30%, transparent);
  border-radius: 8px;
  background: var(--provider-soft);
  color: var(--provider-color);
  font-size: 18px;
  font-weight: 750;
}

.brand-mark img {
  display: block;
  width: 25px;
  height: 25px;
  object-fit: contain;
}

.brand-mark.tone-kimi img {
  width: 34px;
  height: 34px;
  border-radius: 7px;
}

.oauth-dialog {
  display: grid;
  gap: 18px;
}

.dialog-provider {
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--cpa-border);
}

.dialog-provider > div:last-child {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.dialog-provider span {
  overflow: hidden;
  color: var(--cpa-text-muted);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.callback-heading {
  align-items: flex-start;
  gap: 10px;
}

.callback-heading h3,
.callback-heading p,
.result-state h3,
.result-state p,
.dialog-copy {
  margin: 0;
}

.callback-heading h3,
.result-state h3 {
  font-size: 16px;
}

.callback-heading p,
.result-state p,
.dialog-copy {
  margin-top: 4px;
  color: var(--cpa-text-muted);
  font-size: 13px;
  line-height: 1.6;
}

.device-code {
  display: grid;
  gap: 6px;
  padding: 14px;
  border: 1px solid var(--cpa-border);
  border-radius: 8px;
  background: var(--cpa-surface-muted);
}

.device-code span {
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.device-code strong {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 22px;
  letter-spacing: 0;
}

.validating-row {
  gap: 8px;
  color: var(--cpa-text-muted);
  font-size: 13px;
}

.result-state {
  align-items: flex-start;
  gap: 14px;
  padding: 16px;
  border: 1px solid #86c99a;
  border-radius: 8px;
  background: #f2fbf4;
  color: #166534;
}

.result-state p {
  color: #3f6f4c;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.spin-icon {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 1100px) {
  .provider-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .cpa-oauth-page {
    gap: 18px;
  }

  .provider-grid {
    grid-template-columns: 1fr;
    gap: 14px;
  }

  .provider-card {
    grid-template-rows: 132px minmax(150px, 1fr);
    min-height: 282px;
  }

  .provider-wordmark {
    font-size: 22px;
  }

  .dialog-actions .n-button {
    flex: 1 1 100%;
  }
}
</style>
