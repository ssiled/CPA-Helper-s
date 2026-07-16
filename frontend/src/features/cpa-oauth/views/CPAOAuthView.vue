<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NInput, NSpin, NTag, useMessage } from 'naive-ui'
import {
  CheckCircle2,
  Clipboard,
  ExternalLink,
  KeyRound,
  Link2,
  Loader2,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  TerminalSquare,
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
  helper: string
}

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
    description: '\u4e3a Codex CLI / OpenAI \u8d26\u53f7\u751f\u6210\u6388\u6743\u94fe\u63a5\uff0c\u5b8c\u6210\u540e CPA \u4f1a\u4fdd\u5b58\u8ba4\u8bc1\u6587\u4ef6\u3002',
    iconText: 'C',
    tone: 'blue',
    helper: '\u6d4f\u89c8\u5668\u6388\u6743\u540e\uff0c\u901a\u5e38\u4f1a\u8df3\u8f6c\u5230 localhost \u56de\u8c03\u5730\u5740\u3002',
  },
  anthropic: {
    shortName: 'Claude',
    tag: 'Anthropic',
    description: '\u767b\u5f55 Anthropic Claude \u670d\u52a1\uff0c\u9002\u5408 Claude Code / Claude CLI \u8ba4\u8bc1\u3002',
    iconText: '\u2726',
    tone: 'orange',
    helper: '\u5b8c\u6210\u6388\u6743\u540e\u590d\u5236\u5b8c\u6574\u56de\u8c03 URL \u63d0\u4ea4\u5230 CPA\u3002',
  },
  gemini: {
    shortName: 'Gemini',
    tag: 'Google',
    description: '\u901a\u8fc7 Google OAuth \u4e3a Gemini CLI \u83b7\u53d6\u51ed\u636e\uff0c\u53ef\u6309\u9700\u586b\u5199 GCP Project ID\u3002',
    iconText: 'G',
    tone: 'green',
    helper: '\u5982\u679c\u6709\u6307\u5b9a\u9879\u76ee\uff0c\u8bf7\u5148\u586b\u5199 Project ID \u518d\u53d1\u8d77\u3002',
  },
  antigravity: {
    shortName: 'Antigravity',
    tag: 'Google',
    description: '\u4e3a Antigravity \u83b7\u53d6 Google \u6388\u6743\uff0c\u81ea\u52a8\u4ea4\u7531 CPA \u7ba1\u7406\u8ba4\u8bc1\u72b6\u6001\u3002',
    iconText: 'A',
    tone: 'purple',
    helper: '\u6388\u6743\u6d41\u7a0b\u4e0e Google \u8d26\u53f7\u7ed1\u5b9a\uff0c\u6ce8\u610f\u9009\u62e9\u6b63\u786e\u8d26\u53f7\u3002',
  },
  xai: {
    shortName: 'Grok',
    tag: 'xAI',
    description: '\u4f7f\u7528 xAI \u8bbe\u5907\u6388\u6743\u6d41\u7a0b\u767b\u5f55 Grok\uff0c\u5b8c\u6210\u540e CPA \u4f1a\u4fdd\u5b58\u5bf9\u5e94\u8d26\u53f7\u51ed\u636e\u3002',
    iconText: 'X',
    tone: 'dark',
    helper: 'CPA \u8fd4\u56de\u8bbe\u5907\u6388\u6743\u94fe\u63a5\u548c\u72b6\u6001\uff0c\u6309\u9875\u9762\u63d0\u793a\u5b8c\u6210\u786e\u8ba4\u5373\u53ef\u3002',
  },
  kimi: {
    shortName: 'Kimi',
    tag: 'Moonshot',
    description: '\u4f7f\u7528\u8bbe\u5907\u6388\u6743\u6d41\u7a0b\u767b\u5f55 Kimi \u670d\u52a1\uff0c\u6309 CPA \u8fd4\u56de\u7684\u63d0\u793a\u5b8c\u6210\u786e\u8ba4\u3002',
    iconText: 'K',
    tone: 'dark',
    helper: 'Kimi \u53ef\u80fd\u8fd4\u56de\u8bbe\u5907\u7801\u6216\u9875\u9762\u63d0\u793a\uff0c\u800c\u4e0d\u4e00\u5b9a\u662f\u666e\u901a URL\u3002',
  },
}

const providers = ref<CPAOAuthProvider[]>(defaultProviders)
const selectedProvider = ref('codex')
const projectID = ref('')
const redirectURL = ref('')
const isLoadingProviders = ref(false)
const isStarting = ref(false)
const isChecking = ref(false)
const isSubmittingCallback = ref(false)
const errorMessage = ref<string | null>(null)
const authURLResponse = ref<CPAOAuthAuthURLResponse | null>(null)
const statusResponse = ref<CPAOAuthStatusResponse | null>(null)
const callbackSuccessMessage = ref<string | null>(null)

