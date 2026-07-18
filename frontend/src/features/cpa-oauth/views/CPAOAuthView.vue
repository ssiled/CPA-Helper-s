<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NInput, NModal, NSpin, NTag, useMessage } from 'naive-ui'
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
  description: string
  iconText: string
  tone: 'blue' | 'orange' | 'green' | 'purple' | 'dark' | 'teal'
}

type FlowStage = 'waiting' | 'validating' | 'success' | 'error'

const message = useMessage()
const { errorText, t } = useI18n()

const defaultProviders: CPAOAuthProvider[] = [
  { id: 'codex', label: 'Codex / OpenAI' },
  { id: 'anthropic', label: 'Claude' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'antigravity', label: 'Antigravity' },
  { id: 'xai', label: 'Grok / xAI' },
  { id: 'kimi', label: 'Kimi' },
]

const providerMeta: Record<string, Omit<ProviderView, 'id' | 'label'>> = {
  codex: {
    shortName: 'Codex',
    tag: 'OpenAI',
    description: 'Codex CLI / OpenAI OAuth',
    iconText: 'C',
    tone: 'blue',
  },
  anthropic: {
    shortName: 'Claude',
    tag: 'Anthropic',
    description: 'Claude Code / Claude CLI OAuth',
    iconText: '\u2726',
    tone: 'orange',
  },
  gemini: {
    shortName: 'Gemini',
    tag: 'Google',
    description: 'Gemini CLI OAuth',
    iconText: 'G',
    tone: 'green',
  },
  antigravity: {
    shortName: 'Antigravity',
    tag: 'Google',
    description: 'Antigravity OAuth',
    iconText: 'A',
    tone: 'purple',
  },
  xai: {
    shortName: 'Grok',
    tag: 'xAI',
    description: 'Grok / xAI device authorization',
    iconText: 'X',
    tone: 'dark',
  },
  kimi: {
    shortName: 'Kimi',
    tag: 'Moonshot',
    description: 'Kimi device authorization',
    iconText: 'K',
    tone: 'teal',
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

const providerCards = computed(() => providers.value.map(toProviderView))
const activeProvider = computed(() => {
  return providerCards.value.find((provider) => provider.id === selectedProvider.value)
    ?? toProviderView({ id: selectedProvider.value, label: selectedProvider.value })
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

function toProviderView(provider: CPAOAuthProvider): ProviderView {
  const meta = providerMeta[provider.id] ?? providerMeta[provider.id.toLowerCase()]
  return {
    id: provider.id,
    label: provider.label,
    shortName: meta?.shortName ?? provider.label,
    tag: meta?.tag ?? 'OAuth',
    description: meta?.description ?? `${provider.label} OAuth`,
    iconText: meta?.iconText ?? provider.label.slice(0, 1).toUpperCase(),
    tone: meta?.tone ?? 'teal',
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
        <h1 class="page-title">{{ t('登录服务', 'Sign-in services') }}</h1>
        <p class="page-subtitle">{{ t('选择服务并添加 CPA 账号。', 'Choose a service to add a CPA account.') }}</p>
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
          <div class="provider-card-top">
            <div class="brand-mark">{{ provider.iconText }}</div>
            <NTag size="small" :bordered="false">{{ provider.tag }}</NTag>
          </div>
          <div class="provider-copy">
            <h2>{{ provider.shortName }}</h2>
            <p>{{ provider.description }}</p>
          </div>
          <span class="provider-action">
            <Loader2 v-if="startingProvider === provider.id" :size="16" class="spin-icon" />
            <LogIn v-else :size="16" />
            <span>{{ t('登录', 'Sign in') }}</span>
          </span>
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
            <div class="brand-mark" :class="`tone-${activeProvider.tone}`">{{ activeProvider.iconText }}</div>
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
  gap: 18px;
}

.page-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.page-heading h1,
.page-heading p {
  margin: 0;
}

.page-heading p {
  margin-top: 6px;
  color: var(--cpa-text-muted);
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 14px;
}

.provider-card {
  --provider-color: #0f766e;
  display: grid;
  min-height: 210px;
  padding: 18px;
  gap: 18px;
  border: 1px solid var(--cpa-border);
  border-top: 3px solid var(--provider-color);
  border-radius: 8px;
  background: var(--cpa-surface);
  color: inherit;
  cursor: pointer;
  font: inherit;
  text-align: left;
  transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease;
}

.provider-card:hover,
.provider-card:focus-visible {
  border-color: color-mix(in srgb, var(--provider-color) 55%, var(--cpa-border));
  box-shadow: 0 8px 22px rgb(15 23 42 / 8%);
  transform: translateY(-2px);
  outline: none;
}

.provider-card:disabled {
  cursor: wait;
}

.provider-card-top,
.dialog-provider,
.callback-heading,
.result-state,
.validating-row {
  display: flex;
  align-items: center;
}

.provider-card-top {
  justify-content: space-between;
  gap: 12px;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 42px;
  height: 42px;
  flex: 0 0 42px;
  border: 1px solid color-mix(in srgb, var(--provider-color) 30%, transparent);
  border-radius: 8px;
  background: color-mix(in srgb, var(--provider-color) 10%, var(--cpa-surface));
  color: var(--provider-color);
  font-size: 18px;
  font-weight: 750;
}

.provider-copy {
  align-self: start;
}

.provider-copy h2,
.provider-copy p {
  margin: 0;
}

.provider-copy h2 {
  font-size: 18px;
}

.provider-copy p {
  margin-top: 5px;
  color: var(--cpa-text-muted);
  font-size: 13px;
  line-height: 1.55;
}

.provider-action {
  display: inline-flex;
  min-height: 34px;
  align-self: end;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 7px 14px;
  border-radius: 5px;
  background: var(--cpa-primary);
  color: white;
  font-size: 14px;
  font-weight: 650;
}

.tone-blue { --provider-color: #2563eb; }
.tone-orange { --provider-color: #c2410c; }
.tone-green { --provider-color: #15803d; }
.tone-purple { --provider-color: #7c3aed; }
.tone-dark { --provider-color: #334155; }
.tone-teal { --provider-color: #0f766e; }

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

@media (max-width: 640px) {
  .provider-grid {
    grid-template-columns: 1fr;
  }

  .provider-card {
    min-height: 190px;
  }

  .dialog-actions .n-button {
    flex: 1 1 100%;
  }
}
</style>
