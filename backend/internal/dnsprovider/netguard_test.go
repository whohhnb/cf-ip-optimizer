package dnsprovider

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // 本机
		"127.1.2.3",       // 本机网段
		"10.0.0.5",        // 私有
		"172.16.3.4",      // 私有
		"192.168.1.1",     // 私有
		"169.254.169.254", // 云厂商元数据
		"0.0.0.0",         // 未指定
		"::1",             // IPv6 本机
		"fe80::1",         // IPv6 链路本地
		"fc00::1",         // IPv6 ULA
		"224.0.0.1",       // 组播
	}
	for _, s := range blocked {
		if !isBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s 应当被拦截", s)
		}
	}

	allowed := []string{"1.1.1.1", "8.8.8.8", "104.16.0.1", "2606:4700::1"}
	for _, s := range allowed {
		if isBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s 不应被拦截", s)
		}
	}
}

func TestCheckOutboundURLRejectsPrivate(t *testing.T) {
	SetAllowPrivateOutbound(false)
	defer SetAllowPrivateOutbound(false)

	bad := []string{
		"http://127.0.0.1:8080/hook",
		"http://localhost:9000/x",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.1.2.3/hook",
		"ftp://example.com/hook",
		"not-a-url",
	}
	for _, u := range bad {
		if err := CheckOutboundURL(context.Background(), u); err == nil {
			t.Errorf("%s 应当被拒绝", u)
		}
	}

	if err := CheckOutboundURL(context.Background(), "https://1.1.1.1/hook"); err != nil {
		t.Errorf("公网地址不应被拒: %v", err)
	}
}

func TestAllowPrivateOutboundSwitch(t *testing.T) {
	SetAllowPrivateOutbound(true)
	defer SetAllowPrivateOutbound(false)

	if err := CheckOutboundURL(context.Background(), "http://127.0.0.1:8080/hook"); err != nil {
		t.Fatalf("开关打开后应放行内网地址: %v", err)
	}
}

// 连接层才是真正的防线：即使域名在校验时解析到公网、连接时改指内网
// （DNS rebinding），guardedTransport 也会在内网 IP 上拒绝建连
func TestGuardedTransportBlocksRebinding(t *testing.T) {
	SetAllowPrivateOutbound(false)
	defer SetAllowPrivateOutbound(false)

	tr := guardedTransport(2e9)
	defer tr.CloseIdleConnections()

	// localhost 一定解析到 127.0.0.1，模拟 rebinding 的最终结果
	_, err := tr.DialContext(context.Background(), "tcp", "localhost:9")
	if err == nil {
		t.Fatal("应当拒绝连接本机地址")
	}
	if !strings.Contains(err.Error(), "内网") && !strings.Contains(err.Error(), "保留") {
		t.Fatalf("错误信息应说明被拦截，实际: %v", err)
	}
}
