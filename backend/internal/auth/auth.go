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
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func sign(secret string, data string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(data))
	return b64(m.Sum(nil))
}

// Issue creates an HS256 JWT for the given identity.
func Issue(secret string, id int64, email, role string, ttl time.Duration) (string, error) {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	nowT := time.Now()
	claims := Claims{Sub: id, Email: email, Role: role, Iat: nowT.Unix(), Exp: nowT.Add(ttl).Unix()}
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
