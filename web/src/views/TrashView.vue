<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { errorMessage } from '@/api/settings'
import { trashApi } from '@/api/trash'
import type { Asset } from '@/api/types'
import PageShell from '@/components/PageShell.vue'
import { formatBytes, formatDate } from '@/utils/format'

const items = ref<Asset[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 100
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const selected = ref(new Set<string>())
const sentinel = ref<HTMLElement | null>(null)

let observer: IntersectionObserver | undefined

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
    if (reset) selected.value = new Set()
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

function toggle(hash: string): void {
  const next = new Set(selected.value)
  if (next.has(hash)) next.delete(hash)
  else next.add(hash)
  selected.value = next
}

function selectAll(): void {
  selected.value = new Set(items.value.map((item) => item.hash))
}

function clearSelection(): void {
  selected.value = new Set()
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
  observer = new IntersectionObserver(
    (entries) => {
      if (entries.some((entry) => entry.isIntersecting)) loadMore()
    },
    { rootMargin: '240px' },
  )
  if (sentinel.value) observer.observe(sentinel.value)
})

onBeforeUnmount(() => {
  observer?.disconnect()
})
</script>

<template>
  <PageShell title="回收站" :subtitle="`共 ${total} 个已删除文件`">
    <div class="toolbar filter-bar">
      <button class="btn" :disabled="busy || !selectedCount" @click="restore([...selected])">
        {{ busy ? '处理中…' : `恢复所选（${selectedCount}）` }}
      </button>
      <button class="btn" :disabled="busy" @click="selectAll">全选</button>
      <button class="btn" :disabled="!selectedCount" @click="clearSelection">清空选择</button>
      <button class="btn btn-danger" :disabled="busy || !total" @click="emptyTrash">清空回收站</button>
      <button class="btn" :disabled="loading" @click="load(true)">刷新</button>
    </div>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <table v-if="items.length" class="data-table panel">
      <thead>
        <tr>
          <th class="check-col"></th>
          <th>文件名</th>
          <th>删除时间</th>
          <th>大小</th>
          <th class="actions-col">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="asset in items" :key="asset.hash">
          <td class="check-col">
            <input
              type="checkbox"
              :checked="selected.has(asset.hash)"
              @change="toggle(asset.hash)"
            />
          </td>
          <td>
            <span class="ellipsis" :title="asset.original_name">{{ asset.original_name }}</span>
          </td>
          <td class="dim">{{ formatDate(asset.deleted_at) }}</td>
          <td class="dim">{{ formatBytes(asset.size) }}</td>
          <td class="actions">
            <button class="btn btn-sm" :disabled="busy" @click="restore([asset.hash])">恢复</button>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-else-if="!loading" class="empty">
      <strong>回收站是空的</strong>
      <p>删除的图片会先放在这里，可随时恢复。</p>
    </div>

    <div ref="sentinel" class="load-more">
      <span v-if="loading" class="dim">加载中…</span>
      <span v-else-if="items.length && !loadedAll" class="dim">
        继续向下滚动自动加载（已显示 {{ items.length }}/{{ total }}）
      </span>
      <span v-else-if="items.length" class="dim">已加载全部 {{ total }} 张</span>
    </div>
  </PageShell>
</template>

<style scoped>
.check-col {
  width: 36px;
}

.actions-col {
  width: 100px;
}

.data-table.panel {
  padding: 6px 10px;
}
</style>
