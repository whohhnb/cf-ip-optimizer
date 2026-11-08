import type { Probe } from "@/lib/api"

/**
 * 线路在库里存的是服务商原生值（DNSPod 是「电信」，阿里云是 telecom），
 * 界面上统一按 label 显示，所以哪里要显示线路都要走这个函数。
 */
export function lineLabel(value: string, lines?: { value: string; label: string }[]) {
  if (!value) return "默认"
  if (!lines || lines.length === 0) {
    return value === "default" ? "默认" : value
  }
  return lines.find((l) => l.value === value)?.label ?? value
}

/** 一个域名可以绑多个探针，展示时要把它们都列出来 */
export function probeNames(ids: number[] | undefined, probes: Probe[]) {
  return (ids ?? []).map((id) => probes.find((p) => p.id === id)?.name ?? "已删除")
}
