package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	Sub   int64  `json:"sub"`
	Email string `json:"email"`
	Role  string `json:"role"`
	// Ver 对应 users.token_version，改密码或登出后旧 token 立即失效
	Ver int64 `json:"ver"`
	Iat int64 `json:"iat"`
	Exp int64 `json:"exp"`
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash 用于抹平「账号不存在」与「密码错误」的响应时间差。
// 直接 return 的话，不存在的邮箱 0ms 返回、存在的要跑一次 bcrypt（约 60ms），
// 攻击者据此可以枚举出哪些邮箱注册过。
var dummyHash string

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("edgepilot-timing-equalizer"), bcrypt.DefaultCost)
	if err == nil {
		dummyHash = string(h)
	}
}

// EqualizeTiming 在没有真实哈希可比时消耗与一次校验相当的时间
func EqualizeTiming(pw string) {
	if dummyHash == "" {
		return
	}
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(pw))
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func sign(secret string, data string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(data))
	return b64(m.Sum(nil))
}

// Issue creates an HS256 JWT for the given identity.
func Issue(secret string, id int64, email, role string, ver int64, ttl time.Duration) (string, error) {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	nowT := time.Now()
	claims := Claims{Sub: id, Email: email, Role: role, Ver: ver, Iat: nowT.Unix(), Exp: nowT.Add(ttl).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	p := b64(payload)
	return header + "." + p + "." + sign(secret, header+"."+p), nil
}

func Parse(secret, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	expected := sign(secret, parts[0]+"."+parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if c.Exp < time.Now().Unix() {
		return nil, ErrInvalidToken
	}
	return &c, nil
}
