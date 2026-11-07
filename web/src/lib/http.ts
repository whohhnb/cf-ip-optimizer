import { getToken } from "@/lib/token"

/** Fired when the backend answers 401 so the app can drop the session. */
export const UNAUTHORIZED_EVENT = "edgepilot:unauthorized"

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

type PendingListener = (pending: number) => void

let pending = 0
const listeners = new Set<PendingListener>()

function emit() {
  for (const fn of listeners) fn(pending)
}

/** Subscribe to the global in-flight request counter (drives the top bar). */
export function subscribePending(fn: PendingListener): () => void {
  listeners.add(fn)
  fn(pending)
  return () => {
    listeners.delete(fn)
  }
}

export function pendingCount(): number {
  return pending
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

type Options = RequestInit & { timeoutMs?: number }

/**
 * Single entry point for every backend call.
 *
 * - tracks in-flight requests for the global progress bar
 * - normalises error payloads into ApiError
 * - fires UNAUTHORIZED_EVENT on 401 so auth state can be dropped in one place
 * - enforces a client-side timeout
 */
export async function http<T>(path: string, options: Options = {}): Promise<T> {
  const { timeoutMs = 20000, signal, ...rest } = options

  const headers = new Headers(rest.headers)
  if (rest.body) headers.set("Content-Type", "application/json")
  const token = getToken()
  if (token) headers.set("Authorization", `Bearer ${token}`)

  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeoutMs)
  if (signal) {
    signal.addEventListener("abort", () => controller.abort(), { once: true })
  }

  pending += 1
  emit()
  try {
    const res = await fetch(path, {
      ...rest,
      headers,
      credentials: "include",
      signal: controller.signal,
    })
    const text = await res.text()
    const data = text ? safeParse(text) : null

    if (res.status === 401) {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
    }
    if (!res.ok) {
      const message =
        (data && typeof data === "object" && "error" in data
          ? String((data as { error: unknown }).error)
          : "") || `请求失败 (${res.status})`
      throw new ApiError(res.status, message)
    }
    return data as T
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new ApiError(0, "请求超时，请检查网络后重试")
    }
    throw err
  } finally {
    pending -= 1
    emit()
    window.clearTimeout(timer)
  }
}
