import { useEffect, useState } from "react"
import { subscribePending } from "@/lib/http"
import { cn } from "@/lib/utils"

/**
 * Thin indeterminate bar pinned to the top of the viewport. It reflects the
 * global in-flight counter maintained by lib/http, so every request in the app
 * shows feedback without each page wiring its own spinner.
 */
export function GlobalProgress() {
  const [pending, setPending] = useState(0)
  const [visible, setVisible] = useState(false)

  useEffect(() => subscribePending(setPending), [])

  useEffect(() => {
    if (pending > 0) {
      // delay slightly so fast requests don't flash the bar
      const t = window.setTimeout(() => setVisible(true), 160)
      return () => window.clearTimeout(t)
    }
    setVisible(false)
    return
  }, [pending])

  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-x-0 top-0 z-[100] h-0.5 overflow-hidden"
    >
      <div
        className={cn(
          "h-full w-1/3 rounded-full bg-primary transition-opacity duration-200",
          visible ? "animate-[pilot-progress_1.1s_ease-in-out_infinite] opacity-100" : "opacity-0",
        )}
      />
    </div>
  )
}
