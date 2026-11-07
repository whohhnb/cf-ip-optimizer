import { ArrowDown, ArrowRight, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { cn } from "@/lib/utils"

type Props = {
  slots?: number
  className?: string
  showTitle?: boolean
  /** "column" for narrow dialogs, "row" for wide cards */
  variant?: "column" | "row"
}

function Chip({ label, fresh }: { label: string; fresh?: boolean }) {
  return (
    <Badge
      variant={fresh ? "default" : "outline"}
      className={cn("px-1.5 font-mono text-[11px]", fresh && "shadow-sm")}
    >
      {label}
    </Badge>
  )
}

function Group({ label, values, fresh }: { label: string; values: string[]; fresh: number }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="w-10 shrink-0 text-[11px] text-muted-foreground">{label}</span>
      {values.map((v, i) => (
        <Chip key={v} label={v} fresh={i < fresh} />
      ))}
    </div>
  )
}

/**
 * Visual explainer for the rolling-replace strategy. Composed purely from
 * shadcn primitives so it inherits the base-nova theme.
 */
export function RotationHint({ slots = 2, className, showTitle = true, variant = "column" }: Props) {
  if (slots < 2) {
    return (
      <div className={cn("rounded-lg border bg-muted/40 p-3 text-xs", className)}>
        <div className="flex items-center gap-1.5 font-medium">
          <RefreshCw className="size-3.5" />
          单条记录模式
        </div>
        <p className="mt-1.5 text-muted-foreground">
          只维护 1 条 A 记录，切换瞬间会有最长 TTL 的解析空窗，建议 TTL 调小。
        </p>
      </div>
    )
  }

  const footer = (
    <p className="text-[11px] leading-relaxed text-muted-foreground">
      任何时刻都至少有 1 条记录指向可用 IP，代价是最优解要等下一轮才完全生效。
    </p>
  )

  if (variant === "row") {
    return (
      <div className={cn("rounded-lg border bg-muted/40 p-3", className)}>
        {showTitle ? (
          <div className="mb-2 flex items-center gap-1.5 text-xs font-medium">
            <RefreshCw className="size-3.5" />
            滚动替换 · 每轮只换 1 条
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Group label="当前" values={["A", "B"]} fresh={0} />
          <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
          <Group label="轮次 1" values={["C", "B"]} fresh={1} />
          <ArrowRight className="size-3.5 shrink-0 text-muted-foreground" />
          <Group label="轮次 2" values={["C", "D"]} fresh={2} />
          <Separator orientation="vertical" className="hidden h-6 md:block" />
          <p className="text-[11px] leading-snug text-muted-foreground">
            任何时刻至少 1 条可用，代价是最优解要等下一轮才完全生效。
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className={cn("rounded-lg border bg-muted/40 p-3", className)}>
      {showTitle ? (
        <div className="mb-2.5 flex items-center gap-1.5 text-xs font-medium">
          <RefreshCw className="size-3.5" />
          滚动替换 · 每轮只换 1 条
        </div>
      ) : null}

      <div className="flex flex-col items-start gap-1">
        <Group label="当前" values={["A", "B"]} fresh={0} />
        <div className="flex items-center gap-1.5 pl-10">
          <ArrowDown className="size-3 text-muted-foreground" />
          <span className="text-[11px] text-muted-foreground">下一轮</span>
        </div>
        <Group label="轮次 1" values={["C", "B"]} fresh={1} />
        <div className="flex items-center gap-1.5 pl-10">
          <ArrowDown className="size-3 text-muted-foreground" />
          <span className="text-[11px] text-muted-foreground">再下一轮</span>
        </div>
        <Group label="轮次 2" values={["C", "D"]} fresh={2} />
      </div>

      <Separator className="my-2.5" />
      {footer}
    </div>
  )
}
