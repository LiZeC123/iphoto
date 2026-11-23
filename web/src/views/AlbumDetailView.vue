<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { albumsApi } from '@/api/albums'
import { assetsApi } from '@/api/assets'
import { errorMessage } from '@/api/settings'
import type { Album, Asset, MonthCount } from '@/api/types'
import AssetGrid from '@/components/AssetGrid.vue'
import PageShell from '@/components/PageShell.vue'
import PhotoViewer from '@/components/PhotoViewer.vue'
import { ruleSummary } from '@/utils/smartRule'

const route = useRoute()
const router = useRouter()

const albumId = computed(() => Number(route.params.id))
const album = ref<Album | null>(null)
const months = ref<MonthCount[]>([])
const month = ref('')
const anchorIndex = ref<number | null>(null)
const items = ref<Asset[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const error = ref('')
const notice = ref('')

const selectable = ref(false)
const selected = ref(new Set<string>())
const viewerOpen = ref(false)
const viewerIndex = ref(0)

const pickerOpen = ref(false)
const pickerLoading = ref(false)
const pickerItems = ref<Asset[]>([])
const pickerSelected = ref(new Set<string>())
const pickerAnchor = ref<number | null>(null)

const isSmart = computed(() => album.value?.type === 'smart')
const subtitle = computed(() =>
  album.value
    ? `${isSmart.value ? '智能相册' : '普通相册'} · ${total.value} 个文件`
    : '加载中…',
)
const loadedAll = computed(() => items.value.length >= total.value)
const selectedCount = computed(() => selected.value.size)

async function load(reset = true): Promise<void> {
  if (!albumId.value) return
  loading.value = true
  error.value = ''
  try {
    album.value = await albumsApi.get(albumId.value)
    const nextPage = reset ? 1 : page.value + 1
    const data = await albumsApi.items(
      albumId.value,
      nextPage,
      200,
      month.value,
      month.value,
    )
    items.value = reset ? data.items : [...items.value, ...data.items]
    total.value = data.total
    page.value = nextPage
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function loadMonths(): Promise<void> {
  if (!albumId.value) return
  try {
    months.value = (await albumsApi.months(albumId.value)).months
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function loadMore(): void {
  if (loading.value || loadedAll.value) return
  void load(false)
}

function selectMonth(value: string): void {
  if (month.value === value) return
  month.value = value
  anchorIndex.value = null
  void load(true)
}

function openViewer(index: number): void {
  viewerIndex.value = index
  viewerOpen.value = true
}

// Shift 连选：锚点与当前点击之间的图片全部加入选择。
function onPick(payload: { hash: string; index: number; shift: boolean }): void {
  const next = new Set(selected.value)
  if (payload.shift && anchorIndex.value !== null) {
    const start = Math.min(anchorIndex.value, payload.index)
    const end = Math.max(anchorIndex.value, payload.index)
    for (let i = start; i <= end; i += 1) {
      const item = items.value[i]
      if (item) next.add(item.hash)
    }
  } else {
    if (next.has(payload.hash)) next.delete(payload.hash)
    else next.add(payload.hash)
    anchorIndex.value = payload.index
  }
  selected.value = next
}

function exitSelection(): void {
  selectable.value = false
  selected.value = new Set()
  anchorIndex.value = null
}

async function removeSelected(): Promise<void> {
  if (!selectedCount.value) return
  if (!window.confirm(`从相册移除选中的 ${selectedCount.value} 个文件？`)) return
  try {
    const res = await albumsApi.removeItems(albumId.value, [...selected.value])
    notice.value = `已移除 ${res.removed} 张`
    exitSelection()
    await Promise.all([load(), loadMonths()])
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function openPicker(): Promise<void> {
  pickerOpen.value = true
  pickerSelected.value = new Set()
  pickerAnchor.value = null
  pickerLoading.value = true
  try {
    pickerItems.value = (await assetsApi.list({ sort: 'taken_at_desc', page_size: 500 })).items
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    pickerLoading.value = false
  }
}

function onViewerDeleted(): void {
  void Promise.all([load(), loadMonths()])
}

function onPickerPick(payload: { hash: string; index: number; shift: boolean }): void {
  const next = new Set(pickerSelected.value)
  if (payload.shift && pickerAnchor.value !== null) {
    const start = Math.min(pickerAnchor.value, payload.index)
    const end = Math.max(pickerAnchor.value, payload.index)
    for (let i = start; i <= end; i += 1) {
      const item = pickerItems.value[i]
      if (item) next.add(item.hash)
    }
  } else {
    if (next.has(payload.hash)) next.delete(payload.hash)
    else next.add(payload.hash)
    pickerAnchor.value = payload.index
  }
  pickerSelected.value = next
}

async function confirmAdd(): Promise<void> {
  if (!pickerSelected.value.size) return
  try {
    const res = await albumsApi.addItems(albumId.value, [...pickerSelected.value])
    notice.value = `已加入 ${res.added} 张`
    pickerOpen.value = false
    await Promise.all([load(), loadMonths()])
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function exportAlbum(): Promise<void> {
  if (!items.value.length || !album.value) return
  const name = window.prompt('导出子目录名称（创建在 runtime/exports/ 下）', album.value.name)
  if (name === null || !name.trim()) return
  try {
    const res = await assetsApi.batchExport(
      items.value.map((item) => item.hash),
      name.trim(),
    )
    notice.value = `已导出 ${res.exported} 张到 ${res.target_dir}${
      res.failures.length ? `（${res.failures.length} 张失败）` : ''
    }`
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function deleteAlbum(): Promise<void> {
  if (!album.value) return
  if (!window.confirm(`删除相册「${album.value.name}」？图片不会被删除。`)) return
  try {
    await albumsApi.remove(albumId.value)
    void router.push('/albums')
  } catch (err) {
    error.value = errorMessage(err)
  }
}

watch(albumId, () => {
  exitSelection()
  month.value = ''
  void Promise.all([load(), loadMonths()])
})

onMounted(() => {
  void Promise.all([load(), loadMonths()])
})
</script>

<template>
  <PageShell :title="album?.name ?? '相册'" :subtitle="subtitle">
    <template #actions>
      <RouterLink to="/albums" class="btn btn-sm">返回相册</RouterLink>
      <template v-if="!isSmart">
        <button class="btn btn-sm" @click="openPicker">添加图片</button>
        <button v-if="!selectable" class="btn btn-sm" @click="selectable = true">选择</button>
        <button v-else class="btn btn-sm" @click="exitSelection">退出选择</button>
      </template>
      <button class="btn btn-sm" :disabled="!items.length" @click="exportAlbum">导出相册</button>
      <button class="btn btn-sm btn-danger" @click="deleteAlbum">删除相册</button>
    </template>

    <p v-if="isSmart && album" class="banner rule-banner">
      规则：{{ ruleSummary(album.rule_json) }}（成员随图库变化，不能手动增删）
    </p>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <div v-if="months.length" class="month-chips">
      <button type="button" class="chip" :class="{ active: month === '' }" @click="selectMonth('')">
        全部
      </button>
      <button
        v-for="entry in months"
        :key="entry.month"
        type="button"
        class="chip"
        :class="{ active: month === entry.month }"
        @click="selectMonth(entry.month)"
      >
        {{ entry.month }}
        <span class="chip-count">{{ entry.count }}</span>
      </button>
    </div>

    <AssetGrid
      :items="items"
      :selectable="selectable && !isSmart"
      :selected="selected"
      @open="openViewer"
      @pick="onPick"
      @load-more="loadMore"
    >
      <template #empty>
        <div class="empty">
          <strong>相册还没有文件</strong>
          <p v-if="isSmart">调整规则，或等图库中出现符合条件的图片。</p>
          <p v-else>点击「添加图片」从图库挑选。</p>
        </div>
      </template>
    </AssetGrid>

    <div class="load-more">
      <span v-if="loading" class="dim">加载中…</span>
      <span v-else-if="items.length && !loadedAll" class="dim">
        继续向下滚动自动加载（已显示 {{ items.length }}/{{ total }}）
      </span>
    </div>

    <div v-if="selectable && !isSmart" class="batch-bar">
      <span>已选 {{ selectedCount }} 张</span>
      <button class="btn btn-sm btn-danger" :disabled="!selectedCount" @click="removeSelected">
        移出相册
      </button>
      <button class="btn btn-sm" :disabled="!selectedCount" @click="selected = new Set()">清空选择</button>
    </div>

    <PhotoViewer
      :assets="items"
      :start-index="viewerIndex"
      :open="viewerOpen"
      @close="viewerOpen = false"
      @updated="load()"
      @deleted="onViewerDeleted"
    />

    <Teleport to="body">
      <div v-if="pickerOpen" class="modal-backdrop" @click.self="pickerOpen = false">
        <div class="modal">
          <div class="panel-head">
            <h2>从图库添加（已选 {{ pickerSelected.size }} 个文件）</h2>
            <button class="btn btn-sm" @click="pickerOpen = false">关闭</button>
          </div>
          <p v-if="pickerLoading" class="dim">加载中…</p>
          <AssetGrid
            v-else
            :items="pickerItems"
            selectable
            :selected="pickerSelected"
            @pick="onPickerPick"
          >
            <template #empty>
              <div class="empty">图库还没有文件</div>
            </template>
          </AssetGrid>
          <div class="toolbar modal-actions">
            <button class="btn btn-primary" :disabled="!pickerSelected.size" @click="confirmAdd">
              加入相册
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </PageShell>
</template>

<style scoped>
.rule-banner {
  background: rgba(76, 141, 255, 0.1);
  border-color: rgba(76, 141, 255, 0.35);
}

.modal :deep(.asset-grid) {
  height: 52vh;
  min-height: 240px;
}

.modal-actions {
  margin-top: 10px;
  justify-content: flex-end;
}
</style>
