<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { phoneApi, type PhoneSyncMode } from '@/api/phone'
import { errorMessage } from '@/api/settings'
import type { PhoneDevice, PhoneProbe } from '@/api/types'
import TagSelector from '@/components/TagSelector.vue'
import { formatDate } from '@/utils/format'

const props = defineProps<{ busy: boolean }>()
const emit = defineEmits<{ started: [batchId: number] }>()

const address = ref('')
const token = ref('')
const tags = ref<string[]>([])
const devices = ref<PhoneDevice[]>([])
const probe = ref<PhoneProbe | null>(null)
const probing = ref(false)
const starting = ref(false)
const fullMode = ref(false)
const busyDevice = ref(0)
const error = ref('')
const notice = ref('')

const pendingTotal = computed(() =>
  probe.value ? probe.value.images.pending + probe.value.videos.pending : 0,
)
const canProbe = computed(() => !probing.value && !starting.value && !props.busy && !!address.value.trim())
const canSync = computed(() => !!probe.value && !starting.value && !props.busy)

const STATUS_LABELS: Record<string, string> = {
  done: '已完成',
  cancelled: '已取消',
  failed: '失败',
  interrupted: '已中断',
}

function countsText(counts: Record<string, number> | undefined): string {
  if (!counts) return ''
  const parts: string[] = []
  if (counts.synced) parts.push(`已同步 ${counts.synced}`)
  if (counts.failed) parts.push(`待重试 ${counts.failed}`)
  if (counts.unsupported) parts.push(`不支持 ${counts.unsupported}`)
  if (counts.missing) parts.push(`已不可用 ${counts.missing}`)
  return parts.join(' · ')
}

