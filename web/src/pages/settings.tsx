import { useCallback, useEffect, useState } from "react"
import { Loader2, Save, Settings2, Trash2, Users } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
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
import { RotationHint } from "@/components/rotation-hint"
import { api, type User } from "@/lib/api"
import { useAuth } from "@/lib/auth"

const numericFields: { key: string; label: string; hint: string; min: number; max: number }[] = [
  { key: "scan_candidates", label: "候选 IP 数（个）", hint: "每次扫描随机采样的地址数", min: 20, max: 3000 },
  { key: "scan_concurrency", label: "并发探测数", hint: "单次扫描内部的并发，过高可能被运营商限速", min: 1, max: 256 },
  { key: "scan_max_concurrent", label: "全局同时扫描数", hint: "多个域名同时到点时，超出这个数量的任务排队等待", min: 1, max: 16 },
  { key: "scan_timeout_ms", label: "探测超时 (ms)", hint: "单次 TCP 握手超时时间", min: 200, max: 10000 },
  { key: "scan_samples", label: "采样次数", hint: "每个 IP 的握手次数，用于算抖动与丢包", min: 1, max: 8 },
  { key: "scan_speed_top", label: "测速节点数（个）", hint: "延迟排序后进入下载测速的节点数", min: 1, max: 60 },
  { key: "scan_speed_bytes", label: "测速字节数", hint: "下载测速的样本大小（字节）", min: 65536, max: 52428800 },
  { key: "scan_latency_max", label: "延迟上限 (ms)", hint: "超过这个延迟的节点在下载测速前就被剔除，0 = 不限", min: 0, max: 3000 },
  { key: "scan_speed_floor", label: "速度下限 (Mbps)", hint: "低于此速度的节点不计入达标数，0 = 不限", min: 0, max: 1000 },
  { key: "scan_speed_ok", label: "达标即停（个）", hint: "凑够这么多个达到速度下限的节点就停止测速，0 = 全部测完", min: 0, max: 60 },
]

