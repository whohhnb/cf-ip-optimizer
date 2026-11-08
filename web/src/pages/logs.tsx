import { useCallback, useEffect, useState } from "react"
import { RefreshCw, ScrollText, Search } from "lucide-react"
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
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, type LogEntry } from "@/lib/api"

const LEVELS = [
  { value: "all", label: "全部" },
  { value: "info", label: "信息" },
  { value: "warn", label: "警告" },
  { value: "error", label: "错误" },
]

function levelBadge(level: string) {
  if (level === "error") return <Badge variant="destructive">error</Badge>
  if (level === "warn") return <Badge variant="outline">warn</Badge>
  if (level === "debug") return <Badge variant="ghost">debug</Badge>
  return <Badge variant="secondary">info</Badge>
}

export default function LogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [level, setLevel] = useState("all")
  const [keyword, setKeyword] = useState("")
  const [auto, setAuto] = useState(true)

  const load = useCallback(async () => {
    try {
      const res = await api.logs(500, level)
      setLogs(res.logs ?? [])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }, [level])

  useEffect(() => {
    void load()
  }, [load])

  // keep the tail of the log live while the page is open
  useEffect(() => {
    if (!auto) return
    const timer = window.setInterval(() => void load(), 5000)
    return () => window.clearInterval(timer)
  }, [auto, load])

  const shown = keyword
    ? logs.filter((l) => l.message.toLowerCase().includes(keyword.toLowerCase()))
    : logs

  const errorCount = logs.filter((l) => l.level === "error").length
  const warnCount = logs.filter((l) => l.level === "warn").length

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">运行日志</h1>
          <p className="text-sm text-muted-foreground">
            扫描、下发、健康巡检与接口异常的完整轨迹；调试级日志与 2xx 请求只输出到控制台
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant={auto ? "secondary" : "outline"}
            size="sm"
            onClick={() => setAuto((v) => !v)}
          >
            {auto ? "自动刷新中" : "已暂停刷新"}
          </Button>
          <Button variant="outline" size="sm" onClick={() => void load()}>
            <RefreshCw />
            刷新
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ScrollText className="size-4" />
            最近 {shown.length} 条
          </CardTitle>
          <CardDescription>
            保留最近 5000 条，超出后自动清理最旧记录
            {errorCount > 0 ? ` · 含 ${errorCount} 条错误` : ""}
            {warnCount > 0 ? ` · ${warnCount} 条警告` : ""}
          </CardDescription>
          <CardAction>
            <div className="relative w-40 sm:w-56">
              <Search className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                placeholder="筛选内容…"
                className="h-8 pl-7 text-xs"
              />
            </div>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Tabs value={level} onValueChange={setLevel}>
            <TabsList className="w-full justify-start overflow-x-auto sm:w-auto">
              {LEVELS.map((l) => (
                <TabsTrigger key={l.value} value={l.value}>
                  {l.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>

          {loading ? (
            <Skeleton className="h-64 w-full" />
          ) : shown.length === 0 ? (
            <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
              暂无日志
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-28 whitespace-nowrap sm:w-40">时间</TableHead>
                  <TableHead className="w-16">级别</TableHead>
                  <TableHead>内容</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {shown.map((l) => (
                  <TableRow key={l.id}>
                    <TableCell className="align-top font-mono text-[11px] whitespace-nowrap text-muted-foreground">
                      {new Date(l.createdAt).toLocaleString("zh-CN")}
                    </TableCell>
                    <TableCell className="align-top">{levelBadge(l.level)}</TableCell>
                    <TableCell className="text-sm break-words">{l.message}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
