<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NButton, NEmpty, NSpin, NTag, useMessage } from 'naive-ui'
import { Activity, CheckCircle2, Clock3, RadioTower } from 'lucide-vue-next'

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
  return { total, available, rate: total ? Math.round((available / total) * 10000) / 100 : 0 }
})

async function refresh() {
  isLoading.value = true
  try {
    const response = await getChannelStatus()
    channels.value = response.items
    refreshedAt.value = response.refreshed_at
  } catch (error) {
    message.error(errorText(error, '加载渠道状态失败', 'Failed to load channel status'))
  } finally {
    isLoading.value = false
  }
}

function statusLabel(item: ChannelStatusItem): string {
  if (item.status === 'normal') return t('正常', 'Normal')
  if (item.status === 'quota_exhausted') return t('额度耗尽', 'Quota exhausted')
  if (item.status === 'disabled') return t('已停用', 'Disabled')
  return t('异常', 'Error')
}

function statusType(item: ChannelStatusItem): 'success' | 'warning' | 'error' {
  if (item.status === 'normal') return 'success'
  if (item.status === 'quota_exhausted') return 'warning'
  return 'error'
}

function remainingPercent(item: ChannelStatusItem): number | null {
  if (typeof item.primary_remaining_percent === 'number') return item.primary_remaining_percent
  if (typeof item.secondary_remaining_percent === 'number') return item.secondary_remaining_percent
  return null
}

function successRate(item: ChannelStatusItem): number {
  if (!item.window_records) return item.available ? 100 : 0
  return Math.round((item.window_success_records / item.window_records) * 10000) / 100
}

function sparkPoints(item: ChannelStatusItem): boolean[] {
  const total = Math.max(item.window_records, 24)
  const failed = Math.min(item.window_failed_records, total)
  return Array.from({ length: Math.min(total, 60) }, (_, index) => index < total - failed)
}

onMounted(refresh)
</script>

<template>
  <section class="channel-status-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('渠道状态', 'Channel Status') }}</h1>
        <p class="page-subtitle">
          {{ t('所有用户可见的 CPA 渠道健康概览，账号名称和邮箱已脱敏。', 'A shared CPA channel health overview with account names and emails hidden.') }}
        </p>
      </div>
      <NButton secondary :loading="isLoading" @click="refresh">{{ t('刷新', 'Refresh') }}</NButton>
    </div>

    <div class="summary-strip">
      <div class="summary-card">
        <CheckCircle2 :size="18" />
        <span>{{ t('可用渠道', 'Available') }}</span>
        <strong>{{ formatInteger(summary.available) }} / {{ formatInteger(summary.total) }}</strong>
      </div>
      <div class="summary-card">
        <Activity :size="18" />
        <span>{{ t('可用率', 'Availability') }}</span>
        <strong>{{ summary.rate }}%</strong>
      </div>
      <div class="summary-card">
        <Clock3 :size="18" />
        <span>{{ t('刷新时间', 'Refreshed') }}</span>
        <strong>{{ formatDateTime(refreshedAt) }}</strong>
      </div>
    </div>

    <NSpin :show="isLoading">
      <NEmpty v-if="!channels.length && !isLoading" :description="t('暂无渠道状态', 'No channel status yet')" />
      <div v-else class="channel-grid">
        <article v-for="channel in channels" :key="channel.id" class="channel-card">
          <div class="channel-head">
            <div class="channel-icon"><RadioTower :size="24" /></div>
            <div class="channel-title">
              <h2>{{ channel.name }}</h2>
              <div class="channel-tags">
                <NTag size="small">{{ channel.account_type }}</NTag>
                <NTag size="small" :type="statusType(channel)">{{ statusLabel(channel) }}</NTag>
              </div>
            </div>
          </div>

          <div class="metric-grid">
            <div class="metric-box">
              <span>{{ t('状态码', 'Status') }}</span>
              <strong>{{ channel.status_code ?? (channel.available ? 200 : '-') }}</strong>
            </div>
            <div class="metric-box">
              <span>{{ t('剩余额度', 'Remaining') }}</span>
              <strong>{{ remainingPercent(channel) ?? '-' }}<small v-if="remainingPercent(channel) !== null">%</small></strong>
            </div>
          </div>

          <div class="availability-row">
            <span>{{ t('窗口成功率', 'Window success') }}</span>
            <strong>{{ successRate(channel) }}%</strong>
          </div>

          <div class="sparkline" :aria-label="t('窗口记录', 'Window records')">
            <i v-for="(ok, index) in sparkPoints(channel)" :key="index" :class="ok ? 'is-ok' : 'is-fail'" />
          </div>

          <div class="channel-foot">
            <span>{{ t('窗口记录', 'Window records') }} {{ formatInteger(channel.window_records) }}</span>
            <span>{{ t('费用', 'Cost') }} {{ formatUsd(channel.window_cost_usd) }}</span>
          </div>
          <div class="channel-foot muted">
            <span>{{ t('最近健康', 'Last healthy') }} {{ formatDateTime(channel.last_healthy_at ?? null) }}</span>
          </div>
        </article>
      </div>
    </NSpin>
  </section>
</template>

<style scoped>
.channel-status-page { display: grid; gap: 18px; }
.summary-strip { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.summary-card { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border: 1px solid var(--border-subtle); border-radius: 14px; background: var(--surface-panel); }
.summary-card span { color: var(--text-muted); }
.summary-card strong { margin-left: auto; font-size: 18px; }
.channel-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }
.channel-card { display: grid; gap: 14px; padding: 18px; border: 1px solid var(--border-subtle); border-radius: 18px; background: var(--surface-panel); box-shadow: var(--shadow-soft); }
.channel-head { display: flex; gap: 12px; align-items: center; }
.channel-icon { display: grid; place-items: center; width: 48px; height: 48px; border-radius: 14px; color: #db6b1d; background: #fff3e6; }
.channel-title { min-width: 0; }
.channel-title h2 { margin: 0 0 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 18px; }
.channel-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.metric-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.metric-box { padding: 14px; border: 1px solid var(--border-subtle); border-radius: 14px; background: var(--surface-muted); }
.metric-box span, .availability-row span, .channel-foot { color: var(--text-muted); font-size: 13px; }
.metric-box strong { display: block; margin-top: 8px; font-size: 22px; }
.metric-box small { font-size: 13px; }
.availability-row { display: flex; align-items: baseline; justify-content: space-between; padding: 10px 0; border-top: 1px solid var(--border-subtle); }
.availability-row strong { font-size: 30px; }
.sparkline { display: grid; grid-template-columns: repeat(30, 1fr); gap: 3px; align-items: end; min-height: 34px; }
.sparkline i { display: block; height: 26px; border-radius: 999px; background: #5ac489; }
.sparkline i.is-fail { height: 8px; background: #e75f5f; }
.channel-foot { display: flex; justify-content: space-between; gap: 10px; }
.channel-foot.muted { justify-content: flex-start; }
@media (max-width: 760px) { .summary-strip { grid-template-columns: 1fr; } }
</style>
