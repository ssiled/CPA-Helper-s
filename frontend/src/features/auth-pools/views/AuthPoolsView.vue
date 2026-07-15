<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { NButton, NDataTable, NForm, NFormItem, NInput, NModal, NSelect, NSpace, NTag, useDialog, useMessage, type DataTableColumns } from 'naive-ui'
import { deleteAuthPool, getAuthPoolStatus, saveAuthPool } from '@/features/auth-pools/api/authPoolsApi'
import { listCodexKeeperAccounts } from '@/features/codex-keeper/api/codexKeeperApi'
import type { AuthPool, CodexKeeperAccount } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'

const message = useMessage()
const dialog = useDialog()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isSaving = ref(false)
const pools = ref<AuthPool[]>([])
const accounts = ref<CodexKeeperAccount[]>([])
const editorVisible = ref(false)
const poolID = ref('')
const poolName = ref('')
const poolDescription = ref('')
const selectedAuthIDs = ref<string[]>([])
const selectedBatchType = ref<string | null>(null)
const selectedAccountTypes = ref<string[]>([])

const accountOptions = computed(() => accounts.value.map((account) => ({
  label: accountLabel(account),
  value: account.name,
})))

const accountTypeOptions = computed(() => Array.from(new Set(accounts.value.map((account) => account.account_type?.trim()).filter(Boolean) as string[]))
  .sort((left, right) => left.localeCompare(right))
  .map((type) => ({ label: type, value: type })))

const healthyAuthIDs = computed(() => accounts.value.filter((account) => !account.disabled && (!account.last_status_code || account.last_status_code < 400)).map((account) => account.name))

const columns = computed<DataTableColumns<AuthPool>>(() => [
  { title: t('\u53f7\u6c60', 'Pool'), key: 'name', render: (row) => row.name },
  { title: t('ID', 'ID'), key: 'id' },
  { title: t('\u8d26\u53f7\u6570', 'Accounts'), key: 'auth_ids', render: (row) => String(row.auth_ids.length + dynamicTypeAccountCount(row.account_types ?? [], row.auth_ids)) },
  {
    title: t('\u53f7\u6c60\u8d26\u53f7', 'Pool accounts'),
    key: 'accounts',
    render: (row) => row.auth_ids.length || row.account_types?.length
      ? [...row.auth_ids.map((id) => hTag(id, accountStatus(id))), ...(row.account_types ?? []).map((type) => hTypeTag(type))].slice(0, 8)
      : t('\u6682\u65e0\u8d26\u53f7', 'No accounts'),
  },
  {
    title: t('\u64cd\u4f5c', 'Actions'),
    key: 'actions',
    width: 180,
    render: (row) => [
      hButton(t('\u7f16\u8f91', 'Edit'), () => editPool(row)),
      hButton(t('\u5220\u9664', 'Delete'), () => confirmDelete(row), 'error'),
    ],
  },
])

function hTag(id: string, status: string) {
  return h(NTag, { size: 'small', type: status === '\u6b63\u5e38' ? 'success' : status === '\u7981\u7528' ? 'warning' : 'default', style: 'margin-right: 6px; margin-bottom: 4px;' }, { default: () => `${id} · ${status}` })
}

function hTypeTag(type: string) {
  return h(NTag, { size: 'small', type: 'info', style: 'margin-right: 6px; margin-bottom: 4px;' }, { default: () => `${type} · ${t('\u52a8\u6001\u7c7b\u578b', 'dynamic type')}` })
}

function hButton(label: string, onClick: () => void, type: 'default' | 'error' = 'default') {
  return h(NButton, { size: 'small', text: true, type, style: 'margin-right: 10px;', onClick }, { default: () => label })
}

function accountStatus(id: string): string {
  const account = accounts.value.find((item) => item.name === id)
  if (!account) return '\u672a\u77e5'
  if (account.disabled) return '\u7981\u7528'
  if (account.last_status_code && account.last_status_code >= 400) return `\u5f02\u5e38 ${account.last_status_code}`
  return '\u6b63\u5e38'
}

function accountLabel(account: CodexKeeperAccount): string {
  return [account.name, account.email, account.account_type].filter((item) => item && item.trim()).join(' · ')
}

