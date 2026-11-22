<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { errorMessage } from '@/api/settings'
import { tagsApi } from '@/api/tags'
import type { Tag } from '@/api/types'
import PageShell from '@/components/PageShell.vue'

const router = useRouter()

const tags = ref<Tag[]>([])
const newName = ref('')
const loading = ref(false)
const error = ref('')
const notice = ref('')

async function load(): Promise<void> {
  loading.value = true
  try {
    tags.value = (await tagsApi.list()).items
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function create(): Promise<void> {
  error.value = ''
  notice.value = ''
  const name = newName.value.trim()
  if (!name) {
    error.value = '请输入标签名'
    return
  }
  try {
    await tagsApi.create(name)
    newName.value = ''
    notice.value = `已创建标签「${name}」`
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function rename(tag: Tag): Promise<void> {
  const name = window.prompt('重命名标签', tag.name)
  if (!name || !name.trim() || name.trim() === tag.name) return
  try {
    await tagsApi.rename(tag.id, name.trim())
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

async function remove(tag: Tag): Promise<void> {
  if (!window.confirm(`删除标签「${tag.name}」？关联会被移除，图片不受影响。`)) return
  try {
    await tagsApi.remove(tag.id)
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function openTag(tag: Tag): void {
  void router.push({ path: '/', query: { tag: tag.name } })
}

onMounted(load)
</script>

<template>
  <PageShell title="标签" :subtitle="`共 ${tags.length} 个标签`">
    <section class="panel">
      <label class="field-label" for="new-tag">新建标签</label>
      <div class="toolbar">
        <input
          id="new-tag"
          v-model="newName"
          class="input grow"
          placeholder="例如：旅行、家人、2024"
          @keyup.enter="create"
        />
        <button class="btn btn-primary" :disabled="loading" @click="create">创建</button>
      </div>
    </section>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <table v-if="tags.length" class="data-table panel">
      <thead>
        <tr>
          <th>标签</th>
          <th class="count-col">图片数</th>
          <th class="actions-col">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="tag in tags" :key="tag.id">
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

    <div v-else-if="!loading" class="empty">
      <strong>还没有标签</strong>
      <p>先创建一个，然后在图库中批量给图片打标签。</p>
    </div>
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
</style>
