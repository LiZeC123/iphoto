export interface SmartTagNode {
  type: 'tag'
  value: string
}

export interface SmartTimeNode {
  type: 'time_range'
  from?: string
  to?: string
}

export type SmartNode = SmartTagNode | SmartTimeNode

export interface SmartRule {
  op: 'and'
  children: SmartNode[]
}

export function buildRule(tag: string, from: string, to: string): SmartRule | null {
  const children: SmartNode[] = []
  if (tag.trim()) children.push({ type: 'tag', value: tag.trim() })
  if (from || to) {
    const time: SmartTimeNode = { type: 'time_range' }
    if (from) time.from = from
    if (to) time.to = to
    children.push(time)
  }
  if (!children.length) return null
  return { op: 'and', children }
}

export function parseRule(json: string | null): SmartRule | null {
  if (!json) return null
  try {
    const parsed = JSON.parse(json) as SmartRule
    return parsed && parsed.op === 'and' && Array.isArray(parsed.children) ? parsed : null
  } catch {
    return null
  }
}

export function ruleSummary(json: string | null): string {
  const rule = parseRule(json)
  if (!rule) return '规则无效'
  const parts: string[] = []
  for (const node of rule.children) {
    if (node.type === 'tag') parts.push(`标签「${node.value}」`)
    else if (node.type === 'time_range') {
      if (node.from && node.to) parts.push(`时间 ${node.from} ~ ${node.to}`)
      else if (node.from) parts.push(`时间 ≥ ${node.from}`)
      else if (node.to) parts.push(`时间 ≤ ${node.to}`)
    }
  }
  return parts.length ? parts.join(' 且 ') : '空规则'
}