const providerCards = computed(() => providers.value.map(toProviderView))
const activeProvider = computed(() => {
  return providerCards.value.find((provider) => provider.id === selectedProvider.value) ?? toProviderView({ id: selectedProvider.value, label: selectedProvider.value })
})
const authURL = computed(() => getFirstString(authURLResponse.value, ['url', 'auth_url', 'authUrl', 'authorization_url', 'authorizationUrl', 'login_url', 'loginUrl']))
const authState = computed(() => getFirstString(authURLResponse.value, ['state', 'oauth_state', 'oauthState']))
const responseStatus = computed(() => getFirstString(authURLResponse.value, ['status', 'message', 'detail']))
const statusValue = computed(() => {
  const status = statusResponse.value?.status
  return typeof status === 'string' ? status : ''
})
const statusText = computed(() => (statusResponse.value ? JSON.stringify(statusResponse.value, null, 2) : ''))
const responseText = computed(() => (authURLResponse.value ? JSON.stringify(authURLResponse.value, null, 2) : ''))
const needsProjectID = computed(() => selectedProvider.value === 'gemini')
const hasActiveFlow = computed(() => Boolean(authURLResponse.value))
const startButtonText = computed(() => t('开始授权', 'Start authorization'))
const phaseText = computed(() => {
  if (callbackSuccessMessage.value) {
    return t('Callback submitted', 'Callback submitted')
  }
  if (statusResponse.value) {
    return statusValue.value || t('Status returned', 'Status returned')
  }
  if (authURLResponse.value) {
    return authURL.value ? t('Waiting for browser authorization', 'Waiting for browser authorization') : t('Waiting for next step', 'Waiting for next step')
  }
  return t('Not started', 'Not started')
})
const phaseType = computed<'default' | 'info' | 'success' | 'warning'>(() => {
  const normalized = statusValue.value.toLowerCase()
  if (callbackSuccessMessage.value) return 'success'
  if (normalized.includes('success') || normalized.includes('complete') || normalized.includes('ok')) return 'success'
  if (authURLResponse.value) return 'warning'
  return 'default'
})

onMounted(async () => {
  isLoadingProviders.value = true
  try {
    const response = await listCPAOAuthProviders()
    if (response.providers.length) {
      providers.value = response.providers
      if (!response.providers.some((provider) => provider.id === selectedProvider.value)) {
        selectedProvider.value = response.providers[0]?.id ?? 'codex'
      }
    }
  } catch (error) {
    errorMessage.value = errorText(error, '加载 CPA OAuth 提供商失败', 'Failed to load CPA OAuth providers')
  } finally {
    isLoadingProviders.value = false
  }
})


function getFirstString(source: Record<string, unknown> | null | undefined, keys: string[]) {
  if (!source) return ''
  for (const key of keys) {
    const value = source[key]
    if (typeof value === 'string' && value.trim()) return value
  }
  return ''
}

function openPendingOAuthWindow(providerLabel: string) {
  const popup = window.open('about:blank', '_blank')
  if (!popup) return null

  try {
    popup.opener = null
  } catch {
    // Ignore browsers that restrict opener changes.
  }

  try {
    popup.document.title = 'CPA OAuth'
    popup.document.body.style.margin = '0'
    popup.document.body.style.fontFamily = 'system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif'
    popup.document.body.innerHTML = `
      <main style="min-height:100vh;display:grid;place-items:center;background:#f6f8fb;color:#1f2937">
        <section style="max-width:420px;padding:28px;border:1px solid #d8dee9;border-radius:18px;background:white;box-shadow:0 18px 45px rgba(15,23,42,.10);text-align:center">
          <h1 id="oauth-pending-title" style="margin:0 0 10px;font-size:20px"></h1>
          <p style="margin:0;color:#64748b;line-height:1.6">CPA-Helper is creating the OAuth URL. This window will redirect automatically.</p>
        </section>
      </main>
    `
    const title = popup.document.getElementById('oauth-pending-title')
    if (title) title.textContent = `Creating ${providerLabel} authorization link...`
  } catch {
    // Ignore browsers that disallow writing to the placeholder window.
  }

  return popup
}

function navigateOAuthWindow(popup: Window | null, url: string) {
  if (popup && !popup.closed) {
    popup.location.href = url
    return true
  }
  const opened = window.open(url, '_blank', 'noopener')
  return Boolean(opened)
}

