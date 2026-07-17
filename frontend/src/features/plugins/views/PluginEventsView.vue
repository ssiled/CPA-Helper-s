<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NEmpty,
  NInput,
  NPopconfirm,
  NSelect,
  NSpace,
  NSwitch,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { Eye, RefreshCw, Trash2 } from 'lucide-vue-next'

import { clearAuthPoolPluginEvents, getAuthPoolPluginEvents } from '@/features/auth-pools/api/authPoolsApi'
import type { AuthPoolPluginEvent } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import { formatDateTime, formatInteger } from '@/shared/utils/format'

const message = useMessage()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isClearing = ref(false)
const autoRefresh = ref(true)
const events = ref<AuthPoolPluginEvent[]>([])
const remoteTotal = ref(0)
const capacity = ref(0)
const targetErrors = ref<Array<{ target_id: string; target_name: string; error: string }>>([])
const selectedEvent = ref<AuthPoolPluginEvent | null>(null)
const search = ref('')
const targetFilter = ref<string | null>(null)
const phaseFilter = ref<string | null>(null)
const statusFilter = ref<string | null>(null)
const poolFilter = ref<string | null>(null)
let refreshTimer: number | undefined

const targetOptions = computed(() => uniqueOptions(events.value.map((event) => ({ label: event.target_name || event.target_id, value: event.target_id }))))
const poolOptions = computed(() => uniqueOptions(events.value
  .filter((event) => event.pool_id)
  .map((event) => ({ label: event.pool_name ? `${event.pool_name} · ${event.pool_id}` : event.pool_id!, value: event.pool_id! }))))
const phaseOptions = computed(() => [
  { label: t('调度选择', 'Selection'), value: 'selection' },
  { label: t('请求完成', 'Completion'), value: 'completion' },
])
const statusOptions = computed(() => [
  { label: t('已选择', 'Selected'), value: 'selected' },
  { label: t('成功', 'Success'), value: 'success' },
  { label: t('已阻止', 'Blocked'), value: 'blocked' },
  { label: t('失败', 'Failed'), value: 'failed' },
  { label: t('已忽略', 'Ignored'), value: 'ignored' },
])

const filteredEvents = computed(() => {
  const query = search.value.trim().toLowerCase()
  return events.value.filter((event) => {
    if (targetFilter.value && event.target_id !== targetFilter.value) return false
    if (phaseFilter.value && event.phase !== phaseFilter.value) return false
    if (statusFilter.value && event.status !== statusFilter.value) return false
    if (poolFilter.value && event.pool_id !== poolFilter.value) return false
    if (!query) return true
    return [event.selected_auth_id, event.pool_id, event.pool_name, event.model, event.provider, event.username, event.reason, event.target_name]
      .some((value) => value?.toLowerCase().includes(query))
  })
})

const summary = computed(() => ({
  visible: filteredEvents.value.length,
  selected: events.value.filter((event) => event.status === 'selected' || event.status === 'success').length,
  failures: events.value.filter((event) => event.status === 'blocked' || event.status === 'failed').length,
  targets: new Set(events.value.map((event) => event.target_id)).size,
}))

const columns = computed<DataTableColumns<AuthPoolPluginEvent>>(() => [
  {
    title: t('时间', 'Time'),
    key: 'timestamp',
    width: 168,
    render: (row) => h('span', { class: 'event-time' }, formatDateTime(row.timestamp)),
  },
  {
    title: t('阶段 / 状态', 'Phase / Status'),
    key: 'status',
    width: 150,
    render: (row) => h('div', { class: 'status-stack' }, [
      h('span', { class: 'phase-label' }, phaseLabel(row.phase)),
      h(NTag, { size: 'small', type: statusType(row.status), round: true }, { default: () => statusLabel(row.status) }),
    ]),
  },
  {
    title: t('目标 / 号池', 'Target / Pool'),
    key: 'pool_id',
    minWidth: 180,
    render: (row) => h('div', { class: 'identity-stack' }, [
      h('strong', row.target_name || row.target_id || '-'),
      h('span', row.pool_name ? `${row.pool_name} · ${row.pool_id}` : row.pool_id || '-'),
    ]),
  },
  {
    title: t('模型', 'Model'),
    key: 'model',
    minWidth: 150,
    render: (row) => h('div', { class: 'identity-stack' }, [
      h('strong', row.model || '-'),
      h('span', [row.provider || '-', row.stream ? 'stream' : ''].filter(Boolean).join(' · ')),
    ]),
  },
  {
    title: t('请求账号', 'Selected account'),
    key: 'selected_auth_id',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render: (row) => h('code', { class: 'auth-id' }, row.selected_auth_id || '-'),
  },
  {
    title: t('候选', 'Candidates'),
    key: 'candidate_count',
    width: 104,
    render: (row) => `${formatInteger(row.matched_count)} / ${formatInteger(row.candidate_count)}`,
  },
  {
    title: t('原因 / HTTP', 'Reason / HTTP'),
    key: 'reason',
    minWidth: 170,
    ellipsis: { tooltip: true },
    render: (row) => h('div', { class: 'identity-stack' }, [
      h('strong', reasonLabel(row.reason) || '-'),
      h('span', row.http_status ? `HTTP ${row.http_status}` : row.duration_ms !== undefined ? `${row.duration_ms} ms` : '-'),
    ]),
  },
  {
    title: '',
    key: 'actions',
    width: 58,
    fixed: 'right',
    render: (row) => h(NButton, {
      quaternary: true,
      circle: true,
      size: 'small',
      title: t('查看详情', 'View details'),
      onClick: () => { selectedEvent.value = row },
    }, { icon: () => h(Eye, { size: 17 }) }),
  },
])

