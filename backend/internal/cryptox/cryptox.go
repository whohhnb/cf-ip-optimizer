// Package cryptox provides AES-256-GCM sealing for at-rest credentials and
// password hashing helpers.
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrWrongKey 表示密文无法用当前密钥解开。绝大多数情况是 PILOT_SECRET 变了
// （换过环境变量、换过数据目录导致重新生成了 secret.key），而库里存的是用
// 旧密钥加密的凭据。GCM 的认证标签会直接失败，没有任何补救余地，只能重新填。
var ErrWrongKey = errors.New("credential cannot be decrypted with the current key")

type Box struct {
	aead cipher.AEAD
}

// New derives a 32-byte key from an arbitrary secret string.
func New(secret string) (*Box, error) {
	sum := sha256.Sum256([]byte("edgepilot/v1:" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal returns base64(nonce||ciphertext).
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrWrongKey, err)
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("%w: ciphertext too short", ErrWrongKey)
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrWrongKey, err)
	}
	return string(plain), nil
}