function closeOAuthWindow(popup: Window | null) {
  if (popup && !popup.closed) {
    popup.close()
  }
}

function toProviderView(provider: CPAOAuthProvider): ProviderView {
  const meta = providerMeta[provider.id] ?? providerMeta[provider.id.toLowerCase()]
  return {
    id: provider.id,
    label: provider.label,
    shortName: meta?.shortName ?? provider.label,
    tag: meta?.tag ?? 'OAuth',
    description: meta?.description ?? `通过 CPA 发起 ${provider.label} OAuth 登录并保存认证。`,
    iconText: meta?.iconText ?? provider.label.slice(0, 1).toUpperCase(),
    tone: meta?.tone ?? 'teal',
    helper: meta?.helper ?? '发起授权后按 CPA 返回的页面提示继续。',
  }
}

function selectProvider(providerID: string) {
  selectedProvider.value = providerID
  errorMessage.value = null
  callbackSuccessMessage.value = null
}

function resetFlow() {
  authURLResponse.value = null
  statusResponse.value = null
  redirectURL.value = ''
  errorMessage.value = null
  callbackSuccessMessage.value = null
}

async function startOAuth(providerID = selectedProvider.value) {
  selectedProvider.value = providerID
  const providerLabel = activeProvider.value.shortName || selectedProvider.value
  const pendingWindow = openPendingOAuthWindow(providerLabel)
  isStarting.value = true
  errorMessage.value = null
  statusResponse.value = null
  callbackSuccessMessage.value = null
  try {
    const payload: { provider: string; project_id?: string } = { provider: selectedProvider.value }
    if (needsProjectID.value && projectID.value.trim()) {
      payload.project_id = projectID.value.trim()
    }
    const response = await createCPAOAuthURL(payload)
    authURLResponse.value = response
    const nextURL = authURL.value
    if (nextURL) {
      const opened = navigateOAuthWindow(pendingWindow, nextURL)
      if (opened) {
        message.success(t('CPA OAuth sign-in page opened', 'CPA OAuth sign-in page opened'))
      } else {
        message.warning(t('The browser blocked the popup. Click Reopen below.', 'The browser blocked the popup. Click Reopen below.'))
      }
    } else {
      closeOAuthWindow(pendingWindow)
      message.info(t('CPA returned sign-in information. Continue with the details below.', 'CPA returned sign-in information. Continue with the details below.'))
    }
  } catch (error) {
    closeOAuthWindow(pendingWindow)
    errorMessage.value = errorText(error, 'Failed to create CPA OAuth sign-in URL', 'Failed to create CPA OAuth sign-in URL')
  } finally {
    isStarting.value = false
  }
}

async function checkStatus() {
  if (!authState.value) {
    message.warning(t('当前没有可查询的 OAuth state', 'No OAuth state to query'))
    return
  }
  isChecking.value = true
  errorMessage.value = null
  callbackSuccessMessage.value = null
  try {
    statusResponse.value = await getCPAOAuthStatus(authState.value)
  } catch (error) {
    errorMessage.value = errorText(error, '查询 CPA OAuth 状态失败', 'Failed to query CPA OAuth status')
  } finally {
    isChecking.value = false
  }
}

async function submitCallback() {
  if (!redirectURL.value.trim()) {
    message.warning(t('Paste the OAuth callback URL', 'Paste the OAuth callback URL'))
    return
  }
  isSubmittingCallback.value = true
  errorMessage.value = null
  callbackSuccessMessage.value = null
  try {
    statusResponse.value = await submitCPAOAuthCallback({
      provider: selectedProvider.value,
      redirect_url: redirectURL.value.trim(),
    })
    callbackSuccessMessage.value = t('OAuth callback submitted to CPA. The response is shown in the status panel.', 'OAuth callback submitted to CPA. The response is shown in the status panel.')
    message.success(t('OAuth callback submitted to CPA', 'OAuth callback submitted to CPA'))
  } catch (error) {
    errorMessage.value = errorText(error, 'Failed to submit OAuth callback', 'Failed to submit OAuth callback')
  } finally {
    isSubmittingCallback.value = false
  }
}

function openAuthURL() {
  if (!authURL.value) {
    message.warning(t('当前没有授权链接', 'No authorization URL available'))
    return
  }
  if (!window.open(authURL.value, '_blank', 'noopener')) {
    message.warning(t('The browser blocked the popup. Copy the link and open it manually.', 'The browser blocked the popup. Copy the link and open it manually.'))
  }
}

