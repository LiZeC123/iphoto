<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { assetsApi } from '@/api/assets'
import { errorMessage } from '@/api/settings'
import type { Asset, MonthCount } from '@/api/types'
import AssetGrid from '@/components/AssetGrid.vue'
import PageShell from '@/components/PageShell.vue'
import PhotoViewer from '@/components/PhotoViewer.vue'

const PAGE_SIZE = 300

const months = ref<MonthCount[]>([])
const selectedYear = ref('')
const selectedMonth = ref('')
const items = ref<Asset[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const error = ref('')

const viewerOpen = ref(false)
const viewerIndex = ref(0)

interface MonthNode {
  month: string
  label: string
  count: number
}

interface YearNode {
  year: string
  count: number
  months: MonthNode[]
}

// 第一行是年（m 个），第二行是该年的月份（n 个），选项数从 m*n 降到 m+n。
const years = computed<YearNode[]>(() => {
  const map = new Map<string, YearNode>()
  for (const entry of months.value) {
    const year = entry.month.slice(0, 4)
    let node = map.get(year)
    if (!node) {
      node = { year, count: 0, months: [] }
      map.set(year, node)
    }
    node.count += entry.count
    node.months.push({
      month: entry.month,
      label: `${Number(entry.month.slice(5, 7))} 月`,
      count: entry.count,
    })
  }
  const out = [...map.values()].sort((a, b) => b.year.localeCompare(a.year))
  for (const node of out) node.months.sort((a, b) => a.month.localeCompare(b.month))
  return out
})

const activeYear = computed(() => years.value.find((node) => node.year === selectedYear.value) ?? null)
const filter = computed(() => selectedMonth.value || selectedYear.value)
const loadedAll = computed(() => items.value.length >= total.value)
const subtitle = computed(() => {
  if (selectedMonth.value) return `${selectedMonth.value} 共 ${total.value} 个文件`
  if (selectedYear.value) return `${selectedYear.value} 年 共 ${total.value} 个文件`
  return `共 ${total.value} 个文件`
})

async function loadMonths(): Promise<void> {
  try {
    months.value = (await assetsApi.timeline()).months
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function load(reset: boolean): Promise<void> {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const nextPage = reset ? 1 : page.value + 1
    const res = await assetsApi.list({
      from: filter.value,
      to: filter.value,
      sort: 'taken_at_desc',
      page: nextPage,
      page_size: PAGE_SIZE,
    })
    items.value = reset ? res.items : [...items.value, ...res.items]
    total.value = res.total
    page.value = nextPage
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

function selectYear(year: string): void {
  if (selectedYear.value === year && !selectedMonth.value) return
  selectedYear.value = year
  selectedMonth.value = ''
  void load(true)
}

function selectMonth(month: string): void {
  if (selectedMonth.value === month) return
  selectedMonth.value = month
  void load(true)
}

function resetFilter(): void {
  if (!selectedYear.value && !selectedMonth.value) return
  selectedYear.value = ''
  selectedMonth.value = ''
  void load(true)
}

function loadMore(): void {
  if (loading.value || loadedAll.value) return
  void load(false)
}

function openViewer(index: number): void {
  viewerIndex.value = index
  viewerOpen.value = true
}

function onUpdated(asset: Asset): void {
  const index = items.value.findIndex((item) => item.hash === asset.hash)
  if (index >= 0) items.value[index] = asset
  void loadMonths()
}

function onDeleted(hash: string): void {
  items.value = items.value.filter((item) => item.hash !== hash)
  total.value = Math.max(0, total.value - 1)
  void loadMonths()
}

onMounted(async () => {
  await Promise.all([loadMonths(), load(true)])
})
</script>

<template>
  <PageShell title="时间线" :subtitle="subtitle">
    <div class="timeline-page">
      <div class="month-chips year-row">
        <button type="button" class="chip" :class="{ active: !selectedYear }" @click="resetFilter">
          全部
        </button>
        <button
          v-for="node in years"
          :key="node.year"
          type="button"
          class="chip"
          :class="{ active: selectedYear === node.year }"
          @click="selectYear(node.year)"
        >
          {{ node.year }} 年
          <span class="chip-count">{{ node.count }}</span>
        </button>
      </div>

      <div v-if="activeYear" class="month-chips month-row">
        <button
          type="button"
          class="chip"
          :class="{ active: !selectedMonth }"
          @click="selectMonth('')"
        >
          {{ activeYear.year }} 全年
          <span class="chip-count">{{ activeYear.count }}</span>
        </button>
        <button
          v-for="node in activeYear.months"
          :key="node.month"
          type="button"
          class="chip"
          :class="{ active: selectedMonth === node.month }"
          @click="selectMonth(node.month)"
        >
          {{ node.label }}
          <span class="chip-count">{{ node.count }}</span>
        </button>
      </div>
      <p v-else-if="years.length" class="hint dim month-hint">
        先选年份，第二行会列出该年份的月份（选项数从「年 × 月」降为「年 + 月」）。
      </p>

      <p v-if="error" class="banner error">{{ error }}</p>

      <AssetGrid :items="items" @open="openViewer" @load-more="loadMore">
        <template #empty>
          <div class="empty">
            <strong>没有可展示的文件</strong>
            <p v-if="months.length">换一个年月试试，或先导入图片。</p>
            <p v-else>照片导入后，会按拍摄年月自动聚合到这里。</p>
          </div>
        </template>
      </AssetGrid>

      <div class="load-more">
        <span v-if="loading" class="dim">加载中…</span>
        <span v-else-if="items.length && !loadedAll" class="dim">
          继续向下滚动自动加载（已显示 {{ items.length }}/{{ total }}）
        </span>
        <span v-else-if="items.length" class="dim">已加载全部 {{ total }} 个文件</span>
      </div>

      <PhotoViewer
        :assets="items"
        :start-index="viewerIndex"
        :open="viewerOpen"
        @close="viewerOpen = false"
        @updated="onUpdated"
        @deleted="onDeleted"
      />
    </div>
  </PageShell>
</template>

<style scoped>
.year-row {
  margin-bottom: 8px;
}

.month-row {
  padding-top: 8px;
  border-top: 1px dashed var(--border);
}

.month-hint {
  margin: 0 0 14px;
}

/* 两行年月导航占用了更多纵向空间，同步收缩网格高度。 */
.timeline-page :deep(.asset-grid) {
  height: calc(100dvh - 320px);
}
</style>
