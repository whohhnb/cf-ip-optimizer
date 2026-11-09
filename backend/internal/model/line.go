package model

import "strings"

// LineKey normalises a provider-specific line value into one canonical label.
//
// 同一个「电信」在不同服务商那里叫法完全不同：DNSPod 用中文，阿里云用
// telecom。不归一的话，同一条线路会在候选前缀表里裂成两行，各配各的。
func LineKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "", "默认", "default", "0", "all":
		return "默认"
	case "电信", "telecom", "ct", "chinatelecom", "中国电信":
		return "电信"
	case "联通", "unicom", "cu", "chinaunicom", "中国联通":
		return "联通"
	case "移动", "mobile", "cm", "chinamobile", "中国移动":
		return "移动"
	case "教育网", "edu", "cernet", "中国教育网":
		return "教育网"
	case "境外", "overseas", "海外", "abroad":
		return "境外"
	}
	return strings.TrimSpace(raw)
}
