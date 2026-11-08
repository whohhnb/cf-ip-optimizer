import { useCallback, useEffect, useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import { Globe2, ListPlus, Loader2, Pencil, Plus, Trash2, Zap } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
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
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { BatchDomainsDialog } from "@/components/batch-domains-dialog"
import { RotationHint } from "@/components/rotation-hint"
import {
  api,
  type Account,
  type Domain,
  type DomainRow,
  type Probe,
  type ProviderDescriptor,
} from "@/lib/api"

type FormState = {
  accountId: string
  rootDomain: string
  subDomain: string
  recordLine: string
  probeId: string
  ttl: string
  slots: string
  intervalMin: string
  autoPush: boolean
  enabled: boolean
}

const emptyForm: FormState = {
  accountId: "",
  rootDomain: "",
  subDomain: "@",
  recordLine: "默认",
  probeId: "0",
  ttl: "600",
  slots: "2",
  intervalMin: "60",
  autoPush: true,
  enabled: true,
}

function lineLabel(value: string, lines?: { value: string; label: string }[]) {
  if (!value) return "默认"
  if (!lines || lines.length === 0) {
    return value === "default" ? "默认" : value
  }
  return lines.find((l) => l.value === value)?.label ?? value
}

function statusBadge(status: string) {
  if (status === "ok") return <Badge variant="secondary">正常</Badge>
  if (status === "scanning")
    return (
      <Badge variant="outline">
        <Loader2 className="animate-spin" />
        扫描中
      </Badge>
    )
  if (status === "error") return <Badge variant="destructive">异常</Badge>
  return <Badge variant="outline">待扫描</Badge>
}

export default function DomainsPage() {
  const navigate = useNavigate()
  const [rows, setRows] = useState<DomainRow[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [providers, setProviders] = useState<ProviderDescriptor[]>([])
  const [probes, setProbes] = useState<Probe[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Domain | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [saving, setSaving] = useState(false)
  const [removing, setRemoving] = useState<Domain | null>(null)
  const [batchOpen, setBatchOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const [dm, ac, pv, pb] = await Promise.all([
        api.domains(),
        api.accounts(),
        api.providers(),
        api.probes(),
      ])
      setRows(dm.domains ?? [])
      setAccounts(ac.accounts ?? [])
      setProviders(pv.providers ?? [])
      setProbes(pb.probes ?? [])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // while a first scan is running, refresh so the status chip stays truthful
  const scanning = rows.some((r) => r.running || r.domain.status === "scanning")
  useEffect(() => {
    if (!scanning) return
    const timer = window.setInterval(() => void load(), 5000)
    return () => window.clearInterval(timer)
  }, [scanning, load])

  const selectedAccount = accounts.find((a) => String(a.id) === form.accountId)
  const selectedProvider = providers.find((p) => p.name === selectedAccount?.provider)
  const maxSlots = selectedProvider?.maxSlots ?? 2
  // 线路必须是该服务商真实支持的值；不支持分线路的服务商直接禁用选择
  const lineOptions = selectedProvider?.lines ?? []
  const defaultLine =
    lineOptions.find((l) => l.label === "默认")?.value ?? lineOptions[0]?.value ?? "默认"
  const lineValue = lineOptions.some((l) => l.value === form.recordLine)
    ? form.recordLine
    : defaultLine
  // keep the slot selector inside what the chosen vendor can actually hold
  const slotValue = String(Math.min(Math.max(Number(form.slots) || 1, 1), maxSlots))

  function openCreate() {
    setEditing(null)
    setForm({ ...emptyForm, accountId: accounts[0] ? String(accounts[0].id) : "" })
    setOpen(true)
  }

  function openEdit(d: Domain) {
    setEditing(d)
    setForm({
      accountId: String(d.accountId),
      rootDomain: d.rootDomain,
      subDomain: d.subDomain,
      recordLine: d.recordLine || "默认",
      probeId: String(d.probeId ?? 0),
      ttl: String(d.ttl),
      slots: String(d.slots),
      intervalMin: String(d.intervalMin),
      autoPush: d.autoPush,
      enabled: d.enabled,
    })
    setOpen(true)
  }

  async function save() {
    if (!form.accountId) {
      toast.error("请先选择 DNS 账号")
      return
    }
    setSaving(true)
    const payload = {
      accountId: Number(form.accountId),
      rootDomain: form.rootDomain,
      subDomain: form.subDomain,
      ttl: Number(form.ttl),
      slots: Number(slotValue),
      intervalMin: Number(form.intervalMin),
      autoPush: form.autoPush,
      enabled: form.enabled,
      recordLine: lineValue,
      probeId: Number(form.probeId || 0),
    }
    try {
      if (editing) {
        await api.updateDomain(editing.id, payload)
        toast.success("已保存")
      } else {
        const created = await api.createDomain(payload)
        if (created.job) {
          toast.success("域名已添加，正在执行首次扫描", {
            action: {
              label: "查看进度",
              onClick: () => navigate(`/domains/${created.domain.id}`),
            },
          })
        } else if (payload.enabled) {
          toast.success("域名已添加，将在下一轮调度中扫描")
        } else {
          toast.success("域名已添加（定时任务未启用，请手动扫描）")
        }
      }
      setOpen(false)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function remove() {
    if (!removing) return
    try {
      await api.deleteDomain(removing.id)
      toast.success("已删除")
      setRemoving(null)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "删除失败")
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">域名调度</h1>
          <p className="text-sm text-muted-foreground">
            为每个域名维护最多 2 条 A 记录，自动指向评分最高的优选 IP
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" onClick={() => setBatchOpen(true)}>
            <ListPlus />
            按线路批量添加
          </Button>
          <Button onClick={openCreate}>
            <Plus />
            添加域名
          </Button>
        </div>
      </div>

      {accounts.length === 0 && !loading ? (
        <Alert>
          <Zap />
          <AlertTitle>先配置 DNS 凭据</AlertTitle>
          <AlertDescription className="flex items-center justify-between gap-4">
            需要先添加 DNS 服务商凭据才能写入解析记录。
            <Button size="sm" variant="outline" render={<Link to="/accounts" />}>
              去配置
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">已接入域名</CardTitle>
          <CardDescription>共 {rows.length} 个</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <Skeleton className="h-48 w-full" />
          ) : rows.length === 0 ? (
            <div className="flex h-40 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
              <Globe2 className="size-6" />
              还没有添加域名
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>域名</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="hidden md:table-cell">策略</TableHead>
                  <TableHead className="hidden lg:table-cell">最近扫描</TableHead>
                  <TableHead className="hidden xl:table-cell">最近下发</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map(({ domain, full, running, provider }) => (
                  <TableRow key={domain.id}>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-2">
                        <Link
                          to={`/domains/${domain.id}`}
                          className="font-medium hover:underline max-sm:inline-block max-sm:py-1"
                        >
                          {full}
                        </Link>
                        <Badge
                          variant="outline"
                          className="text-[10px] font-normal text-muted-foreground"
                        >
                          {provider.label}
                        </Badge>
                        {lineLabel(domain.recordLine, provider.lines) !== "默认" ? (
                          <Badge variant="secondary" className="text-[10px] font-normal">
                            {lineLabel(domain.recordLine, provider.lines)}
                          </Badge>
                        ) : null}
                        {domain.probeId > 0 ? (
                          <Badge variant="outline" className="text-[10px] font-normal">
                            探针 · {probes.find((p) => p.id === domain.probeId)?.name ?? "已删除"}
                          </Badge>
                        ) : null}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {domain.rootDomain} · {domain.recordLine}
                      </div>
                      <div className="text-xs text-muted-foreground md:hidden">
                        {domain.slots} 条 A · 每 {domain.intervalMin} 分钟
                      </div>
                    </TableCell>
                    <TableCell>{statusBadge(domain.status)}</TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground md:table-cell">
                      {domain.slots} 条 A · TTL {domain.ttl}s
                      <br />
                      每 {domain.intervalMin} 分钟 · {domain.autoPush ? "自动下发" : "手动下发"}
                    </TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground lg:table-cell">
                      {domain.lastScanAt
                        ? new Date(domain.lastScanAt).toLocaleString("zh-CN")
                        : "从未"}
                      {running ? " · 进行中" : ""}
                    </TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground xl:table-cell">
                      {domain.lastPushAt
                        ? new Date(domain.lastPushAt).toLocaleString("zh-CN")
                        : "从未"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => openEdit(domain)}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setRemoving(domain)}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑域名" : "添加域名"}</DialogTitle>
            <DialogDescription>
              {selectedProvider
                ? `将通过「${selectedProvider.label}」写入解析，最多维护 ${selectedProvider.maxSlots} 条 A 记录。`
                : "域名需已托管在所选 DNS 服务商，且记录由本平台接管。"}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-2">
            <div className="grid gap-2">
              <Label>DNS 账号</Label>
              <Select
                value={form.accountId}
                items={Object.fromEntries(accounts.map((a) => [String(a.id), a.name]))}
                onValueChange={(v) => setForm((s) => ({ ...s, accountId: String(v ?? "") }))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="选择账号" />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((a) => (
                    <SelectItem key={a.id} value={String(a.id)}>
                      {a.name}
                      {a.meta ? ` · ${a.meta}` : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="root">根域名</Label>
                <Input
                  id="root"
                  placeholder="example.com"
                  value={form.rootDomain}
                  onChange={(e) => setForm((s) => ({ ...s, rootDomain: e.target.value }))}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="sub">子域名</Label>
                <Input
                  id="sub"
                  placeholder="@ 或 cdn"
                  value={form.subDomain}
                  onFocus={(e) => e.currentTarget.select()}
                  onChange={(e) => setForm((s) => ({ ...s, subDomain: e.target.value }))}
                />
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label>解析线路</Label>
                {lineOptions.length === 0 ? (
                  <Input value="默认（该服务商不支持分线路）" disabled />
                ) : (
                  <Select
                    value={lineValue}
                    items={Object.fromEntries(lineOptions.map((l) => [l.value, l.label]))}
                    onValueChange={(v) => setForm((s) => ({ ...s, recordLine: String(v ?? "") }))}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {lineOptions.map((l) => (
                        <SelectItem key={l.value} value={l.value}>
                          {l.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                <p className="text-xs text-muted-foreground">
                  {lineOptions.length === 0
                    ? `${selectedProvider?.label ?? "该服务商"} 不支持按运营商分线路，同一主机名只能有一条记录组`
                    : "只接管这条线路上的 A 记录，同一主机名可以按线路配多份"}
                </p>
              </div>
              <div className="grid gap-2">
                <Label>扫描探针</Label>
                <Select
                  value={form.probeId}
                  items={{
                    "0": "本机扫描",
                    ...Object.fromEntries(
                      probes.map((p) => [
                        String(p.id),
                        `${p.name} · ${p.line}${p.online ? "" : "（离线）"}`,
                      ]),
                    ),
                  }}
                  onValueChange={(v) => setForm((s) => ({ ...s, probeId: String(v ?? "0") }))}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">本机扫描（面板所在机器）</SelectItem>
                    {probes.map((p) => (
                      <SelectItem key={p.id} value={String(p.id)}>
                        {p.name} · {p.line}
                        {p.online ? "" : "（离线）"}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">
                  选探针才会得到该线路视角的结果；探针离线时直接报错，不会退回本机
                </p>
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-3">
              <div className="grid gap-2">
                <Label htmlFor="ttl">TTL（秒）</Label>
                <Input
                  id="ttl"
                  type="number"
                  min={60}
                  max={86400}
                  value={form.ttl}
                  onChange={(e) => setForm((s) => ({ ...s, ttl: e.target.value }))}
                />
              </div>
              <div className="grid gap-2">
                <Label>A 记录条数</Label>
                <Select
                  value={slotValue}
                  items={{ "1": "1 条", "2": "2 条" }}
                  onValueChange={(v) => setForm((s) => ({ ...s, slots: String(v ?? "2") }))}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1">1 条</SelectItem>
                    {maxSlots >= 2 ? <SelectItem value="2">2 条</SelectItem> : null}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="interval">扫描间隔（分钟）</Label>
                <Input
                  id="interval"
                  type="number"
                  min={5}
                  max={1440}
                  value={form.intervalMin}
                  onChange={(e) => setForm((s) => ({ ...s, intervalMin: e.target.value }))}
                />
              </div>
            </div>

            <RotationHint slots={Number(slotValue)} />

            <div className="flex items-center justify-between rounded-lg border p-3">
              <div>
                <p className="text-sm font-medium">自动下发</p>
                <p className="text-xs text-muted-foreground">
                  扫描完成后自动把最优 IP 写入解析
                </p>
              </div>
              <Switch
                checked={form.autoPush}
                onCheckedChange={(v) => setForm((s) => ({ ...s, autoPush: Boolean(v) }))}
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border p-3">
              <div>
                <p className="text-sm font-medium">启用定时任务</p>
                <p className="text-xs text-muted-foreground">关闭后仅支持手动扫描</p>
              </div>
              <Switch
                checked={form.enabled}
                onCheckedChange={(v) => setForm((s) => ({ ...s, enabled: Boolean(v) }))}
              />
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button onClick={() => void save()} disabled={saving}>
              {saving ? <Loader2 className="animate-spin" /> : null}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <BatchDomainsDialog
        open={batchOpen}
        onOpenChange={setBatchOpen}
        accounts={accounts}
        providers={providers}
        probes={probes}
        onDone={() => void load()}
      />

      <AlertDialog open={!!removing} onOpenChange={(v) => !v && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除域名调度？</AlertDialogTitle>
            <AlertDialogDescription>
              仅移除本平台的调度配置与历史数据，不会删除服务商上已有的解析记录。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void remove()}>确认删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
