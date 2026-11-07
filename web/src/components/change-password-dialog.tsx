import { useState } from "react"
import { Loader2 } from "lucide-react"
import { toast } from "sonner"
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
import { api, setToken } from "@/lib/api"

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ChangePasswordDialog({ open, onOpenChange }: Props) {
  const [oldPw, setOldPw] = useState("")
  const [newPw, setNewPw] = useState("")
  const [confirm, setConfirm] = useState("")
  const [busy, setBusy] = useState(false)

  function reset() {
    setOldPw("")
    setNewPw("")
    setConfirm("")
  }

  async function submit() {
    if (!oldPw || !newPw) {
      toast.error("请填写原密码与新密码")
      return
    }
    if (newPw.length < 8) {
      toast.error("新密码至少 8 位")
      return
    }
    if (newPw !== confirm) {
      toast.error("两次输入的新密码不一致")
      return
    }
    setBusy(true)
    try {
      const res = await api.changePassword(oldPw, newPw)
      // 服务端把 token 版本 +1 了，旧 token 已作废，换上新签发的这个
      setToken(res.token)
      toast.success("密码已更新，其他设备已下线")
      reset()
      onOpenChange(false)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "修改失败")
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) reset()
        onOpenChange(v)
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>修改密码</DialogTitle>
          <DialogDescription>
            改完后其他设备上的登录会立即失效，当前设备保持登录
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 py-2">
          <div className="grid gap-2">
            <Label htmlFor="old-pw">原密码</Label>
            <Input
              id="old-pw"
              type="password"
              autoComplete="current-password"
              value={oldPw}
              onChange={(e) => setOldPw(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="new-pw">新密码</Label>
            <Input
              id="new-pw"
              type="password"
              autoComplete="new-password"
              placeholder="至少 8 位"
              value={newPw}
              onChange={(e) => setNewPw(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="confirm-pw">确认新密码</Label>
            <Input
              id="confirm-pw"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => void submit()} disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : null}
            确认修改
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
