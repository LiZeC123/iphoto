import { reactive } from 'vue'

// 最近使用的标签（仅当前会话，不持久化）。用于标签选择器与侧边栏排序。
const MAX_RECENT = 24

export const recentTagNames = reactive<string[]>([])

export function markTagUsed(name: string): void {
  const trimmed = name.trim()
  if (!trimmed) return
  const index = recentTagNames.indexOf(trimmed)
  if (index >= 0) recentTagNames.splice(index, 1)
  recentTagNames.unshift(trimmed)
  if (recentTagNames.length > MAX_RECENT) recentTagNames.length = MAX_RECENT
}

/** 最近使用的标签排前面，其余按图片数、名称排序。 */
export function sortTagsByRecent<T extends { name: string; count?: number }>(tags: T[]): T[] {
  const rank = (name: string): number => {
    const index = recentTagNames.indexOf(name)
    return index < 0 ? Number.MAX_SAFE_INTEGER : index
  }
  return [...tags].sort(
    (a, b) =>
      rank(a.name) - rank(b.name) ||
      (b.count ?? 0) - (a.count ?? 0) ||
      a.name.localeCompare(b.name, 'zh-Hans-CN'),
  )
}
