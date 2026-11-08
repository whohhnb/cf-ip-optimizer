import { useCallback, useEffect, useMemo, useState } from "react"
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
import { LiveRefresh } from "@/components/live-refresh"
import { usePoll } from "@/lib/use-poll"
import { Checkbox } from "@/components/ui/checkbox"
import { RotationHint } from "@/components/rotation-hint"
import {
  api,
  type Account,
  type Domain,
  type DomainRow,
  type Probe,
  type ProviderInfo,
  type ProviderDescriptor,
} from "@/lib/api"

type FormState = {
  accountId: string
  rootDomain: string
  subDomain: string
  recordLine: string
  coloAllow: string
  probeIds: number[]
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
  coloAllow: "",
  probeIds: [],
  ttl: "600",
  slots: "2",
  intervalMin: "60",
  autoPush: true,
  enabled: true,
}

// 一个域名可以绑多个探针，列表里要把它们都列出来，不能只显示第一个
function probeNames(ids: number[] | undefined, probes: Probe[]) {
  return (ids ?? []).map((id) => probes.find((p) => p.id === id)?.name ?? "已删除")
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
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Domain | null>(null)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [saving, setSaving] = useState(false)
  const [removing, setRemoving] = useState<Domain | null>(null)
  const [batchOpen, setBatchOpen] = useState(false)

  const load = useCallback(async (silent = false) => {
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
      // 后台轮询失败不打扰用户，下一次自己会补上
      if (!silent) toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
      setUpdatedAt(Date.now())
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // 扫描进度与下发结果都靠这一条刷新到界面上
  usePoll(() => load(true), 5000)

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
  const currentLineLabel = lineOptions.find((l) => l.value === lineValue)?.label ?? "默认"
  const selectedProbes = probes.filter((p) => form.probeIds.includes(p.id))
  // 探针的线路是中文标签，线路的值是服务商原生值，按标签比
  const mismatched = selectedProbes.filter(
    (p) => currentLineLabel !== "默认" && p.line !== currentLineLabel,
  )

  function toggleProbe(id: number, on: boolean) {
    setForm((s) => ({
      ...s,
      probeIds: on ? [...s.probeIds, id] : s.probeIds.filter((x) => x !== id),
    }))
  }
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
      coloAllow: d.coloAllow || "",
      probeIds: d.probeIds ?? (d.probeId ? [d.probeId] : []),
      ttl: String(d.ttl),
      slots: String(d.slots),
      intervalMin: String(d.intervalMin),
      autoPush: d.autoPush,
      enabled: d.enabled,
    })
    setOpen(true)
  }

  // 同一主机名的多条线路配置在库里是独立记录（一条线路一份），
  // 界面上按主机名收成一张卡，看起来才是「一个域名、四条线路」
  const groups = useMemo(() => {
    const order = ["默认", "电信", "联通", "移动", "教育网", "境外"]
    const rank = (v: string) => {
      const i = order.indexOf(v)
      return i < 0 ? 99 : i
    }
    const map = new Map<
      string,
      { full: string; root: string; provider: ProviderInfo; lines: DomainRow[] }
    >()
    for (const r of rows) {
      const g = map.get(r.full) ?? {
        full: r.full,
        root: r.domain.rootDomain,
        provider: r.provider,
        lines: [],
      }
      g.lines.push(r)
      map.set(r.full, g)
    }
    const out = [...map.values()]
    for (const g of out) {
      g.lines.sort(
        (a, b) =>
          rank(lineLabel(a.domain.recordLine, a.provider.lines)) -
          rank(lineLabel(b.domain.recordLine, b.provider.lines)),
      )
    }
    out.sort((a, b) => a.full.localeCompare(b.full))
    return out
  }, [rows])

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
      coloAllow: form.coloAllow.trim(),
      probeIds: form.probeIds,
    }
    try {
      if (editing) {
        await api.updateDomain(editing.id, payload)
        toast.success("已保存")
      } else {
        const created = await api.createDomain(payload)
        if (created.warning) {
          // 建域名成功但首次扫描没起来（探针离线等），必须说出来，
          // 否则用户会以为「下一轮调度就会扫」
          toast.error(created.warning)
        } else if (created.job) {
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
            一个主机名可挂多条线路，电信 / 联通 / 移动各走自己的优选 IP，默认线路兜底
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <LiveRefresh updatedAt={updatedAt} onRefresh={() => void load()} />
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
          <AlertTitle>先配置 DNS 账号</AlertTitle>
          <AlertDescription className="flex items-center justify-between gap-4">
            需要先添加 DNS 服务商账号才能写入解析记录
            <Button size="sm" variant="outline" render={<Link to="/accounts" />}>
              去配置
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {loading ? (
        <Skeleton className="h-48 w-full" />
      ) : groups.length === 0 ? (
        <Card>
          <CardContent className="flex h-40 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
            <Globe2 className="size-6" />
            还没有添加域名
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col gap-4">
          {groups.map((g) => (
            <Card key={g.full}>
              <CardHeader className="pb-3">
                <CardTitle className="flex flex-wrap items-center gap-2 text-base">
                  <Link to={`/domains/${g.lines[0].domain.id}`} className="hover:underline">
                    {g.full}
                  </Link>
                  <Badge
                    variant="outline"
                    className="text-[10px] font-normal text-muted-foreground"
                  >
                    {g.provider.label}
                  </Badge>
                  <Badge variant="secondary" className="text-[10px] font-normal">
                    {g.lines.length} 条线路
                  </Badge>
                </CardTitle>
                <CardDescription>
                  {g.root} · TTL {g.lines[0].domain.ttl}s · 每 {g.lines[0].domain.intervalMin} 分钟
                  · {g.lines[0].domain.autoPush ? "自动下发" : "手动下发"}
                </CardDescription>
              </CardHeader>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-24">线路</TableHead>
                      <TableHead className="w-24">状态</TableHead>
                      <TableHead>当前生效 IP</TableHead>
                      <TableHead className="hidden md:table-cell">探针</TableHead>
                      <TableHead className="w-24 text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {g.lines.map((r) => {
                      const label = lineLabel(r.domain.recordLine, r.provider.lines)
                      const names = probeNames(r.domain.probeIds, probes)
                      return (
                        <TableRow key={r.domain.id} className="align-top">
                          <TableCell>
                            <Badge
                              variant={label === "默认" ? "outline" : "secondary"}
                              className="text-[10px] font-normal"
                            >
                              {label}
                            </Badge>
                          </TableCell>
                          <TableCell>{statusBadge(r.domain.status)}</TableCell>
                          <TableCell className="whitespace-normal">
                            {r.managed.length > 0 ? (
                              <div className="flex flex-wrap items-center gap-1.5">
                                {r.managed.map((m) => (
                                  <code
                                    key={m.id}
                                    className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
                                  >
                                    {m.value}
                                  </code>
                                ))}
                              </div>
                            ) : r.domain.status === "error" && r.domain.statusMsg ? (
                              <span className="text-xs text-destructive">{r.domain.statusMsg}</span>
                            ) : (
                              <span className="text-xs text-muted-foreground">尚未下发</span>
                            )}
                          </TableCell>
                          <TableCell className="hidden whitespace-normal text-xs text-muted-foreground md:table-cell">
                            {names.length > 0 ? names.join("、") : "本机扫描"}
                          </TableCell>
                          <TableCell className="text-right">
                            <div className="flex justify-end gap-1">
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label="编辑"
                                title="编辑这条线路"
                                onClick={() => openEdit(r.domain)}
                              >
                                <Pencil />
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label="删除"
                                title="删除这条线路"
                                onClick={() => setRemoving(r.domain)}
                              >
                                <Trash2 />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑域名" : "添加域名"}</DialogTitle>
            <DialogDescription>
              {selectedProvider
                ? `将通过「${selectedProvider.label}」写入解析，最多维护 ${selectedProvider.maxSlots} 条 A 记录`
                : "域名需已托管在所选 DNS 服务商，且记录由本平台接管"}
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
                <Label>扫描探针（可多选）</Label>
                <div className="max-h-44 divide-y overflow-y-auto rounded-lg border">
                  <label className="flex cursor-pointer items-start gap-3 p-2.5">
                    <Checkbox
                      className="mt-0.5"
                      checked={form.probeIds.length === 0}
                      onCheckedChange={() => setForm((s) => ({ ...s, probeIds: [] }))}
                    />
                    <div className="min-w-0">
                      <p className="text-sm">本机扫描</p>
                      <p className="text-xs text-muted-foreground">
                        用面板所在机器的网络视角，不绑定探针
                      </p>
                    </div>
                  </label>
                  {probes.map((p) => (
                    <label key={p.id} className="flex cursor-pointer items-start gap-3 p-2.5">
                      <Checkbox
                        className="mt-0.5"
                        checked={form.probeIds.includes(p.id)}
                        onCheckedChange={(v) => toggleProbe(p.id, Boolean(v))}
                      />
                      <div className="min-w-0">
                        <p className="truncate text-sm">{p.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {p.line} · {p.online ? "在线" : "离线，扫描会直接报错"}
                        </p>
                      </div>
                    </label>
                  ))}
                </div>
                <p className="text-xs text-muted-foreground">
                  选多个时按「最差值」聚合：一个 IP 只要在任一探针上表现差就会被淘汰，宁可选一个各处都稳的
                </p>
                {mismatched.length > 0 ? (
                  <p className="text-xs text-destructive">
                    {mismatched.map((p) => `「${p.name}」在${p.line}`).join("、")}
                    ，与所选「{currentLineLabel}」线路不一致，测出来的不是这条线路的视角
                  </p>
                ) : null}
              </div>
            </div>

            <div className="grid gap-2">
              <Label htmlFor="colo">机房白名单（这条线路专用）</Label>
              <Input
                id="colo"
                className="max-w-64 font-mono uppercase"
                placeholder="留空跟随系统设置，如 HKG,NRT"
                value={form.coloAllow}
                onChange={(e) => setForm((s) => ({ ...s, coloAllow: e.target.value }))}
              />
              <p className="text-xs text-muted-foreground">
                不同运营商的最优机房不一样：移动常落 NRT、电信常落 HKG。这条留空就用系统设置的全局白名单
              </p>
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
              仅移除本平台的调度配置与历史数据，不会删除服务商上已有的解析记录
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
