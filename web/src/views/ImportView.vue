<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { importApi } from '@/api/import'
import { basename, errorMessage, settingsApi } from '@/api/settings'
import type { DuplicateItem, ImportProgress, ImportScan, UnsupportedInfo } from '@/api/types'
import PageShell from '@/components/PageShell.vue'
import PhoneImportPanel from '@/components/PhoneImportPanel.vue'
import TagSelector from '@/components/TagSelector.vue'
import UnsupportedReport from '@/components/UnsupportedReport.vue'
import { formatBytes, formatPercent } from '@/utils/format'

const path = ref('')
const importTags = ref<string[]>([])
const scan = ref<ImportScan | null>(null)
const scanning = ref(false)
const starting = ref(false)
const progress = ref<ImportProgress | null>(null)
const duplicates = ref<DuplicateItem[]>([])
const error = ref('')
const notice = ref('')
const busy = ref(new Set<number>())

let timer: ReturnType<typeof setTimeout> | undefined

const batchId = computed(() => progress.value?.batch_id ?? 0)
const running = computed(() => progress.value?.status === 'running')
const isPhone = computed(() => progress.value?.source === 'phone')
const percent = computed(() =>
  formatPercent(progress.value?.processed ?? 0, progress.value?.total ?? 0),
)
const reportUrl = computed(() => (batchId.value ? importApi.reportUrl(batchId.value) : ''))
function emptyUnsupported(): UnsupportedInfo {
  return { total: 0, by_ext: [], files: [], truncated: false }
}

const STATUS_LABELS: Record<string, string> = {
  running: '进行中',
  done: '已完成',
  cancelled: '已取消',
  failed: '失败',
  interrupted: '已中断',
}
const statusText = computed(() => {
  const status = progress.value?.status ?? ''
  return STATUS_LABELS[status] ?? status
})

function clearTimer(): void {
  if (timer) {
    clearTimeout(timer)
    timer = undefined
  }
}

async function refresh(id: number): Promise<void> {
  try {
    const snapshot = await importApi.progress(id)
    progress.value = snapshot
    if (snapshot.status === 'running') {
      clearTimer()
      timer = setTimeout(() => void refresh(id), 500)
    } else {
      clearTimer()
      await loadDuplicates(id)
    }
  } catch (err) {
    clearTimer()
    error.value = errorMessage(err)
  }
}

