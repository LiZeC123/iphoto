<script setup lang="ts">
import PhotoSwipe from 'photoswipe'
import 'photoswipe/style.css'
import type { SlideData } from 'photoswipe'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { assetsApi } from '@/api/assets'
import { errorMessage } from '@/api/settings'
import { tagsApi } from '@/api/tags'
import type { Asset } from '@/api/types'
import TagSelector from '@/components/TagSelector.vue'
import { formatBytes, formatDate } from '@/utils/format'

const props = defineProps<{ assets: Asset[]; startIndex: number; open: boolean }>()
const emit = defineEmits<{
  close: []
  updated: [asset: Asset]
  deleted: [hash: string]
}>()

const currentIndex = ref(0)
const panelOpen = ref(true)
const note = ref('')
const override = ref('')
const nameInput = ref('')
const saving = ref(false)
const renaming = ref(false)
const revealing = ref(false)
const deleting = ref(false)
const error = ref('')
const notice = ref('')
const assetTags = ref<string[]>([])
const tagsBusy = ref(false)
// 删除后等待父组件更新列表，再按目标下标重新打开查看器。
const pendingIndex = ref<number | null>(null)

let pswp: PhotoSwipe | null = null
// 程序内部重建查看器（如删除后切到下一张）时抑制 close 事件，避免父组件误关。
let suppressClose = false

const current = computed<Asset | null>(() => props.assets[currentIndex.value] ?? null)

// 只在切换图片（哈希变化）时重置表单；保存/重命名后父组件替换同一条记录不应清空提示。
watch(
  () => current.value?.hash ?? '',
  () => {
    const asset = current.value
    note.value = asset?.note ?? ''
    override.value = asset?.taken_at_override ? asset.taken_at_override.slice(0, 16) : ''
    nameInput.value = asset?.original_name ?? ''
    error.value = ''
    notice.value = ''
    assetTags.value = []
    if (asset) void loadTags(asset.hash)
  },
  { immediate: true },
)

async function loadTags(hash: string): Promise<void> {
  try {
    const detail = await assetsApi.get(hash)
    if (current.value?.hash === hash) assetTags.value = detail.tags
  } catch (err) {
    error.value = errorMessage(err)
  }
}

// 标签改动即时生效：只发送增删差异。
async function applyTags(next: string[]): Promise<void> {
  const asset = current.value
  if (!asset) return
  const added = next.filter((name) => !assetTags.value.includes(name))
  const removed = assetTags.value.filter((name) => !next.includes(name))
  if (!added.length && !removed.length) return
  tagsBusy.value = true
  error.value = ''
  try {
    await tagsApi.batch([asset.hash], added, removed)
    assetTags.value = next
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    tagsBusy.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open) openViewer()
    else if (pswp) pswp.close()
  },
  { immediate: true },
)

