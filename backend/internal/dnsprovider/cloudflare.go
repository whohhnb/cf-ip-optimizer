package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Cloudflare manages records through the v4 API using an API Token.
// proxied is forced to true: a grey-clouded record pointing at an edge IP
// would not be served by Cloudflare at all.
type Cloudflare struct {
	token  string
	client *http.Client
}

const cfAPI = "https://api.cloudflare.com/client/v4"

func init() {
	Register(Descriptor{
		Name:     "cloudflare",
		Label:    "Cloudflare",
		Docs:     "https://dash.cloudflare.com/profile/api-tokens",
		MaxSlots: 2,
		Note:     "Token 需要 Zone.DNS 编辑权限；新建记录会自动开启代理（橙云）",
		Fields: []Field{
			{Key: "token", Label: "API Token", Secret: true, Required: true, Placeholder: "Bearer Token"},
		},
	}, func(c Credentials) (Provider, error) {
		return &Cloudflare{token: c.Get("token"), client: httpClient()}, nil
	})
}

func (cf *Cloudflare) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cf.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cf.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return httpErr("Cloudflare", resp, raw)
	}
	if !envelope.Success {
		msg := "未知错误"
		if len(envelope.Errors) > 0 {
			msg = envelope.Errors[0].Message
		}
		return fmt.Errorf("Cloudflare: %s", msg)
	}
	if out != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

func (cf *Cloudflare) zoneID(ctx context.Context, root string) (string, error) {
	var zones []struct {
		ID string `json:"id"`
	}
	q := url.Values{}
	q.Set("name", root)
	q.Set("per_page", "1")
	if err := cf.do(ctx, http.MethodGet, "/zones?"+q.Encode(), nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("Cloudflare 账号下找不到域名 %s", root)
	}
	return zones[0].ID, nil
}

type cfRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

func (cf *Cloudflare) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	zid, err := cf.zoneID(ctx, root)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("type", "A")
	q.Set("name", fqdn(root, sub))
	q.Set("per_page", "100")
	var list []cfRecord
	if err := cf.do(ctx, http.MethodGet, "/zones/"+zid+"/dns_records?"+q.Encode(), nil, &list); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(list))
	for _, r := range list {
		out = append(out, Record{
			RecordID: r.ID, Name: r.Name, Value: r.Content, TTL: r.TTL,
			Remark: r.Comment, Status: map[bool]string{true: "ENABLE", false: "DISABLE"}[r.Proxied],
		})
	}
	return out, nil
}

func (cf *Cloudflare) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	zid, err := cf.zoneID(ctx, root)
	if err != nil {
		return Record{}, err
	}
	body := map[string]any{
		"type":    "A",
		"name":    fqdn(root, sub),
		"content": value,
		"ttl":     ttl,
		"proxied": true,
	}
	if remark != "" {
		body["comment"] = remark
	}
	var rec cfRecord
	if err := cf.do(ctx, http.MethodPost, "/zones/"+zid+"/dns_records", body, &rec); err != nil {
		return Record{}, err
	}
	return Record{RecordID: rec.ID, Name: rec.Name, Value: rec.Content, TTL: rec.TTL, Remark: remark, Status: "ENABLE"}, nil
}

func (cf *Cloudflare) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	zid, err := cf.zoneID(ctx, root)
	if err != nil {
		return err
	}
	return cf.do(ctx, http.MethodPatch, "/zones/"+zid+"/dns_records/"+recordID,
		map[string]any{"content": value, "ttl": ttl}, nil)
}

func (cf *Cloudflare) Delete(ctx context.Context, root, recordID string) error {
	zid, err := cf.zoneID(ctx, root)
	if err != nil {
		return err
	}
	return cf.do(ctx, http.MethodDelete, "/zones/"+zid+"/dns_records/"+recordID, nil, nil)
}

func (cf *Cloudflare) Verify(ctx context.Context, root string) error {
	if root == "" {
		var zones []cfRecord
		return cf.do(ctx, http.MethodGet, "/zones?per_page=1", nil, &zones)
	}
	_, err := cf.zoneID(ctx, root)
	return err
}
