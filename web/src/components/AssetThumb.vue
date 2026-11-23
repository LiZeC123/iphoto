<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{ hash: string; alt?: string; mime?: string }>()

const isVideo = computed(() => (props.mime ?? '').toLowerCase().startsWith('video/'))

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
  <div
    v-else
    class="thumb-placeholder"
    :class="{ 'video-placeholder': isVideo }"
    :title="
      isVideo
        ? '视频缩略图需要 ffmpeg；未安装时仍可点开在详情页播放'
        : '该图片暂时没有缩略图（例如动图 WebP 或文件损坏）'
    "
  >
    {{ isVideo ? '▶ 视频' : '暂无缩略图' }}
  </div>
</template>
