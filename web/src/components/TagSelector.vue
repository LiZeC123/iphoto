<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { errorMessage } from '@/api/settings'
import { tagsApi } from '@/api/tags'
import type { Tag } from '@/api/types'
import { markTagUsed, recentTagNames, sortTagsByRecent } from '@/utils/tagRecency'

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    placeholder?: string
    /** 单选模式：选中一个后替换，而非追加。 */
    single?: boolean
    /** 下拉方向；靠近视口底部时用 top。 */
    placement?: 'bottom' | 'top'
  }>(),
  { placeholder: '输入或选择标签', single: false, placement: 'bottom' },
)
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()

const allTags = ref<Tag[]>([])
const query = ref('')
const open = ref(false)
const activeIndex = ref(-1)
const root = ref<HTMLElement | null>(null)
const inputEl = ref<HTMLInputElement | null>(null)
const error = ref('')

const suggestions = computed(() => {
  const selected = new Set(props.modelValue)
  const q = query.value.trim().toLowerCase()
  const matched = allTags.value
    .filter((tag) => !selected.has(tag.name))
    .filter((tag) => !q || tag.name.toLowerCase().includes(q))
  return sortTagsByRecent(matched).slice(0, 8)
})

const canCreate = computed(() => {
  const q = query.value.trim()
  if (!q) return false
  if (props.modelValue.includes(q)) return false
  return !allTags.value.some((tag) => tag.name.toLowerCase() === q.toLowerCase())
})

// 每次打开都刷新，确保刚创建的标签能立即出现在其它图片的选择器里。
async function load(): Promise<void> {
  try {
    const fetched = (await tagsApi.list()).items
    const known = new Set(fetched.map((tag) => tag.name.toLowerCase()))
    // 本次会话里刚创建、服务端列表可能尚未返回的标签先本地补齐。
    recentTagNames.forEach((name, index) => {
      if (!known.has(name.toLowerCase())) {
        fetched.push({ id: -1000 - index, name, count: 0 })
      }
    })
    allTags.value = fetched
  } catch (err) {
    error.value = errorMessage(err)
  }
}

function openMenu(): void {
  if (!open.value) void load()
  open.value = true
}

function add(name: string): void {
  const value = name.trim()
  if (!value) return
  markTagUsed(value)
  if (!allTags.value.some((tag) => tag.name.toLowerCase() === value.toLowerCase())) {
    allTags.value = [...allTags.value, { id: -1, name: value, count: 0 }]
  }
  if (props.single) {
    emit('update:modelValue', [value])
  } else if (!props.modelValue.includes(value)) {
    emit('update:modelValue', [...props.modelValue, value])
  }
  query.value = ''
  activeIndex.value = -1
  open.value = false
}

function remove(name: string): void {
  emit(
    'update:modelValue',
    props.modelValue.filter((value) => value !== name),
  )
}

function onKeydown(event: KeyboardEvent): void {
  // 阻止冒泡，避免 PhotoSwipe 等全局快捷键拦截输入。
  event.stopPropagation()
  if (event.key === 'Enter') {
    event.preventDefault()
    if (activeIndex.value >= 0 && suggestions.value[activeIndex.value]) {
      add(suggestions.value[activeIndex.value].name)
    } else if (canCreate.value) {
      add(query.value)
    }
    return
  }
  if (event.key === 'Backspace' && !query.value && props.modelValue.length) {
    remove(props.modelValue[props.modelValue.length - 1])
    return
  }
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    open.value = true
    activeIndex.value = Math.min(activeIndex.value + 1, suggestions.value.length - 1)
    return
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    activeIndex.value = Math.max(activeIndex.value - 1, -1)
    return
  }
  if (event.key === 'Escape') {
    open.value = false
  }
}

function onRootClick(event: MouseEvent): void {
  const target = event.target as HTMLElement
  // 选项与删除按钮自己处理点击，避免菜单刚关闭又被根节点重新打开。
  if (target.closest('.chip-remove') || target.closest('.tag-option')) return
  inputEl.value?.focus()
  openMenu()
}

function onDocumentPointer(event: PointerEvent): void {
  if (root.value && !root.value.contains(event.target as Node)) {
    open.value = false
  }
}

onMounted(() => {
  void load()
  document.addEventListener('pointerdown', onDocumentPointer)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointer)
})
</script>

<template>
  <div ref="root" class="tag-select" @click="onRootClick">
    <span v-for="name in modelValue" :key="name" class="tag-chip">
      {{ name }}
      <button type="button" class="chip-remove" :aria-label="`移除 ${name}`" @click.stop="remove(name)">
        ×
      </button>
    </span>
    <input
      ref="inputEl"
      v-model="query"
      class="tag-input"
      :placeholder="modelValue.length ? '' : placeholder"
      @focus="openMenu"
      @input="open = true"
      @keydown="onKeydown"
    />
    <ul
      v-if="open && (suggestions.length || canCreate)"
      class="tag-menu"
      :class="{ top: placement === 'top' }"
    >
      <li
        v-if="canCreate"
        class="tag-option create"
        :class="{ active: activeIndex === -1 }"
        @mousedown.prevent="add(query)"
      >
        创建「{{ query.trim() }}」
      </li>
      <li
        v-for="(tag, index) in suggestions"
        :key="`${tag.id}-${tag.name}`"
        class="tag-option"
        :class="{ active: activeIndex === index }"
        @mousedown.prevent="add(tag.name)"
      >
        <span>{{ tag.name }}</span>
        <span class="tag-count">{{ tag.count }}</span>
      </li>
    </ul>
    <p v-if="error" class="tag-error">{{ error }}</p>
  </div>
</template>

<style scoped>
.tag-select {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  min-height: 34px;
  padding: 3px 6px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  cursor: text;
}

.tag-select:focus-within {
  border-color: rgba(76, 141, 255, 0.6);
}

.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  max-width: 180px;
  padding: 2px 4px 2px 8px;
  border-radius: 999px;
  background: rgba(76, 141, 255, 0.16);
  color: var(--accent);
  font-size: 12px;
  line-height: 1.5;
}

.chip-remove {
  padding: 0 2px;
  border: none;
  background: none;
  color: inherit;
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.7;
}

.chip-remove:hover {
  opacity: 1;
}

.tag-input {
  flex: 1;
  min-width: 90px;
  height: 24px;
  padding: 0 4px;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font: inherit;
  font-size: 13px;
}

.tag-menu {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  z-index: 20;
  width: 100%;
  max-height: 220px;
  margin: 0;
  padding: 4px;
  overflow: auto;
  list-style: none;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: #1b1f26;
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.4);
}

.tag-menu.top {
  top: auto;
  bottom: calc(100% + 4px);
}

.tag-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 6px 8px;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
}

.tag-option:hover,
.tag-option.active {
  background: rgba(76, 141, 255, 0.16);
}

.tag-option.create {
  color: var(--accent);
}

.tag-count {
  color: var(--text-dim);
  font-size: 11px;
}

.tag-error {
  width: 100%;
  margin: 2px 0 0;
  color: var(--bad);
  font-size: 12px;
}
</style>
