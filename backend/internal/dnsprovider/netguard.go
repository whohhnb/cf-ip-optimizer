package dnsprovider

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Webhook 型服务商允许用户填写任意回调地址，服务端会主动 POST 过去。
// 这天然是一个 SSRF 入口：地址指向 127.0.0.1 就能打本机管理接口，
// 指向 169.254.169.254 就能读云厂商的实例元数据（里面常带临时凭据）。
//
// 默认只允许公网地址，确实需要内网 webhook 的部署可以在系统设置里
// 打开 allow_private_webhook。
var allowPrivateOutbound atomic.Bool

// SetAllowPrivateOutbound 由配置层在启动和改设置时调用
func SetAllowPrivateOutbound(v bool) { allowPrivateOutbound.Store(v) }

// AllowPrivateOutbound 供测试与诊断读取当前策略
func AllowPrivateOutbound() bool { return allowPrivateOutbound.Load() }

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	// IPv6 唯一本地地址 fc00::/7
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return true
	}
	// 云厂商元数据常用的 169.254.169.254 已被 LinkLocalUnicast 覆盖，
	// 这里额外挡掉 0.0.0.0/8 这类非常规目标
	if len(ip) == net.IPv4len && ip[0] == 0 {
		return true
	}
	return false
}

// CheckOutboundURL 在保存凭据时做一次前置校验，给出即时反馈。
// 真正防 DNS rebinding 的是 guardedDialer（连接时再校验一次解析结果）。
func CheckOutboundURL(ctx context.Context, raw string) error {
	if allowPrivateOutbound.Load() {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("回调地址无法解析")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("回调地址只支持 http 或 https")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("回调地址指向内网或保留地址 %s，已拒绝（如需允许请在系统设置中开启内网 Webhook）", ip)
		}
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return fmt.Errorf("回调地址域名解析失败: %w", err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			return fmt.Errorf("回调地址解析到内网或保留地址 %s，已拒绝（如需允许请在系统设置中开启内网 Webhook）", ip.IP)
		}
	}
	return nil
}

// guardedDialer 在建立连接前校验实际要连的 IP，避免「校验时解析到公网、
// 真正请求时解析到内网」的 DNS rebinding
func guardedDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
}

func guardedTransport(timeout time.Duration) *http.Transport {
	dialer := guardedDialer(timeout)
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if allowPrivateOutbound.Load() {
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				if isBlockedIP(ip.IP) {
					lastErr = fmt.Errorf("目标地址 %s 属于内网或保留网段，已拦截", ip.IP)
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("目标地址无可用的公网 IP")
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
		MaxIdleConns:          1,
	}
}
