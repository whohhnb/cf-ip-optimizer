import { useEffect, useRef } from "react"

/**
 * 页面可见时按固定间隔静默刷新，切到后台标签页就停。
 *
 * 没用 SSE：扫描进度是按域名订阅的，列表页要同时盯多个域名，开一堆连接
 * 不划算。轮询在这里够用，而且不会因为某次请求失败就断掉。
 */
export function usePoll(fn: () => void | Promise<void>, ms: number) {
  const ref = useRef(fn)
  ref.current = fn

  useEffect(() => {
    if (ms <= 0) return
    let timer: number | undefined

    const stop = () => {
      if (timer !== undefined) window.clearInterval(timer)
      timer = undefined
    }
    const start = () => {
      stop()
      timer = window.setInterval(() => {
        if (document.visibilityState === "visible") void ref.current()
      }, ms)
    }
    const onVisibility = () => {
      if (document.visibilityState === "visible") {
        // 切回来先立刻刷一次，不用等下一个周期
        void ref.current()
        start()
      } else {
        stop()
      }
    }

    start()
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      stop()
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [ms])
}