function uniqueOptions(options: Array<{ label: string; value: string }>) {
  const seen = new Set<string>()
  return options.filter((option) => {
    if (!option.value || seen.has(option.value)) return false
    seen.add(option.value)
    return true
  })
}

function statusType(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  if (status === 'selected') return 'info'
  if (status === 'success') return 'success'
  if (status === 'blocked') return 'warning'
  if (status === 'failed') return 'error'
  return 'default'
}

function statusLabel(status: string): string {
  return statusOptions.value.find((option) => option.value === status)?.label ?? status
}

function phaseLabel(phase: string): string {
  return phaseOptions.value.find((option) => option.value === phase)?.label ?? phase
}

function reasonLabel(reason?: string): string {
  if (!reason) return ''
  const labels: Record<string, string> = {
    untrusted_proxy_key: t('代理密钥不受信任', 'Untrusted proxy key'),
    unbound_api_key: t('API Key 未绑定号池', 'API key is not bound'),
    auth_pool_unavailable: t('号池不可用', 'Pool unavailable'),
    model_not_allowed: t('模型不在号池范围', 'Model outside pool'),
    no_eligible_candidates: t('没有匹配账号', 'No eligible candidates'),
    auth_pool_busy: t('号池并发已满', 'Pool concurrency full'),
    no_available_candidates: t('没有可用账号', 'No available candidates'),
  }
  return labels[reason] ?? reason
}

async function refresh(silent = false) {
  if (isLoading.value) return
  if (!silent) isLoading.value = true
  try {
    const response = await getAuthPoolPluginEvents(200)
    events.value = response.items
    remoteTotal.value = response.total
    capacity.value = response.capacity
    targetErrors.value = response.errors
  } catch (error) {
    if (!silent) message.error(errorText(error, '加载插件监控日志失败', 'Failed to load plugin event log'))
  } finally {
    if (!silent) isLoading.value = false
  }
}

async function clearEvents() {
  isClearing.value = true
  try {
    const response = await clearAuthPoolPluginEvents()
    targetErrors.value = response.errors
    message.success(t(`已清空 ${response.cleared} 条插件日志`, `Cleared ${response.cleared} plugin events`))
    await refresh()
  } catch (error) {
    message.error(errorText(error, '清空插件日志失败', 'Failed to clear plugin events'))
  } finally {
    isClearing.value = false
  }
}

onMounted(() => {
  void refresh()
  refreshTimer = window.setInterval(() => {
    if (autoRefresh.value) void refresh(true)
  }, 10_000)
})

onBeforeUnmount(() => {
  if (refreshTimer !== undefined) window.clearInterval(refreshTimer)
})
</script>