async function copyText(text: string, emptyMessage: string) {
  if (!text) {
    message.warning(emptyMessage)
    return
  }
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', 'true')
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
  }
  message.success(t('已复制到剪贴板', 'Copied to clipboard'))
}
</script>

<template>
  <section class="page cpa-oauth-page">
    <section class="oauth-hero panel">
      <div class="hero-glow" aria-hidden="true" />
      <div class="hero-copy">
        <div class="eyebrow">
          <Sparkles :size="16" />
          <span>{{ t('CPA OAuth 控制台', 'CPA OAuth Console') }}</span>
        </div>
        <h1 class="page-title">{{ t('一站式接入 Codex、Claude、Gemini 等 OAuth 认证', 'Connect Codex, Claude, Gemini and more from one OAuth console') }}</h1>
        <p class="page-subtitle">
          {{ t('选择服务后发起授权，CPA-Helper 会代理 CPA management API 创建登录链接、提交回调并查询状态。', 'Choose a service, start authorization, and CPA-Helper will proxy CPA management APIs to create sign-in links, submit callbacks and query status.') }}
        </p>
        <div class="hero-badges">
          <NTag round type="info">{{ t('所有登录用户可见', 'Visible to signed-in users') }}</NTag>
          <NTag round :bordered="false">{{ t('凭据由 CPA 保存', 'Credentials saved by CPA') }}</NTag>
          <NTag round :bordered="false">{{ t('支持手动回调', 'Manual callback supported') }}</NTag>
        </div>
      </div>
      <div class="hero-steps" aria-label="OAuth steps">
        <div class="hero-step is-active">
          <span>1</span>
          <strong>{{ t('发起', 'Start') }}</strong>
          <small>{{ t('创建授权链接', 'Create auth URL') }}</small>
        </div>
        <div class="hero-step">
          <span>2</span>
          <strong>{{ t('授权', 'Authorize') }}</strong>
          <small>{{ t('浏览器完成登录', 'Finish in browser') }}</small>
        </div>
        <div class="hero-step">
          <span>3</span>
          <strong>{{ t('保存', 'Save') }}</strong>
          <small>{{ t('提交回调 / 查询状态', 'Submit callback / check') }}</small>
        </div>
      </div>
    </section>

    <NAlert v-if="errorMessage" type="error" :bordered="false" closable @close="errorMessage = null">
      {{ errorMessage }}
    </NAlert>

    <section class="provider-area panel">
      <div class="panel-inner">
        <div class="section-heading">
          <div>
            <h2 class="section-title">{{ t('选择登录服务', 'Choose a sign-in service') }}</h2>
            <p>{{ t('从下方卡片选择需要写入 CPA 的认证类型。', 'Pick the credential type you want CPA to write.') }}</p>
          </div>
          <NButton tertiary :loading="isLoadingProviders" @click="resetFlow">
            <template #icon><RotateCcw :size="15" /></template>
            {{ t('重置流程', 'Reset flow') }}
          </NButton>
        </div>

        <NSpin :show="isLoadingProviders">
          <div class="provider-grid">
            <article
              v-for="provider in providerCards"
              :key="provider.id"
              class="provider-card"
              :class="[`tone-${provider.tone}`, { 'is-selected': selectedProvider === provider.id }]"
              role="button"
              tabindex="0"
              @click="selectProvider(provider.id)"
              @keydown.enter.prevent="selectProvider(provider.id)"
              @keydown.space.prevent="selectProvider(provider.id)"
            >
              <div class="provider-card-bg" aria-hidden="true" />
              <div class="provider-card-top">
                <div class="brand-mark">{{ provider.iconText }}</div>
                <NTag round size="small" :bordered="false">{{ provider.tag }}</NTag>
              </div>
              <h3>{{ provider.shortName }}</h3>
              <p>{{ provider.description }}</p>
              <div class="provider-helper">
                <KeyRound :size="14" />
                <span>{{ provider.helper }}</span>
              </div>
              <div class="provider-footer">
                <span v-if="selectedProvider === provider.id" class="selected-pill">
                  <CheckCircle2 :size="14" />
                  {{ t('当前选择', 'Selected') }}
                </span>
                <span v-else class="select-hint">{{ t('点击选择', 'Click to select') }}</span>
                <NButton
                  size="small"
                  type="primary"
                  :loading="isStarting && selectedProvider === provider.id"
                  @click.stop="startOAuth(provider.id)"
                >
                  {{ startButtonText }}
                </NButton>
              </div>
            </article>
          </div>
        </NSpin>
      </div>
    </section>

    <div class="workspace-grid">
      <section class="panel workbench-panel">
        <div class="panel-inner">
          <div class="workbench-header">
            <div class="workbench-title">
              <div class="brand-mark large" :class="`tone-${activeProvider.tone}`">{{ activeProvider.iconText }}</div>
              <div>
                <h2>{{ t('当前授权工作台', 'Current authorization workspace') }}</h2>
                <p>{{ activeProvider.label }} · {{ activeProvider.tag }}</p>
              </div>
            </div>
            <NButton type="primary" size="large" :loading="isStarting" @click="startOAuth()">
              <template #icon><ExternalLink :size="17" /></template>
              {{ t('打开授权页', 'Open authorization page') }}
            </NButton>
          </div>

          <div v-if="needsProjectID" class="project-field">
            <label>{{ t('Gemini GCP Project ID（可选）', 'Gemini GCP Project ID (optional)') }}</label>
            <NInput v-model:value="projectID" placeholder="my-gcp-project" clearable />
          </div>

          <div class="auth-surface" :class="{ 'is-empty': !hasActiveFlow }">
            <template v-if="authURLResponse">
              <div class="auth-surface-header">
                <div>
                  <span class="mini-label">{{ t('授权链接', 'Authorization URL') }}</span>
                  <strong>{{ authURL ? t('已生成，已尝试在新窗口打开', 'Generated and opened in a new window') : t('CPA 返回了登录信息', 'CPA returned sign-in information') }}</strong>
                </div>
                <NTag v-if="responseStatus" round type="info">{{ responseStatus }}</NTag>
              </div>

              <div v-if="authURL" class="url-box">
                <Link2 :size="16" />
                <code>{{ authURL }}</code>
              </div>
              <pre v-else class="compact-json">{{ responseText }}</pre>

              <div class="action-row">
                <NButton secondary :disabled="!authURL" @click="copyText(authURL, t('当前没有授权链接', 'No authorization URL available'))">
                  <template #icon><Clipboard :size="15" /></template>
                  {{ t('复制链接', 'Copy link') }}
                </NButton>
                <NButton secondary :disabled="!authURL" @click="openAuthURL">
                  <template #icon><ExternalLink :size="15" /></template>
                  {{ t('重新打开', 'Reopen') }}
                </NButton>
                <NButton secondary :disabled="!authState" :loading="isChecking" @click="checkStatus">
                  <template #icon><RefreshCw :size="15" /></template>
                  {{ t('查询状态', 'Check status') }}
                </NButton>
              </div>
            </template>
            <template v-else>
              <div class="empty-flow-icon"><TerminalSquare :size="28" /></div>
              <h3>{{ t('还没有授权会话', 'No authorization session yet') }}</h3>
              <p>{{ t('点击上方服务卡片或「打开授权页」后，这里会展示授权链接、state 与后续操作。', 'Click a service card or “Open authorization page”; the auth URL, state and next actions will appear here.') }}</p>
            </template>
          </div>

          <div class="callback-panel">
            <div class="callback-heading">
              <div>
                <h3>{{ t('回调 URL', 'Callback URL') }}</h3>
                <p>{{ t('如果授权后停在 localhost / provider 回调页，复制浏览器地址栏完整 URL 并提交。', 'If the browser stops at localhost / provider callback, paste the full address bar URL here and submit it.') }}</p>
              </div>
              <NTag round :bordered="false" type="warning">{{ t('可选', 'Optional') }}</NTag>
            </div>
            <NInput
              v-model:value="redirectURL"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              placeholder="http://127.0.0.1:8317/codex/callback?code=...&state=..."
            />
            <div class="action-row">
              <NButton secondary :loading="isSubmittingCallback" @click="submitCallback">
                <template #icon><ShieldCheck :size="15" /></template>
                {{ t('提交回调到 CPA', 'Submit callback to CPA') }}
              </NButton>
              <NButton tertiary :disabled="!redirectURL" @click="copyText(redirectURL, t('请先粘贴回调 URL', 'Paste callback URL first'))">
                <template #icon><Clipboard :size="15" /></template>
                {{ t('复制回调', 'Copy callback') }}
              </NButton>
            </div>
            <NAlert v-if="callbackSuccessMessage" type="success" :bordered="false" closable @close="callbackSuccessMessage = null">
              {{ callbackSuccessMessage }}
            </NAlert>
          </div>
        </div>
      </section>

      <aside class="panel status-panel">
        <div class="panel-inner">
          <div class="status-header">
            <h2>{{ t('认证状态', 'Authorization status') }}</h2>
            <NTag round :type="phaseType">{{ phaseText }}</NTag>
          </div>

          <div class="status-card" :class="`phase-${phaseType}`">
            <div class="status-orb">
              <Loader2 v-if="isStarting || isChecking || isSubmittingCallback" :size="20" class="spin-icon" />
              <CheckCircle2 v-else-if="phaseType === 'success'" :size="20" />
              <ShieldCheck v-else :size="20" />
            </div>
            <div>
              <strong>{{ activeProvider.shortName }}</strong>
              <p>{{ phaseText }}</p>
            </div>
          </div>

          <dl class="info-list">
            <div>
              <dt>{{ t('Provider', 'Provider') }}</dt>
              <dd>{{ selectedProvider }}</dd>
            </div>
            <div>
              <dt>{{ t('State', 'State') }}</dt>
              <dd>
                <code>{{ authState || '-' }}</code>
              </dd>
            </div>
            <div v-if="needsProjectID">
              <dt>{{ t('Project ID', 'Project ID') }}</dt>
              <dd>{{ projectID || '-' }}</dd>
            </div>
          </dl>

          <NButton block secondary :disabled="!authState" :loading="isChecking" @click="checkStatus">
            <template #icon><RefreshCw :size="15" /></template>
            {{ t('刷新 CPA 状态', 'Refresh CPA status') }}
          </NButton>

          <div v-if="statusResponse" class="raw-panel">
            <div class="raw-heading">
              <span>{{ t('CPA 状态响应', 'CPA status response') }}</span>
              <NButton text size="tiny" @click="copyText(statusText, t('当前没有状态响应', 'No status response available'))">
                {{ t('复制', 'Copy') }}
              </NButton>
            </div>
            <pre>{{ statusText }}</pre>
          </div>
          <div v-else class="tips-card">
            <strong>{{ t('提示', 'Tips') }}</strong>
            <ul>
              <li>{{ t('授权链接会自动在新标签打开。', 'The authorization URL opens in a new tab automatically.') }}</li>
              <li>{{ t('如浏览器无法回跳，请手动提交完整回调 URL。', 'If the browser cannot redirect back, submit the full callback URL manually.') }}</li>
              <li>{{ t('管理员需先在系统设置中配置 CPA 地址与 Management Key。', 'Admins must configure the CPA URL and Management Key in Settings first.') }}</li>
            </ul>
          </div>

          <details v-if="authURLResponse" class="raw-details">
            <summary>{{ t('查看登录响应原文', 'View raw sign-in response') }}</summary>
            <pre>{{ responseText }}</pre>
          </details>
        </div>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.cpa-oauth-page {
  gap: 16px;
}

