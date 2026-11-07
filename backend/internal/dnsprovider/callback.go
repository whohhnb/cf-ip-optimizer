package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Callback posts the chosen address to a user-supplied webhook. It is the
// escape hatch for any DNS vendor without a native adapter: point it at your
// own bridge script and the platform can still drive it.
type Callback struct {
	url    string
	secret string
	client *http.Client
}

func init() {
	Register(Descriptor{
		Name:     "callback",
		Label:    "自定义 Webhook",
		NoList:   true,
		Docs:     "https://github.com/jeessy2/ddns-go",
		MaxSlots: 1,
		Note:     "每次下发都会 POST 一条 JSON 到你提供的地址，由你的脚本负责改解析",
		Fields: []Field{
			{Key: "url", Label: "回调地址", Placeholder: "https://example.com/hook", Required: true},
			{Key: "secret", Label: "鉴权 Token（可选）", Secret: true, Help: "会以 Authorization: Bearer <token> 发送"},
		},
	}, func(c Credentials) (Provider, error) {
		return &Callback{url: c.Get("url"), secret: c.Get("secret"), client: httpClient()}, nil
	})
}

type callbackPayload struct {
	RootDomain string `json:"rootDomain"`
	SubDomain  string `json:"subDomain"`
	Host       string `json:"host"`
	Type       string `json:"type"`
	Value      string `json:"value"`
	TTL        int    `json:"ttl"`
	Remark     string `json:"remark,omitempty"`
}

func (c *Callback) post(ctx context.Context, p callbackPayload) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode >= 400 {
		return httpErr("Webhook", resp, body)
	}
	return nil
}

// ListA cannot enumerate remote state, so it reports nothing and every write is
// an idempotent push.
func (c *Callback) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	return nil, nil
}

func (c *Callback) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	err := c.post(ctx, callbackPayload{
		RootDomain: root, SubDomain: normSub(sub), Host: fqdn(root, sub),
		Type: "A", Value: value, TTL: ttl, Remark: remark,
	})
	if err != nil {
		return Record{}, err
	}
	return Record{RecordID: normSub(sub), Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (c *Callback) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	_, err := c.Create(ctx, root, sub, value, line, ttl, "")
	return err
}

func (c *Callback) Delete(ctx context.Context, root, recordID string) error {
	return fmt.Errorf("Webhook 模式不支持删除记录")
}

func (c *Callback) Verify(ctx context.Context, root string) error {
	if c.url == "" {
		return fmt.Errorf("请填写回调地址")
	}
	return nil
}
