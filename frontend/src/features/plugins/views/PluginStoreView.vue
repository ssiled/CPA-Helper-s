<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NCard, NEmpty, NInput, NSpace, NSpin, NTag, useDialog, useMessage } from 'naive-ui'

import { getPluginStore, installPluginFromStore } from '@/features/plugins/api/pluginStoreApi'
import type { PluginStoreEntry, PluginStoreResponse } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'

const message = useMessage()
const dialog = useDialog()
const { errorText, t } = useI18n()

const isLoading = ref(false)
const installingStoreID = ref<string | null>(null)
const store = ref<PluginStoreResponse | null>(null)
const search = ref('')
const filter = ref<'all' | 'installed' | 'not_installed' | 'updates'>('all')

const plugins = computed(() => store.value?.plugins ?? [])
const counts = computed(() => {
  const items = plugins.value
  return {
    all: items.length,
    installed: items.filter((plugin) => plugin.installed).length,
    not_installed: items.filter((plugin) => !plugin.installed).length,
    updates: items.filter((plugin) => plugin.update_available).length,
  }
})

const filteredPlugins = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return plugins.value.filter((plugin) => {
    if (filter.value === 'installed' && !plugin.installed) return false
    if (filter.value === 'not_installed' && plugin.installed) return false
    if (filter.value === 'updates' && !plugin.update_available) return false
    if (!keyword) return true
    return [plugin.id, plugin.name, plugin.author, plugin.source_name, ...(plugin.tags ?? [])]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(keyword))
  })
})

async function refresh() {
  isLoading.value = true
  try {
    store.value = await getPluginStore()
  } catch (error) {
    message.error(errorText(error, '加载插件商店失败', 'Failed to load plugin store'))
  } finally {
    isLoading.value = false
  }
}

function actionLabel(plugin: PluginStoreEntry): string {
  if (plugin.update_available) return t('更新', 'Update')
  if (plugin.installed) return t('已安装', 'Installed')
  return t('安装', 'Install')
}

function canInstall(plugin: PluginStoreEntry): boolean {
  if (installingStoreID.value !== null) return false
  if (plugin.auth_required && !plugin.auth_configured) return false
  if (plugin.installed && !plugin.update_available) return false
  if (plugin.install_source_status === 'different') return false
  return true
}

function statusType(plugin: PluginStoreEntry): 'success' | 'warning' | 'error' | 'default' | 'info' {
  if (plugin.update_available) return 'warning'
  if (plugin.effective_enabled) return 'success'
  if (plugin.installed) return 'info'
  if (plugin.auth_required && !plugin.auth_configured) return 'error'
  return 'default'
}

function statusLabel(plugin: PluginStoreEntry): string {
  if (plugin.update_available) return t('可更新', 'Update available')
  if (plugin.effective_enabled) return t('生效中', 'Active')
  if (plugin.installed) return t('已安装', 'Installed')
  if (plugin.auth_required && !plugin.auth_configured) return t('需要认证', 'Auth required')
  return t('未安装', 'Not installed')
}

function pluginDescription(plugin: PluginStoreEntry): string {
  return plugin.description || t('该插件未提供描述。', 'This plugin does not provide a description.')
}

function installHint(plugin: PluginStoreEntry): string | null {
  if (plugin.auth_required && !plugin.auth_configured) return t('该插件源需要在 CPA 后端配置 store-auth 后才能安装。', 'This source requires store-auth in CPA before installation.')
  if (plugin.install_source_status === 'different') return t('已从其他插件源安装；为避免来源切换，需先在 CPA 确认来源。', 'Installed from another source. Confirm source switching in CPA first.')
  if (plugin.installed && !plugin.update_available) return t('当前版本已安装。', 'Current version is already installed.')
  return null
}

function confirmInstall(plugin: PluginStoreEntry) {
  if (!canInstall(plugin)) return
  dialog.warning({
    title: plugin.update_available ? t('确认更新插件', 'Confirm plugin update') : t('确认安装插件', 'Confirm plugin install'),
    content: t(
      `插件 ${plugin.id} 会在 CPA 后端以完整权限运行。请确认该插件来源可信：${plugin.source_name}`,
      `Plugin ${plugin.id} will run inside the CPA backend with full privileges. Confirm the source is trusted: ${plugin.source_name}`,
    ),
    positiveText: actionLabel(plugin),
    negativeText: t('取消', 'Cancel'),
    onPositiveClick: () => install(plugin),
  })
}