.oauth-hero {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.48fr);
  gap: 18px;
  overflow: hidden;
  padding: 22px;
  background:
    radial-gradient(circle at 10% 0%, color-mix(in srgb, var(--cpa-primary-weak) 72%, transparent), transparent 30%),
    linear-gradient(135deg, var(--cpa-surface) 0%, var(--cpa-bg-soft) 100%);
}

.hero-glow {
  position: absolute;
  inset: auto -80px -120px auto;
  width: 300px;
  height: 300px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--cpa-accent-purple) 18%, transparent);
  filter: blur(20px);
  pointer-events: none;
}

.hero-copy,
.hero-steps {
  position: relative;
  z-index: 1;
}

.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 10px;
  color: var(--cpa-primary);
  font-size: 13px;
  font-weight: 760;
}

.oauth-hero .page-title {
  max-width: 920px;
  font-size: clamp(24px, 3vw, 34px);
}

.oauth-hero .page-subtitle {
  max-width: 760px;
  margin-top: 10px;
  font-size: 14px;
  line-height: 1.7;
}

.hero-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

.hero-steps {
  display: grid;
  gap: 10px;
  align-content: center;
}

.hero-step {
  display: grid;
  grid-template-columns: 38px 1fr;
  grid-template-areas:
    'num title'
    'num text';
  gap: 2px 12px;
  padding: 13px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: color-mix(in srgb, var(--cpa-surface) 82%, transparent);
  box-shadow: var(--cpa-shadow-card), var(--cpa-shadow-hairline);
}

