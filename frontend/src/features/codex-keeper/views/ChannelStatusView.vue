<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NButton, NEmpty, NSpin, NTag, useMessage } from 'naive-ui'
import { CheckCircle2, Clock3, FolderKanban, RadioTower, WalletCards } from 'lucide-vue-next'

import { getChannelStatus } from '@/features/codex-keeper/api/codexKeeperApi'
import type { ChannelStatusItem } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import { formatDateTime, formatInteger, formatUsd } from '@/shared/utils/format'

const message = useMessage()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const channels = ref<ChannelStatusItem[]>([])
const refreshedAt = ref<string | null>(null)

const summary = computed(() => {
  const total = channels.value.length
  const available = channels.value.filter((item) => item.available).length
  return {
    total,
    available,
    rate: total ? Math.round((available / total) * 10000) / 100 : 0,
  }
})

async function refresh() {
  isLoading.value = true
  try {
    const response = await getChannelStatus()
    channels.value = response.items
    refreshedAt.value = response.refreshed_at ?? latestRefreshedAt(response.items)
  } catch (error) {
    message.error(errorText(error, '加载渠道状态失败', 'Failed to load channel status'))
  } finally {
    isLoading.value = false
  }
}

function latestRefreshedAt(items: ChannelStatusItem[]): string | null {
  const values = items.map((item) => item.refreshed_at).filter(Boolean).sort()
  return values.length ? values[values.length - 1] ?? null : null
}

function statusLabel(item: ChannelStatusItem): string {
  if (item.status === 'normal') return t('正常', 'Normal')
  if (item.status === 'degraded') return t('部分异常', 'Degraded')
  if (item.status === 'quota_exhausted') return t('额度耗尽', 'Quota exhausted')
  if (item.status === 'disabled') return t('已停用', 'Disabled')
  if (item.status === 'empty') return t('无账号', 'Empty')
  return t('异常', 'Error')
}

function statusType(item: ChannelStatusItem): 'success' | 'warning' | 'error' | 'default' {
  if (item.status === 'normal') return 'success'
  if (item.status === 'degraded' || item.status === 'quota_exhausted') return 'warning'
  if (item.status === 'empty') return 'default'
  return 'error'
}

function remainingPercent(item: ChannelStatusItem): number | null {
  if (typeof item.primary_remaining_percent === 'number') return item.primary_remaining_percent
  if (typeof item.secondary_remaining_percent === 'number') return item.secondary_remaining_percent
  return null
}

function remainingText(item: ChannelStatusItem): string {
  const value = remainingPercent(item)
  return value === null ? '-' : `${value}%`
}

function successRate(item: ChannelStatusItem): number {
  if (!item.window_records) return item.available ? 100 : 0
  return Math.round((item.window_success_records / item.window_records) * 10000) / 100
}

function sparkPoints(item: ChannelStatusItem): Array<'ok' | 'fail'> {
  const total = Math.max(item.window_records, 24)
  const failed = Math.min(item.window_failed_records, total)
  return Array.from({ length: Math.min(total, 36) }, (_, index) => (index < total - failed ? 'ok' : 'fail'))
}

function typeLabel(item: ChannelStatusItem): string {
  return item.account_types.length ? item.account_types.join(' / ') : t('手动账号', 'Manual accounts')
}

function availabilityCaption(item: ChannelStatusItem): string {
  return `${t('可用账号', 'Available accounts')} ${formatInteger(item.available_accounts)} / ${formatInteger(item.account_count)}`
}

onMounted(refresh)
</script>