async function loadDuplicates(id: number): Promise<void> {
  if (!id) return
  try {
    duplicates.value = (await importApi.duplicates(id)).items
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function doScan(): Promise<void> {
  error.value = ''
  notice.value = ''
  scanning.value = true
  try {
    scan.value = await importApi.scan(path.value.trim())
  } catch (err) {
    scan.value = null
    error.value = errorMessage(err)
  } finally {
    scanning.value = false
  }
}

async function startImport(): Promise<void> {
  error.value = ''
  notice.value = ''
  starting.value = true
  try {
    const { batch_id } = await importApi.start(path.value.trim(), importTags.value)
    scan.value = null
    duplicates.value = []
    progress.value = {
      batch_id,
      status: 'running',
      total: 0,
      processed: 0,
      imported: 0,
      duplicate_skipped: 0,
      duplicate_pending: 0,
      failed: 0,
      unsupported: emptyUnsupported(),
      started_at: '',
    }
    clearTimer()
    timer = setTimeout(() => void refresh(batch_id), 300)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    starting.value = false
  }
}

async function cancelImport(): Promise<void> {
  if (!batchId.value) return
  try {
    await importApi.cancel(batchId.value)
    notice.value = '已请求取消，正在停止…'
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function resolveOne(id: number, action: 'skip' | 'replace'): Promise<void> {
  busy.value.add(id)
  try {
    await importApi.resolve(batchId.value, [{ id, action }])
    duplicates.value = duplicates.value.filter((item) => item.id !== id)
    notice.value = action === 'replace' ? '已替换库内文件名' : '已跳过'
    await refresh(batchId.value)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    busy.value.delete(id)
  }
}

async function resolveAll(action: 'skip' | 'replace'): Promise<void> {
  if (!duplicates.value.length) return
  const items = duplicates.value.map((item) => ({ id: item.id, action }))
  try {
    await importApi.resolve(batchId.value, items)
    duplicates.value = []
    notice.value = action === 'replace' ? '已全部替换' : '已全部跳过'
    await refresh(batchId.value)
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function cleanup(): Promise<void> {
  if (!batchId.value) return
  if (!window.confirm('清理该批次的导入明细？报告 CSV 将不再可用（图片不受影响）。')) return
  try {
    const res = await importApi.cleanup(batchId.value)
    duplicates.value = []
    notice.value = `已清理 ${res.deleted} 条导入明细`
  } catch (err) {
    error.value = errorMessage(err)
  }
}

/** 手机面板启动同步后，立刻接管进度显示（沿用现有的轮询逻辑）。 */
function onPhoneStarted(batchId: number): void {
  scan.value = null
  duplicates.value = []
  progress.value = {
    batch_id: batchId,
    status: 'running',
    total: 0,
    processed: 0,
    imported: 0,
    duplicate_skipped: 0,
    duplicate_pending: 0,
    failed: 0,
    unsupported: emptyUnsupported(),
    started_at: '',
    source: 'phone',
  }
  clearTimer()
  timer = setTimeout(() => void refresh(batchId), 300)
}

onMounted(async () => {
  try {
    const settings = await settingsApi.all()
    path.value = settings.last_import_path ?? ''
    const active = await importApi.active()
    if (active.batch_id) {
      progress.value = {
        batch_id: active.batch_id,
        status: 'running',
        total: 0,
        processed: 0,
        imported: 0,
        duplicate_skipped: 0,
        duplicate_pending: 0,
        failed: 0,
        unsupported: emptyUnsupported(),
        started_at: '',
      }
      await refresh(active.batch_id)
    }
  } catch (err) {
    error.value = errorMessage(err)
  }
})

onBeforeUnmount(clearTimer)
</script>

<template>
  <PageShell title="导入" subtitle="从本地目录或手机（PhoneDock）导入到 library/。">
    <section class="panel">
      <div class="panel-head">
        <h2>从本地目录导入</h2>
      </div>
      <label class="field-label" for="src-path">源目录</label>
      <div class="toolbar">
        <input
          id="src-path"
          v-model="path"
          class="input grow"
          placeholder="/Users/you/Pictures/2023"
          spellcheck="false"
        />
        <button class="btn" :disabled="scanning || running || !path.trim()" @click="doScan">
          {{ scanning ? '扫描中…' : '扫描' }}
        </button>
        <button
          class="btn btn-primary"
          :disabled="starting || running || !path.trim()"
          @click="startImport"
        >
          {{ starting ? '启动中…' : '开始导入' }}
        </button>
      </div>
      <p v-if="scan" class="hint">
        扫描到 <strong>{{ scan.total }}</strong> 个可导入文件<template v-if="scan.unreadable"
          >，{{ scan.unreadable }} 个路径不可读</template
        ><template v-if="scan.unsupported.total"
          >，<strong class="warn-text">{{ scan.unsupported.total }}</strong> 个文件类型不支持</template
        >。<span class="mono dim">{{ scan.path }}</span>
      </p>
      <UnsupportedReport v-if="scan" :info="scan.unsupported" />
      <label class="field-label import-tag-label" for="import-tags">导入标签（可选）</label>
      <TagSelector id="import-tags" v-model="importTags" placeholder="选择或输入标签，导入完成后统一添加" />

      <p class="hint dim">
        支持 jpg/jpeg/png/gif/webp/bmp/tif/tiff/avif/heic/heif 与 mp4 视频，递归所有子目录；跳过隐藏文件与符号链接。源文件只读，不移动、不改名、不删除。
        mp4 会原样入库并可在详情页播放；缩略图需要系统安装 ffmpeg（未安装时显示 ▶ 占位，不影响播放）。
        动图 WebP 可导入和查看，但暂时不会生成网格缩略图；HEIC/HEIF 在查看器中由服务端转成 JPEG 预览（AVIF 浏览器原生支持）；标签只会添加到本次新导入的文件。
      </p>
    </section>

    <PhoneImportPanel :busy="running" @started="onPhoneStarted" />

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <section v-if="progress" class="panel">
      <div class="panel-head">
        <h2>
          批次 #{{ progress.batch_id }}
          <span class="tag">{{ statusText }}</span>
          <span v-if="isPhone" class="tag">{{ progress.device_name || '手机' }}</span>
        </h2>
        <button v-if="running" class="btn btn-danger" @click="cancelImport">
          {{ isPhone ? '取消同步' : '取消导入' }}
        </button>
      </div>

      <div class="progress-track">
        <div class="progress-bar" :style="{ width: percent + '%' }" />
      </div>
      <div class="stat-row">
        <span>{{ progress.processed }} / {{ progress.total }}（{{ percent }}%）</span>
        <span>新增 {{ progress.imported }}</span>
        <span v-if="isPhone">未变更 {{ progress.unchanged ?? 0 }}</span>
        <span v-if="!isPhone">跳过 {{ progress.duplicate_skipped }}</span>
        <span v-if="isPhone && progress.duplicate_skipped">库内已有 {{ progress.duplicate_skipped }}</span>
        <span v-if="!isPhone">待处理重复 {{ progress.duplicate_pending }}</span>
        <span>失败 {{ progress.failed }}</span>
        <span v-if="progress.unsupported.total" class="warn-text">
          类型不支持 {{ progress.unsupported.total }}
        </span>
        <span v-if="isPhone && progress.bytes_total" class="dim">
          下载 {{ formatBytes(progress.bytes_done ?? 0) }} / {{ formatBytes(progress.bytes_total) }}
        </span>
        <span v-if="progress.current" class="dim current">
          {{ basename(progress.current ?? '') }}
        </span>
        <span v-else-if="running && isPhone" class="dim">
          {{ progress.phase === 'listing' ? '读取手机媒体列表…' : '准备中…' }}
        </span>
      </div>

      <p v-if="isPhone && !running && !progress.total && progress.status === 'done'" class="hint">
        本次没有新增或变更的媒体，已把本轮手机媒体库水位记为
        <span class="mono">{{ progress.checkpoint }}</span>。
      </p>

      <UnsupportedReport :info="progress.unsupported" />

      <div v-if="!running" class="toolbar">
        <a v-if="batchId" class="btn" :href="reportUrl" download>导出 CSV 报告</a>
        <button v-if="!isPhone" class="btn" @click="loadDuplicates(batchId)">刷新重复列表</button>
        <button class="btn" @click="cleanup">清理导入历史</button>
      </div>
    </section>

    <section v-if="duplicates.length" class="panel">
      <div class="panel-head">
        <h2>重复图片（内容相同、文件名不同）</h2>
        <div class="toolbar">
          <button class="btn btn-sm" @click="resolveAll('skip')">全部跳过</button>
          <button class="btn btn-sm btn-primary" @click="resolveAll('replace')">全部替换</button>
        </div>
      </div>
      <table class="data-table">
        <thead>
          <tr>
            <th>源文件</th>
            <th>库内文件</th>
            <th>大小（库内 / 源）</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in duplicates" :key="item.id">
            <td class="mono ellipsis" :title="item.source_path">{{ item.source_path }}</td>
            <td>{{ item.existing_name }}</td>
            <td class="dim">
              {{ formatBytes(item.existing_size) }} / {{ formatBytes(item.incoming_size) }}
            </td>
            <td class="actions">
              <button class="btn btn-sm" :disabled="busy.has(item.id)" @click="resolveOne(item.id, 'skip')">
                跳过
              </button>
              <button
                class="btn btn-sm btn-primary"
                :disabled="busy.has(item.id)"
                @click="resolveOne(item.id, 'replace')"
              >
                替换
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p class="hint dim">替换只改库内文件名，不复制内容、不改动源文件。</p>
    </section>
  </PageShell>
</template>
