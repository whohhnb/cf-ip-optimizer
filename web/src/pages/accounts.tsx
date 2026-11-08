import { useCallback, useEffect, useMemo, useState } from "react"
import { ExternalLink, KeyRound, Loader2, Pencil, Plus, ShieldCheck, Trash2 } from "lucide-react"
import { toast } from "sonner"
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { api, type Account, type ProviderDescriptor } from "@/lib/api"

export default function AccountsPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [providers, setProviders] = useState<ProviderDescriptor[]>([])
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [removing, setRemoving] = useState<Account | null>(null)
  const [editing, setEditing] = useState<Account | null>(null)
  const [provider, setProvider] = useState("dnspod")
  const [name, setName] = useState("")
  const [root, setRoot] = useState("")
  const [cred, setCred] = useState<Record<string, string>>({})

  const load = useCallback(async () => {
    try {
      const [ac, pv] = await Promise.all([api.accounts(), api.providers()])
      setAccounts(ac.accounts ?? [])
      setProviders(pv.providers ?? [])
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const current = useMemo(
    () => providers.find((p) => p.name === provider),
    [providers, provider],
  )

  const providerLabel = useCallback(
    (key: string) => providers.find((p) => p.name === key)?.label ?? key,
    [providers],
  )

  function openDialog(account?: Account) {
    setEditing(account ?? null)
    if (account) {
      setProvider(account.provider)
      setName(account.name)
      setCred({})
      setRoot("")
    } else {
      const first = providers[0]
      setProvider(first?.name ?? "dnspod")
      setName(first?.label ?? "")
      setCred({})
      setRoot("")
    }
    setOpen(true)
  }

  function switchProvider(next: string) {
    setProvider(next)
    const d = providers.find((p) => p.name === next)
    setName(d?.label ?? "")
    setCred({})
  }

  async function test() {
    setTesting(true)
    try {
      await api.testAccount({ provider, credentials: cred, root: root.trim() })
      toast.success("凭据有效")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "校验失败")
    } finally {
      setTesting(false)
    }
  }

  async function save() {
    setSaving(true)
    const payload = {
      name: name.trim() || providerLabel(provider),
      provider,
      credentials: cred,
      root: root.trim(),
    }
    try {
      if (editing) {
        const res = await api.updateAccount(editing.id, payload)
        toast.success(
          res.credentialsUpdated ? "已更新，新凭据已生效" : "已保存（凭据保持不变）",
        )
      } else {
        await api.createAccount(payload)
        toast.success("账号已保存")
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
      await api.deleteAccount(removing.id)
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
          <h1 className="text-xl font-semibold">DNS 账号</h1>
          <p className="text-sm text-muted-foreground">
            已接入 {providers.length} 家服务商，凭据加密后仅用于写入解析
          </p>
        </div>
        <Button onClick={() => openDialog()}>
          <Plus />
          添加账号
        </Button>
      </div>

      <Alert>
        <ShieldCheck />
        <AlertTitle>建议使用最小权限子账号</AlertTitle>
        <AlertDescription>
          只授予目标域名的解析读写权限，避免主账号密钥泄露。所有密钥使用 AES-256-GCM
          加密落库，只在写解析时于内存解密。
        </AlertDescription>
      </Alert>

      {loading ? (
        <Skeleton className="h-40 w-full" />
      ) : accounts.length === 0 ? (
        <Card>
          <CardContent className="flex h-40 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
            <KeyRound className="size-6" />
            还没有配置 DNS 账号
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {accounts.map((a) => (
            <Card key={a.id}>
              <CardHeader>
                <CardTitle className="flex items-center justify-between gap-2 text-base">
                  <span className="truncate">{a.name}</span>
                  <Badge variant="outline">{providerLabel(a.provider)}</Badge>
                </CardTitle>
                <CardDescription className="font-mono text-xs">
                  {a.meta || "凭据已加密"}
                </CardDescription>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                添加于 {new Date(a.createdAt).toLocaleDateString("zh-CN")}
              </CardContent>
              <CardFooter className="justify-end gap-1">
                <Button variant="ghost" size="sm" onClick={() => openDialog(a)}>
                  <Pencil />
                  编辑
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setRemoving(a)}>
                  <Trash2 />
                  删除
                </Button>
              </CardFooter>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑 DNS 账号" : "添加 DNS 账号"}</DialogTitle>
            <DialogDescription>
              {editing
                ? "凭据字段留空则保持现有密钥不变；填写后会先校验再替换"
                : "选择服务商后按提示填写凭据，保存前会自动校验一次"}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-2">
            <div className="grid gap-2">
              <Label>服务商</Label>
              <Select
                value={provider}
                items={Object.fromEntries(providers.map((p) => [p.name, p.label]))}
                onValueChange={(v) => switchProvider(String(v ?? ""))}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="选择 DNS 服务商" />
                </SelectTrigger>
                <SelectContent>
                  {providers.map((p) => (
                    <SelectItem key={p.name} value={p.name}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {current ? (
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <Badge variant="secondary">最多 {current.maxSlots} 条 A 记录</Badge>
                  {current.docs ? (
                    <a
                      className="inline-flex items-center gap-1 underline-offset-4 hover:underline"
                      href={current.docs}
                      target="_blank"
                      rel="noreferrer"
                    >
                      获取凭据
                      <ExternalLink className="size-3" />
                    </a>
                  ) : null}
                </div>
              ) : null}
            </div>

            <div className="grid gap-2">
              <Label htmlFor="acc-name">备注名称</Label>
              <Input
                id="acc-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>

            {current?.note ? (
              <Alert>
                <AlertDescription className="text-xs">{current.note}</AlertDescription>
              </Alert>
            ) : null}

            {current?.fields.map((f) => (
              <div key={f.key} className="grid gap-2">
                <Label htmlFor={`cred-${f.key}`}>
                  {f.label}
                  {f.required && !editing ? <span className="text-destructive"> *</span> : null}
                </Label>
                <Input
                  id={`cred-${f.key}`}
                  type={f.secret ? "password" : "text"}
                  placeholder={
                    editing ? "留空则保持原值" : (f.placeholder ?? "")
                  }
                  value={cred[f.key] ?? ""}
                  onChange={(e) =>
                    setCred((s) => ({ ...s, [f.key]: e.target.value }))
                  }
                />
                {f.help ? (
                  <p className="text-xs text-muted-foreground">{f.help}</p>
                ) : null}
              </div>
            ))}

            <div className="grid gap-2">
              <Label htmlFor="acc-root">校验用域名（可选）</Label>
              <Input
                id="acc-root"
                placeholder="example.com"
                value={root}
                onChange={(e) => setRoot(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                填写后会校验该域名是否在当前账号下，留空则只验证凭据本身
              </p>
            </div>
          </div>

          <DialogFooter className="justify-between sm:justify-between">
            <Button variant="outline" onClick={() => void test()} disabled={testing}>
              {testing ? <Loader2 className="animate-spin" /> : null}
              测试连接
            </Button>
            <div className="flex gap-2">
              <Button variant="ghost" onClick={() => setOpen(false)}>
                取消
              </Button>
              <Button onClick={() => void save()} disabled={saving}>
                {saving ? <Loader2 className="animate-spin" /> : null}
                保存
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!removing} onOpenChange={(v) => !v && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除该 DNS 账号？</AlertDialogTitle>
            <AlertDialogDescription>
              关联的域名调度配置也会一并删除，DNS 服务商上的解析记录保持不变
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
