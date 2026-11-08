import {
  ArrowRight,
  Globe2,
  KeyRound,
  LayoutDashboard,
  LogOut,
  RefreshCw,
  Navigation,
  Router,
  ScrollText,
  Settings,
  UserRound,
} from "lucide-react"
import { useState } from "react"
import { NavLink, useLocation, useNavigate } from "react-router-dom"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ChangePasswordDialog } from "@/components/change-password-dialog"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar"
import { ThemeSwitcher } from "@/components/theme-switcher"
import { useAuth } from "@/lib/auth"

const nav = [
  { to: "/", label: "总览", icon: LayoutDashboard },
  { to: "/domains", label: "域名调度", icon: Globe2 },
  { to: "/probes", label: "探针节点", icon: Router },
  { to: "/accounts", label: "DNS 账号", icon: KeyRound },
  { to: "/logs", label: "运行日志", icon: ScrollText },
  { to: "/settings", label: "系统设置", icon: Settings, adminOnly: true },
]

export function AppSidebar() {
  const { pathname } = useLocation()
  const { user, logout } = useAuth()
  const { setOpenMobile } = useSidebar()
  const navigate = useNavigate()
  const [pwOpen, setPwOpen] = useState(false)

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
          <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Navigation className="size-4 fill-current" />
          </div>
          <div className="flex min-w-0 flex-col leading-tight group-data-[collapsible=icon]:hidden">
            <span className="truncate text-sm font-semibold tracking-tight">
              EdgePilot
            </span>
            <span className="truncate text-xs text-muted-foreground">
              边缘优选调度
            </span>
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>控制台</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {nav
                .filter((item) => !item.adminOnly || user?.role === "admin")
                .map((item) => {
                  const active =
                    item.to === "/" ? pathname === "/" : pathname.startsWith(item.to)
                  return (
                    <SidebarMenuItem key={item.to}>
                      <SidebarMenuButton
                        isActive={active}
                        tooltip={item.label}
                        render={<NavLink to={item.to} />}
                        onClick={() => setOpenMobile(false)}
                      >
                        <item.icon />
                        <span>{item.label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  )
                })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup className="mt-auto">
          <SidebarGroupContent>
            <div className="rounded-lg border bg-muted/40 p-3 group-data-[collapsible=icon]:hidden">
              <div className="mb-2 flex items-center gap-1.5 text-xs font-medium">
                <RefreshCw className="size-3.5" />
                双 IP 滚动替换
              </div>
              <div className="flex items-center gap-1">
                <Badge variant="outline" className="px-1.5 font-mono text-[10px]">
                  A
                </Badge>
                <Badge variant="outline" className="px-1.5 font-mono text-[10px]">
                  B
                </Badge>
                <ArrowRight className="size-3 text-muted-foreground" />
                <Badge className="px-1.5 font-mono text-[10px]">C</Badge>
                <Badge variant="outline" className="px-1.5 font-mono text-[10px]">
                  B
                </Badge>
              </div>
              <p className="mt-2 text-[11px] leading-snug text-muted-foreground">
                每轮只换 1 条，始终留一条可用
              </p>
            </div>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <ThemeSwitcher asMenuButton />
          </SidebarMenuItem>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<SidebarMenuButton tooltip={user?.email ?? "账号"} />}
              >
                <UserRound />
                <span className="truncate">{user?.email ?? "未登录"}</span>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="right" align="end" className="w-56">
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="truncate text-xs font-normal text-muted-foreground">
                    {user?.email}
                  </DropdownMenuLabel>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => setPwOpen(true)}>
                  <KeyRound />
                  修改密码
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    void logout().then(() => navigate("/login"))
                  }}
                >
                  <LogOut />
                  退出登录
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
        <Button
          variant="ghost"
          size="xs"
          className="w-full justify-start text-xs text-muted-foreground group-data-[collapsible=icon]:hidden"
          render={<a href="https://www.cloudflare.com/ips/" target="_blank" rel="noreferrer" />}
        >
          Cloudflare 官方地址段
        </Button>
      </SidebarFooter>
      <SidebarRail />
      <ChangePasswordDialog open={pwOpen} onOpenChange={setPwOpen} />
    </Sidebar>
  )
}
