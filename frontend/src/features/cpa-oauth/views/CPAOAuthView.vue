<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NCard, NInput, NSelect, NSpace, NSpin, NTag, useMessage } from 'naive-ui'
import { ExternalLink, RefreshCw, ShieldCheck } from 'lucide-vue-next'

import {
  createCPAOAuthURL,
  getCPAOAuthStatus,
  listCPAOAuthProviders,
  submitCPAOAuthCallback,
} from '@/features/cpa-oauth/api/cpaOAuthApi'
import { useI18n } from '@/shared/i18n'
import type { CPAOAuthAuthURLResponse, CPAOAuthProvider, CPAOAuthStatusResponse } from '@/shared/types/api'

const message = useMessage()
const { errorText, t } = useI18n()

const providers = ref<CPAOAuthProvider[]>([
  { id: 'codex', label: 'Codex / OpenAI' },
  { id: 'anthropic', label: 'Claude' },
  { id: 'gemini', label: 'Gemini CLI' },
  { id: 'antigravity', label: 'Antigravity' },
  { id: 'kimi', label: 'Kimi' },
])
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

const providerOptions = computed(() => providers.value.map((provider) => ({ label: provider.label, value: provider.id })))
const authURL = computed(() => (typeof authURLResponse.value?.url === 'string' ? authURLResponse.value.url : ''))
const authState = computed(() => (typeof authURLResponse.value?.state === 'string' ? authURLResponse.value.state : ''))
const statusText = computed(() => (statusResponse.value ? JSON.stringify(statusResponse.value, null, 2) : ''))
const responseText = computed(() => (authURLResponse.value ? JSON.stringify(authURLResponse.value, null, 2) : ''))
const needsProjectID = computed(() => selectedProvider.value === 'gemini')

onMounted(async () => {
  isLoadingProviders.value = true
  try {
    const response = await listCPAOAuthProviders()
    if (response.providers.length) {
      providers.value = response.providers
    }
  } catch (error) {
    errorMessage.value = errorText(error, '加载 CPA OAuth 提供商失败', 'Failed to load CPA OAuth providers')
  } finally {
    isLoadingProviders.value = false
  }
})

async function startOAuth() {
  isStarting.value = true
  errorMessage.value = null
  statusResponse.value = null
  try {
    const payload = { provider: selectedProvider.value }
    if (needsProjectID.value && projectID.value.trim()) {
      Object.assign(payload, { project_id: projectID.value.trim() })
    }
    const response = await createCPAOAuthURL(payload)
    authURLResponse.value = response
    if (typeof response.url === 'string' && response.url) {
      window.open(response.url, '_blank', 'noopener')
      message.success(t('已打开 CPA OAuth 登录页', 'CPA OAuth sign-in page opened'))
    } else {
      message.info(t('CPA 已返回登录信息，请按页面提示继续', 'CPA returned sign-in information. Continue with the details below.'))
    }
  } catch (error) {
    errorMessage.value = errorText(error, '创建 CPA OAuth 登录链接失败', 'Failed to create CPA OAuth sign-in URL')
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
    message.warning(t('请粘贴 OAuth 回调 URL', 'Paste the OAuth callback URL'))
    return
  }
  isSubmittingCallback.value = true
  errorMessage.value = null
  try {
    statusResponse.value = await submitCPAOAuthCallback({
      provider: selectedProvider.value,
      redirect_url: redirectURL.value.trim(),
    })
    message.success(t('OAuth 回调已提交到 CPA', 'OAuth callback submitted to CPA'))
  } catch (error) {
    errorMessage.value = errorText(error, '提交 OAuth 回调失败', 'Failed to submit OAuth callback')
  } finally {
    isSubmittingCallback.value = false
  }
}
</script>

