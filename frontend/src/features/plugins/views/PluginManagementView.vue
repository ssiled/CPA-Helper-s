<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NInputGroup, NInputNumber, NSpace, NSwitch, NTag, useMessage } from 'naive-ui'
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

const concurrencyTiers = [
  { id: 'default', label: '默认', description: '未识别等级的 Codex 账号' },
  { id: 'free', label: 'Free', description: '免费账号' },
  { id: 'plus', label: 'Plus', description: 'Plus 账号' },
  { id: 'team', label: 'Team', description: 'Team 账号' },
  { id: 'pro', label: 'Pro', description: 'Pro 账号' },
  { id: 'business', label: 'Business', description: 'Business 账号' },
  { id: 'enterprise', label: 'Enterprise', description: 'Enterprise 账号' },
  { id: 'edu', label: 'Edu', description: '教育账号' },
]

const targets = reactive<AuthPoolProxyTargetForm[]>([])
const concurrencyLimits = reactive<Record<string, number | null>>({})

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

  const limits = next.codex_concurrency_limits || next.concurrency?.limits || {}
  for (const tier of concurrencyTiers) {
    concurrencyLimits[tier.id] = Math.max(0, Number(limits[tier.id] || 0))
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
    ? t('已保存，留空不修改', 'Saved; leave blank to keep unchanged')
    : t('CPA Management Key', 'CPA management secret')
}

function apiKeyPlaceholder(target: AuthPoolProxyTargetForm) {
  if (!target.api_key_set) {
    return t('只给 CPA-Helper 使用的 CPA API KEY', 'CPA API key used only by CPA-Helper')
  }
  const preview = target.api_key_preview ? ` ${target.api_key_preview}` : ''
  return t(`已保存${preview}，留空不修改`, `Saved${preview}; leave blank to keep unchanged`)
}

function concurrencyCount(tier: string) {
  return config.value?.concurrency?.counts?.[tier] || 0
}

function concurrencyLimitPayload() {
  const payload: Record<string, number> = {}
  for (const tier of concurrencyTiers) {
    payload[tier.id] = Math.max(0, Number(concurrencyLimits[tier.id] || 0))
  }
  return payload
}

function concurrencyLimitValue(tier: string) {
  return concurrencyLimits[tier] ?? 0
}

