<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NButton, NIcon, NInput, NSelect, NSpin, NSwitch, NTag } from 'naive-ui'
import {
  AlertTriangle,
  Clock3,
  ExternalLink,
  MessageCircle,
  Package,
  RefreshCw,
  Search,
  ShoppingBag,
  Star,
  Store,
} from 'lucide-vue-next'

import {
  getCardShopFavorites,
  getCardShops,
  updateCardShopFavorite,
} from '@/features/card-shops/api/cardShopsApi'
import type { CardShop, CardShopProductItem } from '@/shared/types/api'
import { useI18n } from '@/shared/i18n'
import { formatDateTime, formatInteger } from '@/shared/utils/format'

type CardShopSortKey = 'relevance' | 'priceAsc' | 'stockDesc' | 'recent' | 'salesDesc'

interface CardShopRow {
  shop: CardShop
  shopKey: string
  isFavorite: boolean
  visibleProducts: CardShopProductItem[]
  productCount: number
  matchedProductCount: number
  minPrice: number | null
  totalStock: number
  sales: number
  latestMs: number
  score: number
}

const HOT_TAGS = ['Codex', 'RT', '接码', 'PP', 'kiro', 'Claude', 'Gemini', 'team', 'GPT'] as const
const DEFAULT_EMPTY_TEXT = '-'

const { currentLanguage, errorText, t } = useI18n()
const shops = ref<CardShop[]>([])
const fetchedAt = ref<string | null>(null)
const isLoading = ref(false)
const loadError = ref<string | null>(null)
const favoriteError = ref<string | null>(null)
const favoriteOnly = ref(false)
const favoriteKeys = ref<Set<string>>(new Set())
const favoriteUpdatingKeys = ref<Set<string>>(new Set())
const searchDraft = ref('')
const searchTerms = ref<string[]>([])
const sortKey = ref<CardShopSortKey>('salesDesc')

const sortOptions = computed<Array<{ label: string; value: CardShopSortKey }>>(() => [
  { label: t('综合排序', 'Best match'), value: 'relevance' },
  { label: t('价格低到高', 'Price: low to high'), value: 'priceAsc' },
  { label: t('库存多到少', 'Stock: high to low'), value: 'stockDesc' },
  { label: t('最近同步', 'Recently synced'), value: 'recent' },
  { label: t('销量高到低', 'Sales: high to low'), value: 'salesDesc' },
])

const searchQueries = computed(() => {
  const queries = searchTerms.value.map((term) => normalizeSearchText(term))
  const draft = normalizeSearchText(searchDraft.value)
  if (draft) {
    queries.push(draft)
  }
  return Array.from(new Set(queries))
})

const rows = computed<CardShopRow[]>(() => {
  const nextRows = shops.value
    .map((shop, index) => buildShopRow(shop, index, searchQueries.value))
    .filter((row): row is CardShopRow => row !== null)
    .filter((row) => !favoriteOnly.value || row.isFavorite)

  return sortRows(nextRows, sortKey.value)
})

const totalProductCount = computed(() =>
  shops.value.reduce((total, shop) => total + shopProducts(shop).length, 0),
)
const favoriteShopCount = computed(() =>
  shops.value.reduce((total, shop, index) => total + (favoriteKeys.value.has(shopKeyForShop(shop, index)) ? 1 : 0), 0),
)
const visibleProductCount = computed(() =>
  rows.value.reduce((total, row) => total + row.visibleProducts.length, 0),
)
const fetchedAtText = computed(() =>
  fetchedAt.value ? formatDateTime(fetchedAt.value, { includeSecond: false }) : DEFAULT_EMPTY_TEXT,
)
const resultSummary = computed(() =>
  t(
    `共 ${formatInteger(rows.value.length)} / ${formatInteger(shops.value.length)} 个店铺，展示 ${formatInteger(visibleProductCount.value)} / ${formatInteger(totalProductCount.value)} 个商品`,
    `${formatInteger(rows.value.length)} / ${formatInteger(shops.value.length)} shops, ${formatInteger(visibleProductCount.value)} / ${formatInteger(totalProductCount.value)} products shown`,
  ),
)
const metricItems = computed(() => [
  {
    key: 'shops',
    label: t('收录店铺', 'Shops'),
    value: formatInteger(shops.value.length),
    footnote: t('公开店铺快照', 'Public shop snapshot'),
    icon: Store,
    tone: 'is-teal',
  },
  {
    key: 'products',
    label: t('在售商品', 'Products'),
    value: formatInteger(totalProductCount.value),
    footnote: t('按接口当前返回统计', 'Counted from the latest response'),
    icon: Package,
    tone: 'is-blue',
  },
  {
    key: 'filtered',
    label: t('筛选结果', 'Filtered'),
    value: formatInteger(rows.value.length),
    footnote: resultSummary.value,
    icon: Search,
    tone: 'is-purple',
  },
  {
    key: 'fetched',
    label: t('本次拉取', 'Fetched'),
    value: fetchedAtText.value,
    footnote: t('进入页面或点击刷新时更新', 'Updated on page load or refresh'),
    icon: Clock3,
    tone: 'is-orange',
  },
])