function openViewer(index = props.startIndex): void {
  destroy()
  if (!props.assets.length) return

  const dataSource: SlideData[] = props.assets.map((asset) => ({
    // TIFF 浏览器无法直接显示，改读服务端生成的 JPEG 预览；其它格式仍读原图。
    src: asset.mime === 'image/tiff' ? assetsApi.previewUrl(asset.hash) : assetsApi.fileUrl(asset.hash),
    msrc: assetsApi.thumbUrl(asset.hash),
    ...slideSize(asset),
    alt: asset.original_name,
  }))

  currentIndex.value = Math.min(Math.max(index, 0), props.assets.length - 1)
  const instance = new PhotoSwipe({
    dataSource,
    index: currentIndex.value,
    showHideAnimationType: 'fade',
    bgOpacity: 0.94,
    arrowKeys: true,
    closeOnVerticalDrag: true,
    // 关闭焦点陷阱，否则面板里的输入框无法保持焦点。
    trapFocus: false,
    // 默认最大仅到「原图大小」，小尺寸图片仍会显得偏小；
    // 这里允许在适配尺寸基础上继续放大（约 8 倍适配尺寸，且至少 4 倍原图）。
    secondaryZoomLevel: (zoomLevel) => Math.min(Math.max(zoomLevel.fit * 3, 1), 4),
    maxZoomLevel: (zoomLevel) => Math.max(4, zoomLevel.fit * 8),
  })
  // 把缩放控件放到 PhotoSwipe 右上角的工具栏里（与自带的缩放、关闭按钮同排）。
  instance.on('uiRegister', () => {
    const ui = instance.ui
    if (!ui) return
    ui.registerElement({
      name: 'iphotoZoomOut',
      className: 'iphoto-zoom',
      title: '缩小（S）',
      ariaLabel: '缩小',
      order: 8,
      isButton: true,
      html: '<span aria-hidden="true">−</span>',
      onClick: () => zoomBy(1 / 1.5),
    })
    ui.registerElement({
      name: 'iphotoZoomIn',
      className: 'iphoto-zoom',
      title: '放大（W，也可滚轮 / 双击图片）',
      ariaLabel: '放大',
      order: 9,
      isButton: true,
      html: '<span aria-hidden="true">+</span>',
      onClick: () => zoomBy(1.5),
    })
    ui.registerElement({
      name: 'iphotoZoomActual',
      className: 'iphoto-zoom iphoto-zoom--actual',
      title: '缩放到原图实际大小（1:1）',
      ariaLabel: '实际大小',
      order: 9.5,
      isButton: true,
      html: '<span aria-hidden="true">1:1</span>',
      onClick: () => zoomActual(),
    })
  })

  pswp = instance
  // 旧实例的动画回调可能在重新打开之后才触发，所有回调都要校验实例身份。
  instance.on('change', () => {
    if (pswp === instance) currentIndex.value = instance.currIndex
  })
  instance.on('close', () => {
    if (!suppressClose) emit('close')
  })
  instance.on('destroy', () => {
    if (pswp === instance) pswp = null
  })
  instance.init()
}

// 预览图的后端长边上限（与 internal/media.PreviewLongEdge 保持一致）。
const PREVIEW_LONG_EDGE = 2560

// TIFF 的 slide 尺寸按预览图计算，避免「1:1」把 2560px 的预览放大到原始尺寸导致糊成一片。
function slideSize(asset: Asset): { width: number; height: number } {
  let width = asset.width ?? 1600
  let height = asset.height ?? 1200
  if (asset.mime === 'image/tiff') {
    const scale = Math.min(1, PREVIEW_LONG_EDGE / Math.max(width, height))
    if (scale < 1) {
      width = Math.max(1, Math.round(width * scale))
      height = Math.max(1, Math.round(height * scale))
    }
  }
  return { width, height }
}

function destroy(): void {
  if (!pswp) return
  const instance = pswp
  pswp = null
  // PhotoSwipe.destroy() 内部会先 close()，这里屏蔽它触发的 close 事件。
  suppressClose = true
  try {
    instance.destroy()
  } finally {
    suppressClose = false
  }
}

// ---- 缩放 ----

function zoomBy(ratio: number): void {
  const slide = pswp?.currSlide
  if (!slide) return
  const levels = slide.zoomLevels
  const base = slide.currZoomLevel || levels.initial || 1
  const target = Math.min(levels.max, Math.max(levels.min, base * ratio))
  slide.zoomTo(target, undefined, 160)
}

function zoomActual(): void {
  const slide = pswp?.currSlide
  if (!slide) return
  const target = Math.min(1, slide.zoomLevels.max)
  slide.zoomTo(Math.max(slide.zoomLevels.fit, target), undefined, 160)
}

// ---- 保存 / 重命名 / 删除 / 打开目录 ----