async function install(plugin: PluginStoreEntry) {
  installingStoreID.value = plugin.store_id
  try {
    const result = await installPluginFromStore(plugin.id, { source: plugin.source_id, version: plugin.version })
    if (result.restart_required) {
      message.warning(t('插件已写入，但 CPA 需要重启后生效。', 'Plugin was written, but CPA must restart before it takes effect.'))
    } else {
      message.success(plugin.update_available ? t('插件已更新', 'Plugin updated') : t('插件已安装', 'Plugin installed'))
    }
    await refresh()
  } catch (error) {
    message.error(errorText(error, '安装插件失败', 'Failed to install plugin'))
  } finally {
    installingStoreID.value = null
  }
}

function sourceLine(plugin: PluginStoreEntry): string {
  return [plugin.source_name, plugin.author].filter(Boolean).join(' · ')
}

function versionLine(plugin: PluginStoreEntry): string {
  if (plugin.installed_version && plugin.installed_version !== plugin.version) {
    return `${plugin.installed_version} → ${plugin.version}`
  }
  return plugin.version || '-'
}

onMounted(refresh)
</script>

<template>
  <section class="plugin-store-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('插件商店', 'Plugin Store') }}</h1>
        <p class="page-subtitle">{{ t('浏览插件注册表，为当前后端安装或更新插件。', 'Browse plugin registries and install or update plugins for the current backend.') }}</p>
      </div>
      <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
    </div>

    <NAlert type="warning" class="risk-alert" :show-icon="true">
      <template #header>{{ t('第三方插件以后端完整权限运行', 'Third-party plugins run with full backend privileges') }}</template>
      {{ t('插件会在代理服务内部执行代码，可读取你的凭据与流量。请仅安装你信任的插件——尤其要警惕任何并非由官方组织 router-for-me 发布的插件。', 'Plugins execute inside the proxy service and can read credentials and traffic. Only install plugins you trust, especially plugins not published by the official router-for-me organization.') }}
    </NAlert>

    <div class="summary-grid">
      <NCard :bordered="false" class="summary-card">
        <span>{{ t('全局状态', 'Global status') }}</span>
        <strong>{{ store?.plugins_enabled ? t('已启用', 'Enabled') : t('未启用', 'Disabled') }}</strong>
      </NCard>
      <NCard :bordered="false" class="summary-card">
        <span>{{ t('插件目录', 'Plugin directory') }}</span>
        <strong>{{ store?.plugins_dir || '-' }}</strong>
      </NCard>
      <NCard :bordered="false" class="summary-card">
        <span>{{ t('可用插件', 'Available plugins') }}</span>
        <strong>{{ counts.all }}</strong>
      </NCard>
    </div>

    <NAlert v-for="sourceError in store?.source_errors ?? []" :key="`${sourceError.source_id}-${sourceError.message}`" type="error" class="source-alert">
      {{ sourceError.source_name || sourceError.source_url }}：{{ sourceError.message }}
    </NAlert>

    <NCard :bordered="false" class="store-panel">
      <div class="toolbar">
        <NInput v-model:value="search" clearable :placeholder="t('搜索插件 ID、名称、作者或标签...', 'Search plugin ID, name, author, or tags...')" />
        <NSpace class="filter-tabs" size="small">
          <NButton :type="filter === 'all' ? 'primary' : 'default'" secondary @click="filter = 'all'">{{ t('全部', 'All') }} {{ counts.all }}</NButton>
          <NButton :type="filter === 'installed' ? 'primary' : 'default'" secondary @click="filter = 'installed'">{{ t('已安装', 'Installed') }} {{ counts.installed }}</NButton>
          <NButton :type="filter === 'not_installed' ? 'primary' : 'default'" secondary @click="filter = 'not_installed'">{{ t('未安装', 'Not installed') }} {{ counts.not_installed }}</NButton>
          <NButton :type="filter === 'updates' ? 'primary' : 'default'" secondary @click="filter = 'updates'">{{ t('可更新', 'Updates') }} {{ counts.updates }}</NButton>
        </NSpace>
      </div>

      <NSpin :show="isLoading">
        <NEmpty v-if="!filteredPlugins.length && !isLoading" :description="t('没有匹配的插件。', 'No matching plugins.')" />
        <div v-else class="plugin-grid">
          <article v-for="plugin in filteredPlugins" :key="plugin.store_id" class="plugin-card">
            <header class="plugin-card__header">
              <div>
                <div class="plugin-card__title-row">
                  <h2>{{ plugin.name || plugin.id }}</h2>
                  <NTag size="small" :type="statusType(plugin)">{{ statusLabel(plugin) }}</NTag>
                </div>
                <p>{{ plugin.id }}</p>
              </div>
              <NButton
                type="primary"
                secondary
                :disabled="!canInstall(plugin)"
                :loading="installingStoreID === plugin.store_id"
                @click="confirmInstall(plugin)"
              >
                {{ actionLabel(plugin) }}
              </NButton>
            </header>

            <p class="plugin-card__description">{{ pluginDescription(plugin) }}</p>

            <div class="plugin-card__meta">
              <span>{{ t('版本', 'Version') }}：{{ versionLine(plugin) }}</span>
              <span>{{ t('来源', 'Source') }}：{{ sourceLine(plugin) }}</span>
              <span>{{ t('类型', 'Type') }}：{{ plugin.install_type || '-' }}</span>
              <span v-if="plugin.path">{{ t('路径', 'Path') }}：{{ plugin.path }}</span>
            </div>

            <div class="plugin-card__tags">
              <NTag v-for="tag in plugin.tags ?? []" :key="`${plugin.store_id}-${tag}`" size="small">{{ tag }}</NTag>
              <NTag v-if="plugin.auth_required" size="small" :type="plugin.auth_configured ? 'success' : 'error'">
                {{ plugin.auth_configured ? t('认证已配置', 'Auth configured') : t('需要认证', 'Auth required') }}
              </NTag>
              <NTag v-if="plugin.registered" size="small" type="success">{{ t('已注册', 'Registered') }}</NTag>
              <NTag v-if="plugin.configured" size="small" type="info">{{ t('已配置', 'Configured') }}</NTag>
            </div>

            <p v-if="installHint(plugin)" class="plugin-card__hint">{{ installHint(plugin) }}</p>
          </article>
        </div>
      </NSpin>
    </NCard>
  </section>
