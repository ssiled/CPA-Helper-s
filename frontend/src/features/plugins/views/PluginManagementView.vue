<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NInputGroup, NSpace, NSwitch, NTag, useMessage } from 'naive-ui'
import { getAuthPoolProxyConfig, updateAuthPoolProxyConfig } from '@/features/auth-pools/api/authPoolsApi'
import type { AuthPoolProxyConfig, AuthPoolProxyTargetPayload } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'

const message = useMessage()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isSaving = ref(false)
const config = ref<AuthPoolProxyConfig | null>(null)

interface AuthPoolProxyTargetForm extends AuthPoolProxyTargetPayload {
  management_key_set: boolean
  management_key_preview: string
  api_key_set: boolean
  api_key_preview: string
}

const targets = reactive<AuthPoolProxyTargetForm[]>([])

function emptyTarget(index: number): AuthPoolProxyTargetForm {
  return {
    id: `cpa-${index + 1}`,
    name: `CPA ${index + 1}`,
    cpa_url: '',
    management_key: '',
    management_key_set: false,
    management_key_preview: '',
    api_key: '',
    api_key_set: false,
    api_key_preview: '',
    enabled: true,
  }
}

function applyConfig(next: AuthPoolProxyConfig) {
  config.value = next
  targets.splice(0, targets.length, ...next.targets.map((item, index) => ({
    id: item.id || `cpa-${index + 1}`,
    name: item.name || `CPA ${index + 1}`,
    cpa_url: item.cpa_url || '',
    management_key: '',
    management_key_set: item.management_key_set,
    management_key_preview: item.management_key_preview || '',
    api_key: '',
    api_key_set: item.api_key_set,
    api_key_preview: item.api_key_preview || '',
    enabled: item.enabled,
  })))
  if (targets.length === 0) {
    targets.push(emptyTarget(0))
  }
}

async function refresh() {
  isLoading.value = true
  try {
    applyConfig(await getAuthPoolProxyConfig())
  } catch (error) {
    message.error(errorText(error, '加载插件配置失败', 'Failed to load plugin settings'))
  } finally {
    isLoading.value = false
  }
}

function addTarget() {
  targets.push(emptyTarget(targets.length))
}

function removeTarget(index: number) {
  targets.splice(index, 1)
  if (targets.length === 0) {
    targets.push(emptyTarget(0))
  }
}

function managementKeyPlaceholder(target: AuthPoolProxyTargetForm) {
  return target.management_key_set
    ? t('\u5df2\u4fdd\u5b58\uff0c\u7559\u7a7a\u4e0d\u4fee\u6539', 'Saved; leave blank to keep unchanged')
    : t('CPA Management Key', 'CPA management secret')
}

function apiKeyPlaceholder(target: AuthPoolProxyTargetForm) {
  if (!target.api_key_set) {
    return t('\u53ea\u7ed9 CPA-Helper \u4f7f\u7528\u7684 CPA API KEY', 'CPA API key used only by CPA-Helper')
  }
  const preview = target.api_key_preview ? ` ${target.api_key_preview}` : ''
  return t(`\u5df2\u4fdd\u5b58${preview}\uff0c\u7559\u7a7a\u4e0d\u4fee\u6539`, `Saved${preview}; leave blank to keep unchanged`)
}