function dynamicTypeAccountCount(types: string[], manualAuthIDs: string[]): number {
  const normalizedTypes = new Set(types.map((type) => type.trim().toLowerCase()).filter(Boolean))
  if (normalizedTypes.size === 0) return 0
  const manualIDs = new Set(manualAuthIDs)
  return accounts.value.filter((account) => {
    const accountType = account.account_type?.trim().toLowerCase()
    return accountType && normalizedTypes.has(accountType) && !manualIDs.has(account.name)
  }).length
}

function mergeSelectedAuthIDs(ids: string[]) {
  selectedAuthIDs.value = Array.from(new Set([...selectedAuthIDs.value, ...ids]))
}

function selectAllHealthyAccounts() {
  mergeSelectedAuthIDs(healthyAuthIDs.value)
}

function selectAccountsByType() {
  if (!selectedBatchType.value) return
  const accountType = selectedBatchType.value.trim().toLowerCase()
  if (!accountType) return
  selectedAccountTypes.value = Array.from(new Set([...selectedAccountTypes.value, accountType]))
}

function clearSelectedAccounts() {
  selectedAuthIDs.value = []
  selectedAccountTypes.value = []
}

async function refresh() {
  isLoading.value = true
  try {
    const [status, accountResponse] = await Promise.all([getAuthPoolStatus(), listCodexKeeperAccounts()])
    pools.value = status.pools
    accounts.value = accountResponse.items
  } catch (error) {
    message.error(errorText(error, '\u52a0\u8f7d\u53f7\u6c60\u5931\u8d25', 'Failed to load auth pools'))
  } finally {
    isLoading.value = false
  }
}

function openCreate() {
  poolID.value = ''
  poolName.value = ''
  poolDescription.value = ''
  selectedAuthIDs.value = []
  selectedBatchType.value = null
  selectedAccountTypes.value = []
  editorVisible.value = true
}

function editPool(pool: AuthPool) {
  poolID.value = pool.id
  poolName.value = pool.name
  poolDescription.value = pool.description ?? ''
  selectedAuthIDs.value = [...pool.auth_ids]
  selectedBatchType.value = null
  selectedAccountTypes.value = [...(pool.account_types ?? [])]
  editorVisible.value = true
}

async function savePool() {
  const id = poolID.value.trim()
  const name = poolName.value.trim()
  if (!id || !name) {
    message.error(t('\u53f7\u6c60 ID \u548c\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a', 'Pool ID and name are required'))
    return
  }
  isSaving.value = true
  try {
    await saveAuthPool({ id, name, description: poolDescription.value.trim(), auth_ids: selectedAuthIDs.value, account_types: selectedAccountTypes.value })
    message.success(t('\u53f7\u6c60\u5df2\u4fdd\u5b58', 'Pool saved'))
    editorVisible.value = false
    await refresh()
  } catch (error) {
    message.error(errorText(error, '\u4fdd\u5b58\u53f7\u6c60\u5931\u8d25', 'Failed to save pool'))
  } finally {
    isSaving.value = false
  }
}

function confirmDelete(pool: AuthPool) {
  dialog.warning({
    title: t('\u5220\u9664\u53f7\u6c60', 'Delete pool'),
    content: t(`\u786e\u5b9a\u5220\u9664 ${pool.name}\uff1f\u5df2\u7ed1\u5b9a\u7684 API Key \u4f1a\u81ea\u52a8\u89e3\u7ed1\u3002`, `Delete pool ${pool.name}; bound keys will be unbound.`),
    positiveText: t('\u5220\u9664', 'Delete'),
    negativeText: t('\u53d6\u6d88', 'Cancel'),
    onPositiveClick: async () => {
      await deleteAuthPool(pool.id)
      message.success(t('\u53f7\u6c60\u5df2\u5220\u9664', 'Pool deleted'))
      await refresh()
    },
  })
}

onMounted(refresh)
</script>