onMounted(() => {
  void refresh()
})

async function refresh() {
  isLoading.value = true
  loadError.value = null
  favoriteError.value = null
  try {
    const [shopsResponse, favoritesResponse] = await Promise.all([
      getCardShops(),
      getCardShopFavorites(),
    ])
    shops.value = shopsResponse.shops ?? []
    fetchedAt.value = shopsResponse.fetched_at
    favoriteKeys.value = new Set(favoritesResponse.shop_keys ?? [])
  } catch (error) {
    loadError.value = errorText(error, '加载卡网收录失败', 'Failed to load card shops')
  } finally {
    isLoading.value = false
  }
}

function applyHotTag(tag: string) {
  const normalizedTag = normalizeSearchText(tag)
  const existingIndex = searchTerms.value.findIndex((term) => normalizeSearchText(term) === normalizedTag)
  if (existingIndex >= 0) {
    searchTerms.value.splice(existingIndex, 1)
    return
  }
  addSearchTerm(tag)
  if (normalizeSearchText(searchDraft.value) === normalizedTag) {
    searchDraft.value = ''
  }
}

function addSearchDraft() {
  addSearchTerm(searchDraft.value)
  searchDraft.value = ''
}

function addSearchTerm(value: string) {
  const term = value.trim()
  if (!term) {
    return
  }
  const normalizedTerm = normalizeSearchText(term)
  if (searchTerms.value.some((item) => normalizeSearchText(item) === normalizedTerm)) {
    return
  }
  searchTerms.value.push(term)
}

function removeSearchTerm(term: string) {
  const normalizedTerm = normalizeSearchText(term)
  searchTerms.value = searchTerms.value.filter((item) => normalizeSearchText(item) !== normalizedTerm)
}

function clearSearchFilters() {
  searchTerms.value = []
  searchDraft.value = ''
}

async function toggleFavorite(row: CardShopRow) {
  const nextFavorite = !row.isFavorite
  favoriteError.value = null
  setFavoriteUpdating(row.shopKey, true)
  try {
    const response = await updateCardShopFavorite({
      shop_key: row.shopKey,
      favorite: nextFavorite,
    })
    favoriteKeys.value = new Set(response.shop_keys ?? [])
    if (favoriteOnly.value && favoriteKeys.value.size === 0) {
      favoriteOnly.value = false
    }
  } catch (error) {
    favoriteError.value = errorText(error, '更新收藏失败', 'Failed to update favorite')
  } finally {
    setFavoriteUpdating(row.shopKey, false)
  }
}

function setFavoriteUpdating(shopKey: string, updating: boolean) {
  const nextKeys = new Set(favoriteUpdatingKeys.value)
  if (updating) {
    nextKeys.add(shopKey)
  } else {
    nextKeys.delete(shopKey)
  }
  favoriteUpdatingKeys.value = nextKeys
}

function isFavoriteUpdating(shopKey: string): boolean {
  return favoriteUpdatingKeys.value.has(shopKey)
}

function isSearchTermActive(value: string): boolean {
  const normalizedValue = normalizeSearchText(value)
  return searchQueries.value.includes(normalizedValue)
}

function normalizeSearchText(value: string): string {
  return value.trim().toLowerCase()
}

function textValue(value: string | null | undefined): string {
  return value?.trim() || ''
}

function displayText(value: string | null | undefined): string {
  return textValue(value) || DEFAULT_EMPTY_TEXT
}

