import { Check, Moon, Palette, Sun } from "lucide-react"
import { useTheme } from "next-themes"
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
import { SidebarMenuButton } from "@/components/ui/sidebar"
import { COLOR_THEMES, useColorTheme } from "@/lib/theme"

type Props = {
  /** render as a sidebar menu row (used in the footer) */
  asMenuButton?: boolean
}

export function ThemeSwitcher({ asMenuButton = false }: Props) {
  const { theme, setTheme } = useColorTheme()
  const { resolvedTheme, setTheme: setMode } = useTheme()
  const isDark = resolvedTheme === "dark"

  const trigger = asMenuButton ? (
    <SidebarMenuButton tooltip="主题与外观">
      <Palette />
      <span>主题与外观</span>
    </SidebarMenuButton>
  ) : (
    <Button variant="ghost" size="sm" className="w-full justify-start">
      <Palette />
      主题
    </Button>
  )

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} />
      <DropdownMenuContent align="start" side="right" sideOffset={8} className="w-48">
        <DropdownMenuGroup>
          <DropdownMenuLabel>配色（Material 3）</DropdownMenuLabel>
          {COLOR_THEMES.map((t) => (
            <DropdownMenuItem key={t.id} onClick={() => setTheme(t.id)}>
              <span
                className="size-3.5 shrink-0 rounded-full ring-1 ring-black/10 dark:ring-white/20"
                style={{ background: t.swatch }}
              />
              <span className="flex-1">{t.label}</span>
              {theme === t.id ? <Check className="size-3.5" /> : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => setMode(isDark ? "light" : "dark")}>
          {isDark ? <Sun /> : <Moon />}
          <span className="flex-1">{isDark ? "切换为浅色" : "切换为深色"}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