<template>
  <section class="auth-pool-page dashboard-page">
    <div class="page-heading">
      <div>
        <h1 class="page-title">{{ t('\u53f7\u6c60\u7ba1\u7406', 'Auth Pools') }}</h1>
        <p class="page-subtitle">{{ t('\u50cf\u6587\u4ef6\u5939\u4e00\u6837\u7ba1\u7406 CPA \u8d26\u53f7\uff1b\u7ed1\u5b9a\u53f7\u6c60\u7684 API Key \u53ea\u4f1a\u5728\u8be5\u53f7\u6c60\u5185\u8c03\u5ea6\u8bf7\u6c42\u3002', 'Manage CPA accounts like folders. API keys bound to a pool are scheduled only within that pool.') }}</p>
      </div>
      <NSpace>
        <NButton secondary :loading="isLoading" @click="refresh">{{ t('\u5237\u65b0', 'Refresh') }}</NButton>
        <NButton type="primary" @click="openCreate">{{ t('\u65b0\u5efa\u53f7\u6c60', 'New pool') }}</NButton>
      </NSpace>
    </div>

    <NDataTable :loading="isLoading" :columns="columns" :data="pools" :pagination="{ pageSize: 10 }" />

    <NModal v-model:show="editorVisible" preset="card" :title="poolID ? t('\u7f16\u8f91\u53f7\u6c60', 'Edit pool') : t('\u65b0\u5efa\u53f7\u6c60', 'New pool')" :style="{ width: 'min(720px, calc(100vw - 32px))' }">
      <NForm label-placement="top">
        <NFormItem :label="t('\u53f7\u6c60 ID', 'Pool ID')">
          <NInput v-model:value="poolID" :disabled="isSaving" placeholder="codex-team-a" />
        </NFormItem>
        <NFormItem :label="t('\u53f7\u6c60\u540d\u79f0', 'Pool name')">
          <NInput v-model:value="poolName" :disabled="isSaving" placeholder="Team A" />
        </NFormItem>
        <NFormItem :label="t('\u63cf\u8ff0', 'Description')">
          <NInput v-model:value="poolDescription" :disabled="isSaving" />
        </NFormItem>
        <NFormItem :label="t('\u9009\u62e9\u8d26\u53f7', 'Select accounts')">
          <div class="account-picker">
            <NSelect v-model:value="selectedAuthIDs" multiple filterable :options="accountOptions" :disabled="isSaving" :placeholder="t('\u9009\u62e9\u8981\u52a0\u5165\u8be5\u53f7\u6c60\u7684 CPA \u8d26\u53f7', 'Select CPA accounts for this pool')" />
            <div class="batch-actions">
              <NButton size="small" secondary :disabled="isSaving || healthyAuthIDs.length === 0" @click="selectAllHealthyAccounts">
                {{ t('\u6279\u91cf\u52a0\u5165\u6b63\u5e38\u8d26\u53f7', 'Add healthy accounts') }}
              </NButton>
              <NSelect v-model:value="selectedBatchType" size="small" clearable filterable class="batch-type-select" :options="accountTypeOptions" :disabled="isSaving || accountTypeOptions.length === 0" :placeholder="t('\u6309\u8d26\u53f7\u7c7b\u578b\u6279\u91cf\u9009\u62e9', 'Select by account type')" />
              <NButton size="small" secondary :disabled="isSaving || !selectedBatchType" @click="selectAccountsByType">
                {{ t('\u81ea\u52a8\u68c0\u6d4b\u8be5\u7c7b\u578b', 'Auto-detect type') }}
              </NButton>
              <NButton size="small" quaternary :disabled="isSaving || (selectedAuthIDs.length === 0 && selectedAccountTypes.length === 0)" @click="clearSelectedAccounts">
                {{ t('\u6e05\u7a7a', 'Clear') }}
              </NButton>
            </div>
            <div v-if="selectedAccountTypes.length" class="selected-type-rules">
              <NTag v-for="type in selectedAccountTypes" :key="type" size="small" closable type="info" @close="selectedAccountTypes = selectedAccountTypes.filter((item) => item !== type)">
                {{ type }} · {{ t('\u81ea\u52a8\u52a0\u5165\u65b0\u8d26\u53f7', 'auto-add new accounts') }}
              </NTag>
            </div>
          </div>
        </NFormItem>
        <div class="modal-actions">
          <NButton secondary :disabled="isSaving" @click="editorVisible = false">{{ t('\u53d6\u6d88', 'Cancel') }}</NButton>
          <NButton type="primary" :loading="isSaving" @click="savePool">{{ t('\u4fdd\u5b58', 'Save') }}</NButton>
        </div>
      </NForm>
    </NModal>
  </section>
</template>

<style scoped>
.auth-pool-page { display: grid; gap: 16px; }
.account-picker { display: grid; gap: 10px; width: 100%; }
.batch-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.batch-type-select { min-width: 200px; max-width: 280px; }
.selected-type-rules { display: flex; flex-wrap: wrap; gap: 6px; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