function numberValue(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function shopProducts(shop: CardShop): CardShopProductItem[] {
  const productItems = Array.isArray(shop.productItems) ? shop.productItems : []
  if (productItems.length > 0) {
    return productItems
  }
  const names = Array.isArray(shop.productsInStock) ? shop.productsInStock : []
  return names.map((name) => ({ name }))
}

function searchableProductTitleText(product: CardShopProductItem): string {
  return textValue(product.name).toLowerCase()
}

function shopKeyForShop(shop: CardShop, index: number): string {
  return textValue(shop.id) || textValue(shop.shopUrl) || textValue(shop.shopName) || `shop-${index}`
}

function buildShopRow(shop: CardShop, index: number, queries: string[]): CardShopRow | null {
  const products = shopProducts(shop)
  const productTitleTexts = products.map((product) => searchableProductTitleText(product))
  const matchedProducts = queries.length > 0
    ? products.filter((_, productIndex) =>
        queries.every((query) => productTitleTexts[productIndex]?.includes(query) ?? false),
      )
    : products
  if (queries.length > 0 && matchedProducts.length === 0) {
    return null
  }

  const visibleProducts = queries.length > 0 ? matchedProducts : products
  const shopKey = shopKeyForShop(shop, index)
  return {
    shop,
    shopKey,
    isFavorite: favoriteKeys.value.has(shopKey),
    visibleProducts,
    productCount: products.length,
    matchedProductCount: visibleProducts.length,
    minPrice: minProductPrice(visibleProducts),
    totalStock: visibleProducts.reduce((total, product) => total + (numberValue(product.stockCount) ?? 0), 0),
    sales: numberValue(shop.shopSellCount) ?? 0,
    latestMs: updatedAtMs(shop),
    score: queries.length > 0 ? relevanceScore(visibleProducts, queries) : 0,
  }
}

function relevanceScore(products: CardShopProductItem[], queries: string[]): number {
  return queries.reduce((total, query) => total + relevanceScoreForQuery(products, query), 0)
}

function relevanceScoreForQuery(products: CardShopProductItem[], query: string): number {
  let score = 0
  products.forEach((product) => {
    const name = searchableProductTitleText(product)
    if (name === query) {
      score += 36
    } else if (name.includes(query)) {
      score += 18
    }
  })
  return score
}

function minProductPrice(products: CardShopProductItem[]): number | null {
  const prices = products
    .map((product) => numberValue(product.price))
    .filter((price): price is number => price !== null)
  return prices.length > 0 ? Math.min(...prices) : null
}

function updatedAtMs(shop: CardShop): number {
  const updatedAt = textValue(shop.updatedAt)
  if (!updatedAt) {
    return 0
  }
  const parsed = Date.parse(updatedAt)
  return Number.isFinite(parsed) ? parsed : 0
}

function sortRows(sourceRows: CardShopRow[], key: CardShopSortKey): CardShopRow[] {
  const nextRows = [...sourceRows]
  nextRows.sort((left, right) => {
    const favoriteComparison = compareFavoriteRows(left, right)
    if (favoriteComparison !== 0) {
      return favoriteComparison
    }

    switch (key) {
      case 'priceAsc':
        return compareNullableNumberAsc(left.minPrice, right.minPrice) || compareShopNames(left, right)
      case 'stockDesc':
        return right.totalStock - left.totalStock || compareShopNames(left, right)
      case 'recent':
        return right.latestMs - left.latestMs || compareShopNames(left, right)
      case 'salesDesc':
        return right.sales - left.sales || compareShopNames(left, right)
      case 'relevance':
        return (
          right.score - left.score ||
          right.latestMs - left.latestMs ||
          right.sales - left.sales ||
          right.totalStock - left.totalStock ||
          compareShopNames(left, right)
        )
    }
  })
  return nextRows
}

function compareFavoriteRows(left: CardShopRow, right: CardShopRow): number {
  if (left.isFavorite === right.isFavorite) {
    return 0
  }
  return left.isFavorite ? -1 : 1
}

function compareNullableNumberAsc(left: number | null, right: number | null): number {
  if (left === null && right === null) {
    return 0
  }
  if (left === null) {
    return 1
  }
  if (right === null) {
    return -1
  }
  return left - right
}

function compareShopNames(left: CardShopRow, right: CardShopRow): number {
  return displayText(left.shop.shopName).localeCompare(displayText(right.shop.shopName), currentLanguage.value)
}

function formatShopPrice(value: number | null | undefined): string {
  const price = numberValue(value)
  if (price === null) {
    return DEFAULT_EMPTY_TEXT
  }
  return new Intl.NumberFormat(currentLanguage.value === 'zh' ? 'zh-CN' : 'en-US', {
    style: 'currency',
    currency: 'CNY',
    maximumFractionDigits: price < 10 ? 2 : 1,
  }).format(price)
}

function formatCount(value: number | null | undefined): string {
  const count = numberValue(value)
  return count === null ? DEFAULT_EMPTY_TEXT : formatInteger(count)
}

function telegramHref(value: string | null | undefined): string | null {
  const telegram = textValue(value)
  if (!telegram) {
    return null
  }
  if (telegram.startsWith('http://') || telegram.startsWith('https://')) {
    return telegram
  }
  if (telegram.startsWith('@') && telegram.length > 1) {
    return `https://t.me/${encodeURIComponent(telegram.slice(1))}`
  }
  return null
}
</script>

<template>
  <section class="page card-shops-page">
    <div class="page-header card-shops-header">
      <div>
        <h1 class="page-title">{{ t('卡网收录', 'Card shops') }}</h1>
        <p class="page-subtitle">
          {{ t('同步公开卡网店铺与商品快照，用于检索和甄别。', 'Browse public card-shop and product snapshots for lookup and screening.') }}
        </p>
      </div>
      <NButton type="primary" :loading="isLoading" @click="refresh">
        <template #icon>
          <NIcon :component="RefreshCw" />
        </template>
        {{ t('刷新', 'Refresh') }}
      </NButton>
    </div>

    <NAlert class="risk-alert" type="warning" :show-icon="false">
      <div class="risk-alert-content">
        <NIcon :component="AlertTriangle" :size="18" />
        <span>{{ t('仅做公开店铺信息收录，不参与交易，也不对店铺商品、售后和风险负责。使用前请自行甄别。', 'This only indexes public shop information. CPA-Helper does not participate in transactions and is not responsible for products, after-sales service, or risk. Assess independently before use.') }}</span>
      </div>
    </NAlert>

    <section class="panel">
      <div class="panel-inner card-shop-toolbar">
        <div class="search-row">
          <NInput
            v-model:value="searchDraft"
            clearable
            :placeholder="t('商品标题名', 'Product title')"
            @keydown.enter.prevent="addSearchDraft"
          >
            <template #prefix>
              <NIcon :component="Search" />
            </template>
          </NInput>
          <div class="sort-control">
            <span>{{ t('排序', 'Sort') }}</span>
            <NSelect v-model:value="sortKey" :options="sortOptions" />
          </div>
          <label class="favorite-filter">
            <span>{{ t('收藏筛选', 'Favorites') }}</span>
            <span class="favorite-switch-line">
              <NSwitch v-model:value="favoriteOnly" />
              <span>
                {{ t(`仅看收藏 ${formatInteger(favoriteShopCount)}`, `Favorites only ${formatInteger(favoriteShopCount)}`) }}
              </span>
            </span>
          </label>
        </div>
        <div class="tag-row">
          <span>{{ t('热门搜索标签:', 'Popular tags:') }}</span>
          <NButton
            v-for="tag in HOT_TAGS"
            :key="tag"
            size="small"
            secondary
            :type="isSearchTermActive(tag) ? 'primary' : 'default'"
            @click="applyHotTag(tag)"
          >
            {{ tag }}
          </NButton>
        </div>
        <div v-if="searchTerms.length > 0" class="selected-term-row">
          <span>{{ t('已选条件:', 'Selected filters:') }}</span>
          <NTag
            v-for="term in searchTerms"
            :key="term"
            size="small"
            type="info"
            closable
            :bordered="false"
            @close="removeSearchTerm(term)"
          >
            {{ term }}
          </NTag>
          <NButton size="tiny" quaternary @click="clearSearchFilters">
            {{ t('清空', 'Clear') }}
          </NButton>
        </div>
      </div>
    </section>

    <NAlert v-if="favoriteError" class="favorite-error" type="error" :show-icon="false">
      {{ favoriteError }}
    </NAlert>

    <div class="metric-grid card-shop-metrics">
      <div v-for="metric in metricItems" :key="metric.key" class="metric-card card-shop-metric" :class="metric.tone">
        <div class="metric-icon">
          <NIcon :component="metric.icon" :size="24" />
        </div>
        <div class="metric-label">{{ metric.label }}</div>
        <div class="metric-value">{{ metric.value }}</div>
        <div class="metric-footnote">{{ metric.footnote }}</div>
      </div>
    </div>

    <section v-if="loadError" class="panel error-panel">
      <div class="panel-inner error-state">
        <strong>{{ loadError }}</strong>
        <NButton secondary :loading="isLoading" @click="refresh">
          <template #icon>
            <NIcon :component="RefreshCw" />
          </template>
          {{ t('重试', 'Retry') }}
        </NButton>
      </div>
    </section>

    <NSpin :show="isLoading && shops.length === 0">
      <section v-if="!loadError && rows.length > 0" class="shop-list">
        <article v-for="row in rows" :key="row.shopKey" class="panel shop-card" :class="{ 'is-favorite': row.isFavorite }">
          <div class="shop-card-head">
            <div class="shop-title-block">
              <div class="shop-title-line">
                <h2>{{ displayText(row.shop.shopName) }}</h2>
                <a
                  v-if="textValue(row.shop.shopUrl)"
                  class="external-link"
                  :href="textValue(row.shop.shopUrl)"
                  target="_blank"
                  rel="noreferrer"
                  :aria-label="t('打开店铺链接', 'Open shop link')"
                >
                  <NIcon :component="ExternalLink" :size="16" />
                </a>
              </div>
              <a
                v-if="textValue(row.shop.shopUrl)"
                class="shop-url"
                :href="textValue(row.shop.shopUrl)"
                target="_blank"
                rel="noreferrer"
              >
                {{ textValue(row.shop.shopUrl) }}
              </a>
            </div>
            <div class="shop-actions">
              <NButton
                size="small"
                secondary
                :type="row.isFavorite ? 'warning' : 'default'"
                :loading="isFavoriteUpdating(row.shopKey)"
                @click="toggleFavorite(row)"
              >
                <template #icon>
                  <NIcon :component="Star" />
                </template>
                {{ row.isFavorite ? t('已收藏', 'Favorited') : t('收藏', 'Favorite') }}
              </NButton>
              <div class="shop-tags">
                <NTag size="small" type="success" :bordered="false">
                  {{ t(`销量 ${formatCount(row.shop.shopSellCount)}`, `Sales ${formatCount(row.shop.shopSellCount)}`) }}
                </NTag>
                <NTag size="small" type="info" :bordered="false">
                  {{ t(`商品 ${formatInteger(row.productCount)}`, `${formatInteger(row.productCount)} products`) }}
                </NTag>
                <NTag size="small" :bordered="false">
                  {{ t(`库存 ${formatInteger(row.totalStock)}`, `Stock ${formatInteger(row.totalStock)}`) }}
                </NTag>
              </div>
            </div>
          </div>

          <div class="shop-meta-row">
            <span>
              <NIcon :component="MessageCircle" :size="15" />
              <a
                v-if="telegramHref(row.shop.telegram)"
                :href="telegramHref(row.shop.telegram) ?? undefined"
                target="_blank"
                rel="noreferrer"
              >
                {{ displayText(row.shop.telegram) }}
              </a>
              <template v-else>{{ displayText(row.shop.telegram) }}</template>
            </span>
            <span>
              <NIcon :component="Clock3" :size="15" />
              {{ formatDateTime(row.shop.updatedAt ?? null, { includeSecond: false }) }}
            </span>
            <span>
              <NIcon :component="ShoppingBag" :size="15" />
              {{ t(`展示 ${formatInteger(row.matchedProductCount)} 个商品`, `${formatInteger(row.matchedProductCount)} products shown`) }}
            </span>
          </div>

          <p v-if="textValue(row.shop.notes)" class="shop-notes">{{ textValue(row.shop.notes) }}</p>

          <div v-if="row.visibleProducts.length > 0" class="product-grid">
            <div
              v-for="(product, index) in row.visibleProducts"
              :key="`${row.shopKey}-${textValue(product.itemUrl) || textValue(product.name) || index}`"
              class="product-item"
            >
              <a
                v-if="textValue(product.itemUrl)"
                class="product-name"
                :href="textValue(product.itemUrl)"
                target="_blank"
                rel="noreferrer"
              >
                {{ displayText(product.name) }}
              </a>
              <strong v-else class="product-name">{{ displayText(product.name) }}</strong>
              <div class="product-meta">
                <span>{{ formatShopPrice(product.price) }}</span>
                <span>{{ t(`库存 ${formatCount(product.stockCount)}`, `Stock ${formatCount(product.stockCount)}`) }}</span>
                <span>{{ displayText(product.group) }}</span>
                <span>{{ displayText(product.category) }}</span>
              </div>
            </div>
          </div>
          <div v-else class="product-empty">
            {{ t('暂无匹配商品明细', 'No matching product details') }}
          </div>
        </article>
      </section>

      <section v-else-if="!loadError && !isLoading" class="panel empty-panel">
        <div class="panel-inner empty-state">
          {{ t('当前筛选下暂无店铺', 'No shops match the current filters') }}
        </div>
      </section>
    </NSpin>
  </section>
</template>

<style scoped>
.card-shops-page {
  gap: 12px;
}

.card-shops-header :deep(.n-button) {
  flex: 0 0 auto;
}

.risk-alert {
  border-radius: var(--cpa-radius);
}

.risk-alert-content {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  color: var(--cpa-warning);
  font-size: 13px;
  line-height: 1.55;
  text-wrap: pretty;
}

.risk-alert-content :deep(.n-icon) {
  flex: 0 0 auto;
  margin-top: 1px;
}

.card-shop-toolbar {
  display: grid;
  gap: 12px;
  padding-block: 14px;
}

.search-row {
  display: grid;
  grid-template-columns: minmax(240px, 1fr) minmax(180px, 280px) minmax(150px, 190px);
  gap: 12px;
  align-items: end;
  min-width: 0;
}

.sort-control,
.favorite-filter {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.sort-control span,
.favorite-filter > span,
.tag-row > span,
.selected-term-row > span {
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 700;
}

.favorite-switch-line {
  display: flex;
  align-items: center;
  min-height: 34px;
  min-width: 0;
  gap: 8px;
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}

.favorite-switch-line span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tag-row,
.selected-term-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.tag-row :deep(.n-button) {
  min-width: 58px;
  border-radius: var(--cpa-radius-sm);
}

.selected-term-row {
  padding-top: 2px;
}

.selected-term-row :deep(.n-tag) {
  max-width: min(260px, 100%);
  border-radius: var(--cpa-radius-sm);
}

.selected-term-row :deep(.n-tag__content) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.favorite-error {
  border-radius: var(--cpa-radius);
}

.card-shop-metrics {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.card-shop-metric {
  min-height: 96px;
  padding: 14px;
}

.card-shop-metric .metric-value {
  font-size: 20px;
}

.card-shop-metric .metric-footnote {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.error-state {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.error-state strong {
  color: var(--cpa-danger);
  font-size: 13px;
  font-weight: 700;
}

.shop-list {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.shop-card {
  display: grid;
  gap: 10px;
  padding: 14px;
}

.shop-card.is-favorite {
  --favorite-ink: var(--cpa-text-strong);
  --favorite-text: var(--cpa-text-muted);
  --favorite-muted: var(--cpa-text-muted);
  --favorite-accent: var(--cpa-text-muted);
  --favorite-border: color-mix(in srgb, #d97706 28%, var(--cpa-border));
  --favorite-bg: color-mix(in srgb, #fffbeb 68%, var(--cpa-surface));
  --favorite-panel: color-mix(in srgb, #fffbeb 78%, var(--cpa-surface-muted));
  --favorite-product-bg: color-mix(in srgb, #fef3c7 46%, var(--cpa-surface-raised));
  border-color: var(--favorite-border);
  background: var(--favorite-bg);
}

.shop-card-head {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  min-width: 0;
}

.shop-title-block {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.shop-title-line {
  display: flex;
  gap: 7px;
  align-items: center;
  min-width: 0;
}

.shop-title-line h2 {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 17px;
  font-weight: 760;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.external-link {
  display: inline-grid;
  flex: 0 0 auto;
  width: 24px;
  height: 24px;
  place-items: center;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  color: var(--cpa-primary);
  text-decoration: none;
}

.external-link:hover {
  border-color: color-mix(in srgb, var(--cpa-primary) 28%, var(--cpa-border));
  background: var(--cpa-primary-wash);
}

.shop-card.is-favorite .external-link {
  border-color: color-mix(in srgb, #d97706 24%, var(--cpa-border));
  color: var(--favorite-muted);
  background: color-mix(in srgb, #fffbeb 62%, transparent);
}

.shop-card.is-favorite .external-link:hover {
  border-color: color-mix(in srgb, #d97706 38%, var(--cpa-border));
  background: color-mix(in srgb, #fef3c7 56%, transparent);
}

.shop-url {
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-primary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-decoration: none;
}

.shop-url:hover,
.shop-meta-row a:hover,
.product-name:hover {
  text-decoration: underline;
}

.shop-actions {
  display: grid;
  justify-items: end;
  gap: 8px;
}

.shop-actions :deep(.n-button) {
  border-radius: var(--cpa-radius-sm);
}

.shop-tags {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
}

.shop-meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
  min-width: 0;
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.shop-meta-row span {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  gap: 5px;
}

.shop-meta-row a {
  color: var(--cpa-primary);
  text-decoration: none;
}

.shop-card.is-favorite .shop-title-line h2 {
  color: var(--favorite-ink);
}

.shop-card.is-favorite .shop-meta-row {
  color: var(--favorite-muted);
}

.shop-card.is-favorite .shop-url,
.shop-card.is-favorite .shop-meta-row a {
  color: var(--favorite-accent);
}

.shop-notes {
  margin: 0;
  padding: 8px 10px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  color: var(--cpa-text-muted);
  background: var(--cpa-surface-muted);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.shop-card.is-favorite .shop-notes {
  border-color: color-mix(in srgb, #d97706 18%, var(--cpa-border));
  color: var(--favorite-muted);
  background: var(--favorite-panel);
}

.product-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 8px;
  min-width: 0;
}

.product-empty {
  min-height: 52px;
  padding: 14px 12px;
  border: 1px dashed var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  color: var(--cpa-text-muted);
  background: var(--cpa-surface-muted);
  font-size: 13px;
  text-align: center;
}

.product-item {
  display: grid;
  gap: 6px;
  min-width: 0;
  min-height: 72px;
  padding: 9px 10px;
  border: 1px solid color-mix(in srgb, #15803d 24%, var(--cpa-border));
  border-radius: var(--cpa-radius-sm);
  background: color-mix(in srgb, #22c55e 9%, var(--cpa-surface-raised));
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.24);
}

.shop-card.is-favorite .product-item {
  border-color: color-mix(in srgb, #d97706 24%, var(--cpa-border));
  background: var(--favorite-product-bg);
}

.product-name {
  display: -webkit-box;
  min-width: 0;
  overflow: hidden;
  color: var(--cpa-text-strong);
  font-size: 13px;
  font-weight: 720;
  line-height: 1.38;
  overflow-wrap: anywhere;
  text-decoration: none;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.shop-card.is-favorite .product-name {
  color: var(--favorite-ink);
}

.product-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  min-width: 0;
}

.product-meta span {
  display: inline-flex;
  max-width: 100%;
  min-width: 0;
  align-items: center;
  height: 22px;
  padding: 0 7px;
  overflow: hidden;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius-sm);
  color: var(--cpa-text-muted);
  background: var(--cpa-surface-muted);
  font-size: 11px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.product-meta span:first-child {
  color: var(--cpa-primary);
  background: var(--cpa-primary-wash);
  border-color: color-mix(in srgb, var(--cpa-primary) 20%, var(--cpa-border));
  font-variant-numeric: tabular-nums;
}

.shop-card.is-favorite .product-meta span {
  border-color: color-mix(in srgb, #d97706 16%, var(--cpa-border));
  color: var(--favorite-muted);
  background: color-mix(in srgb, #fffbeb 74%, var(--cpa-surface-muted));
}

.shop-card.is-favorite .product-meta span:first-child {
  border-color: color-mix(in srgb, #d97706 28%, var(--cpa-border));
  color: var(--favorite-accent);
  background: color-mix(in srgb, #fef3c7 62%, var(--cpa-surface-muted));
}

.empty-state {
  display: grid;
  min-height: 120px;
  place-items: center;
  color: var(--cpa-text-muted);
  font-size: 13px;
}

@media (max-width: 1180px) {
  .card-shop-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .search-row,
  .shop-card-head {
    grid-template-columns: 1fr;
  }

  .card-shop-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .shop-tags {
    justify-content: flex-start;
  }

  .shop-actions {
    justify-items: start;
  }

  .product-grid {
    grid-template-columns: 1fr;
  }

  .error-state {
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-width: 460px) {
  .card-shop-metrics {
    grid-template-columns: 1fr;
  }
}
</style>