function setConcurrencyLimit(tier: string, value: number | null) {
  concurrencyLimits[tier] = Math.max(0, Number(value || 0))
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
    message.error(t('启用的 CPA 连接必须填写 URL、Management Key 和转发 API KEY；已保存的密钥可以留空不修改', 'Enabled CPA connections require URL, management key, and forwarding API key. Saved secrets can be left blank to keep them unchanged.'))
    return
  }
  isSaving.value = true
  try {
    applyConfig(await updateAuthPoolProxyConfig({
      targets: payload,
      codex_concurrency_limits: concurrencyLimitPayload(),
    }))
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
        <p class="page-subtitle">{{ t('管理 cpa-auth-pool 插件连接和 Codex 并发限制。未配置代理连接时，CPA-Helper 使用原来的 CPA API Key 同步模式。', 'Manage cpa-auth-pool plugin connections and Codex concurrency limits. Without proxy targets, CPA-Helper uses the original CPA API key sync mode.') }}</p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
        <NButton type="primary" :loading="isSaving" @click="save">{{ t('保存插件设置', 'Save plugin settings') }}</NButton>
      </NSpace>
    </div>

    <NAlert v-if="config?.plugin_installed" type="success" class="panel-alert" :show-icon="true">
      <template #header>{{ t('插件已连接', 'Plugin connected') }}</template>
      {{ t('已检测到 cpa-auth-pool 插件，CPA-Helper 会通过插件管理号池、转发 Key 和并发限制。', 'cpa-auth-pool is detected. CPA-Helper can manage auth pools, forwarding keys, and concurrency limits through the plugin.') }}
    </NAlert>

    <NAlert v-if="config && !config.plugin_installed" type="warning" class="panel-alert">
      {{ t('未检测到 cpa-auth-pool 插件。请先在 CPA 安装并启用 cpa-auth-pool；未安装时 CPA-Helper 会继续使用原来的 CPA API Key 同步模式。', 'cpa-auth-pool is not detected. Install and enable it in CPA first. Until then, CPA-Helper keeps using the original CPA API key sync mode.') }}
      <template v-if="config.plugin_error">
        <br>{{ config.plugin_error }}
      </template>
    </NAlert>

    <NCard :bordered="false" class="panel-card">
      <template #header>
        <div class="card-header">
          <div>
            <div class="card-title">{{ t('CPA 转发连接', 'CPA forwarding connections') }}</div>
            <div class="card-subtitle">{{ t('可配置多个 CPA；当前请求使用第一个已启用连接。保存时会把每个转发 Key 注册到对应 CPA 的 cpa-auth-pool 插件。', 'Configure multiple CPA instances. Requests use the first enabled connection. Saving registers each forwarding key with that CPA cpa-auth-pool plugin.') }}</div>
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
                <div v-if="target.management_key_set" class="secret-hint">{{ t('Management Key 已保存，重新填写才会覆盖', 'Management key is saved. Enter a new value only to replace it.') }}</div>
              </NFormItem>
              <NFormItem :label="t('CPA 转发 API KEY', 'CPA forwarding API key')">
                <NInputGroup>
                  <NInput v-model:value="target.api_key" type="password" show-password-on="click" :placeholder="apiKeyPlaceholder(target)" />
                </NInputGroup>
                <div v-if="target.api_key_set" class="secret-hint">{{ t(`转发 Key 已保存：${target.api_key_preview || '已保存'}`, `Forwarding key saved: ${target.api_key_preview || 'saved'}`) }}</div>
              </NFormItem>
            </div>
          </NForm>
        </div>
      </div>
      <NButton secondary @click="addTarget">{{ t('添加 CPA 连接', 'Add CPA connection') }}</NButton>
    </NCard>

    <NCard :bordered="false" class="panel-card">
      <template #header>
        <div class="card-header">
          <div>
            <div class="card-title">{{ t('Codex 等级并发限制', 'Codex tier concurrency limits') }}</div>
            <div class="card-subtitle">{{ t('按账号等级设置单个账号的同时请求上限，0 表示不限制。某个账号达到上限时插件会跳过它继续选择下一个账号；同号池都满时返回 429。', 'Set the per-account concurrent request limit by tier. 0 means unlimited. When one account reaches its limit, the plugin skips it and tries the next account; if all accounts in the pool are full, it returns 429.') }}</div>
          </div>
          <NTag size="small">{{ t('实时占用来自插件状态', 'Live counts from plugin status') }}</NTag>
        </div>
      </template>

      <div class="tier-grid">
        <div v-for="tier in concurrencyTiers" :key="tier.id" class="tier-row">
          <div>
            <div class="tier-label">{{ tier.label }}</div>
            <div class="tier-description">{{ t(tier.description, tier.description) }}</div>
          </div>
          <div class="tier-controls">
            <NTag size="small" :type="concurrencyCount(tier.id) > 0 ? 'warning' : 'default'">
              {{ t('运行中', 'Running') }} {{ concurrencyCount(tier.id) }}
            </NTag>
            <NInputNumber :value="concurrencyLimitValue(tier.id)" :min="0" :max="999" :precision="0" class="tier-input" @update:value="(value) => setConcurrencyLimit(tier.id, value)" />
          </div>
        </div>
      </div>
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
.tier-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.tier-row { display: flex; justify-content: space-between; gap: 12px; align-items: center; padding: 12px; border: 1px solid rgba(148, 163, 184, .22); border-radius: 12px; background: rgba(255, 255, 255, .72); }
.tier-label { font-weight: 700; }
.tier-description { margin-top: 2px; color: var(--cpa-text-muted); font-size: 12px; }
.tier-controls { display: flex; align-items: center; gap: 8px; }
.tier-input { width: 104px; }
@media (max-width: 860px) { .form-grid, .tier-grid { grid-template-columns: 1fr; } .card-header { flex-direction: column; } .tier-row { align-items: flex-start; flex-direction: column; } }
</style>