export default function SettingsPage() {
  const { user } = useAuth()
  const [values, setValues] = useState<Record<string, string>>({})
  const [defaults, setDefaults] = useState<Record<string, string>>({})
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      const [s, u] = await Promise.all([api.settings(), api.users()])
      setValues(s.settings ?? {})
      setDefaults(s.defaults ?? {})
      setUsers(u.users ?? [])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function save() {
    setSaving(true)
    try {
      await api.updateSettings(values)
      toast.success("设置已保存")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function removeUser(id: number) {
    try {
      await api.deleteUser(id)
      toast.success("已删除")
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "删除失败")
    }
  }

  if (user?.role !== "admin") {
    return (
      <Card>
        <CardHeader>
          <CardTitle>需要管理员权限</CardTitle>
          <CardDescription>当前账号无权访问系统设置</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">系统设置</h1>
          <p className="text-sm text-muted-foreground">
            扫描引擎参数与账号策略，修改后对新任务立即生效
          </p>
        </div>
        <Button onClick={() => void save()} disabled={saving}>
          {saving ? <Loader2 className="animate-spin" /> : <Save />}
          保存设置
        </Button>
      </div>

      {loading ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Settings2 className="size-4" />
                扫描引擎
              </CardTitle>
              <CardDescription>调优延迟探测与下载测速的强度</CardDescription>
              <CardAction>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    // 只填回表单，不直接落库：用户还得点保存，避免误触
                    setValues((v) => {
                      const next = { ...v }
                      for (const k of Object.keys(defaults)) next[k] = defaults[k]
                      return next
                    })
                    toast.info("已填入默认值，点保存后生效")
                  }}
                >
                  恢复默认
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
              {numericFields.map((f) => (
                <div key={f.key} className="grid gap-2">
                  <Label htmlFor={f.key}>{f.label}</Label>
                  <Input
                    id={f.key}
                    className="max-w-40"
                    type="number"
                    min={f.min}
                    max={f.max}
                    value={values[f.key] ?? ""}
                    onChange={(e) =>
                      setValues((s) => ({ ...s, [f.key]: e.target.value }))
                    }
                  />
                  <p className="text-xs text-muted-foreground">{f.hint}</p>
                </div>
              ))}

              <div className="grid gap-2 sm:col-span-2 xl:col-span-3">
                <Label htmlFor="scan_colo_allow">机房白名单</Label>
                <Input
                  id="scan_colo_allow"
                  className="max-w-64 font-mono"
                  placeholder="留空不限制，如 HKG,NRT,SIN"
                  value={values.scan_colo_allow ?? ""}
                  onChange={(e) =>
                    setValues((s) => ({ ...s, scan_colo_allow: e.target.value }))
                  }
                />
                <p className="text-xs text-muted-foreground">
                  IATA 三字码，逗号分隔。只保留这些机房的节点；全都命中不了会直接判定为无可用节点，
                  不会悄悄退回全量，免得你以为筛选生效了其实没有。
                </p>
              </div>

              <div className="flex items-start justify-between gap-4 rounded-lg border p-3 sm:col-span-2 xl:col-span-3">
                <div>
                  <p className="text-sm font-medium">跳过下载测速</p>
                  <p className="text-xs text-muted-foreground">
                    只按延迟与丢包排序，扫描时间缩短到几分之一。适合 SSH、游戏这类不在乎带宽的场景。
                  </p>
                </div>
                <Switch
                  checked={values.scan_no_speed === "true"}
                  onCheckedChange={(v) =>
                    setValues((s) => ({ ...s, scan_no_speed: String(Boolean(v)) }))
                  }
                />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">调度策略</CardTitle>
              <CardDescription>影响解析写入行为</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <div className="rounded-lg border p-3">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <p className="text-sm font-medium">滚动替换</p>
                    <p className="text-xs text-muted-foreground">
                      关闭后一次替换到位，收敛更快，但两条记录同时失效时会有短暂全站不可用
                    </p>
                  </div>
                  <Switch
                    checked={values.rolling_replace === "true"}
                    onCheckedChange={(v) =>
                      setValues((s) => ({ ...s, rolling_replace: String(Boolean(v)) }))
                    }
                  />
                </div>
                {/* stacked on phones, single strip on wide cards */}
                <RotationHint
                  slots={2}
                  showTitle={false}
                  className="mt-3 border-0 bg-muted/50 md:hidden"
                />
                <RotationHint
                  slots={2}
                  variant="row"
                  className="mt-3 hidden border-0 bg-muted/50 md:block"
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border p-3">
                <div>
                  <p className="text-sm font-medium">开放注册</p>
                  <p className="text-xs text-muted-foreground">
                    关闭后仅管理员可创建账号
                  </p>
                </div>
                <Switch
                  checked={values.allow_registration === "true"}
                  onCheckedChange={(v) =>
                    setValues((s) => ({ ...s, allow_registration: String(Boolean(v)) }))
                  }
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border p-3">
                <div>
                  <p className="text-sm font-medium">允许内网 Webhook</p>
                  <p className="text-xs text-muted-foreground">
                    默认拦截指向本机、内网和云元数据地址的回调，避免被当成 SSRF 跳板。
                    确实要回调内网服务时再打开。
                  </p>
                </div>
                <Switch
                  checked={values.allow_private_webhook === "true"}
                  onCheckedChange={(v) =>
                    setValues((s) => ({ ...s, allow_private_webhook: String(Boolean(v)) }))
                  }
                />
              </div>
            </CardContent>
            <CardFooter>
              <p className="text-xs text-muted-foreground">
                提示：TTL 建议保持在 600 秒以上，过低会放大 DNS 接口调用频率
              </p>
            </CardFooter>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Users className="size-4" />
                平台账号
              </CardTitle>
              <CardDescription>共 {users.length} 个账号</CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>邮箱</TableHead>
                    <TableHead>角色</TableHead>
                    <TableHead className="hidden sm:table-cell">注册时间</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {users.map((u) => (
                    <TableRow key={u.id}>
                      <TableCell className="font-medium">{u.email}</TableCell>
                      <TableCell>
                        <Badge variant={u.role === "admin" ? "default" : "outline"}>
                          {u.role === "admin" ? "管理员" : "普通用户"}
                        </Badge>
                      </TableCell>
                      <TableCell className="hidden text-xs text-muted-foreground sm:table-cell">
                        {new Date(u.createdAt).toLocaleDateString("zh-CN")}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          disabled={u.id === user?.id}
                          onClick={() => void removeUser(u.id)}
                        >
                          <Trash2 />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Separator className="my-4" />
              <p className="text-xs text-muted-foreground">
                删除账号会级联清理其名下的 DNS 账号、域名配置与扫描历史
              </p>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