.hero-step span {
  display: grid;
  grid-area: num;
  width: 38px;
  height: 38px;
  place-items: center;
  border-radius: 12px;
  background: var(--cpa-surface-muted);
  color: var(--cpa-text-strong);
  font-weight: 800;
}

.hero-step strong {
  grid-area: title;
  color: var(--cpa-text-strong);
  font-size: 14px;
}

.hero-step small {
  grid-area: text;
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.hero-step.is-active span {
  background: var(--cpa-primary-weak);
  color: var(--cpa-primary);
}

.section-heading,
.workbench-header,
.callback-heading,
.status-header,
.auth-surface-header,
.raw-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}

.section-heading {
  margin-bottom: 16px;
}

.section-heading .section-title,
.workbench-header h2,
.status-header h2,
.callback-heading h3 {
  margin: 0;
  color: var(--cpa-text-strong);
}

.section-heading p,
.callback-heading p,
.workbench-title p,
.status-card p {
  margin: 4px 0 0;
  color: var(--cpa-text-muted);
  font-size: 13px;
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(190px, 1fr));
  gap: 12px;
}

.provider-card {
  --provider-color: var(--cpa-primary);
  --provider-wash: var(--cpa-primary-weak);
  position: relative;
  display: flex;
  min-height: 254px;
  flex-direction: column;
  overflow: hidden;
  padding: 16px;
  border: 1px solid var(--cpa-border);
  border-radius: calc(var(--cpa-radius) + 6px);
  background: var(--cpa-surface-raised);
  box-shadow: var(--cpa-shadow-card), var(--cpa-shadow-hairline);
  cursor: pointer;
  outline: none;
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    box-shadow 180ms ease;
}

