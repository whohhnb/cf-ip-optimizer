import { http } from "@/lib/http"

export type User = {
  id: number
  email: string
  role: "admin" | "user"
  createdAt: string
}

export type ProviderField = {
  key: string
  label: string
  placeholder?: string
  help?: string
  secret: boolean
  required: boolean
}

export type ProviderDescriptor = {
  name: string
  label: string
  docs?: string
  fields: ProviderField[]
  maxSlots: number
  note?: string
  /** vendor replaces a whole record set per write (RRset semantics) */
  setBased: boolean
  /** vendor cannot enumerate remote records (webhook etc) */
  noList: boolean
}

/** Compact provider info attached to a domain. */
export type ProviderInfo = {
  name: string
  label: string
  maxSlots: number
  setBased: boolean
  noList: boolean
  note?: string
}

export type Account = {
  id: number
  userId: number
  name: string
  provider: string
  meta: string
  createdAt: string
}

export type Domain = {
  id: number
  userId: number
  accountId: number
  rootDomain: string
  subDomain: string
  recordLine: string
  ttl: number
  slots: number
  autoPush: boolean
  intervalMin: number
  enabled: boolean
  status: string
  statusMsg: string
  lastScanAt: string | null
  lastPushAt: string | null
  createdAt: string
}

export type DomainRow = {
  domain: Domain
  full: string
  running: boolean
  provider: ProviderInfo
}

export type ScanJob = {
  id: number
  domainId: number
  userId: number
  trigger: string
  status: "running" | "done" | "failed" | "canceled"
  candidates: number
  tested: number
  ok: number
  progress: number
  stage: string
  error: string
  pushed: boolean
  topIp: string
  topLatency: number
  topSpeed: number
  startedAt: string
  finishedAt: string | null
}

export type ScanResult = {
  id: number
  jobId: number
  ip: string
  latency: number
  jitter: number
  loss: number
  speed: number
  score: number
  colo: string
  ok: boolean
}

export type ManagedRecord = {
  id: number
  domainId: number
  recordId: string
  slot: number
  value: string
  updatedAt: string
}

export type LiveRecord = {
  RecordID: string
  Name: string
  Value: string
  Line: string
  TTL: number
  Remark: string
  Status: string
}

export type LogEntry = {
  id: number
  userId: number
  domainId: number
  level: "info" | "warn" | "error"
  message: string
  createdAt: string
}

export type Overview = {
  domains: number
  healthy: number
  scanning: number
  avgLatency: number
  bestLatency: number
  pushesToday: number
  lastScanAt: string
}

export type TrendPoint = { t: number; latency: number; speed: number }

export type PublicConfig = {
  allowRegistration: boolean
  needsSetup: boolean
  version: string
}

export type ScanEvent = {
  type: "progress" | "done" | "error"
  jobId: number
  domainId: number
  stage?: string
  tested: number
  total: number
  ok: number
  progress: number
  msg?: string
  error?: string
}

export { getToken, setToken } from "@/lib/token"
export { ApiError } from "@/lib/http"

export const api = {
  publicConfig: () => http<PublicConfig>("/api/config"),

  login: (email: string, password: string) =>
    http<{ token: string; user: User }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),

  register: (email: string, password: string) =>
    http<{ token: string; user: User }>("/api/auth/register", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),

  logout: () => http<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),

  me: () => http<{ user: User; allowRegistration: boolean }>("/api/auth/me"),

  overview: () => http<Overview>("/api/overview"),

  trend: (domainId?: number) =>
    http<{ points: TrendPoint[] }>(
      `/api/trend${domainId ? `?domainId=${domainId}` : ""}`,
    ),

  logs: (limit = 100, level = "all") =>
    http<{ logs: LogEntry[] }>(`/api/logs?limit=${limit}&level=${level}`),

  providers: () =>
    http<{ providers: ProviderDescriptor[] }>("/api/providers"),

  accounts: () => http<{ accounts: Account[] }>("/api/accounts"),

  createAccount: (body: {
    name: string
    provider: string
    credentials: Record<string, string>
    root?: string
  }) =>
    http<{ account: Account }>("/api/accounts", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateAccount: (
    id: number,
    body: {
      name: string
      provider: string
      credentials: Record<string, string>
      root?: string
    },
  ) =>
    http<{ ok: boolean; credentialsUpdated: boolean }>(`/api/accounts/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  testAccount: (body: {
    provider: string
    credentials: Record<string, string>
    root?: string
  }) =>
    http<{ message: string }>("/api/accounts/test", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  deleteAccount: (id: number) =>
    http<{ ok: boolean }>(`/api/accounts/${id}`, { method: "DELETE" }),

  domains: () => http<{ domains: DomainRow[] }>("/api/domains"),

  domain: (id: number) =>
    http<{
      domain: Domain
      full: string
      managed: ManagedRecord[]
      job: ScanJob | null
      top: ScanResult[]
      running: boolean
      provider: ProviderInfo
    }>(`/api/domains/${id}`),

  createDomain: (body: Record<string, unknown>) =>
    http<{ domain: Domain; job: ScanJob | null }>("/api/domains", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateDomain: (id: number, body: Record<string, unknown>) =>
    http<{ domain: Domain }>(`/api/domains/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  deleteDomain: (id: number) =>
    http<{ ok: boolean }>(`/api/domains/${id}`, { method: "DELETE" }),

  scan: (id: number) =>
    http<{ job: ScanJob }>(`/api/domains/${id}/scan`, { method: "POST" }),

  push: (id: number) =>
    http<{ message: string; count: number }>(`/api/domains/${id}/push`, {
      method: "POST",
    }),

  records: (id: number) =>
    http<{ records: LiveRecord[]; full: string }>(`/api/domains/${id}/records`),

  jobs: (id: number, limit = 20) =>
    http<{ jobs: ScanJob[] }>(`/api/domains/${id}/jobs?limit=${limit}`),

  job: (id: number) =>
    http<{ job: ScanJob; results: ScanResult[] }>(`/api/jobs/${id}`),

  settings: () =>
    http<{ settings: Record<string, string>; defaults: Record<string, string> }>(
      "/api/settings",
    ),

  updateSettings: (body: Record<string, string>) =>
    http<{ settings: Record<string, string> }>("/api/settings", {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  users: () => http<{ users: User[] }>("/api/users"),

  deleteUser: (id: number) =>
    http<{ ok: boolean }>(`/api/users/${id}`, { method: "DELETE" }),
}
