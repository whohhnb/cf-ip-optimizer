import { useCallback, useEffect, useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"
import {
  Activity,
  ArrowUpRight,
  Gauge,
  Globe2,
  Loader2,
  Play,
  ServerCog,
  Timer,
} from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { LiveRefresh } from "@/components/live-refresh"
import { lineLabel } from "@/lib/line"
import { fullTime, latestTime, relativeTime } from "@/lib/time"
import { usePoll } from "@/lib/use-poll"
import { api, type DomainRow, type LogEntry, type Overview, type TrendPoint } from "@/lib/api"

const chartConfig = {
  latency: { label: "最优延迟 (ms)", color: "var(--chart-2)" },
  speed: { label: "下载速度 (Mbps)", color: "var(--chart-1)" },
} satisfies ChartConfig


function Metric({
  title,
  value,
  unit,
  hint,
  icon: Icon,
  loading,
}: {
  title: string
  value: string | number
  unit?: string
  hint: string
  icon: React.ComponentType<{ className?: string }>
  loading: boolean
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription className="flex items-center gap-1.5">
          <Icon className="size-3.5" />
          {title}
        </CardDescription>
        {loading ? (
          <Skeleton className="h-8 w-20" />
        ) : (
          <CardTitle className="text-2xl tabular-nums">
            {value}
            {unit ? (
              <span className="ml-1 text-sm font-normal text-muted-foreground">{unit}</span>
            ) : null}
          </CardTitle>
        )}
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  )
}

export default function DashboardPage() {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [domains, setDomains] = useState<DomainRow[]>([])
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)

  const load = useCallback(async (silent = false) => {
    try {
      const [ov, tr, dm, lg] = await Promise.all([
        api.overview(),
        api.trend(),
        api.domains(),
        api.logs(8),
      ])
      setOverview(ov)
      setTrend(tr.points ?? [])
      setDomains(dm.domains ?? [])
      setLogs(lg.logs ?? [])
    } catch (err) {
      if (!silent) toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
      setUpdatedAt(Date.now())
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // 扫描进行中时指标卡和域名表要跟着动，8 秒一次足够，且只在页面可见时跑
  usePoll(() => load(true), 8000)

  // 一个主机名的多条线路在库里是独立配置，首页按主机名收成一行，
  // 线路用带状态色的徽章排开，一眼看出哪条挂了
  const groups = useMemo(() => {
    const order = ["默认", "电信", "联通", "移动", "教育网", "境外"]
    const rank = (v: string) => {
      const i = order.indexOf(v)
      return i < 0 ? 99 : i
    }
    const map = new Map<string, DomainRow[]>()
    for (const r of domains) {
      const arr = map.get(r.full) ?? []
      arr.push(r)
      map.set(r.full, arr)
    }
    return [...map.entries()]
      .map(([full, lines]) => ({
        full,
        lines: [...lines].sort(
          (a, b) =>
            rank(lineLabel(a.domain.recordLine, a.provider.lines)) -
            rank(lineLabel(b.domain.recordLine, b.provider.lines)),
        ),
      }))
      .sort((a, b) => a.full.localeCompare(b.full))
  }, [domains])

  async function scanGroup(g: { full: string; lines: DomainRow[] }) {
    let ok = 0
    const failed: string[] = []
    for (const r of g.lines) {
      try {
        await api.scan(r.domain.id)
        ok++
      } catch (err) {
        failed.push(
          `${lineLabel(r.domain.recordLine, r.provider.lines)}：${
            err instanceof Error ? err.message : "触发失败"
          }`,
        )
      }
    }
    if (ok > 0) toast.success(`已触发 ${ok} 条线路的扫描`)
    if (failed.length > 0) toast.error(failed.join("；"))
    void load(true)
  }



  const chartData = trend.map((p) => ({
    time: new Date(p.t * 1000).toLocaleTimeString("zh-CN", {
      hour: "2-digit",
      minute: "2-digit",
    }),
    latency: Number(p.latency.toFixed(1)),
    speed: Number(p.speed.toFixed(1)),
  }))

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">运行总览</h1>
          <p className="text-sm text-muted-foreground">
            实时掌握各域名的优选 IP 与解析下发状态
          </p>
        </div>
        <LiveRefresh updatedAt={updatedAt} onRefresh={() => void load()} />
      </div>

      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        {/* 一个主机名可能挂多条线路，这里按主机名数，hint 里再交代线路明细，
            否则「4 个域名」和下面只有一行表对不上 */}
        <Metric
          title="调度主机"
          value={groups.length}
          icon={Globe2}
          loading={loading}
          hint={(() => {
            const total = domains.length
            const healthy = domains.filter((r) => r.domain.status === "ok").length
            const scanning = domains.filter((r) => r.domain.status === "scanning").length
            const broken = domains.filter((r) => r.domain.status === "error").length
            const parts = [`${total} 条线路`, `${healthy} 健康`]
            if (broken > 0) parts.push(`${broken} 异常`)
            if (scanning > 0) parts.push(`${scanning} 扫描中`)
            return parts.join(" · ")
          })()}
        />
        <Metric
          title="平均最优延迟"
          value={overview?.avgLatency ? overview.avgLatency.toFixed(0) : "—"}
          unit="ms"
          hint="近 7 天所有扫描的最优 IP 延迟均值"
          icon={Timer}
          loading={loading}
        />
        <Metric
          title="历史最佳延迟"
          value={overview?.bestLatency ? overview.bestLatency.toFixed(0) : "—"}
          unit="ms"
          hint="近 7 天出现过的最优记录"
          icon={Gauge}
          loading={loading}
        />
        <Metric
          title="今日下发次数"
          value={overview?.pushesToday ?? 0}
          hint="今天完成下发的扫描任务数，跨全部服务商"
          icon={ServerCog}
          loading={loading}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Activity className="size-4" />
            优选趋势
          </CardTitle>
          <CardDescription>每次扫描得到的最优 IP 的延迟与下载速度</CardDescription>
        </CardHeader>
        <CardContent>
          {chartData.length < 2 ? (
            <div className="flex h-[240px] flex-col items-center justify-center gap-1 text-sm text-muted-foreground">
              <span>
                {chartData.length === 0 ? "暂无扫描数据" : "已有 1 次扫描结果"}
              </span>
              <span className="text-xs">
                {chartData.length === 0
                  ? "添加域名并执行一次扫描后即可看到趋势"
                  : "再完成一次扫描就能绘制趋势曲线"}
              </span>
            </div>
          ) : (
            <ChartContainer config={chartConfig} className="h-[240px] w-full">
              <AreaChart data={chartData} margin={{ left: 4, right: 8, top: 8 }}>
                <defs>
                  <linearGradient id="fillLatency" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--color-latency)" stopOpacity={0.35} />
                    <stop offset="95%" stopColor="var(--color-latency)" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="time" tickLine={false} axisLine={false} tickMargin={8} />
                <YAxis tickLine={false} axisLine={false} width={40} />
                <ChartTooltip cursor={false} content={<ChartTooltipContent />} />
                <Area
                  dataKey="latency"
                  type="monotone"
                  fill="url(#fillLatency)"
                  stroke="var(--color-latency)"
                  strokeWidth={2}
                  dot={{ r: 3, fill: "var(--color-latency)", strokeWidth: 0 }}
                  activeDot={{ r: 4 }}
                />
              </AreaChart>
            </ChartContainer>
          )}
        </CardContent>
      </Card>

      <div className="grid items-start gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">域名调度</CardTitle>
            <CardDescription>当前生效的优选结果</CardDescription>
            <CardAction>
              <Button variant="ghost" size="sm" render={<Link to="/domains" />}>
                管理
                <ArrowUpRight />
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {loading ? (
              <Skeleton className="h-40 w-full" />
            ) : domains.length === 0 ? (
              <div className="flex h-40 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
                还没有配置域名
                <Button size="sm" render={<Link to="/domains" />}>
                  立即添加
                </Button>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>域名</TableHead>
                    <TableHead>线路</TableHead>
                    <TableHead className="hidden text-right sm:table-cell">上次扫描</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {groups.map((g) => {
                    const scanning = g.lines.some(
                      (r) => r.running || r.domain.status === "scanning",
                    )
                    const latest = latestTime(g.lines.map((r) => r.domain.lastScanAt))
                    return (
                      <TableRow key={g.full}>
                        <TableCell>
                          <Link
                            to={`/domains/${g.lines[0].domain.id}`}
                            className="font-medium hover:underline max-sm:inline-block max-sm:py-1"
                          >
                            {g.full}
                          </Link>
                          <div className="text-xs text-muted-foreground">
                            {g.lines.length} 条线路 · TTL {g.lines[0].domain.ttl}s
                          </div>
                          <div
                            className="text-xs text-muted-foreground sm:hidden"
                            title={fullTime(latest)}
                          >
                            {relativeTime(latest)}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                            {g.lines.map((r) => {
                              const l = lineLabel(r.domain.recordLine, r.provider.lines)
                              const st = r.domain.status
                              return (
                                <Link
                                  key={r.domain.id}
                                  to={`/domains/${r.domain.id}`}
                                  title={`${l} · ${
                                    st === "error" ? "异常" : st === "scanning" ? "扫描中" : "正常"
                                  }`}
                                >
                                  <Badge
                                    variant={
                                      st === "error"
                                        ? "destructive"
                                        : st === "scanning"
                                          ? "outline"
                                          : "secondary"
                                    }
                                    className="text-[10px] font-normal"
                                  >
                                    {l}
                                  </Badge>
                                </Link>
                              )
                            })}
                          </div>
                        </TableCell>
                        <TableCell
                          className="hidden text-right text-xs text-muted-foreground sm:table-cell"
                          title={fullTime(latest)}
                        >
                          {relativeTime(latest)}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={scanning}
                            onClick={() => void scanGroup(g)}
                          >
                            {scanning ? <Loader2 className="animate-spin" /> : <Play />}
                            {scanning ? "扫描中" : "扫描"}
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">最近动态</CardTitle>
            <CardDescription>扫描与下发事件</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {logs.length === 0 ? (
              <p className="text-sm text-muted-foreground">暂无日志</p>
            ) : (
              logs.map((log) => (
                <div key={log.id} className="flex flex-col gap-0.5 border-b pb-2 last:border-0 last:pb-0">
                  <div className="flex items-center gap-2">
                    <Badge
                      variant={
                        log.level === "error"
                          ? "destructive"
                          : log.level === "warn"
                            ? "outline"
                            : "secondary"
                      }
                    >
                      {log.level}
                    </Badge>
                    <span className="text-xs text-muted-foreground">
                      {new Date(log.createdAt).toLocaleTimeString("zh-CN")}
                    </span>
                  </div>
                  <p className="text-sm leading-snug">{log.message}</p>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
