<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{ hash: string; alt?: string }>()

const url = ref(`/api/assets/${props.hash}/thumb`)
const failed = ref(false)
const retries = ref(0)
let timer: ReturnType<typeof setTimeout> | undefined

// 缩略图由后台队列生成：首次 404 时按退避重试，最终显示占位符。
function onError(): void {
  if (retries.value >= 3) {
    failed.value = true
    return
  }
  retries.value += 1
  timer = setTimeout(() => {
    url.value = `/api/assets/${props.hash}/thumb?retry=${retries.value}`
  }, 700 * retries.value)
}

watch(
  () => props.hash,
  () => {
    failed.value = false
    retries.value = 0
    url.value = `/api/assets/${props.hash}/thumb`
  },
)

onBeforeUnmount(() => {
  if (timer) clearTimeout(timer)
})
</script>

<template>
  <img v-if="!failed" class="thumb-img" :src="url" :alt="alt ?? ''" loading="lazy" @error="onError" />
  <div v-else class="thumb-placeholder">缩略图生成中…</div>
</template>
