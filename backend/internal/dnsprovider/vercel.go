package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Vercel DNS is a plain per-record REST API.
type Vercel struct {
	token  string
	client *http.Client
}

const vercelAPI = "https://api.vercel.com"

func init() {
	Register(Descriptor{
		Name:     "vercel",
		Label:    "Vercel DNS",
		Docs:     "https://vercel.com/account/tokens",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "token", Label: "API Token", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &Vercel{token: c.Get("token"), client: httpClient()}, nil
	})
}

func (v *Vercel) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, vercelAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+v.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("Vercel", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type vercelRecord struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
}

func (v *Vercel) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	var out struct {
		Records []vercelRecord `json:"records"`
	}
	if err := v.do(ctx, http.MethodGet, "/v4/domains/"+root+"/records?limit=100", nil, &out); err != nil {
		return nil, err
	}
	want := normSub(sub)
	records := make([]Record, 0, 2)
	for _, r := range out.Records {
		if r.Type != "A" || r.Name != want {
			continue
		}
		records = append(records, Record{
			RecordID: r.ID, Name: r.Name, Value: r.Value, TTL: r.TTL, Status: "ENABLE",
		})
	}
	return records, nil
}

func (v *Vercel) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	var out vercelRecord
	if err := v.do(ctx, http.MethodPost, "/v2/domains/"+root+"/records", map[string]any{
		"name":  normSub(sub),
		"type":  "A",
		"value": value,
		"ttl":   ttl,
	}, &out); err != nil {
		return Record{}, err
	}
	return Record{RecordID: out.ID, Name: out.Name, Value: value, TTL: ttl}, nil
}

func (v *Vercel) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return v.do(ctx, http.MethodPatch, "/v1/domains/records/"+recordID,
		map[string]any{"value": value, "ttl": ttl}, nil)
}

func (v *Vercel) Delete(ctx context.Context, root, recordID string) error {
	return v.do(ctx, http.MethodDelete, "/v1/domains/records/"+recordID, nil, nil)
}

func (v *Vercel) Verify(ctx context.Context, root string) error {
	if root == "" {
		return v.do(ctx, http.MethodGet, "/v5/domains?limit=1", nil, nil)
	}
	_, err := v.ListA(ctx, root, "@", "")
	if err != nil && strings.Contains(err.Error(), "not_found") {
		return nil
	}
	return err
}