<template>
  <section class="plugin-events-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('插件监控日志', 'Plugin Event Log') }}</h1>
        <p class="page-subtitle">{{ t('CPA 号池调度选择与上游请求完成状态', 'CPA auth-pool selections and upstream completion status') }}</p>
      </div>
      <NSpace align="center">
        <span class="auto-refresh-label">{{ t('自动刷新', 'Auto refresh') }}</span>
        <NSwitch v-model:value="autoRefresh" size="small" />
        <NButton secondary :loading="isLoading" @click="refresh()">
          <template #icon><RefreshCw :size="16" /></template>
          {{ t('刷新', 'Refresh') }}
        </NButton>
        <NPopconfirm @positive-click="clearEvents">
          <template #trigger>
            <NButton secondary type="error" :loading="isClearing">
              <template #icon><Trash2 :size="16" /></template>
              {{ t('清空', 'Clear') }}
            </NButton>
          </template>
          {{ t('清空所有 CPA 目标的插件监控日志？', 'Clear plugin event logs on all CPA targets?') }}
        </NPopconfirm>
      </NSpace>
    </div>

    <div class="signal-strip">
      <div><span>{{ t('当前显示', 'Visible') }}</span><strong>{{ formatInteger(summary.visible) }}</strong></div>
      <div><span>{{ t('选中 / 成功', 'Selected / Success') }}</span><strong class="is-ok">{{ formatInteger(summary.selected) }}</strong></div>
      <div><span>{{ t('阻止 / 失败', 'Blocked / Failed') }}</span><strong class="is-bad">{{ formatInteger(summary.failures) }}</strong></div>
      <div><span>{{ t('CPA 目标', 'CPA targets') }}</span><strong>{{ formatInteger(summary.targets) }}</strong></div>
      <div><span>{{ t('缓存', 'Buffer') }}</span><strong>{{ formatInteger(remoteTotal) }} / {{ formatInteger(capacity) }}</strong></div>
    </div>

    <NAlert v-for="error in targetErrors" :key="error.target_id" type="warning" :title="error.target_name || error.target_id">
      {{ error.error }}
    </NAlert>

    <div class="filter-rail">
      <NInput v-model:value="search" clearable :placeholder="t('搜索账号、模型、号池、用户或原因', 'Search account, model, pool, user, or reason')" />
      <NSelect v-model:value="targetFilter" clearable :options="targetOptions" :placeholder="t('全部目标', 'All targets')" />
      <NSelect v-model:value="phaseFilter" clearable :options="phaseOptions" :placeholder="t('全部阶段', 'All phases')" />
      <NSelect v-model:value="statusFilter" clearable :options="statusOptions" :placeholder="t('全部状态', 'All statuses')" />
      <NSelect v-model:value="poolFilter" clearable filterable :options="poolOptions" :placeholder="t('全部号池', 'All pools')" />
    </div>

    <div class="event-table-shell">
      <NEmpty v-if="!filteredEvents.length && !isLoading" :description="t('暂无匹配的插件事件', 'No matching plugin events')" />
      <NDataTable
        v-else
        :loading="isLoading"
        :columns="columns"
        :data="filteredEvents"
        :row-key="(row: AuthPoolPluginEvent) => `${row.target_id}-${row.id}`"
        :pagination="{ pageSize: 30 }"
        :scroll-x="1320"
        size="small"
      />
    </div>

    <NDrawer :show="selectedEvent !== null" width="min(720px, 92vw)" @update:show="(show) => { if (!show) selectedEvent = null }">
      <NDrawerContent v-if="selectedEvent" :title="t('插件事件详情', 'Plugin event details')" closable>
        <div class="detail-grid">
          <div><span>{{ t('时间', 'Time') }}</span><strong>{{ formatDateTime(selectedEvent.timestamp) }}</strong></div>
          <div><span>{{ t('状态', 'Status') }}</span><NTag :type="statusType(selectedEvent.status)">{{ statusLabel(selectedEvent.status) }}</NTag></div>
          <div><span>{{ t('CPA 目标', 'CPA target') }}</span><strong>{{ selectedEvent.target_name || selectedEvent.target_id }}</strong></div>
          <div><span>{{ t('号池', 'Pool') }}</span><strong>{{ selectedEvent.pool_name || '-' }} · {{ selectedEvent.pool_id || '-' }}</strong></div>
          <div><span>{{ t('模型', 'Model') }}</span><strong>{{ selectedEvent.model || '-' }}</strong></div>
          <div><span>{{ t('Provider', 'Provider') }}</span><strong>{{ selectedEvent.provider || '-' }}</strong></div>
          <div><span>{{ t('用户', 'User') }}</span><strong>{{ selectedEvent.username || selectedEvent.user_id || '-' }}</strong></div>
          <div><span>{{ t('HTTP / 耗时', 'HTTP / Duration') }}</span><strong>{{ selectedEvent.http_status || '-' }} · {{ selectedEvent.duration_ms ?? 0 }} ms</strong></div>
        </div>

        <section class="detail-section">
          <span class="detail-label">{{ t('最终账号', 'Selected account') }}</span>
          <code class="selected-account">{{ selectedEvent.selected_auth_id || '-' }}</code>
          <div class="detail-tags">
            <NTag v-if="selectedEvent.selected_priority !== undefined" size="small">Priority {{ selectedEvent.selected_priority }}</NTag>
            <NTag v-if="selectedEvent.selected_state" size="small">{{ selectedEvent.selected_state }}</NTag>
            <NTag v-for="type in selectedEvent.account_types" :key="type" size="small" type="info">{{ type }}</NTag>
          </div>
        </section>

        <section class="detail-section">
          <span class="detail-label">{{ t('原因', 'Reason') }}</span>
          <pre class="reason-block">{{ reasonLabel(selectedEvent.reason) || '-' }}</pre>
        </section>

        <section class="detail-section">
          <div class="detail-section-heading">
            <span class="detail-label">{{ t('候选账号样本', 'Candidate account sample') }}</span>
            <strong>{{ selectedEvent.matched_count }} / {{ selectedEvent.candidate_count }}</strong>
          </div>
          <div v-if="selectedEvent.candidates?.length" class="candidate-list">
            <article v-for="candidate in selectedEvent.candidates" :key="candidate.id" class="candidate-row">
              <code>{{ candidate.id }}</code>
              <span>{{ candidate.provider || '-' }}</span>
              <span>P{{ candidate.priority ?? 0 }}</span>
              <span>{{ candidate.status || '-' }}</span>
              <span>{{ candidate.account_types?.join(' / ') || '-' }}</span>
            </article>
          </div>
          <NEmpty v-else :description="t('该事件没有候选账号样本', 'No candidate sample for this event')" />
        </section>
      </NDrawerContent>
    </NDrawer>
  </section>