async function loadDevices(): Promise<void> {
  try {
    const res = await phoneApi.devices()
    devices.value = res.items
    const first = res.items[0]
    if (first) {
      if (!address.value.trim()) address.value = first.address
      if (!token.value) token.value = ''
    }
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function doProbe(): Promise<void> {
  error.value = ''
  notice.value = ''
  probe.value = null
  probing.value = true
  try {
    probe.value = await phoneApi.probe(address.value.trim(), token.value.trim())
    fullMode.value = probe.value.known && probe.value.address_changed
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    probing.value = false
  }
}

async function startSync(mode: PhoneSyncMode): Promise<void> {
  error.value = ''
  notice.value = ''
  starting.value = true
  try {
    const { batch_id } = await phoneApi.sync(address.value.trim(), token.value.trim(), mode, tags.value)
    notice.value = mode === 'full' ? '已开始全量同步，进度见下方。' : '已开始增量同步，进度见下方。'
    emit('started', batch_id)
    probe.value = null
    await loadDevices()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    starting.value = false
  }
}

async function syncDevice(device: PhoneDevice, mode: PhoneSyncMode): Promise<void> {
  error.value = ''
  notice.value = ''
  busyDevice.value = device.id
  try {
    const { batch_id } = await phoneApi.sync(device.address, '', mode, tags.value)
    notice.value = `已开始同步 ${device.name}，进度见下方。`
    emit('started', batch_id)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    busyDevice.value = 0
  }
}

async function forgetDevice(device: PhoneDevice): Promise<void> {
  if (
    !window.confirm(
      `忘记设备「${device.name}」？\n\n这是显式重置：会删除该手机的同步记录（含 Token）与账本，` +
        `下次连接将重新导入手机上的全部图片（包括已在 iphoto 删除的）。\n库内已有图片本身不受影响。`,
    )
  ) {
    return
  }
  try {
    await phoneApi.forget(device.id)
    notice.value = `已忘记设备 ${device.name}`
    probe.value = null
    await loadDevices()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

onMounted(loadDevices)
</script>

<template>
  <section class="panel">
    <div class="panel-head">
      <h2>从手机导入</h2>
      <span class="tag">PhoneDock · 局域网</span>
    </div>

    <label class="field-label" for="phone-address">手机地址与 Token</label>
    <div class="toolbar">
      <input
        id="phone-address"
        v-model="address"
        class="input grow"
        placeholder="192.168.1.215:8765"
        spellcheck="false"
        autocomplete="off"
      />
      <input
        v-model="token"
        class="input token-input"
        type="password"
        placeholder="Token"
        spellcheck="false"
        autocomplete="off"
      />
      <button class="btn" :disabled="!canProbe" @click="doProbe">
        {{ probing ? '检查中…' : '检查连接' }}
      </button>
    </div>
    <p class="hint dim">
      在手机 PhoneDock 首页可以看到地址与 4 位 Token。检查通过后才会开始下载；
      同一台手机（按厂商 + 型号识别）第二次起只拉新增与变更，Token 已保存时可留空。
      同步过一次的图片，之后在 iphoto 删除（含清空回收站）不会再被重新导入，以 PC 端的删除为准。
    </p>

    <div v-if="probe" class="probe-card">
      <div class="probe-head">
        <strong>{{ probe.device.name }}</strong>
        <span class="dim">
          Android {{ probe.device.android_version }} · 电量 {{ probe.device.battery_level }}%{{
            probe.device.charging ? '（充电中）' : ''
          }}
        </span>
      </div>
      <ul class="probe-list">
        <li v-if="!probe.known">
          新设备：将从头读取
          <strong>{{ probe.images.total }}</strong> 张图片、<strong>{{ probe.videos.total }}</strong> 个视频。
        </li>
        <li v-else>
          已知设备：上次同步 {{ formatDate(probe.last_sync_at) }}，本次待同步
          <strong>{{ pendingTotal }}</strong> 项（共 {{ probe.images.total }} 图 +
          {{ probe.videos.total }} 视频）。
        </li>
        <li v-if="probe.known" class="dim">
          已记录的同步水位 {{ probe.last_checkpoint }}，手机当前水位 {{ probe.version }}。
        </li>
        <li v-if="probe.address_changed" class="warn-text">
          这台设备的地址与上次不同（上次记录 {{ devices.find((d) => d.id === probe?.device_id)?.address ?? '—' }}）。
          如果是换了另一台同型号手机，请勾选「全量重扫」或先忘记设备。
        </li>
      </ul>
      <label v-if="probe.known" class="checkbox-row">
        <input v-model="fullMode" type="checkbox" />
        全量重扫（重新列出全部媒体；已同步且内容未变的不会重新下载）
      </label>
    </div>

    <label class="field-label" for="phone-tags">导入标签（可选）</label>
    <TagSelector id="phone-tags" v-model="tags" placeholder="选择或输入标签，仅添加到本次新增的图片" />

    <div class="toolbar sync-row">
      <button
        class="btn btn-primary"
        :disabled="!canSync"
        @click="startSync(fullMode ? 'full' : 'incremental')"
      >
        {{
          starting
            ? '启动中…'
            : probe?.known
              ? fullMode
                ? '开始全量同步'
                : '开始增量同步'
              : '开始同步'
        }}
      </button>
      <span v-if="!probe" class="hint dim">先点「检查连接」</span>
      <span v-else-if="probe.known && !fullMode && pendingTotal === 0" class="hint">
        没有待同步的新内容，仍可点一次确认。
      </span>
    </div>
  </section>

  <p v-if="error" class="banner error">{{ error }}</p>
  <p v-if="notice" class="banner">{{ notice }}</p>

  <section v-if="devices.length" class="panel">
    <div class="panel-head">
      <h2>已连接的手机</h2>
      <span class="dim">{{ devices.length }} 台</span>
    </div>
    <table class="data-table">
      <thead>
        <tr>
          <th>设备</th>
          <th>地址</th>
          <th>上次同步</th>
          <th>同步记录</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="device in devices" :key="device.id">
          <td>
            <strong>{{ device.name }}</strong>
            <div class="dim mono">Android {{ device.android_version }}</div>
          </td>
          <td class="mono ellipsis" :title="device.address">{{ device.address }}</td>
          <td>
            <div>{{ formatDate(device.last_sync_at) }}</div>
            <div v-if="device.last_status" class="dim">
              {{ STATUS_LABELS[device.last_status] ?? device.last_status }}
            </div>
          </td>
          <td class="dim">{{ countsText(device.counts) || '—' }}</td>
          <td class="actions">
            <button
              class="btn btn-sm"
              :disabled="props.busy || busyDevice === device.id || !device.has_token"
              @click="syncDevice(device, 'incremental')"
            >
              增量同步
            </button>
            <button
              class="btn btn-sm"
              :disabled="props.busy || busyDevice === device.id || !device.has_token"
              @click="syncDevice(device, 'full')"
            >
              全量重扫
            </button>
            <button
              class="btn btn-sm btn-danger"
              :disabled="props.busy || busyDevice === device.id"
              @click="forgetDevice(device)"
            >
              忘记
            </button>
          </td>
        </tr>
      </tbody>
    </table>
    <p class="hint dim">
      手机换了 IP 时，上面的「增量同步」会连不上：请在上方填新地址与 Token 重新检查，同一台手机会沿用上次进度。
    </p>
    <p class="hint dim">
      <strong>「忘记」是显式重置入口</strong>：会删除该手机的同步记录（含 Token）与同步账本，
      下次连接将重新导入手机上的<strong>全部</strong>图片——包括你已在 iphoto 删除的图片；
      库内已有图片本身不受影响。只想补新图时不要点它。
    </p>
  </section>
</template>

<style scoped>
.token-input {
  width: 120px;
}

.probe-card {
  margin-top: 12px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
}

.probe-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.probe-list {
  margin: 0;
  padding-left: 18px;
  line-height: 1.8;
}

.checkbox-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  color: var(--text-dim);
  font-size: 13px;
  cursor: pointer;
}

.sync-row {
  margin-top: 12px;
  align-items: center;
}
</style>
