<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { albumsApi } from '@/api/albums'
import { errorMessage } from '@/api/settings'
import type { Album } from '@/api/types'
import AssetThumb from '@/components/AssetThumb.vue'
import PageShell from '@/components/PageShell.vue'
import TagSelector from '@/components/TagSelector.vue'
import { buildRule } from '@/utils/smartRule'

const router = useRouter()

const albums = ref<Album[]>([])
const q = ref('')
const type = ref('')
const loading = ref(false)
const creating = ref(false)
const error = ref('')
const notice = ref('')

const form = ref({
  name: '',
  type: 'manual' as 'manual' | 'smart',
  description: '',
  tags: [] as string[],
  from: '',
  to: '',
})

async function load(): Promise<void> {
  loading.value = true
  try {
    albums.value = (await albumsApi.list(q.value, type.value)).items
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function create(): Promise<void> {
  error.value = ''
  notice.value = ''
  if (!form.value.name.trim()) {
    error.value = '请输入相册名'
    return
  }
  let rule: unknown
  if (form.value.type === 'smart') {
    const built = buildRule(form.value.tags[0] ?? '', form.value.from, form.value.to)
    if (!built) {
      error.value = '智能相册至少需要一个标签或时间范围条件'
      return
    }
    rule = built
  }
  creating.value = true
  try {
    const album = await albumsApi.create({
      name: form.value.name.trim(),
      type: form.value.type,
      description: form.value.description.trim() || undefined,
      rule_json: rule,
    })
    notice.value = `已创建${album.type === 'smart' ? '智能' : ''}相册「${album.name}」`
    form.value = { name: '', type: 'manual', description: '', tags: [], from: '', to: '' }
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    creating.value = false
  }
}

function open(album: Album): void {
  void router.push(`/albums/${album.id}`)
}

onMounted(load)
</script>

<template>
  <PageShell title="相册" :subtitle="`共 ${albums.length} 个相册`">
    <section class="panel">
      <div class="panel-head"><h2>新建相册</h2></div>
      <div class="create-grid">
        <label class="field">
          <span>名称</span>
          <input v-model="form.name" class="input" placeholder="相册名" />
        </label>
        <label class="field">
          <span>类型</span>
          <select v-model="form.type" class="select">
            <option value="manual">普通相册</option>
            <option value="smart">智能相册</option>
          </select>
        </label>
        <label class="field grow-field">
          <span>描述</span>
          <input v-model="form.description" class="input" placeholder="可选" />
        </label>
        <template v-if="form.type === 'smart'">
          <label class="field tag-field">
            <span>标签条件</span>
            <TagSelector v-model="form.tags" single placeholder="选择或输入标签" />
          </label>
          <label class="field">
            <span>从</span>
            <input v-model="form.from" class="input" type="month" />
          </label>
          <label class="field">
            <span>到</span>
            <input v-model="form.to" class="input" type="month" />
          </label>
        </template>
        <button class="btn btn-primary create-btn" :disabled="creating" @click="create">
          {{ creating ? '创建中…' : '创建' }}
        </button>
      </div>
      <p class="hint dim">
        普通相册手动加图；智能相册按「标签 + 时间范围」规则动态查询，成员随图库变化。
      </p>
    </section>

    <div class="toolbar filter-bar">
      <input
        v-model="q"
        class="input grow"
        type="search"
        placeholder="搜索相册名或描述"
        @keyup.enter="load"
      />
      <select v-model="type" class="select" @change="load">
        <option value="">全部类型</option>
        <option value="manual">普通</option>
        <option value="smart">智能</option>
      </select>
      <button class="btn" :disabled="loading" @click="load">刷新</button>
    </div>

    <p v-if="error" class="banner error">{{ error }}</p>
    <p v-if="notice" class="banner">{{ notice }}</p>

    <div v-if="albums.length" class="album-grid">
      <button
        v-for="album in albums"
        :key="album.id"
        type="button"
        class="album-card"
        @click="open(album)"
      >
        <span class="album-cover">
          <AssetThumb v-if="album.cover_hash" :hash="album.cover_hash" :alt="album.name" />
          <span v-else class="album-cover-empty">{{ album.type === 'smart' ? '智能' : '空相册' }}</span>
          <span class="album-type">{{ album.type === 'smart' ? '智能' : '普通' }}</span>
        </span>
        <span class="album-info">
          <strong>{{ album.name }}</strong>
          <span class="dim">{{ album.count }} 张</span>
          <span v-if="album.description" class="album-desc dim">{{ album.description }}</span>
        </span>
      </button>
    </div>

    <div v-else-if="!loading" class="empty">
      <strong>还没有相册</strong>
      <p>在上面创建一个普通相册或智能相册。</p>
    </div>
  </PageShell>
</template>

<style scoped>
.create-grid {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 140px;
  color: var(--text-dim);
  font-size: 12px;
}

.grow-field {
  flex: 1;
  min-width: 200px;
}

.create-btn {
  margin-left: auto;
}

.tag-field {
  min-width: 220px;
}

.album-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 14px;
}

.album-card {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.album-card:hover {
  border-color: #3a4450;
}

.album-cover {
  position: relative;
  display: block;
  overflow: hidden;
  aspect-ratio: 16 / 10;
  background: #0f1115;
}

.album-cover :deep(.thumb-img),
.album-cover :deep(.thumb-placeholder) {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  aspect-ratio: auto;
  object-fit: cover;
}

.album-cover-empty {
  display: grid;
  place-items: center;
  height: 100%;
  position: absolute;
  inset: 0;
  color: var(--text-dim);
  font-size: 12px;
}

.album-type {
  position: absolute;
  top: 8px;
  right: 8px;
  padding: 2px 8px;
  border-radius: 999px;
  background: rgba(20, 22, 26, 0.75);
  color: var(--accent);
  font-size: 11px;
}

.album-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px 12px;
  font-size: 12px;
}

.album-info strong {
  color: var(--text);
  font-size: 14px;
  font-weight: 500;
}

.album-desc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.thumb-img),
:deep(.thumb-placeholder) {
  height: 100%;
  aspect-ratio: auto;
}
</style>
