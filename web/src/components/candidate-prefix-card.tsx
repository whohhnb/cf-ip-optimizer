import { useCallback, useEffect, useState } from "react"
import { ExternalLink, Loader2, Save, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api } from "@/lib/api"

const LINES = ["默认", "电信", "联通", "移动", "教育网", "境外"]

export function CandidatePrefixCard() {
  const [lines, setLines] = useState<Record<string, string[]>>({})
  const [line, setLine] = useState("电信")
  const [draft, setDraft] = useState("")
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const apply = useCallback((all: Record<string, string[]>, target: string) => {
    setLines(all)
    setDraft((all[target] ?? []).join("\n"))
  }, [])

  useEffect(() => {
    api
      .candidatePrefixes()
      .then((d) => apply(d.lines ?? {}, "电信"))
      .catch((e) => toast.error(e instanceof Error ? e.message : "读取候选前缀失败"))
      .finally(() => setLoading(false))
  }, [apply])

  const current = lines[line] ?? []

  const onLine = (v: string | null) => {
    const next = v || "默认"
    setLine(next)
    setDraft((lines[next] ?? []).join("\n"))
  }

  const save = async () => {
    setSaving(true)
    try {
      const prefixes = draft
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean)
      const d = await api.saveCandidatePrefixes(line, prefixes)
      setLines((s) => ({ ...s, [d.line]: d.prefixes }))
      setDraft(d.prefixes.join("\n"))
      toast.success(
        d.prefixes.length
          ? `${d.line} 线路已保存 ${d.prefixes.length} 个前缀`
          : `${d.line} 线路的清单已清空`
      )
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  const remove = async (prefix: string) => {
    try {
      await api.deleteCandidatePrefix(line, prefix)
      const next = current.filter((p) => p !== prefix)
      setLines((s) => ({ ...s, [line]: next }))
      setDraft(next.join("\n"))
      toast.success(`已删除 ${prefix}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "删除失败")
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          候选前缀
          <Badge variant="secondary" className="font-normal">
            {current.length} 个
          </Badge>
        </CardTitle>
        <CardDescription>
          按线路维护的静态清单。扫描只读它，用来决定「往哪几个 /24 找地址」，绝不会被扫描结果改写。
        </CardDescription>
      </CardHeader>

      <CardContent className="grid gap-4">
        <div className="grid gap-2 sm:max-w-xs">
          <Label htmlFor="candidate-line">线路</Label>
          <Select value={line} items={Object.fromEntries(LINES.map((l) => [l, l]))} onValueChange={onLine}>
            <SelectTrigger id="candidate-line" className="w-full">
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

        {loading ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            读取中
          </div>
        ) : (
          <>
            {current.length > 0 ? (
              <div className="divide-y rounded-lg border">
                {current.map((p) => (
                  <div key={p} className="flex items-center justify-between gap-3 px-3 py-2">
                    <code className="text-sm">{p}</code>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-7 text-muted-foreground hover:text-destructive"
                      onClick={() => remove(p)}
                      aria-label={`删除 ${p}`}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed px-4 py-6 text-center">
                <p className="text-sm text-muted-foreground">
                  「{line}」还没有配置前缀，这条线路目前是纯随机采样。
                </p>
                <p className="mt-1 text-sm text-muted-foreground">
                  想提高命中率？在对应线路的探针上跑一遍 /24 普查，把输出直接贴到下面。
                </p>
                <a
                  href="https://github.com/whohhnb/cf-ip-optimizer/blob/main/scripts/prefix-survey.py"
                  target="_blank"
                  rel="noreferrer"
                  className="mt-3 inline-flex items-center gap-1 text-sm text-primary underline underline-offset-4 hover:no-underline"
                >
                  查看普查脚本
                  <ExternalLink className="size-3.5" />
                </a>
              </div>
            )}

            <div className="grid gap-2">
              <Label htmlFor="candidate-draft">清单（一行一个，填 IP 或 /24 都行）</Label>
              <textarea
                id="candidate-draft"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                spellCheck={false}
                placeholder={"每行一个，例如：\n172.64.53.0/24"}
                className="min-h-32 w-full resize-y rounded-lg border bg-transparent px-3 py-2 font-mono text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
              />
              <p className="text-xs text-muted-foreground">
                只接受 Cloudflare 官方 15 段内的地址，统一收敛到 /24。保存是整体替换，不是追加。
              </p>
            </div>

            <div className="rounded-lg bg-muted/50 px-3 py-2.5 text-xs leading-relaxed text-muted-foreground">
              <p>扫描时每个前缀最多取 3 个地址实测，测出来什么就是什么——这张表只决定「往哪看」，选谁下发仍由当轮结果决定。</p>
              <p className="mt-1">
                三网前缀不通用：同一个 /24 在电信可能落东京、移动落香港、联通落洛杉矶，每条线路要各配一份。
              </p>
            </div>

            <div>
              <Button onClick={save} disabled={saving}>
                {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                保存「{line}」的清单
              </Button>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}
