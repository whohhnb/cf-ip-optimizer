import { useCallback, useEffect, useState } from "react"
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
import { usePoll } from "@/lib/use-poll"
import { api, type DomainRow, type LogEntry, type Overview, type TrendPoint } from "@/lib/api"

const chartConfig = {
  latency: { label: "最优延迟 (ms)", color: "var(--chart-2)" },
  speed: { label: "下载速度 (Mbps)", color: "var(--chart-1)" },
} satisfies ChartConfig

function statusBadge(status: string) {
  switch (status) {
    case "ok":
      return <Badge variant="secondary">正常</Badge>
    case "scanning":
      return (
        <Badge variant="outline">
          <Loader2 className="animate-spin" />
          扫描中
        </Badge>
      )
    case "error":
      return <Badge variant="destructive">异常</Badge>
    default:
      return <Badge variant="outline">待扫描</Badge>
  }
}

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
  const [busyId, setBusyId] = useState<number | null>(null)

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

  async function scan(id: number) {
    setBusyId(id)
    try {
      await api.scan(id)
      toast.success("已开始扫描，稍后自动下发最优 IP")
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "触发失败")
    } finally {
      setBusyId(null)
    }
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
        <Metric
          title="调度域名"
          value={overview?.domains ?? 0}
          hint={(() => {
            const total = overview?.domains ?? 0
            const healthy = overview?.healthy ?? 0
            const scanning = overview?.scanning ?? 0
            const broken = Math.max(0, total - healthy - scanning)
            const parts = [`${healthy} 个健康`]
            if (broken > 0) parts.push(`${broken} 个异常`)
            parts.push(`${scanning} 个扫描中`)
            return parts.join(" · ")
          })()}
          icon={Globe2}
          loading={loading}
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
                    <TableHead>状态</TableHead>
                    <TableHead className="hidden text-right sm:table-cell">上次扫描</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {domains.map((row) => (
                    <TableRow key={row.domain.id}>
                      <TableCell>
                        <Link
                          to={`/domains/${row.domain.id}`}
                          className="font-medium hover:underline max-sm:inline-block max-sm:py-1"
                        >
                          {row.full}
                        </Link>
                        {/* 一个主机名有多条线路，不标出来四行长得一模一样 */}
                        <Badge variant="secondary" className="ml-2 text-[10px] font-normal">
                          {lineLabel(row.domain.recordLine, row.provider.lines)}
                        </Badge>
                        <div className="text-xs text-muted-foreground">
                          {row.domain.slots} 条 A 记录 · TTL {row.domain.ttl}s
                        </div>
                        <div className="text-xs text-muted-foreground sm:hidden">
                          {row.domain.lastScanAt
                            ? new Date(row.domain.lastScanAt).toLocaleString("zh-CN")
                            : "从未扫描"}
                        </div>
                      </TableCell>
                      <TableCell>{statusBadge(row.domain.status)}</TableCell>
                      <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                        {row.domain.lastScanAt
                          ? new Date(row.domain.lastScanAt).toLocaleString("zh-CN")
                          : "从未"}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busyId === row.domain.id || row.running}
                          onClick={() => void scan(row.domain.id)}
                        >
                          {busyId === row.domain.id || row.running ? (
                            <Loader2 className="animate-spin" />
                          ) : (
                            <Play />
                          )}
                          扫描
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
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
