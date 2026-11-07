package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Gcore keeps an RRset per name, so it is used in whole-set mode.
type Gcore struct {
	apiKey string
	client *http.Client
}

const gcoreAPI = "https://api.gcore.com/dns/v2"

func init() {
	Register(Descriptor{
		Name:     "gcore",
		Label:    "Gcore DNS",
		SetBased: true,
		Docs:     "https://gcore.com/docs/account-settings/create-an-api-token",
		MaxSlots: 2,
		Note:     "Gcore 以 RRset 为单位写入，多条 IP 会合并到同一记录集",
		Fields: []Field{
			{Key: "apiKey", Label: "API Token", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &Gcore{apiKey: c.Get("apiKey"), client: httpClient()}, nil
	})
}

func (g *Gcore) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, gcoreAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "APIKey "+g.apiKey)
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
		return httpErr("Gcore", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (g *Gcore) zone(ctx context.Context, root string) (string, error) {
	q := url.Values{}
	q.Set("name", root)
	var out struct {
		Zones []struct {
			Name string `json:"name"`
		} `json:"zones"`
	}
	if err := g.do(ctx, http.MethodGet, "/zones?"+q.Encode(), nil, &out); err != nil {
		return "", err
	}
	if len(out.Zones) == 0 {
		return "", fmt.Errorf("Gcore 账号下找不到域名 %s", root)
	}
	return out.Zones[0].Name, nil
}

type gcoreRRset struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	TTL             int    `json:"ttl"`
	ResourceRecords []struct {
		Content []string `json:"content"`
	} `json:"resource_records"`
}

func (g *Gcore) get(ctx context.Context, root, sub string) (*gcoreRRset, error) {
	zone, err := g.zone(ctx, root)
	if err != nil {
		return nil, err
	}
	var out gcoreRRset
	path := "/zones/" + zone + "/" + fqdn(root, sub) + "./A"
	if err := g.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *Gcore) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	set, err := g.get(ctx, root, sub)
	if err != nil {
		// a missing rrset is not an error for the caller
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]Record, 0, 2)
	for _, rr := range set.ResourceRecords {
		for _, v := range rr.Content {
			records = append(records, Record{
				RecordID: fqdn(root, sub), Name: strings.TrimSuffix(set.Name, "."), Value: v, TTL: set.TTL, Status: "ENABLE",
			})
		}
	}
	return records, nil
}

// ReplaceA writes the whole RRset.
func (g *Gcore) ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error {
	zone, err := g.zone(ctx, root)
	if err != nil {
		return err
	}
	records := make([]map[string]any, 0, len(values))
	for _, v := range values {
		records = append(records, map[string]any{"content": []string{v}})
	}
	path := "/zones/" + zone + "/" + fqdn(root, sub) + "./A"
	return g.do(ctx, http.MethodPut, path, map[string]any{
		"ttl":              ttl,
		"resource_records": records,
	}, nil)
}

func (g *Gcore) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	zone, err := g.zone(ctx, root)
	if err != nil {
		return Record{}, err
	}
	if err := g.do(ctx, http.MethodPost, "/zones/"+zone+"/rrsets", map[string]any{
		"name":             fqdn(root, sub) + ".",
		"type":             "A",
		"ttl":              ttl,
		"resource_records": []map[string]any{{"content": []string{value}}},
	}, nil); err != nil {
		return Record{}, err
	}
	return Record{RecordID: fqdn(root, sub), Name: fqdn(root, sub), Value: value, TTL: ttl}, nil
}

func (g *Gcore) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return g.ReplaceA(ctx, root, sub, []string{value}, ttl)
}

func (g *Gcore) Delete(ctx context.Context, root, recordID string) error {
	zone, err := g.zone(ctx, root)
	if err != nil {
		return err
	}
	return g.do(ctx, http.MethodDelete, "/zones/"+zone+"/"+recordID+"./A", nil, nil)
}

func (g *Gcore) Verify(ctx context.Context, root string) error {
	if root == "" {
		return g.do(ctx, http.MethodGet, "/zones", nil, nil)
	}
	_, err := g.zone(ctx, root)
	return err
}
