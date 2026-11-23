<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RecycleScroller } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'

import type { Asset } from '@/api/types'
import AssetThumb from '@/components/AssetThumb.vue'
import { formatDate } from '@/utils/format'
import { isVideoAsset } from '@/utils/media'

const props = withDefaults(
  defineProps<{
    items: Asset[]
    minColumnWidth?: number
    selectable?: boolean
    selected?: Set<string>
  }>(),
  {
    minColumnWidth: 168,
    selectable: false,
    selected: () => new Set<string>(),
  },
)
const emit = defineEmits<{
  open: [index: number]
  pick: [payload: { hash: string; index: number; shift: boolean }]
  'load-more': []
}>()

const GAP = 12
const META = 58

const container = ref<HTMLElement | null>(null)
const sentinel = ref<HTMLElement | null>(null)
const width = ref(1200)

let observer: ResizeObserver | undefined
let sentinelObserver: IntersectionObserver | undefined
let lastLoadMoreAt = 0
let scrollEl: HTMLElement | null = null

function measure(): void {
  const el = container.value
  if (el) {
    const w = el.clientWidth
    if (w > 0) width.value = w
  }
}

onMounted(() => {
  measure()
  if (!container.value) return
  observer = new ResizeObserver(measure)
  observer.observe(container.value)
  window.addEventListener('resize', measure)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  sentinelObserver?.disconnect()
  window.removeEventListener('resize', measure)
  detachScrollListener()
})

const columns = computed(() =>
  Math.max(1, Math.floor((width.value + GAP) / (props.minColumnWidth + GAP))),
)
const columnWidth = computed(
  () => (width.value - GAP * (columns.value - 1)) / columns.value,
)
const rowHeight = computed(() => Math.max(80, columnWidth.value + META))

// ---- 触底自动加载 ----

// 节流，避免连续滚动时反复触发请求；force 用于「视口未填满」的补页。
function requestMore(force = false): void {
  const now = Date.now()
  if (!force && now - lastLoadMoreAt < 250) return
  lastLoadMoreAt = now
  emit('load-more')
}

// RecycleScroller 的 scrollEnd 事件受 emitUpdate 控制（默认 false），
// 因此这里直接监听滚动容器的原生 scroll，距离底部不足约 1.5 行时触发加载。
function onScrollerScroll(): void {
  const el = scrollEl
  if (!el || !props.items.length) return
  const remaining = el.scrollHeight - el.scrollTop - el.clientHeight
  if (remaining <= rowHeight.value * 1.5) requestMore()
}

// 冗余触发：开启 emit-update 后，滚动到最后一行的可见位置时也会派发 scrollEnd。
function onReachEnd(): void {
  if (props.items.length) requestMore()
}

function scrollerElement(): HTMLElement | null {
  return (container.value?.firstElementChild as HTMLElement | null) ?? null
}

function attachScrollListener(): void {
  const el = scrollerElement()
  if (!el || el === scrollEl) return
  detachScrollListener()
  scrollEl = el
  el.addEventListener('scroll', onScrollerScroll, { passive: true })
}

function detachScrollListener(): void {
  if (scrollEl) {
    scrollEl.removeEventListener('scroll', onScrollerScroll)
    scrollEl = null
  }
}

// 首屏行数不足以撑满容器时不会产生滚动事件，此时也需要继续加载。
// 只在 items.length 变化后检查一次，因此不受节流限制。
function ensureViewportFilled(): void {
  const el = scrollerElement()
  if (!el || !props.items.length) return
  if (el.scrollHeight <= el.clientHeight + rowHeight.value) requestMore(true)
}

// 列表尾部哨兵：用 IntersectionObserver 兜底，滚动容器变化、页面级滚动都能感知。
function observeSentinel(): void {
  sentinelObserver?.disconnect()
  sentinelObserver = undefined
  const el = sentinel.value
  if (!el || !props.items.length || typeof IntersectionObserver === 'undefined') return
  sentinelObserver = new IntersectionObserver(
    (entries) => {
      if (entries.some((entry) => entry.isIntersecting)) requestMore()
    },
    { rootMargin: '400px 0px' },
  )
  sentinelObserver.observe(el)
}

watch(sentinel, observeSentinel)