</template>

<style scoped>
.plugin-events-page { display: grid; gap: 14px; }
.auto-refresh-label { color: var(--cpa-text-muted); font-size: 12px; }
.signal-strip { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); border: 1px solid var(--cpa-border); border-radius: 8px; background: var(--cpa-surface); overflow: hidden; }
.signal-strip > div { display: grid; gap: 3px; padding: 13px 15px; border-right: 1px solid var(--cpa-border); }
.signal-strip > div:last-child { border-right: 0; }
.signal-strip span { color: var(--cpa-text-muted); font-size: 12px; }
.signal-strip strong { font-size: 19px; font-variant-numeric: tabular-nums; }
.signal-strip .is-ok { color: #14804a; }
.signal-strip .is-bad { color: #c2413b; }
.filter-rail { display: grid; grid-template-columns: minmax(260px, 1.5fr) repeat(4, minmax(140px, 0.7fr)); gap: 8px; padding: 10px; border: 1px solid var(--cpa-border); border-radius: 8px; background: color-mix(in srgb, var(--cpa-surface-muted) 92%, #8da2b8 8%); }
.event-table-shell { min-height: 220px; border: 1px solid var(--cpa-border); border-radius: 8px; background: var(--cpa-surface); overflow: hidden; }
.event-table-shell :deep(.n-empty) { padding: 64px 16px; }
.event-time, .auth-id, .selected-account, .candidate-row code { font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace; font-size: 12px; }
.status-stack, .identity-stack { display: grid; gap: 3px; min-width: 0; }
.status-stack { justify-items: start; }
.phase-label, .identity-stack span { color: var(--cpa-text-muted); font-size: 11px; }
.identity-stack strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.auth-id { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--cpa-text); }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); border: 1px solid var(--cpa-border); border-radius: 8px; overflow: hidden; }
.detail-grid > div { display: grid; gap: 5px; padding: 12px 14px; border-right: 1px solid var(--cpa-border); border-bottom: 1px solid var(--cpa-border); }
.detail-grid > div:nth-child(2n) { border-right: 0; }
.detail-grid > div:nth-last-child(-n + 2) { border-bottom: 0; }
.detail-grid span, .detail-label { color: var(--cpa-text-muted); font-size: 12px; }
.detail-section { display: grid; gap: 10px; margin-top: 18px; }
.detail-section-heading { display: flex; justify-content: space-between; align-items: center; }
.selected-account { padding: 11px 12px; border: 1px solid var(--cpa-border); border-radius: 6px; background: color-mix(in srgb, var(--cpa-surface-muted) 88%, #6b7b8c 12%); overflow-wrap: anywhere; }
.detail-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.reason-block { margin: 0; padding: 11px 12px; border-left: 3px solid #d97706; background: color-mix(in srgb, var(--cpa-surface-muted) 91%, #d97706 9%); white-space: pre-wrap; overflow-wrap: anywhere; font: 12px/1.6 "Cascadia Code", Consolas, monospace; }
.candidate-list { border-top: 1px solid var(--cpa-border); }
.candidate-row { display: grid; grid-template-columns: minmax(220px, 1.5fr) minmax(90px, 0.7fr) 54px 76px minmax(100px, 0.8fr); gap: 10px; align-items: center; padding: 9px 4px; border-bottom: 1px solid var(--cpa-border); font-size: 12px; }
.candidate-row span { color: var(--cpa-text-muted); }
@media (max-width: 980px) {
  .signal-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .signal-strip > div { border-bottom: 1px solid var(--cpa-border); }
  .filter-rail { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .filter-rail :first-child { grid-column: 1 / -1; }
}
@media (max-width: 640px) {
  .signal-strip, .filter-rail, .detail-grid { grid-template-columns: 1fr; }
  .signal-strip > div, .detail-grid > div { border-right: 0; }
  .detail-grid > div:nth-last-child(2) { border-bottom: 1px solid var(--cpa-border); }
  .candidate-row { grid-template-columns: 1fr; gap: 3px; }
}
</style>
