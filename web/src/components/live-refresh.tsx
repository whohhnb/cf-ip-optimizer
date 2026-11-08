import { useEffect, useState } from "react"
import { RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

type Props = {
  /** 上一次成功拉到数据的时间戳，null 表示还没加载完 */
  updatedAt: number | null
  onRefresh: () => void
  /** 暂停时显示灰点，用于日志页那种可以手动停掉的场景 */
  paused?: boolean
}

/**
 * 页面既然会自动刷新，就不该再摆一个「刷新」大按钮——两种模型并存会让人
 * 以为数据是死的。这里改成「更新于 N 秒前」加一个小图标：既说明数据是活的，
 * 又保留了手动兜底。
 */
export function LiveRefresh({ updatedAt, onRefresh, paused = false }: Props) {
  const [, tick] = useState(0)

  useEffect(() => {
    const t = window.setInterval(() => tick((n) => n + 1), 1000)
    return () => window.clearInterval(t)
  }, [])

  const ago = updatedAt ? Math.max(0, Math.floor((Date.now() - updatedAt) / 1000)) : null
  const text = paused
    ? "已暂停刷新"
    : ago === null
      ? "加载中"
      : ago < 3
        ? "刚刚更新"
        : `更新于 ${ago} 秒前`

  return (
    <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <span
        aria-hidden
        className={cn(
          "size-1.5 rounded-full",
          paused ? "bg-muted-foreground/40" : "bg-emerald-500",
        )}
      />
      <span className="tabular-nums">{text}</span>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onRefresh}
        aria-label="立即刷新"
        title="立即刷新"
      >
        <RefreshCw />
      </Button>
    </div>
  )
}