watch(
  () => props.items.length,
  () => {
    void nextTick(() => {
      if (!props.items.length) {
        detachScrollListener()
        return
      }
      attachScrollListener()
      ensureViewportFilled()
      observeSentinel()
    })
  },
  { immediate: true },
)

interface Cell {
  asset: Asset
  index: number
}

function activate(cell: Cell, event: MouseEvent): void {
  if (props.selectable) emit('pick', { hash: cell.asset.hash, index: cell.index, shift: event.shiftKey })
  else emit('open', cell.index)
}
interface Row {
  id: number
  cells: Cell[]
}

// 按行虚拟化（dev.md 12.3）：每行固定高度，RecycleScroller 只渲染可视行。
const rows = computed<Row[]>(() => {
  const out: Row[] = []
  const cols = columns.value
  for (let i = 0; i < props.items.length; i += cols) {
    const slice = props.items.slice(i, i + cols)
    out.push({
      id: i,
      cells: slice.map((asset, j) => ({ asset, index: i + j })),
    })
  }
  return out
})
</script>

<template>
  <div ref="container" class="asset-grid">
    <RecycleScroller
      v-if="items.length"
      class="asset-scroller"
      :items="rows"
      :item-size="rowHeight"
      key-field="id"
      :buffer="600"
      emit-update
      @scroll-end="onReachEnd"
    >
      <template #default="{ item }">
        <div
          class="grid-row"
          :style="{
            height: `${rowHeight - GAP}px`,
            marginBottom: `${GAP}px`,
            gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
            gap: `${GAP}px`,
          }"
        >
          <button
            v-for="cell in item.cells"
            :key="cell.asset.hash"
            type="button"
            class="asset-card"
            :class="{ selected: selectable && selected.has(cell.asset.hash) }"
            @click="activate(cell, $event)"
          >
            <span v-if="selectable" class="select-dot" :class="{ on: selected.has(cell.asset.hash) }">
              {{ selected.has(cell.asset.hash) ? '✓' : '' }}
            </span>
            <span v-if="isVideoAsset(cell.asset)" class="video-badge" aria-hidden="true">▶</span>
            <AssetThumb
              :hash="cell.asset.hash"
              :alt="cell.asset.original_name"
              :mime="cell.asset.mime"
            />
            <span class="asset-meta">
              <strong :title="cell.asset.original_name">{{ cell.asset.original_name }}</strong>
              <span>{{ formatDate(cell.asset.taken_at) }}</span>
            </span>
          </button>
        </div>
      </template>
      <template #after>
        <div ref="sentinel" class="grid-sentinel" aria-hidden="true" />
      </template>
    </RecycleScroller>
    <slot v-else name="empty" />
  </div>
</template>

<style scoped>
.asset-grid {
  height: calc(100dvh - 236px);
  min-height: 320px;
}

.asset-scroller {
  height: 100%;
}

.grid-row {
  display: grid;
}

.grid-sentinel {
  width: 100%;
  height: 1px;
}

.asset-card {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: zoom-in;
  user-select: none;
}

.asset-card:hover {
  border-color: #3a4450;
}

.asset-card.selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent) inset;
}

.asset-card {
  position: relative;
}

.select-dot {
  position: absolute;
  top: 8px;
  left: 8px;
  z-index: 2;
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: 1px solid rgba(255, 255, 255, 0.55);
  border-radius: 50%;
  background: rgba(20, 22, 26, 0.7);
  color: #fff;
  font-size: 12px;
  line-height: 1;
}

.select-dot.on {
  border-color: var(--accent);
  background: var(--accent);
}

.video-badge {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 2;
  display: grid;
  place-items: center;
  min-width: 24px;
  height: 20px;
  padding: 0 6px;
  border-radius: 999px;
  background: rgba(20, 22, 26, 0.78);
  color: #fff;
  font-size: 11px;
  line-height: 1;
  pointer-events: none;
}

.asset-meta {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  gap: 1px;
  padding: 6px 9px 8px;
  font-size: 12px;
  color: var(--text-dim);
}

.asset-card :deep(.thumb-img),
.asset-card :deep(.thumb-placeholder) {
  flex: 1 1 auto;
  min-height: 0;
  height: auto;
  aspect-ratio: auto;
  object-fit: cover;
}

.asset-meta strong {
  overflow: hidden;
  color: var(--text);
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
