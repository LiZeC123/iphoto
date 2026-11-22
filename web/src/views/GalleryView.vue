<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { albumsApi } from '@/api/albums'
import { assetsApi } from '@/api/assets'
import { errorMessage } from '@/api/settings'
import { tagsApi } from '@/api/tags'
import type { Album, Asset, Tag } from '@/api/types'
import AssetGrid from '@/components/AssetGrid.vue'
import PageShell from '@/components/PageShell.vue'
import PhotoViewer from '@/components/PhotoViewer.vue'
import TagSelector from '@/components/TagSelector.vue'

const PAGE_SIZE = 200

const route = useRoute()
const router = useRouter()

const q = ref('')
const sort = ref('taken_at_desc')
const fromMonth = ref('')
const toMonth = ref('')
const items = ref<Asset[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const error = ref('')
const notice = ref('')

const viewerOpen = ref(false)
const viewerIndex = ref(0)

const selectable = ref(false)
const selected = ref(new Set<string>())
const albums = ref<Album[]>([])
const targetAlbum = ref('')
const batchBusy = ref(false)
const batchTags = ref<string[]>([])
const selectedTags = ref<Tag[]>([])
const anchorIndex = ref<number | null>(null)

let summaryTimer: ReturnType<typeof setTimeout> | undefined

async function refreshSelectedTags(): Promise<void> {
  if (!selectable.value || !selectedCount.value) {
    selectedTags.value = []
    return
  }
  try {
    selectedTags.value = (await tagsApi.summary([...selected.value])).items
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function scheduleTagsSummary(): void {
  if (summaryTimer) clearTimeout(summaryTimer)
  summaryTimer = setTimeout(() => void refreshSelectedTags(), 120)
}

watch(selected, scheduleTagsSummary)
onBeforeUnmount(() => {
  if (summaryTimer) clearTimeout(summaryTimer)
})

const activeTag = computed(() => (typeof route.query.tag === 'string' ? route.query.tag : ''))
const loadedAll = computed(() => items.value.length >= total.value)
const selectedCount = computed(() => selected.value.size)
const subtitle = computed(() =>
  activeTag.value ? `标签「${activeTag.value}」共 ${total.value} 张` : `共 ${total.value} 张图片`,
)

async function load(reset: boolean): Promise<void> {
  if (loading.value) return
  if (fromMonth.value && toMonth.value && fromMonth.value > toMonth.value) {
    error.value = '起始年月不能晚于结束年月'
    return
  }
  loading.value = true
  error.value = ''
  try {
    const nextPage = reset ? 1 : page.value + 1
    const res = await assetsApi.list({
      q: q.value.trim(),
      tag: activeTag.value,
      from: fromMonth.value,
      to: toMonth.value,
      sort: sort.value,
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

function search(): void {
  void load(true)
}

function clearFilters(): void {
  q.value = ''
  fromMonth.value = ''
  toMonth.value = ''
  if (activeTag.value) void router.push({ path: '/' })
  else void load(true)
}

function openViewer(index: number): void {
  viewerIndex.value = index
  viewerOpen.value = true
}

function onUpdated(asset: Asset): void {
  const index = items.value.findIndex((item) => item.hash === asset.hash)
  if (index >= 0) items.value[index] = asset
}

function onDeleted(hash: string): void {
  items.value = items.value.filter((item) => item.hash !== hash)
  total.value = Math.max(0, total.value - 1)
}

// ---- 批量选择 ----

async function enterSelection(): Promise<void> {
  selectable.value = true
  selected.value = new Set()
  anchorIndex.value = null
  batchTags.value = []
  notice.value = ''
  try {
    albums.value = (await albumsApi.list('', 'manual')).items
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function exitSelection(): void {
  selectable.value = false
  selected.value = new Set()
  anchorIndex.value = null
}

// Shift 连选：以最近一次点击为锚点选中区间；普通点击切换单张。
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

function selectAll(): void {
  selected.value = new Set(items.value.map((item) => item.hash))
  anchorIndex.value = null
}

function clearSelection(): void {
  selected.value = new Set()
}

async function batchAddTags(): Promise<void> {
  if (!selectedCount.value || !batchTags.value.length) return
  const names = [...batchTags.value]
  batchBusy.value = true
  try {
    await tagsApi.batch([...selected.value], names)
    notice.value = `已为 ${selectedCount.value} 张图片添加标签：${names.join('、')}`
    batchTags.value = []
    await refreshSelectedTags()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    batchBusy.value = false
  }
}

async function removeTagFromSelection(tag: Tag): Promise<void> {
  if (!selectedCount.value) return
  batchBusy.value = true
  error.value = ''
  try {
    await tagsApi.batch([...selected.value], [], [tag.name])
    notice.value = `已从 ${selectedCount.value} 张图片移除标签：${tag.name}`
    selectedTags.value = selectedTags.value.filter((item) => item.id !== tag.id)
    if (activeTag.value === tag.name) {
      exitSelection()
      await load(true)
    }
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    batchBusy.value = false
  }
}

async function batchAddToAlbum(): Promise<void> {
  if (!selectedCount.value || !targetAlbum.value) return
  batchBusy.value = true
  try {
    const res = await albumsApi.addItems(Number(targetAlbum.value), [...selected.value])
    notice.value = `已加入相册 ${res.added} 张`
    await load(true)
    exitSelection()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    batchBusy.value = false
  }
}

async function batchExport(): Promise<void> {
  if (!selectedCount.value) return
  const now = new Date()
  const stamp = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}-${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}`
  const name = window.prompt('导出子目录名称（创建在 runtime/exports/ 下）', `导出-${stamp}`)
  if (name === null || !name.trim()) return
  batchBusy.value = true
  try {
    const res = await assetsApi.batchExport([...selected.value], name.trim())
    notice.value = `已导出 ${res.exported} 张到 ${res.target_dir}${
      res.failures.length ? `（${res.failures.length} 张失败）` : ''
    }`
    exitSelection()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    batchBusy.value = false
  }
}

async function batchDelete(): Promise<void> {
  if (!selectedCount.value) return
  batchBusy.value = true
  try {
    await assetsApi.batchDelete([...selected.value])
    notice.value = `已移入回收站 ${selectedCount.value} 张`
    await load(true)
    exitSelection()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    batchBusy.value = false
  }
}

watch(activeTag, () => {
  void load(true)
})

onMounted(() => {
  void load(true)
})
</script>

<template>
  <PageShell title="图库" :subtitle="subtitle">
    <div class="toolbar filter-bar">
      <input
        v-model="q"
        class="input grow"
        type="search"
        placeholder="搜索文件名或描述，回车确认"
        @keyup.enter="search"
      />
      <label class="inline-field">
        <span>从</span>
        <input v-model="fromMonth" class="input" type="month" @change="search" />
      </label>
      <label class="inline-field">
        <span>到</span>
        <input v-model="toMonth" class="input" type="month" @change="search" />
      </label>
      <select v-model="sort" class="select" @change="search">
        <option value="taken_at_desc">拍摄时间（新→旧）</option>
        <option value="taken_at_asc">拍摄时间（旧→新）</option>
        <option value="added_at_desc">导入时间（新→旧）</option>
        <option value="name_asc">文件名</option>
      </select>
      <button class="btn" :disabled="loading" @click="clearFilters">清除</button>
      <button v-if="!selectable" class="btn btn-primary" @click="enterSelection">选择</button>
      <template v-else>
        <button class="btn" :disabled="loading" @click="selectAll">全选</button>
        <button class="btn" @click="exitSelection">退出选择</button>
      </template>
    </div>

    <div v-if="activeTag" class="active-filter">
      当前标签筛选：<strong>{{ activeTag }}</strong>
      <button class="btn btn-sm" @click="router.push({ path: '/' })">清除</button>
    </div>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <AssetGrid
      :items="items"
      :selectable="selectable"
      :selected="selected"
      @open="openViewer"
      @pick="onPick"
    >
      <template #empty>
        <div class="empty">
          <strong>没有符合条件的图片</strong>
          <p>换个筛选条件，或先到「导入」页导入照片。</p>
          <RouterLink to="/import" class="btn btn-primary">去导入</RouterLink>
        </div>
      </template>
    </AssetGrid>

    <div class="load-more">
      <button v-if="items.length && !loadedAll" class="btn" :disabled="loading" @click="load(false)">
        {{ loading ? '加载中…' : `加载更多（已显示 ${items.length}/${total}）` }}
      </button>
      <span v-else-if="items.length" class="dim">已加载全部 {{ total }} 张</span>
    </div>

    <div v-if="selectable" class="batch-bar">
      <div class="batch-row">
        <span class="batch-count">已选 {{ selectedCount }} 张</span>
        <div class="selected-tags">
          <span v-if="!selectedTags.length" class="dim">选中图片暂无标签</span>
          <button
            v-for="tag in selectedTags"
            :key="tag.id"
            type="button"
            class="selected-tag"
            :disabled="batchBusy || !selectedCount"
            :title="`从选中的图片中移除标签「${tag.name}」`"
            @click="removeTagFromSelection(tag)"
          >
            {{ tag.name }}
            <span class="selected-tag-count">{{ tag.count }}</span>
            <span class="selected-tag-x">×</span>
          </button>
        </div>
      </div>
      <div class="batch-row">
        <TagSelector
          v-model="batchTags"
          placement="top"
          placeholder="选择或输入要添加的标签"
          class="batch-tags"
        />
        <button
          class="btn btn-sm btn-primary"
          :disabled="batchBusy || !selectedCount || !batchTags.length"
          @click="batchAddTags"
        >
          添加标签
        </button>
        <select v-model="targetAlbum" class="select" :disabled="!albums.length">
          <option value="">{{ albums.length ? '选择相册…' : '暂无普通相册' }}</option>
          <option v-for="album in albums" :key="album.id" :value="String(album.id)">
            {{ album.name }}
          </option>
        </select>
        <button
          class="btn btn-sm"
          :disabled="batchBusy || !selectedCount || !targetAlbum"
          @click="batchAddToAlbum"
        >
          加入相册
        </button>
        <button class="btn btn-sm" :disabled="batchBusy || !selectedCount" @click="batchExport">
          导出
        </button>
        <button class="btn btn-sm btn-danger" :disabled="batchBusy || !selectedCount" @click="batchDelete">
          移入回收站
        </button>
        <button class="btn btn-sm" :disabled="!selectedCount" @click="clearSelection">清空选择</button>
      </div>
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