<template>
  <section class="channel-status-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('渠道状态', 'Channel Status') }}</h1>
        <p class="page-subtitle">
          {{ t('按号池展示 CPA 渠道健康状态；页面读取数据库快照，后台每 5 分钟更新，统计窗口为最近 7 天。', 'Pool-based CPA channel health from stored snapshots, refreshed every 5 minutes with a 7-day window.') }}
        </p>
      </div>
      <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新页面', 'Refresh page') }}</NButton>
    </div>

    <div class="summary-strip">
      <div class="summary-card">
        <FolderKanban :size="18" />
        <span>{{ t('号池数量', 'Pools') }}</span>
        <strong>{{ formatInteger(summary.total) }}</strong>
      </div>
      <div class="summary-card">
        <CheckCircle2 :size="18" />
        <span>{{ t('可用号池', 'Available pools') }}</span>
        <strong>{{ formatInteger(summary.available) }} / {{ formatInteger(summary.total) }}</strong>
      </div>
      <div class="summary-card">
        <Clock3 :size="18" />
        <span>{{ t('快照时间', 'Snapshot') }}</span>
        <strong>{{ formatDateTime(refreshedAt) }}</strong>
      </div>
    </div>

    <NSpin :show="isLoading">
      <NEmpty
        v-if="!channels.length && !isLoading"
        :description="t('暂无号池状态快照，请确认 cpa-auth-pool 插件已启用并等待后台刷新。', 'No pool status snapshot yet. Confirm cpa-auth-pool is enabled and wait for background refresh.')"
      />
      <div v-else class="channel-card-grid">
        <article v-for="channel in channels" :key="channel.id" class="channel-panel">
          <header class="channel-panel__header">
            <div class="channel-panel__brand">
              <div class="channel-panel__icon">
                <RadioTower :size="24" />
              </div>
              <div class="channel-panel__title-block">
                <div class="channel-panel__title-row">
                  <h2>{{ channel.name }}</h2>
                  <NTag round size="medium" :type="statusType(channel)">{{ statusLabel(channel) }}</NTag>
                </div>
                <div class="channel-panel__chips">
                  <NTag size="small">{{ channel.id }}</NTag>
                  <NTag size="small" type="info">{{ typeLabel(channel) }}</NTag>
                  <NTag v-if="channel.description" size="small" type="default">{{ channel.description }}</NTag>
                </div>
              </div>
            </div>
          </header>

          <div class="channel-panel__stats-row">
            <section class="channel-stat-card">
              <div class="channel-stat-card__label">
                <WalletCards :size="16" />
                <span>{{ t('可用账号', 'Available accounts') }}</span>
              </div>
              <div class="channel-stat-card__value">
                {{ formatInteger(channel.available_accounts) }}<small>/{{ formatInteger(channel.account_count) }}</small>
              </div>
            </section>
            <section class="channel-stat-card">
              <div class="channel-stat-card__label">
                <CheckCircle2 :size="16" />
                <span>{{ t('剩余额度', 'Remaining') }}</span>
              </div>
              <div class="channel-stat-card__value">{{ remainingText(channel) }}</div>
            </section>
          </div>

          <div class="channel-panel__divider" />

          <section class="channel-panel__availability">
            <div>
              <p class="channel-panel__eyebrow">{{ t('可用性 · 7 天', 'Availability · 7 days') }}</p>
              <p class="channel-panel__caption">{{ availabilityCaption(channel) }}</p>
            </div>
            <strong>{{ successRate(channel) }}%</strong>
          </section>

          <section class="channel-panel__bars">
            <div class="channel-panel__bars-head">
              <span>{{ t('窗口记录分布', 'Window record distribution') }}</span>
              <span>{{ formatDateTime(channel.refreshed_at) }}</span>
            </div>
            <div class="channel-panel__sparkline" :aria-label="t('近 7 天窗口记录', '7-day window records')">
              <i
                v-for="(point, index) in sparkPoints(channel)"
                :key="`${channel.id}-${index}`"
                :class="point === 'ok' ? 'is-ok' : 'is-fail'"
              />
            </div>
          </section>

          <footer class="channel-panel__footer">
            <div class="channel-panel__footer-row">
              <span>{{ t('7 天请求', '7-day records') }} {{ formatInteger(channel.window_records) }}</span>
              <span>{{ t('费用', 'Cost') }} {{ formatUsd(channel.window_cost_usd) }}</span>
            </div>
            <div class="channel-panel__footer-row is-muted">
              <span>{{ t('异常', 'Errors') }} {{ formatInteger(channel.error_accounts) }}</span>
              <span>{{ t('停用', 'Disabled') }} {{ formatInteger(channel.disabled_accounts) }}</span>
              <span>{{ t('耗尽', 'Exhausted') }} {{ formatInteger(channel.quota_exhausted_accounts) }}</span>
              <span>{{ t('最近健康', 'Last healthy') }} {{ formatDateTime(channel.last_healthy_at ?? null) }}</span>
            </div>
          </footer>
        </article>
      </div>
    </NSpin>
  </section>
</template>

<style scoped>
.channel-status-page {
  display: grid;
  gap: 16px;
}

