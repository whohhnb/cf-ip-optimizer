/**
 * 列表和卡片上的时间一律用相对时间：`2026/11/8 18:45:13` 这种绝对时间在
 * 一眼扫过去时几乎读不出「多久以前」。需要精确值时用 title 显示原文。
 */
export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return "从未"
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return "从未"
  const diff = Math.max(0, Date.now() - t)
  const s = Math.floor(diff / 1000)
  if (s < 60) return "刚刚"
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d} 天前`
  return new Date(iso).toLocaleDateString("zh-CN")
}

export function fullTime(iso: string | null | undefined): string {
  if (!iso) return "—"
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN")
}

/** 取一组时间里最近的那个，用于「这张卡整体上次扫描」 */
export function latestTime(list: (string | null | undefined)[]): string | null {
  let best: number | null = null
  for (const v of list) {
    if (!v) continue
    const t = new Date(v).getTime()
    if (Number.isNaN(t)) continue
    if (best === null || t > best) best = t
  }
  return best === null ? null : new Date(best).toISOString()
}