</template>

<style scoped>
.plugin-store-page { display: grid; gap: 16px; }
.risk-alert,
.source-alert,
.store-panel,
.summary-card { border-radius: 16px; }
.summary-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.summary-card :deep(.n-card__content) { display: grid; gap: 6px; }
.summary-card span { color: var(--cpa-text-muted); font-size: 12px; }
.summary-card strong { font-size: 20px; word-break: break-all; }
.toolbar { display: grid; gap: 12px; margin-bottom: 16px; }
.filter-tabs { flex-wrap: wrap; }
.plugin-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 14px; }
.plugin-card { display: grid; gap: 12px; padding: 16px; border: 1px solid rgba(148, 163, 184, .22); border-radius: 16px; background: rgba(248, 250, 252, .72); }
.plugin-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.plugin-card__title-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.plugin-card h2 { margin: 0; font-size: 17px; }
.plugin-card__header p { margin: 4px 0 0; color: var(--cpa-text-muted); font-size: 12px; }
.plugin-card__description { margin: 0; color: var(--cpa-text); line-height: 1.6; min-height: 48px; }
.plugin-card__meta { display: grid; gap: 5px; color: var(--cpa-text-muted); font-size: 12px; word-break: break-all; }
.plugin-card__tags { display: flex; gap: 6px; flex-wrap: wrap; }
.plugin-card__hint { margin: 0; color: #b45309; font-size: 12px; }
@media (max-width: 860px) { .summary-grid { grid-template-columns: 1fr; } .plugin-grid { grid-template-columns: 1fr; } .plugin-card__header { flex-direction: column; } }
</style>
