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
const selected = ref('')
const items = ref<Asset[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const error = ref('')

const viewerOpen = ref(false)
const viewerIndex = ref(0)

const loadedAll = computed(() => items.value.length >= total.value)
const subtitle = computed(() =>
  selected.value ? `${selected.value} 共 ${total.value} 张` : `共 ${total.value} 张图片`,
)

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
      from: selected.value,
      to: selected.value,
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

function selectMonth(month: string): void {
  if (selected.value === month) return
  selected.value = month
  void load(true)
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
    <div class="month-chips">
      <button
        type="button"
        class="chip"
        :class="{ active: selected === '' }"
        @click="selectMonth('')"
      >
        全部
      </button>
      <button
        v-for="month in months"
        :key="month.month"
        type="button"
        class="chip"
        :class="{ active: selected === month.month }"
        @click="selectMonth(month.month)"
      >
        {{ month.month }}
        <span class="chip-count">{{ month.count }}</span>
      </button>
    </div>

    <p v-if="error" class="banner error">{{ error }}</p>

    <AssetGrid :items="items" @open="openViewer">
      <template #empty>
        <div class="empty">
          <strong>没有可展示的图片</strong>
          <p v-if="months.length">换一个年月试试，或先导入图片。</p>
          <p v-else>照片导入后，会按拍摄年月自动聚合到这里。</p>
        </div>
      </template>
    </AssetGrid>

    <div class="load-more">
      <button v-if="items.length && !loadedAll" class="btn" :disabled="loading" @click="load(false)">
        {{ loading ? '加载中…' : `加载更多（已显示 ${items.length}/${total}）` }}
      </button>
      <span v-else-if="items.length" class="dim">已加载全部 {{ total }} 张</span>
    </div>

    <PhotoViewer
      :assets="items"
      :start-index="viewerIndex"
      :open="viewerOpen"
      @close="viewerOpen = false"
      @updated="onUpdated"
      @deleted="onDeleted"
    />
  </PageShell>
</template>