.provider-card:hover,
.provider-card:focus-visible,
.provider-card.is-selected {
  transform: translateY(-2px);
  border-color: color-mix(in srgb, var(--provider-color) 48%, var(--cpa-border));
  box-shadow: 0 16px 34px color-mix(in srgb, var(--provider-color) 12%, transparent), var(--cpa-shadow-hairline);
}

.provider-card.is-selected::after {
  position: absolute;
  inset: 0;
  border: 1px solid color-mix(in srgb, var(--provider-color) 62%, transparent);
  border-radius: inherit;
  content: '';
  pointer-events: none;
}

.provider-card-bg {
  position: absolute;
  inset: -70px -70px auto auto;
  width: 180px;
  height: 180px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--provider-color) 14%, transparent);
  pointer-events: none;
}

.provider-card-top,
.provider-footer,
.provider-helper,
.workbench-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.provider-card-top {
  position: relative;
  justify-content: space-between;
}

.brand-mark {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  border-radius: 13px;
  background: var(--provider-wash);
  color: var(--provider-color);
  font-size: 18px;
  font-weight: 880;
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 70%);
}

.brand-mark.large {
  width: 52px;
  height: 52px;
  flex: 0 0 auto;
  border-radius: 16px;
  font-size: 22px;
}

.provider-card h3 {
  position: relative;
  margin: 18px 0 8px;
  color: var(--cpa-text-strong);
  font-size: 18px;
  line-height: 1.2;
}

.provider-card p {
  position: relative;
  min-height: 62px;
  margin: 0;
  color: var(--cpa-text-muted);
  font-size: 13px;
  line-height: 1.55;
}

.provider-helper {
  position: relative;
  margin-top: auto;
  padding-top: 14px;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.35;
}

.provider-helper svg {
  flex: 0 0 auto;
  color: var(--provider-color);
}

.provider-footer {
  position: relative;
  justify-content: space-between;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--cpa-border);
}

.selected-pill,
.select-hint {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--provider-color);
  font-size: 12px;
  font-weight: 760;
}

.select-hint {
  color: var(--cpa-text-muted);
  font-weight: 650;
}

.tone-blue {
  --provider-color: var(--cpa-accent-blue);
  --provider-wash: var(--cpa-accent-blue-weak);
}

.tone-orange {
  --provider-color: var(--cpa-accent-orange);
  --provider-wash: var(--cpa-accent-orange-weak);
}

.tone-green {
  --provider-color: var(--cpa-success);
  --provider-wash: var(--cpa-success-weak);
}

.tone-purple {
  --provider-color: var(--cpa-accent-purple);
  --provider-wash: var(--cpa-accent-purple-weak);
}

.tone-dark {
  --provider-color: var(--cpa-text-strong);
  --provider-wash: var(--cpa-surface-muted);
}

.tone-teal {
  --provider-color: var(--cpa-primary);
  --provider-wash: var(--cpa-primary-weak);
}

.workspace-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(320px, 0.38fr);
  gap: 16px;
}

.workbench-panel .panel-inner,
.status-panel .panel-inner {
  display: grid;
  gap: 16px;
}

.workbench-header {
  align-items: center;
}

.workbench-header h2 {
  font-size: 18px;
}

.project-field {
  display: grid;
  gap: 7px;
}

.project-field label,
.mini-label {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 720;
}

.auth-surface {
  display: grid;
  gap: 14px;
  min-height: 196px;
  padding: 16px;
  border: 1px dashed color-mix(in srgb, var(--cpa-primary) 34%, var(--cpa-border));
  border-radius: calc(var(--cpa-radius) + 4px);
  background:
    linear-gradient(135deg, color-mix(in srgb, var(--cpa-primary-wash) 62%, transparent), transparent),
    var(--cpa-surface-muted);
}

