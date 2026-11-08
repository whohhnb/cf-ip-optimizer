import { useEffect, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Loader2, Navigation, ShieldCheck } from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, type PublicConfig } from "@/lib/api"
import { useAuth } from "@/lib/auth"

export default function LoginPage() {
  const { user, login, register } = useAuth()
  const navigate = useNavigate()
  const [cfg, setCfg] = useState<PublicConfig | null>(null)
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .publicConfig()
      .then(setCfg)
      .catch(() => setCfg({ allowRegistration: false, needsSetup: false, version: "" }))
  }, [])

  useEffect(() => {
    if (user) navigate("/", { replace: true })
  }, [user, navigate])

  async function submit(mode: "login" | "register") {
    if (!email || !password) {
      toast.error("请填写邮箱与密码")
      return
    }
    setBusy(true)
    try {
      if (mode === "login") await login(email, password)
      else await register(email, password)
      toast.success(mode === "login" ? "欢迎回来" : "账号已创建")
      navigate("/", { replace: true })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "操作失败")
    } finally {
      setBusy(false)
    }
  }

  const form = (mode: "login" | "register") => (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        void submit(mode)
      }}
    >
      <div className="grid gap-2">
        <Label htmlFor={`${mode}-email`}>邮箱</Label>
        <Input
          id={`${mode}-email`}
          type="email"
          autoComplete="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={`${mode}-password`}>密码</Label>
        <Input
          id={`${mode}-password`}
          type="password"
          autoComplete={mode === "login" ? "current-password" : "new-password"}
          placeholder={mode === "register" ? "至少 8 位" : "••••••••"}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
      <Button type="submit" className="w-full" disabled={busy}>
        {busy ? <Loader2 className="animate-spin" /> : null}
        {mode === "login" ? "登录控制台" : "创建账号"}
      </Button>
    </form>
  )

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 p-6">
      <div className="w-full max-w-md">
        <div className="mb-6 flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Navigation className="size-5 fill-current" />
          </div>
          <div>
            <h1 className="text-lg font-semibold leading-tight tracking-tight">
              EdgePilot
            </h1>
            <p className="text-sm text-muted-foreground">
              Cloudflare 优选 IP · 16 家 DNS 服务商 · 双 IP 滚动替换
            </p>
          </div>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>
              {cfg?.needsSetup ? "初始化管理员" : "登录控制台"}
            </CardTitle>
            <CardDescription>
              {cfg?.needsSetup
                ? "首次部署，第一个注册的账号将自动成为管理员"
                : "使用你的账号访问优选调度平台"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {cfg?.allowRegistration ? (
              <Tabs defaultValue={cfg?.needsSetup ? "register" : "login"}>
                <TabsList className="mb-4 grid w-full grid-cols-2">
                  <TabsTrigger value="login">登录</TabsTrigger>
                  <TabsTrigger value="register">注册</TabsTrigger>
                </TabsList>
                <TabsContent value="login">{form("login")}</TabsContent>
                <TabsContent value="register">{form("register")}</TabsContent>
              </Tabs>
            ) : (
              form("login")
            )}
          </CardContent>
          <CardFooter>
            <Alert>
              <ShieldCheck />
              <AlertTitle>凭据本地加密</AlertTitle>
              <AlertDescription>
                DNS SecretKey 使用 AES-256-GCM 加密落库，仅在下发解析时于内存解密
              </AlertDescription>
            </Alert>
          </CardFooter>
        </Card>
      </div>
    </div>
  )
}
