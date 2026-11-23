<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { assetsApi } from '@/api/assets'
import { ApiError } from '@/api/client'
import { errorMessage, settingsApi } from '@/api/settings'
import {
  similarApi,
  type SimilarGroup,
  type SimilarProgress,
  type SimilarResult,
} from '@/api/similar'
import type { Asset } from '@/api/types'
import AssetThumb from '@/components/AssetThumb.vue'
import PageShell from '@/components/PageShell.vue'
import PhotoViewer from '@/components/PhotoViewer.vue'
import { formatDate, formatPercent } from '@/utils/format'

const ALGORITHM = 'dhash'
const MAX_THRESHOLD = 64
const MAX_GROUPS = 100

const from = ref('')
const algorithm = ref(ALGORITHM)
const threshold = ref(8)
const starting = ref(false)
const progress = ref<SimilarProgress | null>(null)
const result = ref<SimilarResult | null>(null)
const error = ref('')
const notice = ref('')
const selected = ref(new Set<string>())
const batchBusy = ref(false)

const viewerOpen = ref(false)
const viewerIndex = ref(0)
const viewerAssets = ref<Asset[]>([])

let timer: ReturnType<typeof setTimeout> | undefined

const running = computed(() => progress.value?.status === 'running')
const percent = computed(() =>
  formatPercent(progress.value?.processed ?? 0, progress.value?.total ?? 0),
)
const selectedCount = computed(() => selected.value.size)
const groups = computed(() => result.value?.groups ?? [])
const statusText = computed(() => {
  const status = progress.value?.status ?? ''
  const labels: Record<string, string> = {
    idle: '未开始',
    running: '检测中',
    done: '已完成',
    cancelled: '已取消',
    failed: '失败',
  }
  return labels[status] ?? status
})

function clearTimer(): void {
  if (timer) {
    clearTimeout(timer)
    timer = undefined
  }
}

function schedulePoll(): void {
  clearTimer()
  timer = setTimeout(() => void poll(), 400)
}

async function poll(): Promise<void> {
  try {
    const snapshot = await similarApi.progress()
    progress.value = snapshot
    if (snapshot.status === 'running') {
      schedulePoll()
      return
    }
    clearTimer()
    if (snapshot.status === 'done' || snapshot.status === 'cancelled') {
      await loadResult()
      if (snapshot.status === 'cancelled') {
        notice.value = '检测已取消，以下是已发现的部分结果。'
      }
    } else if (snapshot.status === 'failed') {
      error.value = snapshot.error || '检测失败'
    }
  } catch (err) {
    clearTimer()
    error.value = errorMessage(err)
  }
}

async function loadResult(): Promise<void> {
  try {
    result.value = await similarApi.result()
    selected.value = new Set()
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      result.value = null
      return
    }
    error.value = errorMessage(err)
  }
}

async function startDetection(): Promise<void> {
  error.value = ''
  notice.value = ''
  const raw = threshold.value
  const value = Number(raw)
  if (raw === null || raw === undefined || String(raw).trim() === '') {
    error.value = `请输入阈值（0–${MAX_THRESHOLD}）`
    return
  }
  if (!Number.isInteger(value) || value < 0 || value > MAX_THRESHOLD) {
    error.value = `阈值需为 0–${MAX_THRESHOLD} 的整数`
    return
  }
  starting.value = true
  viewerOpen.value = false
  try {
    await similarApi.detect(from.value, algorithm.value, value)
    result.value = null
    selected.value = new Set()
    progress.value = {
      id: 0,
      status: 'running',
      total: 0,
      processed: 0,
      groups: 0,
      failures: 0,
      cached: 0,
      computed: 0,
      threshold: value,
      algorithm: algorithm.value,
      from: from.value,
    }
    schedulePoll()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    starting.value = false
  }
}

