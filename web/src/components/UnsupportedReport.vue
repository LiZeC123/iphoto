<script setup lang="ts">
import type { UnsupportedInfo } from '@/api/types'

defineProps<{ info: UnsupportedInfo }>()
</script>

<template>
  <div v-if="info.total" class="unsupported">
    <p class="unsupported-head">
      <strong>{{ info.total }}</strong> 个文件因类型不支持被跳过。
      <span class="dim">它们不会进入图库，也不会出现在导入明细里。</span>
    </p>
    <p class="ext-list">
      <span class="dim">按后缀：</span>
      <span v-for="item in info.by_ext" :key="item.ext" class="ext-chip">
        <span class="mono">{{ item.ext }}</span> × {{ item.count }}
      </span>
    </p>
    <details v-if="info.files.length" class="files">
      <summary>查看文件列表（{{ info.files.length }} / {{ info.total }}）</summary>
      <ul>
        <li v-for="file in info.files" :key="file" class="mono">{{ file }}</li>
      </ul>
      <p v-if="info.truncated" class="dim">
        仅列出前 {{ info.files.length }} 个，共 {{ info.total }} 个不支持的文件。
      </p>
    </details>
  </div>
</template>

<style scoped>
.unsupported {
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid rgba(255, 176, 76, 0.35);
  border-radius: 8px;
  background: rgba(255, 176, 76, 0.08);
  font-size: 13px;
}

.unsupported-head {
  margin: 0 0 8px;
}

.ext-list {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin: 0 0 8px;
}

.ext-chip {
  padding: 2px 8px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.07);
  font-size: 12px;
}

.files summary {
  color: var(--text-dim);
  font-size: 12px;
  cursor: pointer;
}

.files ul {
  max-height: 240px;
  margin: 8px 0;
  padding-left: 18px;
  overflow: auto;
}

.files li {
  line-height: 1.7;
  overflow-wrap: anywhere;
}
</style>
