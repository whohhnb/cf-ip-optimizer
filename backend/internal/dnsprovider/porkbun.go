package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Porkbun uses a JSON API where credentials travel in the request body.
type Porkbun struct {
	apiKey string
	secret string
	client *http.Client
}

const porkbunAPI = "https://api.porkbun.com/api/json/v3"

func init() {
	Register(Descriptor{
		Name:     "porkbun",
		Label:    "Porkbun",
		Docs:     "https://porkbun.com/account/api",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "apiKey", Label: "API Key", Placeholder: "pk1_...", Required: true},
			{Key: "secretApiKey", Label: "Secret API Key", Secret: true, Required: true, Help: "需在 Porkbun 后台为该 Key 开启 API 访问"},
		},
	}, func(c Credentials) (Provider, error) {
		return &Porkbun{apiKey: c.Get("apiKey"), secret: c.Get("secretApiKey"), client: httpClient()}, nil
	})
}

func (p *Porkbun) do(ctx context.Context, path string, body map[string]any, out any) error {
	body["apikey"] = p.apiKey
	body["secretapikey"] = p.secret
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, porkbunAPI+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("Porkbun", resp, data)
	}
	var status struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &status)
	if status.Status == "ERROR" {
		return fmt.Errorf("Porkbun: %s", status.Message)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (p *Porkbun) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	var out struct {
		Records []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Content string `json:"content"`
			TTL     string `json:"ttl"`
		} `json:"records"`
	}
	if err := p.do(ctx, "/dns/retrieve/"+root, map[string]any{}, &out); err != nil {
		return nil, err
	}
	want := normSub(sub)
	records := make([]Record, 0, 2)
	for _, r := range out.Records {
		if r.Type != "A" || r.Name != want {
			continue
		}
		records = append(records, Record{
			RecordID: r.ID, Name: r.Name, Value: r.Content, Status: "ENABLE",
		})
	}
	return records, nil
}

func (p *Porkbun) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	body := map[string]any{
		"name":    normSub(sub),
		"type":    "A",
		"content": value,
		"ttl":     fmt.Sprintf("%d", ttl),
	}
	if err := p.do(ctx, "/dns/create/"+root, body, nil); err != nil {
		return Record{}, err
	}
	// the create call does not return the new id, so re-read it
	list, err := p.ListA(ctx, root, sub, line)
	if err != nil {
		return Record{}, err
	}
	for _, r := range list {
		if r.Value == value {
			return r, nil
		}
	}
	return Record{RecordID: value, Name: normSub(sub), Value: value}, nil
}

func (p *Porkbun) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return p.do(ctx, "/dns/edit/"+root+"/"+recordID, map[string]any{
		"name":    normSub(sub),
		"type":    "A",
		"content": value,
		"ttl":     fmt.Sprintf("%d", ttl),
	}, nil)
}

func (p *Porkbun) Delete(ctx context.Context, root, recordID string) error {
	return p.do(ctx, "/dns/delete/"+root+"/"+recordID, map[string]any{}, nil)
}

func (p *Porkbun) Verify(ctx context.Context, root string) error {
	if root == "" {
		return p.do(ctx, "/ping", map[string]any{}, nil)
	}
	_, err := p.ListA(ctx, root, "@", "")
	return err
}