async function cancelDetection(): Promise<void> {
  try {
    await similarApi.cancel()
    notice.value = '已请求取消，正在停止…'
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function toggle(hash: string): void {
  const next = new Set(selected.value)
  if (next.has(hash)) next.delete(hash)
  else next.add(hash)
  selected.value = next
}

function clearSelection(): void {
  selected.value = new Set()
}

function groupSelectedCount(group: SimilarGroup): number {
  return group.images.filter((image) => selected.value.has(image.hash)).length
}

function groupDistanceRange(group: SimilarGroup): string {
  const values = group.images
    .map((image) => image.distance_from_prev)
    .filter((value): value is number => typeof value === 'number')
  if (!values.length) return ''
  const min = Math.min(...values)
  const max = Math.max(...values)
  return min === max ? `距离 ${min}` : `距离 ${min}–${max}`
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)} 秒`
}

// 从当前结果里移除指定哈希；剩余不足 2 张的组不再展示。
function removeHashes(hashes: Set<string>): void {
  if (!result.value || !hashes.size) return
  const nextSelection = new Set(selected.value)
  for (const hash of hashes) nextSelection.delete(hash)
  selected.value = nextSelection

  const nextGroups: SimilarGroup[] = []
  for (const group of result.value.groups) {
    const images = group.images.filter((image) => !hashes.has(image.hash))
    if (images.length >= 2) nextGroups.push({ ...group, images })
  }
  result.value = {
    ...result.value,
    groups: nextGroups,
    group_count: nextGroups.length,
  }
}

async function deleteSelected(group: SimilarGroup): Promise<void> {
  const hashes = group.images
    .filter((image) => selected.value.has(image.hash))
    .map((image) => image.hash)
  if (!hashes.length || batchBusy.value) return
  if (!window.confirm(`将选中的 ${hashes.length} 张图片移入回收站？可在回收站恢复。`)) return

  batchBusy.value = true
  error.value = ''
  notice.value = ''
  // 删除会改变组内容，先关闭查看器避免继续展示已删除的图片。
  viewerOpen.value = false
  try {
    const res = await assetsApi.batchDelete(hashes)
    const deleted = new Set(res.results.filter((item) => item.ok).map((item) => item.hash))
    const failed = res.results.filter((item) => !item.ok)
    removeHashes(deleted)
    notice.value = failed.length
      ? `已移入回收站 ${deleted.size} 张，${failed.length} 张失败：${failed[0].error ?? ''}`
      : `已移入回收站 ${deleted.size} 张`
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    batchBusy.value = false
  }
}

function openGroup(group: SimilarGroup, index: number): void {
  viewerAssets.value = group.images.slice()
  viewerIndex.value = index
  viewerOpen.value = true
}

function onViewerUpdated(asset: Asset): void {
  if (result.value) {
    for (const group of result.value.groups) {
      const index = group.images.findIndex((image) => image.hash === asset.hash)
      if (index >= 0) group.images[index] = { ...group.images[index], ...asset }
    }
  }
  const viewerPos = viewerAssets.value.findIndex((item) => item.hash === asset.hash)
  if (viewerPos >= 0) viewerAssets.value[viewerPos] = asset
}

function onViewerDeleted(hash: string): void {
  viewerAssets.value = viewerAssets.value.filter((item) => item.hash !== hash)
  removeHashes(new Set([hash]))
}

onMounted(async () => {
  try {
    const settings = await settingsApi.all()
    from.value = (settings.similar_from ?? '').slice(0, 16)
  } catch (err) {
    error.value = errorMessage(err)
  }
  try {
    const snapshot = await similarApi.progress()
    progress.value = snapshot
    if (snapshot.status === 'running') schedulePoll()
    else if (snapshot.status === 'done' || snapshot.status === 'cancelled') await loadResult()
  } catch (err) {
    error.value = errorMessage(err)
  }
})

onBeforeUnmount(clearTimer)
</script>

<template>
  <PageShell title="相似图片" :subtitle="`按时间顺序比对相邻图片的 dHash，检测阈值 ${threshold}`">
    <section class="panel">
      <div class="panel-head"><h2>检测设置</h2></div>
      <div class="toolbar">
        <label class="inline-field">
          <span>起始时间</span>
          <input v-model="from" class="input" type="datetime-local" step="1" />
        </label>
        <label class="inline-field">
          <span>算法</span>
          <select v-model="algorithm" class="select">
            <option value="dhash">dHash（64 位差分哈希）</option>
          </select>
        </label>
        <label class="inline-field">
          <span>阈值</span>
          <input
            v-model.number="threshold"
            class="input threshold-input"
            type="number"
            min="0"
            max="64"
            step="1"
          />
        </label>
        <button
          class="btn btn-primary"
          :disabled="running || starting"
          @click="startDetection"
        >
          {{ starting ? '启动中…' : running ? '检测中…' : '开始检测' }}
        </button>
        <button v-if="running" class="btn btn-danger" @click="cancelDetection">取消检测</button>
      </div>
      <p class="hint dim">
        只检测「起始时间」之后的图片（留空 = 不限），已计算过的 dHash 会从数据库复用，不会重复计算。
        只比对按时间排序后相邻的图片，连续相似会归为一组，因此无需两两全局比较；单次最多返回
        {{ MAX_GROUPS }} 组。阈值是 dHash 汉明距离（0–64），越小越严格，默认 8。
      </p>
    </section>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <section v-if="progress && progress.status !== 'idle'" class="panel">
      <div class="panel-head">
        <h2>检测进度 <span class="tag">{{ statusText }}</span></h2>
        <button v-if="running" class="btn btn-danger" @click="cancelDetection">取消检测</button>
      </div>
      <div class="progress-track">
        <div class="progress-bar" :style="{ width: percent + '%' }" />
      </div>
      <div class="stat-row">
        <span>{{ progress.processed }} / {{ progress.total }}（{{ percent }}%）</span>
        <span>已发现 {{ progress.groups }} 组</span>
        <span>缓存命中 {{ progress.cached }}</span>
        <span>新计算 {{ progress.computed }}</span>
        <span>失败 {{ progress.failures }}</span>
        <span v-if="progress.current" class="dim current">{{ progress.current }}</span>
      </div>
    </section>

    <section v-if="result" class="panel">
      <div class="panel-head">
        <h2>检测结果</h2>
        <div class="toolbar">
          <span class="dim">已勾选 {{ selectedCount }} 张（默认全部保留）</span>
          <button class="btn btn-sm" :disabled="!selectedCount" @click="clearSelection">
            清空选择
          </button>
        </div>
      </div>

      <p class="result-summary">
        扫描 <strong>{{ result.scanned }}</strong> 张，发现
        <strong>{{ result.group_count }}</strong> 组相似图片；新计算 {{ result.computed }} 张 /
        缓存命中 {{ result.cached }} 张，失败 {{ result.failures }} 张，耗时
        {{ formatDuration(result.duration_ms) }}。
      </p>
      <p class="dim result-scope">
        检测范围：{{ result.from ? `${formatDate(result.from)} 之后` : '全部图片' }}
        · 算法 {{ result.algorithm }}（阈值 {{ result.threshold }}）
      </p>
      <p v-if="result.truncated" class="banner error">
        已达到 {{ MAX_GROUPS }} 组上限，本次提前停止；剩余图片未检测。
      </p>
      <p v-if="result.cancelled" class="banner">
        检测已取消，以下为已发现的部分结果。
      </p>

      <p v-if="!result.groups.length" class="dim empty-tip">
        没有发现相似图片。可以调整阈值、起始时间或先导入更多照片后再试。
      </p>

      <div v-for="(group, index) in groups" :key="group.id" class="similar-group">
        <div class="group-head">
          <h3>
            第 {{ index + 1 }} 组
            <span class="dim">· {{ group.images.length }} 张</span>
            <span v-if="groupDistanceRange(group)" class="group-badge">
              {{ groupDistanceRange(group) }}
            </span>
          </h3>
          <div class="toolbar">
            <button class="btn btn-sm" @click="openGroup(group, 0)">查看大图</button>
            <button
              class="btn btn-sm btn-danger"
              :disabled="batchBusy || !groupSelectedCount(group)"
              @click="deleteSelected(group)"
            >
              删除所选（{{ groupSelectedCount(group) }}）
            </button>
          </div>
        </div>

        <div class="similar-items">
          <div
            v-for="(image, imageIndex) in group.images"
            :key="image.hash"
            class="similar-item"
            :class="{ picked: selected.has(image.hash) }"
          >
            <label class="pick" :title="selected.has(image.hash) ? '取消勾选' : '勾选后删除'">
              <input
                type="checkbox"
                :checked="selected.has(image.hash)"
                @change="toggle(image.hash)"
              />
            </label>
            <button
              type="button"
              class="thumb-btn"
              :title="`查看原图：${image.original_name}`"
              @click="openGroup(group, imageIndex)"
            >
              <AssetThumb :hash="image.hash" :alt="image.original_name" />
            </button>
            <span class="item-name" :title="image.original_name">{{ image.original_name }}</span>
            <span class="item-meta dim">
              {{ formatDate(image.taken_at) }}
              <template v-if="image.distance_from_prev != null">
                · 与上一张 Δ{{ image.distance_from_prev }}
              </template>
            </span>
          </div>
        </div>
      </div>
    </section>

    <PhotoViewer
      :assets="viewerAssets"
      :start-index="viewerIndex"
      :open="viewerOpen"
      @close="viewerOpen = false"
      @updated="onViewerUpdated"
      @deleted="onViewerDeleted"
    />
  </PageShell>
</template>

<style scoped>
.threshold-input {
  width: 80px;
}

.result-summary {
  margin: 0 0 6px;
  font-size: 13px;
}

.result-scope {
  margin: 0 0 12px;
  font-size: 12px;
}

.empty-tip {
  margin: 4px 0;
  font-size: 13px;
}

.similar-group {
  margin-bottom: 16px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.015);
}

.similar-group:last-child {
  margin-bottom: 0;
}

.group-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}

.group-head h3 {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}

.group-badge {
  padding: 1px 8px;
  border-radius: 999px;
  background: rgba(76, 141, 255, 0.14);
  color: var(--accent);
  font-size: 11px;
  font-weight: 400;
}

.similar-items {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 10px;
}

.similar-item {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  min-width: 0;
}

.similar-item.picked {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent) inset;
}

.pick {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 2;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  background: rgba(20, 22, 26, 0.78);
  cursor: pointer;
}

.pick input {
  margin: 0;
  cursor: pointer;
}

.thumb-btn {
  padding: 0;
  border: none;
  background: none;
  cursor: zoom-in;
}

.thumb-btn :deep(.thumb-img),
.thumb-btn :deep(.thumb-placeholder) {
  display: block;
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 6px;
}

.item-name {
  overflow: hidden;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-meta {
  font-size: 11px;
}
</style>
