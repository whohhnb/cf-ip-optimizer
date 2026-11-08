import { useEffect, useMemo, useState } from "react"
import { AlertTriangle, Loader2, Plus } from "lucide-react"
import { toast } from "sonner"

import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { Switch } from "@/components/ui/switch"
import { api, type Account, type Probe, type ProviderDescriptor } from "@/lib/api"

type Props = {
  open: boolean
  onOpenChange: (v: boolean) => void
  accounts: Account[]
  providers: ProviderDescriptor[]
  probes: Probe[]
  onDone: () => void
}

type Form = {
  accountId: string
  rootDomain: string
  subDomain: string
  ttl: string
  slots: string
  intervalMin: string
  autoPush: boolean
}

const empty: Form = {
  accountId: "",
  rootDomain: "",
  subDomain: "@",
  ttl: "600",
  slots: "2",
  intervalMin: "60",
  autoPush: true,
}

// 三网 + 默认兜底：只配电信/联通/移动时，境外、教育网和小运营商会解析不到
const PREFERRED = ["默认", "电信", "联通", "移动"]

export function BatchDomainsDialog({
  open,
  onOpenChange,
  accounts,
  providers,
  probes,
  onDone,
}: Props) {
  const [form, setForm] = useState<Form>(empty)
  const [picked, setPicked] = useState<Record<string, boolean>>({})
  const [saving, setSaving] = useState(false)

  const account = accounts.find((a) => String(a.id) === form.accountId)
  const provider = providers.find((p) => p.name === account?.provider)
  const lines = provider?.lines ?? []
  const defaultLine = lines.find((l) => l.label === "默认")?.value ?? ""

  // 打开时按「默认 + 三网」预勾选，并挑第一个账号
  useEffect(() => {
    if (!open) return
    setForm((f) => ({ ...f, accountId: f.accountId || String(accounts[0]?.id ?? "") }))
  }, [open, accounts])

  useEffect(() => {
    if (!open || lines.length === 0) return
    const next: Record<string, boolean> = {}
    for (const l of lines) {
      next[l.value] = PREFERRED.includes(l.label)
    }
    setPicked(next)
  }, [open, provider?.name, lines.length])

  // 特定线路必须有同线路的在线探针：拿本机去测「联通」，测出来的不是联通的视角
  const probeFor = (value: string): { id: number; name: string } | null => {
    const p = probes.find((x) => x.line === value && x.online)
    if (p) return { id: p.id, name: p.name }
    if (value === defaultLine) return { id: 0, name: "本机扫描" }
    return null
  }

  // 只统计真正能创建的：勾了但没探针的不算
  const chosen = useMemo(
    () => lines.filter((l) => picked[l.value] && probeFor(l.value)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [lines, picked, probes],
  )

  async function submit() {
    if (!form.accountId) {
      toast.error("请先选择 DNS 账号")
      return
    }
    if (!form.rootDomain.trim()) {
      toast.error("请填写根域名")
      return
    }
    const ready = chosen
    if (ready.length === 0) {
      toast.error("没有可创建的线路")
      return
    }

    setSaving(true)
    const failed: string[] = []
    let ok = 0
    for (const l of ready) {
      const probe = probeFor(l.value)
      try {
        await api.createDomain({
          accountId: Number(form.accountId),
          rootDomain: form.rootDomain.trim(),
          subDomain: form.subDomain.trim() || "@",
          recordLine: l.value,
          probeId: probe?.id ?? 0,
          ttl: Number(form.ttl),
          slots: Number(form.slots),
          intervalMin: Number(form.intervalMin),
          autoPush: form.autoPush,
          enabled: true,
        })
        ok++
      } catch (err) {
        failed.push(`${l.label}：${err instanceof Error ? err.message : "创建失败"}`)
      }
    }
    setSaving(false)

    if (ok > 0) toast.success(`已创建 ${ok} 条线路配置，正在各自扫描`)
    if (failed.length > 0) toast.error(failed.join("；"))
    onOpenChange(false)
    onDone()
  }

  const unsupported = provider && lines.length === 0

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>按线路批量添加</DialogTitle>
          <DialogDescription>
            同一个主机名一次建多条线路配置，各自用自己的探针扫描、下发到自己的线路
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label>DNS 账号</Label>
            <Select
              value={form.accountId}
              items={Object.fromEntries(accounts.map((a) => [String(a.id), a.name]))}
              onValueChange={(v) => setForm((s) => ({ ...s, accountId: String(v ?? "") }))}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {accounts.map((a) => (
                  <SelectItem key={a.id} value={String(a.id)}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="batch-root">根域名</Label>
              <Input
                id="batch-root"
                placeholder="example.com"
                value={form.rootDomain}
                onChange={(e) => setForm((s) => ({ ...s, rootDomain: e.target.value }))}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="batch-sub">子域名</Label>
              <Input
                id="batch-sub"
                placeholder="@ 或 cdn"
                value={form.subDomain}
                onFocus={(e) => e.currentTarget.select()}
                onChange={(e) => setForm((s) => ({ ...s, subDomain: e.target.value }))}
              />
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <div className="grid gap-2">
              <Label htmlFor="batch-ttl">TTL（秒）</Label>
              <Input
                id="batch-ttl"
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
                value={form.slots}
                items={{ "1": "1 条", "2": "2 条" }}
                onValueChange={(v) => setForm((s) => ({ ...s, slots: String(v ?? "2") }))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">1 条</SelectItem>
                  <SelectItem value="2">2 条</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="batch-interval">扫描间隔（分钟）</Label>
              <Input
                id="batch-interval"
                type="number"
                min={5}
                max={1440}
                value={form.intervalMin}
                onChange={(e) => setForm((s) => ({ ...s, intervalMin: e.target.value }))}
              />
            </div>
          </div>

          {unsupported ? (
            <Alert>
              <AlertTriangle />
              <AlertTitle>{provider?.label} 不支持分线路</AlertTitle>
              <AlertDescription>
                该服务商的 API 没有线路概念，只能维护一条默认记录组。请用「添加域名」逐条创建。
              </AlertDescription>
            </Alert>
          ) : (
            <div className="grid gap-2">
              <Label>要创建的线路</Label>
              <div className="divide-y rounded-lg border">
                {lines.map((l) => {
                  const probe = probeFor(l.value)
                  return (
                    <div key={l.value} className="flex items-center justify-between gap-3 p-3">
                      <div className="min-w-0">
                        <p className="text-sm font-medium">{l.label}</p>
                        <p className="truncate text-xs text-muted-foreground">
                          {probe ? (
                            <>探针：{probe.name}</>
                          ) : (
                            <span className="text-destructive">没有在线探针，无法创建</span>
                          )}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        {l.value === defaultLine ? (
                          <Badge variant="outline" className="text-[10px] font-normal">
                            兜底
                          </Badge>
                        ) : null}
                        <Switch
                          checked={!!picked[l.value] && !!probe}
                          disabled={!probe}
                          onCheckedChange={(v) =>
                            setPicked((s) => ({ ...s, [l.value]: Boolean(v) }))
                          }
                        />
                      </div>
                    </div>
                  )
                })}
              </div>
              <p className="text-xs text-muted-foreground">
                「默认」线路是兜底：境外、教育网和小运营商都会落到这里。只配电信/联通/移动的话，
                这些用户会解析不到地址
              </p>
            </div>
          )}

          <div className="flex items-center justify-between rounded-lg border p-3">
            <div>
              <p className="text-sm font-medium">自动下发</p>
              <p className="text-xs text-muted-foreground">扫描完成后自动写入对应线路</p>
            </div>
            <Switch
              checked={form.autoPush}
              onCheckedChange={(v) => setForm((s) => ({ ...s, autoPush: Boolean(v) }))}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={saving || chosen.length === 0}>
            {saving ? <Loader2 className="animate-spin" /> : <Plus />}
            创建 {chosen.length} 条
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
