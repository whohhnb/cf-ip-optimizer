package auth

import (
	"testing"
	"time"
)

func TestIssueParseCarriesTokenVersion(t *testing.T) {
	token, err := Issue("secret", 7, "a@example.com", "admin", 3, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse("secret", token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Ver != 3 {
		t.Fatalf("版本号未随 token 传递，得到 %d", claims.Ver)
	}
	if claims.Sub != 7 || claims.Role != "admin" {
		t.Fatalf("claims 内容异常: %+v", claims)
	}
}

func TestParseRejectsTamperedToken(t *testing.T) {
	token, _ := Issue("secret", 1, "a@example.com", "user", 0, time.Hour)
	// 篡改 payload（把版本号改大）必须导致签名校验失败
	tampered := token[:len(token)-4] + "AAAA"
	if _, err := Parse("secret", tampered); err == nil {
		t.Fatal("签名被篡改仍然通过")
	}
	if _, err := Parse("another-secret", token); err == nil {
		t.Fatal("换密钥后仍然通过")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	token, _ := Issue("secret", 1, "a@example.com", "user", 0, -time.Minute)
	if _, err := Parse("secret", token); err == nil {
		t.Fatal("过期 token 仍然通过")
	}
}

// 账号不存在时的假校验必须真的消耗时间，否则时间侧信道依然存在
func TestEqualizeTimingCostsTime(t *testing.T) {
	start := time.Now()
	EqualizeTiming("whatever-password")
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("假校验耗时过短（%s），无法抹平与真实校验的差异", elapsed)
	}
}
