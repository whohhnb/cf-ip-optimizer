import { useCallback, useEffect, useState } from "react"
import {
  AlertTriangle,
  Check,
  Copy,
  Loader2,
  Plus,
  Router,
  Trash2,
  Wifi,
  WifiOff,
} from "lucide-react"
import { toast } from "sonner"

import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type Probe } from "@/lib/api"

const LINES = ["默认", "电信", "联通", "移动", "教育网", "境外"]

function lineBadge(line: string) {
  return (
    <Badge variant="outline" className="font-normal">
      {line}
    </Badge>
  )
}

function relative(iso: string | null) {
  if (!iso) return "从未上线"
  const t = new Date(iso).getTime()
  const diff = Math.max(0, Date.now() - t)
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s} 秒前`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  return new Date(iso).toLocaleDateString("zh-CN")
}

export default function ProbesPage() {
  const [probes, setProbes] = useState<Probe[]>([])
  const [loading, setLoading] = useState(true)

  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [line, setLine] = useState("电信")
  const [saving, setSaving] = useState(false)

  const [issued, setIssued] = useState<{ name: string; token: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const [removing, setRemoving] = useState<Probe | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await api.probes()
      setProbes(res.probes ?? [])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // 心跳每 30s 一次，页面开着时定期刷新在线状态
  useEffect(() => {
    const timer = window.setInterval(() => void load(), 15000)
    return () => window.clearInterval(timer)
  }, [load])

  function openCreate() {
    setName("")
    setLine("电信")
    setOpen(true)
  }

  async function save() {
    if (!name.trim()) {
      toast.error("请填写探针名称")
      return
    }
    setSaving(true)
    try {
      const res = await api.createProbe({ name: name.trim(), line })
      setOpen(false)
      setIssued({ name: res.probe.name, token: res.token })
      setCopied(false)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "创建失败")
    } finally {
      setSaving(false)
    }
  }

  async function remove() {
    if (!removing) return
    try {
      await api.deleteProbe(removing.id)
      toast.success("已删除")
      setRemoving(null)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "删除失败")
    }
  }

  async function copyToken() {
    if (!issued) return
    try {
      await navigator.clipboard.writeText(issued.token)
      setCopied(true)
      toast.success("令牌已复制")
    } catch {
      toast.error("复制失败，请手动选中")
    }
  }

  const online = probes.filter((p) => p.online).length

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">探针节点</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            把探针部署在电信 / 联通 / 移动的机器上，各线路才能测到自己视角下的最优 IP
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus />
          添加探针
        </Button>
      </div>

      <Alert>
        <AlertTriangle />
        <AlertTitle>为什么需要探针</AlertTitle>
        <AlertDescription>
          同一个 Cloudflare anycast IP，三家运营商走的国际出口不同，延迟能差好几倍。
          在面板所在机器上扫出来的「最优」只代表那台机器的网络，所以分线路解析必须由对应线路的探针来测。
        </AlertDescription>
      </Alert>

      {loading ? (
        <Skeleton className="h-40 w-full" />
      ) : probes.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">还没有探针</CardTitle>
            <CardDescription>
              添加后会把令牌显示一次，在对应线路的机器上执行 `edgepilot probe --token ...` 即可上线
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="outline" onClick={openCreate}>
              <Plus />
              添加第一个探针
            </Button>
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Router className="size-4" />
            共 {probes.length} 个探针，{online} 个在线
          </div>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {probes.map((p) => (
              <Card key={p.id} className="flex flex-col">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base">
                    {p.online ? (
                      <Wifi className="size-4 text-emerald-600 dark:text-emerald-400" />
                    ) : (
                      <WifiOff className="size-4 text-muted-foreground" />
                    )}
                    <span className="truncate">{p.name}</span>
                  </CardTitle>
                  <CardDescription className="flex flex-wrap items-center gap-2">
                    {lineBadge(p.line)}
                    <Badge variant={p.online ? "secondary" : "outline"}>
                      {p.online ? "在线" : "离线"}
                    </Badge>
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex-1 text-xs text-muted-foreground">
                  <dl className="grid gap-1.5">
                    <div className="flex justify-between gap-2">
                      <dt>最后心跳</dt>
                      <dd className="tabular-nums">{relative(p.lastSeenAt)}</dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>来源 IP</dt>
                      <dd className="font-mono">{p.lastIp || "—"}</dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>版本</dt>
                      <dd className="font-mono">{p.version || "—"}</dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>令牌</dt>
                      <dd className="font-mono">{p.tokenHint}…</dd>
                    </div>
                  </dl>
                </CardContent>
                <CardFooter className="justify-end">
                  <Button variant="ghost" size="sm" onClick={() => setRemoving(p)}>
                    <Trash2 />
                    删除
                  </Button>
                </CardFooter>
              </Card>
            ))}
          </div>
        </>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>添加探针</DialogTitle>
            <DialogDescription>
              线路只是标记这台机器在哪个网络，用于和域名的解析线路对应
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4">
            <div className="grid gap-2">
              <Label htmlFor="probe-name">名称</Label>
              <Input
                id="probe-name"
                placeholder="如：香港-电信-1"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label>线路</Label>
              <Select value={line} items={Object.fromEntries(LINES.map((l) => [l, l]))} onValueChange={(v) => setLine(String(v ?? "电信"))}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LINES.map((l) => (
                    <SelectItem key={l} value={l}>
                      {l}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button onClick={() => void save()} disabled={saving}>
              {saving ? <Loader2 className="animate-spin" /> : null}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!issued} onOpenChange={(v) => !v && setIssued(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>探针「{issued?.name}」已创建</DialogTitle>
            <DialogDescription>
              令牌只显示这一次，关闭后无法再查看，只能重新创建
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="flex items-center gap-2 rounded-lg border bg-muted/40 p-3">
              <code className="flex-1 break-all font-mono text-xs">{issued?.token}</code>
              <Button variant="outline" size="sm" onClick={() => void copyToken()}>
                {copied ? <Check /> : <Copy />}
                {copied ? "已复制" : "复制"}
              </Button>
            </div>
            <div className="grid gap-1.5">
              <p className="text-xs font-medium">在对应线路的机器上运行：</p>
              <pre className="overflow-x-auto rounded-lg border bg-muted/40 p-3 text-xs leading-relaxed">
{`edgepilot probe \\
  --server ${window.location.origin} \\
  --token ${issued?.token ?? ""}`}
              </pre>
              <p className="text-xs text-muted-foreground">
                探针每 30 秒发一次心跳，面板超过 90 秒收不到就判定离线；离线时绑定它的域名会直接报错，不会悄悄改用本机扫描
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setIssued(null)}>我已保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!removing} onOpenChange={(v) => !v && setRemoving(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>删除探针「{removing?.name}」？</DialogTitle>
            <DialogDescription>
              删除后该令牌立即失效。仍有域名绑定它时不允许删除，需要先改绑
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoving(null)}>
              取消
            </Button>
            <Button variant="destructive" onClick={() => void remove()}>
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