.auth-surface.is-empty {
  place-items: center;
  align-content: center;
  text-align: center;
}

.auth-surface h3 {
  margin: 2px 0 0;
  color: var(--cpa-text-strong);
}

.auth-surface p {
  max-width: 560px;
  margin: 0;
  color: var(--cpa-text-muted);
  line-height: 1.6;
}

.empty-flow-icon {
  display: grid;
  width: 58px;
  height: 58px;
  place-items: center;
  border-radius: 18px;
  background: var(--cpa-surface);
  color: var(--cpa-primary);
  box-shadow: var(--cpa-shadow-card), var(--cpa-shadow-hairline);
}

.auth-surface-header strong {
  display: block;
  margin-top: 2px;
  color: var(--cpa-text-strong);
}

.url-box {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  gap: 10px;
  max-height: 118px;
  overflow: auto;
  padding: 13px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: var(--cpa-surface);
  color: var(--cpa-text);
}

.url-box svg {
  margin-top: 3px;
  color: var(--cpa-primary);
}

.url-box code,
.info-list code {
  font-size: 12px;
  line-height: 1.55;
  overflow-wrap: anywhere;
  word-break: break-all;
}

.action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.callback-panel {
  display: grid;
  gap: 12px;
  padding: 15px;
  border: 1px solid var(--cpa-border);
  border-radius: calc(var(--cpa-radius) + 4px);
  background: var(--cpa-surface);
}

.status-header {
  align-items: center;
}

.status-card {
  display: flex;
  gap: 12px;
  align-items: center;
  padding: 14px;
  border: 1px solid var(--cpa-border);
  border-radius: calc(var(--cpa-radius) + 4px);
  background: var(--cpa-surface-muted);
}

.status-card strong {
  color: var(--cpa-text-strong);
}

.status-card p {
  line-height: 1.35;
}

.status-orb {
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 15px;
  background: var(--cpa-primary-weak);
  color: var(--cpa-primary);
}

.phase-success .status-orb {
  background: var(--cpa-success-weak);
  color: var(--cpa-success);
}

.phase-warning .status-orb {
  background: var(--cpa-warning-weak);
  color: var(--cpa-warning);
}

.info-list {
  display: grid;
  gap: 8px;
  margin: 0;
}

.info-list > div {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: 10px;
  align-items: start;
  padding: 10px 0;
  border-bottom: 1px solid var(--cpa-border);
}

.info-list dt {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 720;
}

.info-list dd {
  min-width: 0;
  margin: 0;
  color: var(--cpa-text-strong);
  font-size: 13px;
  text-align: right;
}

.raw-panel,
.tips-card,
.raw-details {
  padding: 13px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: var(--cpa-surface-muted);
}

.raw-heading {
  align-items: center;
  margin-bottom: 8px;
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
}

.raw-panel pre,
.raw-details pre,
.compact-json {
  max-height: 260px;
  margin: 0;
  overflow: auto;
  color: var(--cpa-text);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.compact-json {
  padding: 13px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: var(--cpa-surface);
}

.tips-card strong {
  color: var(--cpa-text-strong);
}

.tips-card ul {
  display: grid;
  gap: 8px;
  margin: 10px 0 0;
  padding-left: 18px;
  color: var(--cpa-text-muted);
  font-size: 12px;
  line-height: 1.45;
}

.raw-details summary {
  cursor: pointer;
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 760;
}

.raw-details pre {
  margin-top: 10px;
}

.spin-icon {
  animation: oauth-spin 900ms linear infinite;
}

@keyframes oauth-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1380px) {
  .provider-grid {
    grid-template-columns: repeat(3, minmax(210px, 1fr));
  }
}

@media (max-width: 1080px) {
  .oauth-hero,
  .workspace-grid {
    grid-template-columns: 1fr;
  }

  .hero-steps {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .oauth-hero,
  .provider-area .panel-inner,
  .workbench-panel .panel-inner,
  .status-panel .panel-inner {
    padding: 14px;
  }

  .hero-steps,
  .provider-grid {
    grid-template-columns: 1fr;
  }

  .section-heading,
  .workbench-header,
  .callback-heading,
  .auth-surface-header {
    align-items: stretch;
    flex-direction: column;
  }

  .provider-card {
    min-height: 220px;
  }

  .workbench-title {
    align-items: flex-start;
  }

  .info-list > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .info-list dd {
    text-align: left;
  }
}
</style>
