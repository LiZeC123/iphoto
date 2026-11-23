<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { errorMessage } from '@/api/settings'
import { tagsApi } from '@/api/tags'
import type { Tag } from '@/api/types'
import PageShell from '@/components/PageShell.vue'

const router = useRouter()

const tags = ref<Tag[]>([])
const untaggedCount = ref(0)
const keyword = ref('')
const loading = ref(false)
const error = ref('')
const notice = ref('')

const filteredTags = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return tags.value
  return tags.value.filter((tag) => tag.name.toLowerCase().includes(q))
})
const subtitle = computed(
  () => `共 ${tags.value.length} 个标签 · ${untaggedCount.value} 张图片未标记`,
)

async function load(): Promise<void> {
  loading.value = true
  try {
    const res = await tagsApi.list()
    tags.value = res.items
    untaggedCount.value = res.untagged_count
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function rename(tag: Tag): Promise<void> {
  const name = window.prompt('重命名标签', tag.name)
  if (!name || !name.trim() || name.trim() === tag.name) return
  try {
    await tagsApi.rename(tag.id, name.trim())
    notice.value = `已重命名为「${name.trim()}」`
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function remove(tag: Tag): Promise<void> {
  if (!window.confirm(`删除标签「${tag.name}」？关联会被移除，图片不受影响。`)) return
  try {
    await tagsApi.remove(tag.id)
    notice.value = `已删除标签「${tag.name}」`
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function openTag(tag: Tag): void {
  void router.push({ path: '/', query: { tag: tag.name } })
}

// 固定的首项：快速排查还没有打标签的图片。
function openUntagged(): void {
  void router.push({ path: '/', query: { untagged: '1' } })
}

onMounted(load)
</script>

<template>
  <PageShell title="标签" :subtitle="subtitle">
    <section class="panel">
      <label class="field-label" for="tag-search">搜索标签</label>
      <div class="toolbar">
        <input
          id="tag-search"
          v-model="keyword"
          class="input grow"
          type="search"
          placeholder="输入标签名关键词，实时筛选下方列表"
          spellcheck="false"
        />
        <button v-if="keyword" class="btn" @click="keyword = ''">清除搜索</button>
      </div>
      <p class="hint dim">
        列表首项固定为「未标记任何标签」，点击可在图库中查看这些图片；搜索只筛选标签行，不影响该入口。
      </p>
    </section>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <table v-if="!loading" class="data-table panel">
      <thead>
        <tr>
          <th>标签</th>
          <th class="count-col">图片数</th>
          <th class="actions-col">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr class="untagged-row">
          <td>
            <button class="link-button" @click="openUntagged">未标记任何标签</button>
            <span class="dim fixed-badge">固定入口</span>
          </td>
          <td class="dim">{{ untaggedCount }}</td>
          <td class="dim">—</td>
        </tr>
        <tr v-for="tag in filteredTags" :key="tag.id">
          <td>
            <button class="link-button" @click="openTag(tag)">{{ tag.name }}</button>
          </td>
          <td class="dim">{{ tag.count }}</td>
          <td class="actions">
            <button class="btn btn-sm" @click="rename(tag)">重命名</button>
            <button class="btn btn-sm btn-danger" @click="remove(tag)">删除</button>
          </td>
        </tr>
      </tbody>
    </table>

    <p v-if="!loading && !tags.length" class="dim empty-tip">
      还没有标签。可以在图库中选择图片后批量打标签（标签选择器支持直接输入新标签名）。
    </p>
    <p v-else-if="!loading && keyword && !filteredTags.length" class="dim empty-tip">
      没有匹配「{{ keyword }}」的标签。
    </p>
  </PageShell>
</template>

<style scoped>
.count-col {
  width: 90px;
}

.actions-col {
  width: 180px;
}

.link-button {
  padding: 0;
  border: none;
  background: none;
  color: var(--accent);
  font: inherit;
  cursor: pointer;
}

.data-table.panel {
  padding: 6px 10px;
}

.untagged-row td {
  background: rgba(76, 141, 255, 0.06);
}

.fixed-badge {
  margin-left: 8px;
  padding: 1px 6px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  font-size: 11px;
}

.empty-tip {
  margin: 4px 0 0;
  font-size: 13px;
}
</style>