<template>
  <section class="cpa-oauth-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('CPA OAuth 登录', 'CPA OAuth Login') }}</h1>
        <p class="page-subtitle">
          {{ t('在 CPA-Helper 里发起 CPA 的 Codex、Claude、Gemini 等 OAuth 登录流程。此入口位于我的账号，所有登录用户都可以看到。', 'Start CPA OAuth sign-in flows for Codex, Claude, Gemini and more from CPA-Helper. This entry is under My Account and visible to every signed-in user.') }}
        </p>
      </div>
      <NTag round type="info">{{ t('所有用户可见', 'Visible to all users') }}</NTag>
    </div>

    <NAlert v-if="errorMessage" type="error" :bordered="false">{{ errorMessage }}</NAlert>

    <div class="oauth-grid">
      <NCard :title="t('发起登录', 'Start sign-in')" class="oauth-card">
        <NSpin :show="isLoadingProviders">
          <div class="oauth-form">
            <label>
              <span>{{ t('OAuth 提供商', 'OAuth provider') }}</span>
              <NSelect v-model:value="selectedProvider" :options="providerOptions" />
            </label>
            <label v-if="needsProjectID">
              <span>{{ t('Gemini GCP Project ID（可选）', 'Gemini GCP Project ID (optional)') }}</span>
              <NInput v-model:value="projectID" placeholder="my-gcp-project" />
            </label>
            <NButton type="primary" :loading="isStarting" @click="startOAuth">
              <template #icon><ExternalLink :size="16" /></template>
              {{ t('打开 OAuth 登录', 'Open OAuth sign-in') }}
            </NButton>
          </div>
        </NSpin>
      </NCard>

      <NCard :title="t('登录状态', 'Sign-in status')" class="oauth-card">
        <div class="oauth-form">
          <NAlert type="info" :bordered="false">
            {{ t('登录页打开后，请在新窗口完成授权；如 CPA 返回了 state，可在这里查询进度。', 'After the sign-in page opens, finish authorization in the new window. If CPA returned a state, you can query progress here.') }}
          </NAlert>
          <div class="oauth-state">
            <span>{{ t('State', 'State') }}</span>
            <code>{{ authState || '-' }}</code>
          </div>
          <NButton secondary :disabled="!authState" :loading="isChecking" @click="checkStatus">
            <template #icon><RefreshCw :size="16" /></template>
            {{ t('查询状态', 'Check status') }}
          </NButton>
        </div>
      </NCard>
    </div>

    <NCard :title="t('手动提交回调', 'Submit callback manually')" class="oauth-card">
      <div class="oauth-form">
        <NAlert type="warning" :bordered="false">
          {{ t('如果浏览器停在 provider 的回调地址，复制完整 URL 粘贴到这里，CPA-Helper 会转交给 CPA 的 oauth-callback 接口。', 'If the browser stops at the provider callback address, copy the full URL here and CPA-Helper will forward it to CPA oauth-callback.') }}
        </NAlert>
        <NInput
          v-model:value="redirectURL"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 5 }"
          placeholder="http://127.0.0.1:8317/codex/callback?code=...&state=..."
        />
        <NButton secondary :loading="isSubmittingCallback" @click="submitCallback">
          <template #icon><ShieldCheck :size="16" /></template>
          {{ t('提交回调到 CPA', 'Submit callback to CPA') }}
        </NButton>
      </div>
    </NCard>

    <div v-if="authURLResponse || statusResponse" class="oauth-result-grid">
      <NCard v-if="authURLResponse" :title="t('CPA 返回的登录信息', 'CPA sign-in response')">
        <NSpace vertical>
          <a v-if="authURL" :href="authURL" target="_blank" rel="noopener noreferrer">{{ authURL }}</a>
          <pre>{{ responseText }}</pre>
        </NSpace>
      </NCard>
      <NCard v-if="statusResponse" :title="t('CPA OAuth 状态', 'CPA OAuth status')">
        <pre>{{ statusText }}</pre>
      </NCard>
    </div>
  </section>
</template>

<style scoped>
.cpa-oauth-page {
  display: grid;
  gap: 18px;
}

.oauth-grid,
.oauth-result-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 16px;
}

.oauth-card :deep(.n-card-header__main) {
  font-weight: 700;
}

.oauth-form {
  display: grid;
  gap: 14px;
}

.oauth-form label {
  display: grid;
  gap: 8px;
  color: var(--text-muted);
  font-size: 13px;
}

.oauth-state {
  display: grid;
  gap: 6px;
  color: var(--text-muted);
  font-size: 13px;
}

.oauth-state code,
pre {
  display: block;
  overflow: auto;
  margin: 0;
  padding: 12px;
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 12px;
  background: rgba(15, 23, 42, 0.04);
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
