import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"

export type ColorTheme = {
  id: string
  label: string
  /** seed used for the swatch in the picker; mirrors --seed-primary */
  swatch: string
}

/** Material Design 3 inspired palettes. Order matters: first is the default. */
export const DEFAULT_THEME = "emerald"

export const COLOR_THEMES: ColorTheme[] = [
  { id: "emerald", label: "翡翠", swatch: "oklch(0.49 0.12 148)" },
  { id: "violet", label: "紫罗兰", swatch: "oklch(0.50 0.15 302)" },
  { id: "azure", label: "天青", swatch: "oklch(0.51 0.17 255)" },
  { id: "teal", label: "青碧", swatch: "oklch(0.48 0.09 190)" },
  { id: "amber", label: "琥珀", swatch: "oklch(0.63 0.13 72)" },
  { id: "rose", label: "玫红", swatch: "oklch(0.52 0.16 12)" },
  { id: "graphite", label: "石墨", swatch: "oklch(0.32 0.015 260)" },
]

const STORAGE_KEY = "edgepilot.color"

type Ctx = {
  theme: string
  setTheme: (id: string) => void
}

const ColorThemeContext = createContext<Ctx | null>(null)

function applyTheme(id: string) {
  const root = document.documentElement
  root.dataset.theme = id
}

export function ColorThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<string>(() => {
    const saved = localStorage.getItem(STORAGE_KEY)
    return COLOR_THEMES.some((t) => t.id === saved) ? (saved as string) : DEFAULT_THEME
  })

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const setTheme = useCallback((id: string) => {
    if (!COLOR_THEMES.some((t) => t.id === id)) return
    localStorage.setItem(STORAGE_KEY, id)
    setThemeState(id)
  }, [])

  const value = useMemo(() => ({ theme, setTheme }), [theme, setTheme])
  return <ColorThemeContext.Provider value={value}>{children}</ColorThemeContext.Provider>
}

export function useColorTheme(): Ctx {
  const ctx = useContext(ColorThemeContext)
  if (!ctx) throw new Error("useColorTheme must be used inside ColorThemeProvider")
  return ctx
}