.summary-strip {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.summary-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 16px;
  border: 1px solid rgba(149, 157, 165, 0.18);
  border-radius: 18px;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.96), rgba(248, 250, 252, 0.9));
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.06);
}

.summary-card span {
  color: var(--text-muted);
}

.summary-card strong {
  margin-left: auto;
  font-size: 18px;
  font-variant-numeric: tabular-nums;
}

.channel-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 360px));
  justify-content: start;
  gap: 14px;
}

.channel-panel {
  display: grid;
  gap: 12px;
  min-height: 0;
  padding: 16px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 16px;
  background:
    radial-gradient(circle at top left, rgba(214, 250, 229, 0.7), rgba(255, 255, 255, 0) 34%),
    linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(246, 248, 251, 0.96));
  box-shadow:
    0 14px 34px rgba(15, 23, 42, 0.07),
    inset 0 1px 0 rgba(255, 255, 255, 0.9);
}

.channel-panel__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.channel-panel__brand {
  display: flex;
  gap: 12px;
  min-width: 0;
  width: 100%;
}

.channel-panel__icon {
  flex: 0 0 48px;
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  border-radius: 16px;
  color: #3b9f7e;
  background: linear-gradient(180deg, rgba(220, 252, 231, 0.95), rgba(209, 250, 229, 0.78));
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.8);
}

.channel-panel__title-block {
  display: grid;
  gap: 7px;
  min-width: 0;
  flex: 1;
}

.channel-panel__title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}

.channel-panel__title-row h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.1;
  color: #0f172a;
  text-wrap: balance;
}

.channel-panel__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.channel-panel__stats-row {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.channel-stat-card {
  padding: 12px 12px 10px;
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 16px;
  background: rgba(248, 250, 252, 0.86);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.82);
}

.channel-stat-card__label {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #94a3b8;
  font-size: 12px;
}

.channel-stat-card__value {
  margin-top: 8px;
  font-size: 20px;
  font-weight: 700;
  line-height: 1;
  color: #0f172a;
  font-variant-numeric: tabular-nums;
}

.channel-stat-card__value small {
  margin-left: 4px;
  font-size: 13px;
  font-weight: 500;
  color: #64748b;
}

.channel-panel__divider {
  height: 1px;
  background: linear-gradient(90deg, rgba(226, 232, 240, 0.72), rgba(226, 232, 240, 0.18));
}

.channel-panel__availability {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
}

.channel-panel__eyebrow {
  margin: 0;
  color: #94a3b8;
  font-size: 13px;
}

.channel-panel__caption {
  margin: 5px 0 0;
  color: #64748b;
  font-size: 13px;
}

.channel-panel__availability strong {
  font-size: clamp(30px, 4vw, 42px);
  line-height: 0.95;
  color: #56b947;
  letter-spacing: 0;
  font-variant-numeric: tabular-nums;
}

.channel-panel__bars {
  display: grid;
  gap: 8px;
}

.channel-panel__bars-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: #94a3b8;
  font-size: 12px;
}

.channel-panel__sparkline {
  display: grid;
  grid-template-columns: repeat(36, minmax(0, 1fr));
  gap: 3px;
  align-items: end;
  min-height: 34px;
}

.channel-panel__sparkline i {
  display: block;
  height: 24px;
  border-radius: 999px;
  background: linear-gradient(180deg, #57c38a, #4cae7d);
}

.channel-panel__sparkline i.is-fail {
  height: 12px;
  background: linear-gradient(180deg, #f1b04c, #e48e2a);
}

.channel-panel__footer {
  margin-top: auto;
  display: grid;
  gap: 7px;
}

.channel-panel__footer-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
  color: #64748b;
  font-size: 13px;
}

.channel-panel__footer-row.is-muted {
  color: #94a3b8;
}

@media (max-width: 960px) {
  .channel-card-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .summary-strip {
    grid-template-columns: 1fr;
  }

  .channel-panel {
    min-height: auto;
    padding: 18px;
    border-radius: 24px;
  }

  .channel-panel__title-row {
    flex-direction: column;
    align-items: flex-start;
  }

  .channel-panel__stats-row {
    grid-template-columns: 1fr;
  }

  .channel-panel__availability {
    flex-direction: column;
    align-items: flex-start;
  }

  .channel-panel__bars-head {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