async function save() {
  const payload = targets.map((target) => ({
    id: target.id.trim(),
    name: target.name.trim(),
    cpa_url: target.cpa_url.trim().replace(/\/+$/, ''),
    management_key: target.management_key.trim(),
    api_key: target.api_key.trim(),
    enabled: target.enabled,
  }))
  const missingRequired = targets.some((target) => target.enabled && (
    !target.cpa_url.trim() ||
    (!target.management_key.trim() && !target.management_key_set) ||
    (!target.api_key.trim() && !target.api_key_set)
  ))
  if (missingRequired) {
    message.error(t('\u542f\u7528\u7684 CPA \u8fde\u63a5\u5fc5\u987b\u586b\u5199 URL\u3001Management Key \u548c\u8f6c\u53d1 API KEY\uff1b\u5df2\u4fdd\u5b58\u7684\u5bc6\u94a5\u53ef\u4ee5\u7559\u7a7a\u4e0d\u4fee\u6539', 'Enabled CPA connections require URL, management key, and forwarding API key. Saved secrets can be left blank to keep them unchanged.'))
    return
  }
  isSaving.value = true
  try {
    applyConfig(await updateAuthPoolProxyConfig({ targets: payload }))
    message.success(t('插件配置已保存', 'Plugin settings saved'))
  } catch (error) {
    message.error(errorText(error, '保存插件配置失败', 'Failed to save plugin settings'))
  } finally {
    isSaving.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <section class="plugin-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('插件管理', 'Plugin Management') }}</h1>
        <p class="page-subtitle">{{ t('管理 cpa-auth-pool 插件连接；未配置时 CPA-Helper 使用最初的 CPA API Key 同步模式。', 'Manage cpa-auth-pool plugin connections. Without proxy targets, CPA-Helper uses the original CPA API key sync mode.') }}</p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
        <NButton type="primary" :loading="isSaving" @click="save">{{ t('保存插件设置', 'Save plugin settings') }}</NButton>
      </NSpace>
    </div>

    <NAlert v-if="config && !config.plugin_installed" type="warning" class="panel-alert">
      {{ t('未检测到 cpa-auth-pool 插件。请先在 CPA 安装并启用 cpa-auth-pool；未安装时 CPA-Helper 会继续使用最初的 CPA API Key 同步模式。', 'cpa-auth-pool is not detected. Install and enable it in CPA first. Until then, CPA-Helper keeps using the original CPA API key sync mode.') }}
      <template v-if="config.plugin_error">
        <br />{{ config.plugin_error }}
      </template>
    </NAlert>

    <NCard :bordered="false" class="panel-card">
      <template #header>
        <div class="card-header">
          <div>
            <div class="card-title">{{ t('CPA 转发连接', 'CPA forwarding connections') }}</div>
            <div class="card-subtitle">{{ t('可配置多个 CPA；当前请求会使用第一个已启用的连接。保存时会把每个转发 Key 注册到对应 CPA 的 cpa-auth-pool 插件。', 'Configure multiple CPA instances. Requests use the first enabled connection. Saving registers each forwarding key with that CPA cpa-auth-pool plugin.') }}</div>
          </div>
          <NTag :type="config?.mode === 'proxy' ? 'success' : 'default'" size="small">
            {{ config?.mode === 'proxy' ? t('转发模式', 'Proxy mode') : t('原始模式', 'Legacy mode') }}
          </NTag>
        </div>
      </template>

      <div class="target-list">
        <div v-for="(target, index) in targets" :key="index" class="target-card">
          <div class="target-head">
            <strong>{{ target.name || `CPA ${index + 1}` }}</strong>
            <NSpace size="small">
              <NSwitch v-model:value="target.enabled" />
              <NButton size="small" quaternary type="error" @click="removeTarget(index)">{{ t('删除', 'Delete') }}</NButton>
            </NSpace>
          </div>
          <NForm label-placement="top">
            <div class="form-grid">
              <NFormItem :label="t('连接 ID', 'Connection ID')">
                <NInput v-model:value="target.id" placeholder="cpa-main" />
              </NFormItem>
              <NFormItem :label="t('名称', 'Name')">
                <NInput v-model:value="target.name" placeholder="Main CPA" />
              </NFormItem>
              <NFormItem :label="t('CPA URL', 'CPA URL')">
                <NInput v-model:value="target.cpa_url" placeholder="https://your-cpa.example.com" />
              </NFormItem>
              <NFormItem :label="t('Management Key', 'Management Key')">
                <NInput v-model:value="target.management_key" type="password" show-password-on="click" :placeholder="managementKeyPlaceholder(target)" />
                <div v-if="target.management_key_set" class="secret-hint">{{ t('Management Key \u5df2\u4fdd\u5b58\uff0c\u91cd\u65b0\u586b\u5199\u624d\u4f1a\u8986\u76d6', 'Management key is saved. Enter a new value only to replace it.') }}</div>
              </NFormItem>
              <NFormItem :label="t('CPA 转发 API KEY', 'CPA forwarding API key')">
                <NInputGroup>
                  <NInput v-model:value="target.api_key" type="password" show-password-on="click" :placeholder="apiKeyPlaceholder(target)" />
                </NInputGroup>
                <div v-if="target.api_key_set" class="secret-hint">{{ t(`\u8f6c\u53d1 Key \u5df2\u4fdd\u5b58\uff1a${target.api_key_preview || '\u5df2\u4fdd\u5b58'}`, `Forwarding key saved: ${target.api_key_preview || 'saved'}`) }}</div>
              </NFormItem>
            </div>
          </NForm>
        </div>
      </div>
      <NButton secondary @click="addTarget">{{ t('添加 CPA 连接', 'Add CPA connection') }}</NButton>
    </NCard>
  </section>
</template>

<style scoped>
.plugin-page { display: grid; gap: 16px; }
.panel-alert { border-radius: 14px; }
.panel-card { border-radius: 18px; }
.card-header { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.card-title { font-size: 16px; font-weight: 700; }
.card-subtitle { margin-top: 4px; color: var(--cpa-text-muted); font-size: 12px; line-height: 1.6; }
.target-list { display: grid; gap: 12px; margin-bottom: 12px; }
.target-card { padding: 14px; border: 1px solid rgba(148, 163, 184, .24); border-radius: 14px; background: rgba(248, 250, 252, .7); }
.target-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 12px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.secret-hint { margin-top: 6px; font-size: 12px; color: var(--cpa-text-muted); }
@media (max-width: 860px) { .form-grid { grid-template-columns: 1fr; } .card-header { flex-direction: column; } }
</style>