async function save(): Promise<void> {
  const asset = current.value
  if (!asset) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const updated = await assetsApi.patch(asset.hash, {
      note: note.value,
      taken_at_override: override.value,
    })
    emit('updated', updated)
    notice.value = '已保存'
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

async function rename(): Promise<void> {
  const asset = current.value
  if (!asset) return
  const name = nameInput.value.trim()
  if (!name || name === asset.original_name) return
  renaming.value = true
  error.value = ''
  notice.value = ''
  try {
    const updated = await assetsApi.rename(asset.hash, name)
    nameInput.value = updated.original_name
    notice.value = `已重命名为 ${updated.original_name}`
    emit('updated', updated)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    renaming.value = false
  }
}

async function reveal(): Promise<void> {
  const asset = current.value
  if (!asset) return
  revealing.value = true
  error.value = ''
  notice.value = ''
  try {
    await assetsApi.reveal(asset.hash)
    notice.value = '已在系统文件浏览器中打开所在目录'
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    revealing.value = false
  }
}

async function remove(): Promise<void> {
  const asset = current.value
  if (!asset || deleting.value) return
  deleting.value = true
  error.value = ''
  notice.value = ''
  try {
    await assetsApi.remove(asset.hash)
    // 删除后，原来的下一张会顶到当前下标；没有下一张时退出查看器。
    const hasNext = currentIndex.value < props.assets.length - 1
    if (hasNext) pendingIndex.value = currentIndex.value
    emit('deleted', asset.hash)
    if (!hasNext) pswp?.close()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    deleting.value = false
  }
}

// ---- 键盘快捷键：A 上一张 / D 下一张 / W 放大 / S 缩小 / Q 退出 / G 删除 ----

function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

function onKeydown(event: KeyboardEvent): void {
  if (!props.open || !pswp || pswp.isDestroying) return
  if (event.defaultPrevented || event.isComposing) return
  if (event.metaKey || event.ctrlKey || event.altKey) return
  if (isTypingTarget(event.target)) return
  switch (event.key.toLowerCase()) {
    case 'a':
      event.preventDefault()
      pswp.prev()
      break
    case 'd':
      event.preventDefault()
      pswp.next()
      break
    case 'w':
      event.preventDefault()
      zoomBy(1.5)
      break
    case 's':
      event.preventDefault()
      zoomBy(1 / 1.5)
      break
    case 'q':
      event.preventDefault()
      pswp.close()
      break
    case 'g':
      // 屏蔽长按产生的重复事件，避免连续删除多张。
      if (event.repeat) return
      event.preventDefault()
      void remove()
      break
    default:
      break
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})

// 父组件把被删除的图片从列表移除后，按目标下标重新打开；没有剩余图片则退出查看器。
watch(
  () => props.assets,
  (list) => {
    const target = pendingIndex.value
    if (target === null) return
    pendingIndex.value = null
    if (!props.open) return
    if (!list.length) {
      emit('close')
      return
    }
    openViewer(Math.min(target, list.length - 1))
  },
)

onBeforeUnmount(destroy)
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="viewer-panel" :class="{ collapsed: !panelOpen }">
      <button class="panel-toggle btn btn-sm" type="button" @click="panelOpen = !panelOpen">
        {{ panelOpen ? '收起信息' : '图片信息' }}
      </button>

      <div v-if="panelOpen && current" class="panel-body">
        <h3 :title="current.original_name">{{ current.original_name }}</h3>

        <dl class="kv">
          <dt>尺寸</dt>
          <dd>{{ current.width ?? '?' }}×{{ current.height ?? '?' }}</dd>
          <dt>大小</dt>
          <dd>{{ formatBytes(current.size) }}</dd>
          <dt>时间</dt>
          <dd>{{ formatDate(current.taken_at) }}</dd>
          <dt>时间来源</dt>
          <dd>{{ current.taken_at_source ?? '—' }}</dd>
          <dt>路径</dt>
          <dd class="mono break">{{ current.rel_path }}</dd>
        </dl>

        <label class="field">
          <span>重命名（扩展名保持不变）</span>
          <div class="rename-row">
            <input
              v-model="nameInput"
              class="input"
              spellcheck="false"
              @keydown.stop
              @keyup.enter.stop="rename"
            />
            <button
              class="btn btn-sm"
              type="button"
              :disabled="renaming || !nameInput.trim() || nameInput.trim() === current.original_name"
              @click="rename"
            >
              {{ renaming ? '保存中…' : '重命名' }}
            </button>
          </div>
        </label>

        <label class="field">
          <span>标签</span>
          <TagSelector
            :model-value="assetTags"
            placeholder="添加标签"
            @update:model-value="applyTags"
          />
        </label>

        <label class="field">
          <span>备注</span>
          <textarea
            v-model="note"
            rows="3"
            class="input"
            placeholder="为这张图片写点描述"
            @keydown.stop
          />
        </label>
        <label class="field">
          <span>拍摄时间（覆盖）</span>
          <input v-model="override" type="datetime-local" step="1" class="input" @keydown.stop />
        </label>
        <p class="hint dim">留空则使用自动解析时间。</p>
        <p class="hint dim shortcuts">
          快捷键：<kbd>A</kbd> 上一张 · <kbd>D</kbd> 下一张 · <kbd>W</kbd> 放大 · <kbd>S</kbd> 缩小 ·
          <kbd>Q</kbd> 退出 · <kbd>G</kbd> 删除当前图片
        </p>

        <p v-if="error" class="banner error">{{ error }}</p>
        <p v-if="notice" class="banner notice">{{ notice }}</p>

        <div class="toolbar">
          <button class="btn btn-primary btn-sm" :disabled="saving" @click="save">
            {{ saving ? '保存中…' : '保存' }}
          </button>
          <button class="btn btn-sm" type="button" :disabled="revealing" @click="reveal">
            {{ revealing ? '打开中…' : '在文件浏览器中显示' }}
          </button>
          <button class="btn btn-danger btn-sm" :disabled="deleting" @click="remove">
            {{ deleting ? '处理中…' : '移入回收站' }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.viewer-panel {
  position: fixed;
  top: 16px;
  left: 16px;
  z-index: 100001;
  width: 318px;
  max-height: calc(100dvh - 32px);
  /* 不裁剪标签下拉菜单；面板内容本身在 .panel-body 内滚动。 */
  overflow: visible;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(27, 31, 38, 0.96);
  box-shadow: 0 18px 40px rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(10px);
}

.viewer-panel.collapsed {
  width: auto;
}

.panel-toggle {
  margin-bottom: 8px;
}

.panel-body {
  max-height: calc(100dvh - 90px);
  padding-right: 2px;
  overflow-y: auto;
}

.panel-body h3 {
  margin: 2px 0 10px;
  overflow: hidden;
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv {
  display: grid;
  grid-template-columns: 68px 1fr;
  gap: 5px 8px;
  margin: 0 0 12px;
  font-size: 12px;
}

.kv dt {
  color: var(--text-dim);
}

.kv dd {
  margin: 0;
  overflow-wrap: anywhere;
}

.break {
  word-break: break-all;
}

.shortcuts {
  margin-top: 6px;
}

.shortcuts kbd {
  padding: 0 4px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.06);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
}

.field {
  display: block;
  margin-bottom: 10px;
  font-size: 12px;
  color: var(--text-dim);
}

.field span {
  display: block;
  margin-bottom: 4px;
}

.field .input {
  width: 100%;
  height: auto;
  padding: 6px 8px;
  resize: vertical;
}

.rename-row {
  display: flex;
  gap: 6px;
}

.rename-row .input {
  flex: 1;
  min-width: 0;
}

.viewer-panel .toolbar {
  flex-wrap: wrap;
  margin-top: 4px;
}

.banner.notice {
  margin: 8px 0;
  padding: 6px 8px;
  border-color: rgba(63, 191, 127, 0.4);
  background: rgba(63, 191, 127, 0.12);
  font-size: 12px;
}

.banner.error {
  margin: 8px 0;
  padding: 6px 8px;
  font-size: 12px;
}
</style>
