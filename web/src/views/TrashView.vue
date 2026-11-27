<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { errorMessage } from '@/api/settings'
import { trashApi } from '@/api/trash'
import type { Asset } from '@/api/types'
import AssetGrid from '@/components/AssetGrid.vue'
import PageShell from '@/components/PageShell.vue'

const items = ref<Asset[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 100
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const selected = ref(new Set<string>())
// Shift 连选的锚点（当前已加载列表里的下标）。
const anchor = ref<number | null>(null)

const selectedCount = computed(() => selected.value.size)
const loadedAll = computed(() => items.value.length >= total.value)

async function load(reset = true): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const nextPage = reset ? 1 : page.value + 1
    const data = await trashApi.list(nextPage, pageSize)
    items.value = reset ? data.items : [...items.value, ...data.items]
    total.value = data.total
    page.value = nextPage
    if (reset) {
      selected.value = new Set()
      anchor.value = null
    }
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

function loadMore(): void {
  if (loading.value || loadedAll.value) return
  void load(false)
}

/** 点击卡片切换选中；按住 Shift 连选一段（与图库页一致）。 */
function onPick(payload: { hash: string; index: number; shift: boolean }): void {
  const next = new Set(selected.value)
  if (payload.shift && anchor.value !== null) {
    const start = Math.min(anchor.value, payload.index)
    const end = Math.max(anchor.value, payload.index)
    for (let i = start; i <= end; i += 1) {
      const item = items.value[i]
      if (item) next.add(item.hash)
    }
  } else {
    if (next.has(payload.hash)) next.delete(payload.hash)
    else next.add(payload.hash)
    anchor.value = payload.index
  }
  selected.value = next
}

function selectAll(): void {
  selected.value = new Set(items.value.map((item) => item.hash))
  anchor.value = null
}

function clearSelection(): void {
  selected.value = new Set()
  anchor.value = null
}

async function restore(hashes: string[]): Promise<void> {
  if (!hashes.length) return
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    const res = await trashApi.restore(hashes)
    const failures = res.results.filter((r) => !r.ok)
    notice.value = `已恢复 ${res.restored} 张${failures.length ? `，${failures.length} 张失败：${failures[0].error ?? ''}` : ''}`
    await load(true)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    busy.value = false
  }
}

async function emptyTrash(): Promise<void> {
  if (!total.value) return
  if (!window.confirm(`清空回收站将永久删除 ${total.value} 个文件（无法恢复），确认继续？`)) return
  busy.value = true
  error.value = ''
  try {
    const res = await trashApi.empty()
    notice.value = `已删除 ${res.deleted_records} 条记录、${res.deleted_files} 个文件`
    await load(true)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <PageShell title="回收站" :subtitle="`共 ${total} 个已删除文件`">
    <div class="toolbar filter-bar">
      <button class="btn" :disabled="busy || !selectedCount" @click="restore([...selected])">
        {{ busy ? '处理中…' : `恢复所选（${selectedCount}）` }}
      </button>
      <button class="btn" :disabled="busy || !items.length" @click="selectAll">全选</button>
      <button class="btn" :disabled="!selectedCount" @click="clearSelection">清空选择</button>
      <button class="btn btn-danger" :disabled="busy || !total" @click="emptyTrash">清空回收站</button>
      <button class="btn" :disabled="loading" @click="load(true)">刷新</button>
      <span class="dim selection-tip">点击缩略图选择，按住 Shift 连选</span>
    </div>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <AssetGrid
      :items="items"
      selectable
      :selected="selected"
      meta="deleted"
      @pick="onPick"
      @load-more="loadMore"
    >
      <template #empty>
        <div v-if="!loading" class="empty">
          <strong>回收站是空的</strong>
          <p>删除的图片会先放在这里，可随时恢复。</p>
        </div>
      </template>

<style scoped>
.selection-tip {
  font-size: 13px;
}
</style>
    </AssetGrid>

    <div class="load-more">
      <span v-if="loading" class="dim">加载中…</span>
      <span v-else-if="items.length && !loadedAll" class="dim">
        继续向下滚动自动加载（已显示 {{ items.length }}/{{ total }}）
      </span>
      <span v-else-if="items.length" class="dim">已加载全部 {{ total }} 张</span>
    </div>
  </PageShell>
</template>

<style scoped>
.selection-tip {
  font-size: 13px;
}
</style>
