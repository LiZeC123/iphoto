<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { fetchHealth, type Health } from '@/api/health'
import { importApi } from '@/api/import'
import { errorMessage, settingsApi } from '@/api/settings'
import { timeRulesApi } from '@/api/timeRules'
import type { ImportBatch, TimeRule, VerifyReport } from '@/api/types'
import PageShell from '@/components/PageShell.vue'
import { formatDate } from '@/utils/format'

const health = ref<Health | null>(null)
const rules = ref<TimeRule[]>([])
const batches = ref<ImportBatch[]>([])
const error = ref('')
const notice = ref('')

const ruleForm = ref({ name: '', pattern: '', priority: 100 })
const testPattern = ref(`IMG_(?P<year>\\d{4})(?P<month>\\d{2})(?P<day>\\d{2})_(?P<hour>\\d{2})(?P<minute>\\d{2})(?P<second>\\d{2})`)
const testFilename = ref('IMG_20230501_120000.jpg')
const testResult = ref<{ matched: boolean; takenAt: string } | null>(null)
const testError = ref('')

const verifyFull = ref(false)
const verifying = ref(false)
const report = ref<VerifyReport | null>(null)

const busy = ref(false)

async function loadHealth(): Promise<void> {
  try {
    health.value = await fetchHealth()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function loadRules(): Promise<void> {
  try {
    rules.value = (await timeRulesApi.list()).items
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function loadBatches(): Promise<void> {
  try {
    batches.value = (await settingsApi.batches()).items
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function addRule(): Promise<void> {
  error.value = ''
  notice.value = ''
  if (!ruleForm.value.name.trim() || !ruleForm.value.pattern.trim()) {
    error.value = '规则名与正则都不能为空'
    return
  }
  busy.value = true
  try {
    await timeRulesApi.create({
      name: ruleForm.value.name.trim(),
      pattern: ruleForm.value.pattern.trim(),
      priority: Number(ruleForm.value.priority) || 100,
      enabled: true,
    })
    notice.value = '规则已创建'
    ruleForm.value = { name: '', pattern: '', priority: 100 }
    await loadRules()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    busy.value = false
  }
}

async function toggleRule(rule: TimeRule): Promise<void> {
  try {
    await timeRulesApi.update(rule.id, { enabled: !rule.enabled })
    await loadRules()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function deleteRule(rule: TimeRule): Promise<void> {
  if (!window.confirm(`删除规则「${rule.name}」？`)) return
  try {
    await timeRulesApi.remove(rule.id)
    await loadRules()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function runTest(): Promise<void> {
  testError.value = ''
  testResult.value = null
  if (!testPattern.value.trim() || !testFilename.value.trim()) {
    testError.value = '正则与文件名都不能为空'
    return
  }
  try {
    const res = await timeRulesApi.test(testPattern.value.trim(), testFilename.value.trim())
    testResult.value = { matched: res.matched, takenAt: res.taken_at }
  } catch (err) {
    testError.value = errorMessage(err)
  }
}

async function verify(): Promise<void> {
  verifying.value = true
  report.value = null
  error.value = ''
  try {
    report.value = await settingsApi.verify(verifyFull.value)
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    verifying.value = false
  }
}

async function cleanupBatch(batch: ImportBatch): Promise<void> {
  if (!window.confirm(`清理批次 #${batch.id} 的导入明细？CSV 报告将不可用（图片不受影响）。`)) return
  try {
    const res = await importApi.cleanup(batch.id)
    notice.value = `已清理 ${res.deleted} 条导入明细`
    await loadBatches()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

onMounted(async () => {
  await Promise.all([loadHealth(), loadRules(), loadBatches()])
})
</script>

<template>
  <PageShell title="设置" subtitle="运行信息、时间解析规则、完整性与导入历史">
    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <section class="panel">
      <div class="panel-head"><h2>运行信息</h2></div>
      <dl v-if="health" class="kv">
        <dt>版本</dt><dd>{{ health.version }}（schema v{{ health.schema_version }}）</dd>
        <dt>监听</dt><dd>127.0.0.1:{{ health.port }}</dd>
        <dt>library</dt><dd class="mono">{{ health.library }}</dd>
        <dt>runtime</dt><dd class="mono">{{ health.runtime }}</dd>
      </dl>
      <p class="hint dim">
        备份：停止服务后整体复制 library/ 即可；runtime/（缩略图、回收站、日志、导出）不需要备份。端口与路径写死，不可在运行时修改。
      </p>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>完整性校验</h2>
        <label class="inline-check">
          <input v-model="verifyFull" type="checkbox" />
          重算 SHA-256（较慢）
        </label>
        <button class="btn btn-primary" :disabled="verifying" @click="verify">
          {{ verifying ? '校验中…' : '开始校验' }}
        </button>
      </div>
      <p class="hint dim">
        检查 ready 图片的文件是否存在、大小是否一致，扫描 CAS 中的孤儿文件；缺失记录会标记为 missing，不会自动删除。
      </p>
      <div v-if="report" class="verify-report">
        <p>
          检查 {{ report.checked }} 张，耗时 {{ report.duration_ms }}ms
          <template v-if="report.hashed">，重算哈希 {{ report.hashed }} 张</template>
          ；标记 missing {{ report.marked_missing }} 张。
        </p>
        <p v-if="report.missing.length" class="issue-line">
          缺失 {{ report.missing.length }}：
          <span class="mono">{{ report.missing.map((i) => i.original_name).join('、') }}</span>
        </p>
        <p v-if="report.size_mismatch.length" class="issue-line">
          大小不符 {{ report.size_mismatch.length }}：
          <span class="mono">{{ report.size_mismatch.map((i) => i.original_name).join('、') }}</span>
        </p>
        <p v-if="report.hash_mismatch.length" class="issue-line">
          哈希不符 {{ report.hash_mismatch.length }}：
          <span class="mono">{{ report.hash_mismatch.map((i) => i.original_name).join('、') }}</span>
        </p>
        <p v-if="report.orphans.length" class="issue-line">
          孤儿文件 {{ report.orphans.length }}：
          <span class="mono">{{ report.orphans.slice(0, 10).join('、') }}</span>
        </p>
        <p v-if="!report.missing.length && !report.size_mismatch.length && !report.hash_mismatch.length && !report.orphans.length" class="ok-line">
          未发现问题。
        </p>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h2>时间解析规则</h2></div>
      <table class="data-table">
        <thead>
          <tr><th>名称</th><th>正则</th><th class="prio-col">优先级</th><th class="actions-col">操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="rule in rules" :key="rule.id">
            <td>{{ rule.name }}</td>
            <td class="mono ellipsis" :title="rule.pattern">{{ rule.pattern }}</td>
            <td class="dim">{{ rule.priority }}</td>
            <td class="actions">
              <button class="btn btn-sm" @click="toggleRule(rule)">
                {{ rule.enabled ? '禁用' : '启用' }}
              </button>
              <button class="btn btn-sm btn-danger" @click="deleteRule(rule)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>

      <div class="rule-form">
        <input v-model="ruleForm.name" class="input" placeholder="规则名" />
        <input v-model="ruleForm.pattern" class="input grow mono" placeholder="正则（命名组 year/month/day/hour/minute/second）" />
        <input v-model.number="ruleForm.priority" class="input prio-input" type="number" min="1" max="10000" />
        <button class="btn btn-primary" :disabled="busy" @click="addRule">添加规则</button>
      </div>

      <div class="rule-form">
        <input v-model="testPattern" class="input grow mono" placeholder="测试正则" />
        <input v-model="testFilename" class="input" placeholder="文件名" />
        <button class="btn" @click="runTest">测试</button>
      </div>
      <p v-if="testError" class="banner error">{{ testError }}</p>
      <p v-if="testResult" class="hint">
        <template v-if="testResult.matched">匹配成功，解析结果：<strong>{{ testResult.takenAt }}</strong></template>
        <template v-else>未匹配</template>
      </p>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>导入历史</h2>
        <button class="btn btn-sm" @click="loadBatches">刷新</button>
      </div>
      <table v-if="batches.length" class="data-table">
        <thead>
          <tr><th>批次</th><th>源目录</th><th>状态</th><th>明细</th><th>开始时间</th><th class="actions-col">操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="batch in batches" :key="batch.id">
            <td>#{{ batch.id }}</td>
            <td class="mono ellipsis" :title="batch.source_path">{{ batch.source_path }}</td>
            <td class="dim">{{ batch.status }}</td>
            <td class="dim">{{ batch.item_count }}</td>
            <td class="dim">{{ formatDate(batch.started_at) }}</td>
            <td class="actions">
              <button class="btn btn-sm" :disabled="!batch.item_count" @click="cleanupBatch(batch)">清理明细</button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="dim">暂无导入记录。</p>
    </section>
  </PageShell>
</template>

<style scoped>
.kv {
  display: grid;
  grid-template-columns: 72px 1fr;
  gap: 6px 12px;
  margin: 0;
  font-size: 13px;
}

.kv dt {
  color: var(--text-dim);
}

.kv dd {
  margin: 0;
  overflow-wrap: anywhere;
}

.inline-check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text-dim);
  font-size: 12px;
}

.verify-report {
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 13px;
}

.verify-report p {
  margin: 4px 0;
}

.issue-line {
  color: #ffb4b4;
}

.ok-line {
  color: var(--ok);
}

.prio-col {
  width: 80px;
}

.actions-col {
  width: 150px;
}

.rule-form {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

.prio-input {
  width: 90px;
}

.mono.input {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
</style>
