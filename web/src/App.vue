<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import { fetchHealth, type Health } from '@/api/health'
import { tagsApi } from '@/api/tags'
import type { Tag } from '@/api/types'
import { sortTagsByRecent } from '@/utils/tagRecency'

const route = useRoute()

const nav = [
  { to: '/', label: '图库' },
  { to: '/timeline', label: '时间线' },
  { to: '/albums', label: '相册' },
  { to: '/tags', label: '标签' },
  { to: '/import', label: '导入' },
  { to: '/trash', label: '回收站' },
  { to: '/settings', label: '设置' },
]

const health = ref<Health | null>(null)
const healthError = ref('')
const tags = ref<Tag[]>([])
const sidebarTags = computed(() => sortTagsByRecent(tags.value).slice(0, 15))

async function loadTags(): Promise<void> {
  try {
    tags.value = (await tagsApi.list()).items
  } catch {
    // 标签加载失败不影响主导航
  }
}

watch(
  () => route.fullPath,
  () => {
    void loadTags()
  },
)

onMounted(async () => {
  void loadTags()
  try {
    health.value = await fetchHealth()
  } catch (err) {
    healthError.value = err instanceof Error ? err.message : String(err)
  }
})
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark">iP</span>
        <div>
          <strong>iPhoto</strong>
          <small>本地图片库</small>
        </div>
      </div>

      <nav class="nav">
        <RouterLink v-for="item in nav" :key="item.to" :to="item.to" class="nav-link">
          {{ item.label }}
        </RouterLink>
      </nav>

      <div v-if="tags.length" class="sidebar-tags">
        <p class="sidebar-title">标签</p>
        <RouterLink
          v-for="tag in sidebarTags"
          :key="tag.id"
          :to="{ path: '/', query: { tag: tag.name } }"
          class="tag-link"
        >
          <span class="tag-name">{{ tag.name }}</span>
          <span class="tag-badge">{{ tag.count }}</span>
        </RouterLink>
      </div>

      <footer class="sidebar-footer">
        <span v-if="health" class="dot ok" />
        <span v-else-if="healthError" class="dot bad" />
        <span v-else class="dot idle" />
        <span class="status-text">
          {{ health ? `后端 v${health.version}` : healthError ? '后端未连接' : '连接中…' }}
        </span>
      </footer>
    </aside>

    <main class="main">
      <RouterView />
    </main>
  </div>
</template>
