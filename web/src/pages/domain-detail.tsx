import { useCallback, useEffect, useMemo, useState } from "react"
import { Link, useParams } from "react-router-dom"
import {
  AlertTriangle,
  ArrowLeft,
  CloudDownload,
  RefreshCw,
  Download,
  Loader2,
  Rocket,
  Signal,
  Timer,
  Wifi,
} from "lucide-react"
import { toast } from "sonner"
import { LiveRefresh } from "@/components/live-refresh"
import { lineLabel } from "@/lib/line"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  api,
  getToken,
  type Domain,
  type LiveRecord,
  type ManagedRecord,
  type ProviderInfo,
  type ScanEvent,
  type ScanJob,
  type DomainRow,
  type ScanResult,
} from "@/lib/api"

type Detail = {
  domain: Domain
  full: string
  managed: ManagedRecord[]
  job: ScanJob | null
  top: ScanResult[]
  running: boolean
  provider: ProviderInfo
}

export default function DomainDetailPage() {
  const { id } = useParams()
  const domainId = Number(id)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [results, setResults] = useState<ScanResult[]>([])
  const [siblings, setSiblings] = useState<DomainRow[]>([])
  const [jobs, setJobs] = useState<ScanJob[]>([])
  const [records, setRecords] = useState<LiveRecord[]>([])
  const [recordsError, setRecordsError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [scanning, setScanning] = useState(false)
  const [progress, setProgress] = useState<ScanEvent | null>(null)

  const load = useCallback(async () => {
    if (!domainId) return
    try {
      const d = await api.domain(domainId)
      setDetail(d)
      setScanning(d.running)
      // 同一主机名的其他线路：详情页要能直接切过去，否则另外几条没有入口
      api
        .domains()
        .then((r) => setSiblings((r.domains ?? []).filter((x) => x.full === d.full)))
        .catch(() => setSiblings([]))
      const [js, rec] = await Promise.all([
        api.jobs(domainId, 20),
        api
          .records(domainId)
          .then((r) => {
            setRecordsError(null)
            return r
          })
          .catch((e: unknown) => {
            setRecordsError(e instanceof Error ? e.message : "读取解析记录失败")
            return { records: [] as LiveRecord[], full: d.full }
          }),
      ])
      setJobs(js.jobs ?? [])
      setRecords(rec.records ?? [])
      if (d.job) {
        const r = await api.job(d.job.id)
        setResults(r.results ?? [])
      } else {
        setResults([])
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
      setUpdatedAt(Date.now())
    }
  }, [domainId])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!domainId) return

    // 优先靠登录时下发的 HttpOnly Cookie 认证，避免 token 出现在 URL 里
    // 被反代访问日志记下来；Cookie 不可用时才降级到 query 参数重试一次
    let es: EventSource | null = null
    let usedTokenFallback = false

    const connect = (withToken: boolean) => {
      const qs = new URLSearchParams({ domainId: String(domainId) })
      if (withToken) {
        const token = getToken()
        if (token) qs.set("token", token)
      }
      es = new EventSource(`/api/stream?${qs.toString()}`)

      es.onmessage = (evt) => {
        try {
          const data = JSON.parse(evt.data) as ScanEvent
          setProgress(data)
          if (data.type === "done" || data.type === "error") {
            setScanning(false)
            if (data.type === "error") toast.error(data.error ?? "扫描失败")
            else toast.success(data.msg ?? "扫描完成")
            void load()
          }
        } catch {
          /* 忽略无法解析的帧 */
        }
      }

      es.onerror = () => {
        if (!usedTokenFallback) {
          usedTokenFallback = true
          es?.close()
          connect(true)
          return
        }
        // 两次都失败（多半是域名不属于当前用户或登录已过期），别再重连
        es?.close()
      }
    }

    connect(false)
    return () => es?.close()
  }, [domainId, load])

  async function startScan() {
    try {
      setScanning(true)
      setProgress(null)
      await api.scan(domainId)
      toast.success("扫描已启动")
    } catch (err) {
      setScanning(false)
      toast.error(err instanceof Error ? err.message : "触发失败")
    }
  }

  async function pushNow() {
    try {
      const res = await api.push(domainId)
      toast.success(`已下发 ${res.count} 条记录`)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "下发失败")
    }
  }

  const managedIndex = useMemo(() => {
    const byRecord = new Map<string, number>()
    const byValue = new Map<string, number>()
    detail?.managed.forEach((m) => {
      byRecord.set(m.recordId, m.slot + 1)
      byValue.set(m.value, m.slot + 1)
    })
    return { byRecord, byValue }
  }, [detail])

  const slotOf = useCallback(
    (recordId: string, value: string) => {
      const { byRecord, byValue } = managedIndex
      // set-based vendors return one shared record id for every value
      if (detail?.provider.setBased) return byValue.get(value)
      return byRecord.get(recordId) ?? byValue.get(value)
    },
    [managedIndex, detail?.provider.setBased],
  )

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-9 w-64" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (!detail) {
    return (
      <div className="flex flex-col items-center gap-4 py-20 text-sm text-muted-foreground">
        域名不存在或无权访问
        <Button variant="outline" render={<Link to="/domains" />}>
          返回列表
        </Button>
      </div>
    )
  }

  const { domain, full } = detail
  const best = results[0]

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-2">
          <Button variant="ghost" size="sm" className="-ml-2 w-fit" render={<Link to="/domains" />}>
            <ArrowLeft />
            返回
          </Button>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-xl font-semibold">{full}</h1>
            <Badge variant="outline">{detail.provider.label}</Badge>
            {domain.status === "ok" ? (
              <Badge variant="secondary">正常</Badge>
            ) : domain.status === "error" ? (
              <Badge variant="destructive">异常</Badge>
            ) : scanning ? (
              <Badge variant="outline">
                <Loader2 className="animate-spin" />
                扫描中
              </Badge>
            ) : (
              <Badge variant="outline">待扫描</Badge>
            )}
          </div>
          <p className="text-sm text-muted-foreground">
            {domain.slots} 条 A 记录 · TTL {domain.ttl}s · 每 {domain.intervalMin} 分钟扫描 ·{" "}
            {domain.autoPush ? "自动下发" : "手动下发"}
          </p>
          {siblings.length > 1 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-muted-foreground">同一主机的线路</span>
              {siblings.map((sib) => {
                const l = lineLabel(sib.domain.recordLine, sib.provider.lines)
                const current = sib.domain.id === domain.id
                return (
                  <Link
                    key={sib.domain.id}
                    to={`/domains/${sib.domain.id}`}
                    title={`切换到「${l}」线路`}
                  >
                    <Badge
                      variant={current ? "default" : "outline"}
                      className="text-[10px] font-normal"
                    >
                      {l}
                      {sib.domain.status === "error" ? " · 异常" : ""}
                    </Badge>
                  </Link>
                )
              })}
            </div>
          ) : null}
          {domain.status === "error" && domain.statusMsg ? (
            <Alert variant="destructive" className="mt-1">
              <AlertTriangle />
              <AlertTitle>最近一次执行失败</AlertTitle>
              <AlertDescription>{domain.statusMsg}</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          <LiveRefresh updatedAt={updatedAt} onRefresh={() => void load()} />
          <Button variant="outline" onClick={() => void pushNow()}>
            <CloudDownload />
            立即下发
          </Button>
          <Button onClick={() => void startScan()} disabled={scanning}>
            {scanning ? <Loader2 className="animate-spin" /> : <Rocket />}
            {scanning ? "扫描中" : "立即扫描"}
          </Button>
        </div>
      </div>

      {scanning ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <Signal className="size-4" />
              {progress?.stage ?? "准备中"}
            </CardTitle>
            <CardDescription>{progress?.msg ?? "正在采样 Cloudflare 地址段…"}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Progress value={progress?.progress ?? 0} />
            <div className="flex justify-between text-xs text-muted-foreground tabular-nums">
              <span>
                已探测 {progress?.tested ?? 0} / {progress?.total ?? 0}
              </span>
              <span>可用 {progress?.ok ?? 0} 个节点</span>
            </div>
          </CardContent>
        </Card>
      ) : null}

      <div className="grid grid-cols-3 gap-3 sm:gap-4">
        <Card>
          <CardHeader className="pb-1">
            <CardDescription className="flex items-center gap-1.5">
              <Timer className="size-3.5" />
              最优延迟
            </CardDescription>
            <CardTitle className="text-2xl tabular-nums">
              {best ? best.latency.toFixed(0) : "—"}
              <span className="ml-1 text-sm font-normal text-muted-foreground">ms</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            {best ? `来自 ${best.ip}` : "暂无数据"}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1">
            <CardDescription className="flex items-center gap-1.5">
              <Wifi className="size-3.5" />
              下载速度
            </CardDescription>
            <CardTitle className="text-2xl tabular-nums">
              {best && best.speed > 0 ? (
                <>
                  {best.speed.toFixed(1)}
                  <span className="ml-1 text-sm font-normal text-muted-foreground">Mbps</span>
                </>
              ) : (
                <span className="text-muted-foreground">—</span>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            {best && best.speed > 0
              ? best.colo
                ? `机房 ${best.colo}`
                : "机房未知"
              : results.some((r) => r.speed > 0)
                ? "该节点未进入测速"
                : "下载测速未完成"}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1">
            <CardDescription className="flex items-center gap-1.5">
              <Download className="size-3.5" />
              候选 IP
            </CardDescription>
            <CardTitle className="text-2xl tabular-nums">{detail.job?.candidates ?? 0}</CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            实测 {detail.job?.tested ?? 0} 个 · 可用 {detail.job?.ok ?? 0} 个
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <RefreshCw className="size-4" />
            当前生效记录
          </CardTitle>
          <CardDescription>
            {domain.slots < 2
              ? "单条 A 记录模式，替换时会有短暂解析空窗"
              : detail.provider.setBased
                ? "该服务商以记录集为单位写入，两条 IP 在同一记录集内轮换，每次只换一个值"
                : "两条 A 记录轮流替换，每轮只改其中一条"}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center gap-3">
          {detail.managed.length === 0 ? (
            <span className="text-sm text-muted-foreground">
              尚未写入解析，扫描完成后会自动下发
            </span>
          ) : (
            detail.managed.map((m) => (
              <div
                key={m.id}
                className="flex items-center gap-2 rounded-lg border px-3 py-2"
              >
                <Badge variant={m.slot === 0 ? "default" : "outline"}>
                  槽位 {m.slot + 1}
                </Badge>
                <span className="font-mono text-sm">{m.value}</span>
                <span className="text-xs text-muted-foreground">
                  {new Date(m.updatedAt).toLocaleTimeString("zh-CN")}
                </span>
              </div>
            ))
          )}
          {domain.lastPushAt && detail.managed.length > 0 ? (
            <span className="text-xs text-muted-foreground">
              最近下发 {new Date(domain.lastPushAt).toLocaleString("zh-CN")}
            </span>
          ) : null}
        </CardContent>
      </Card>

      <Tabs defaultValue="results">
        <TabsList>
          <TabsTrigger value="results">测速结果</TabsTrigger>
          <TabsTrigger value="records">当前解析</TabsTrigger>
          <TabsTrigger value="history">历史任务</TabsTrigger>
        </TabsList>

        <TabsContent value="results" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">节点排名</CardTitle>
              <CardDescription>
                综合延迟 45% · 下载 30% · 丢包 25% 加权评分。只对延迟最优的前几个节点测速，
                没测的显示 —；机房要等 HTTP 校验通过才有
              </CardDescription>
              {detail.job ? (
                <CardAction>
                  <Button
                    variant="outline"
                    size="sm"
                    render={<a href={`/api/jobs/${detail.job.id}/export.csv`} download />}
                  >
                    <Download />
                    导出 CSV
                  </Button>
                </CardAction>
              ) : null}
            </CardHeader>
            <CardContent>
              {results.length === 0 ? (
                <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
                  还没有扫描结果
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-12">#</TableHead>
                      <TableHead>IP</TableHead>
                      <TableHead className="text-right">延迟</TableHead>
                      <TableHead className="hidden text-right lg:table-cell">抖动</TableHead>
                      <TableHead className="hidden text-right lg:table-cell">丢包</TableHead>
                      <TableHead className="hidden text-right lg:table-cell">HTTP 往返</TableHead>
                      <TableHead className="hidden text-right md:table-cell">下载</TableHead>
                      <TableHead className="hidden md:table-cell">机房</TableHead>
                      <TableHead className="text-right">评分</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {results.slice(0, 30).map((r, i) => (
                      <TableRow key={r.id}>
                        <TableCell className="text-muted-foreground tabular-nums">
                          {i + 1}
                        </TableCell>
                        <TableCell className="font-mono text-xs">{r.ip}</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {r.latency.toFixed(0)} ms
                        </TableCell>
                        <TableCell className="hidden text-right tabular-nums text-muted-foreground lg:table-cell">
                          {r.jitter.toFixed(0)} ms
                        </TableCell>
                        <TableCell className="hidden text-right tabular-nums text-muted-foreground lg:table-cell">
                          {r.loss.toFixed(0)}%
                        </TableCell>
                        <TableCell className="hidden text-right tabular-nums text-muted-foreground lg:table-cell">
                          {r.httpRtt > 0 ? `${r.httpRtt.toFixed(0)} ms` : "—"}
                        </TableCell>
                        <TableCell className="hidden text-right tabular-nums md:table-cell">
                          {r.speed > 0 ? `${r.speed.toFixed(1)} Mbps` : "—"}
                        </TableCell>
                        <TableCell className="hidden md:table-cell">
                          {r.colo ? <Badge variant="outline">{r.colo}</Badge> : "—"}
                        </TableCell>
                        <TableCell className="text-right font-medium tabular-nums">
                          {r.score.toFixed(1)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="records" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{detail.provider.label} 实时解析</CardTitle>
              <CardDescription>
                {detail.provider.noList
                  ? "该服务商只接收下发、不支持回读，实际记录请到回调脚本侧核对"
                  : detail.provider.setBased
                    ? "该服务商一条记录内含多个值，平台整体回写；非平台托管的记录不会被改动"
                    : "标记为「平台托管」的记录会被自动维护，其余记录不会被动到"}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {records.length === 0 ? (
                <div className="flex h-32 flex-col items-center justify-center gap-1 px-4 text-center">
                  {recordsError ? (
                    <>
                      <span className="text-sm text-destructive">读取失败：{recordsError}</span>
                      <span className="text-xs text-muted-foreground">
                        请检查该账号的凭据权限，或点右上角刷新重试
                      </span>
                    </>
                  ) : (
                    <>
                      <span className="text-sm text-muted-foreground">
                        {detail.provider.noList ? "该服务商不支持回读记录" : "未读取到 A 记录"}
                      </span>
                      {!detail.provider.noList ? (
                        <span className="text-xs text-muted-foreground">
                          若域名尚未下发，点上方「立即下发」后再刷新
                        </span>
                      ) : null}
                    </>
                  )}
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>主机记录</TableHead>
                      <TableHead>记录值</TableHead>
                      <TableHead className="hidden md:table-cell">线路</TableHead>
                      <TableHead className="hidden md:table-cell">TTL</TableHead>
                      <TableHead className="hidden md:table-cell">状态</TableHead>
                      <TableHead className="text-right">归属</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {records.map((r) => (
                      <TableRow key={r.RecordID}>
                        <TableCell className="font-mono text-xs">{r.Name}</TableCell>
                        <TableCell className="font-mono text-xs">{r.Value}</TableCell>
                        <TableCell className="hidden text-xs md:table-cell">{r.Line}</TableCell>
                        <TableCell className="hidden text-xs tabular-nums md:table-cell">{r.TTL}</TableCell>
                        <TableCell className="hidden md:table-cell">
                          <Badge variant={r.Status === "ENABLE" ? "secondary" : "outline"}>
                            {r.Status === "ENABLE" ? "启用" : "暂停"}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-right">
                          {slotOf(r.RecordID, r.Value) ? (
                            <Badge>槽位 {slotOf(r.RecordID, r.Value)}</Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">手工</span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="history" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">历史扫描</CardTitle>
              <CardDescription>最近 20 次任务</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex flex-col">
                {jobs.length === 0 ? (
                  <div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
                    暂无任务
                  </div>
                ) : (
                  jobs.map((j, idx) => (
                    <div key={j.id}>
                      {idx > 0 ? <Separator /> : null}
                      <div className="flex flex-wrap items-center justify-between gap-2 py-3">
                        <div className="flex items-center gap-2">
                          <Badge
                            variant={
                              j.status === "done"
                                ? "secondary"
                                : j.status === "failed"
                                  ? "destructive"
                                  : "outline"
                            }
                          >
                            {j.status === "done" ? "成功" : j.status === "failed" ? "失败" : "进行中"}
                          </Badge>
                          <span className="text-sm">
                            {j.topIp || "—"} · {j.topLatency ? `${j.topLatency.toFixed(0)}ms` : "—"}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            {j.trigger === "manual"
                              ? "手动"
                              : j.trigger === "health"
                                ? "故障切换"
                                : j.trigger === "create"
                                  ? "首次"
                                  : "定时"}
                          </span>
                        </div>
                        <div className="flex items-center gap-3 text-xs text-muted-foreground">
                          <span>可用 {j.ok}/{j.tested}</span>
                          <span>{new Date(j.startedAt).toLocaleString("zh-CN")}</span>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            render={<a href={`/api/jobs/${j.id}/export.csv`} download />}
                          >
                            <Download />
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
