package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GoDaddy exposes a set-based API: you PUT the whole A record set for a name.
type GoDaddy struct {
	key    string
	secret string
	client *http.Client
}

const godaddyAPI = "https://api.godaddy.com/v1"

func init() {
	Register(Descriptor{
		Name:     "godaddy",
		Label:    "GoDaddy",
		SetBased: true,
		Docs:     "https://developer.godaddy.com/keys",
		MaxSlots: 2,
		Note:     "GoDaddy 为整组覆盖式接口，写入时会一次性提交全部 A 记录",
		Fields: []Field{
			{Key: "key", Label: "API Key", Required: true},
			{Key: "secret", Label: "API Secret", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &GoDaddy{key: c.Get("key"), secret: c.Get("secret"), client: httpClient()}, nil
	})
}

type godaddyRecord struct {
	Data string `json:"data"`
	Name string `json:"name"`
	TTL  int    `json:"ttl"`
	Type string `json:"type"`
}

func (g *GoDaddy) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, godaddyAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "sso-key "+g.key+":"+g.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("GoDaddy", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (g *GoDaddy) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	var list []godaddyRecord
	if err := g.do(ctx, http.MethodGet, "/domains/"+root+"/records/A/"+normSub(sub), nil, &list); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Record, 0, len(list))
	for i, r := range list {
		out = append(out, Record{
			RecordID: fmt.Sprintf("%d", i),
			Name:     r.Name,
			Value:    r.Data,
			TTL:      r.TTL,
			Status:   "ENABLE",
		})
	}
	return out, nil
}

// ReplaceA overwrites the whole record set in one call.
func (g *GoDaddy) ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error {
	if ttl < 600 {
		ttl = 600 // GoDaddy rejects anything lower
	}
	list := make([]godaddyRecord, 0, len(values))
	for _, v := range values {
		list = append(list, godaddyRecord{Data: v, Name: normSub(sub), TTL: ttl, Type: "A"})
	}
	return g.do(ctx, http.MethodPut, "/domains/"+root+"/records/A/"+normSub(sub), list, nil)
}

func (g *GoDaddy) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	cur, err := g.ListA(ctx, root, sub, line)
	if err != nil {
		return Record{}, err
	}
	values := make([]string, 0, len(cur)+1)
	for _, r := range cur {
		values = append(values, r.Value)
	}
	values = append(values, value)
	if err := g.ReplaceA(ctx, root, sub, values, ttl); err != nil {
		return Record{}, err
	}
	return Record{RecordID: fmt.Sprintf("%d", len(values)-1), Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (g *GoDaddy) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	cur, err := g.ListA(ctx, root, sub, line)
	if err != nil {
		return err
	}
	idx := -1
	fmt.Sscanf(recordID, "%d", &idx)
	if idx < 0 || idx >= len(cur) {
		return fmt.Errorf("记录序号无效: %s", recordID)
	}
	values := make([]string, len(cur))
	for i, r := range cur {
		values[i] = r.Value
	}
	values[idx] = value
	return g.ReplaceA(ctx, root, sub, values, ttl)
}

func (g *GoDaddy) Delete(ctx context.Context, root, recordID string) error {
	return fmt.Errorf("GoDaddy 不支持按记录删除，请使用整组覆盖")
}

func (g *GoDaddy) Verify(ctx context.Context, root string) error {
	if root == "" {
		return g.do(ctx, http.MethodGet, "/domains?limit=1", nil, nil)
	}
	return g.do(ctx, http.MethodGet, "/domains/"+root, nil, nil)
}
