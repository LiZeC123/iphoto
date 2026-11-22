<script setup lang="ts">
import PhotoSwipe from 'photoswipe'
import 'photoswipe/style.css'
import type { SlideData } from 'photoswipe'
import { computed, onBeforeUnmount, ref, watch } from 'vue'

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
const saving = ref(false)
const deleting = ref(false)
const error = ref('')
const assetTags = ref<string[]>([])
const tagsBusy = ref(false)

let pswp: PhotoSwipe | null = null

const current = computed<Asset | null>(() => props.assets[currentIndex.value] ?? null)

watch(
  current,
  (asset) => {
    note.value = asset?.note ?? ''
    override.value = asset?.taken_at_override ? asset.taken_at_override.slice(0, 16) : ''
    error.value = ''
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

function openViewer(): void {
  destroy()
  if (!props.assets.length) return

  const dataSource: SlideData[] = props.assets.map((asset) => ({
    src: assetsApi.fileUrl(asset.hash),
    msrc: assetsApi.thumbUrl(asset.hash),
    width: asset.width ?? 1600,
    height: asset.height ?? 1200,
    alt: asset.original_name,
  }))

  currentIndex.value = Math.min(Math.max(props.startIndex, 0), props.assets.length - 1)
  pswp = new PhotoSwipe({
    dataSource,
    index: currentIndex.value,
    showHideAnimationType: 'fade',
    bgOpacity: 0.94,
    arrowKeys: true,
    closeOnVerticalDrag: true,
    // 关闭焦点陷阱，否则面板里的输入框无法保持焦点。
    trapFocus: false,
  })
  pswp.on('change', () => {
    currentIndex.value = pswp?.currIndex ?? 0
  })
  pswp.on('close', () => {
    emit('close')
  })
  pswp.on('destroy', () => {
    pswp = null
  })
  pswp.init()
}

function destroy(): void {
  if (pswp) {
    pswp.destroy()
    pswp = null
  }
}

function close(): void {
  pswp?.close()
}

async function save(): Promise<void> {
  const asset = current.value
  if (!asset) return
  saving.value = true
  error.value = ''
  try {
    const updated = await assetsApi.patch(asset.hash, {
      note: note.value,
      taken_at_override: override.value,
    })
    emit('updated', updated)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

async function remove(): Promise<void> {
  const asset = current.value
  if (!asset) return
  deleting.value = true
  error.value = ''
  try {
    await assetsApi.remove(asset.hash)
    emit('deleted', asset.hash)
    close()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    deleting.value = false
  }
}

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

        <p v-if="error" class="banner error">{{ error }}</p>

        <div class="toolbar">
          <button class="btn btn-primary btn-sm" :disabled="saving" @click="save">
            {{ saving ? '保存中…' : '保存' }}
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
  width: 308px;
  max-height: calc(100dvh - 32px);
  /* 不裁剪标签下拉菜单；面板内容本身高度可控。 */
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

.viewer-panel .toolbar {
  margin-top: 4px;
}
</style>
