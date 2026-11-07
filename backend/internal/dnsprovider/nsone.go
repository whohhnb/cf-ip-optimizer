package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// NS1 stores every record as a single object holding an answers array, so the
// provider works in whole-set mode.
type NSOne struct {
	apiKey string
	client *http.Client
}

const nsoneAPI = "https://api.nsone.net/v1/zones"

func init() {
	Register(Descriptor{
		Name:     "nsone",
		Label:    "NS1",
		SetBased: true,
		Docs:     "https://app.ns1.com/account/settings/api-keys",
		MaxSlots: 2,
		Note:     "NS1 一条记录内含多个 answer，多条 IP 会合并写入",
		Fields: []Field{
			{Key: "apiKey", Label: "API Key", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &NSOne{apiKey: c.Get("apiKey"), client: httpClient()}, nil
	})
}

func (n *NSOne) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, nsoneAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-NSONE-Key", n.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("NS1", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type nsoneRecord struct {
	Domain  string `json:"domain"`
	Type    string `json:"type"`
	TTL     int    `json:"ttl"`
	Answers []struct {
		Answer []string `json:"answer"`
	} `json:"answers"`
}

func (n *NSOne) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	var rec nsoneRecord
	if err := n.do(ctx, http.MethodGet, "/"+root+"/"+normSub(sub)+"/A", nil, &rec); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]Record, 0, 2)
	for _, a := range rec.Answers {
		for _, v := range a.Answer {
			records = append(records, Record{
				RecordID: normSub(sub), Name: rec.Domain, Value: v, TTL: rec.TTL, Status: "ENABLE",
			})
		}
	}
	return records, nil
}

// ReplaceA writes the whole answers array at once.
func (n *NSOne) ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error {
	answers := make([]map[string]any, 0, len(values))
	for _, v := range values {
		answers = append(answers, map[string]any{"answer": []string{v}})
	}
	return n.do(ctx, http.MethodPut, "/"+root+"/"+normSub(sub)+"/A", map[string]any{
		"zone":    root,
		"domain":  normSub(sub),
		"type":    "A",
		"ttl":     ttl,
		"answers": answers,
	}, nil)
}

func (n *NSOne) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	if err := n.ReplaceA(ctx, root, sub, []string{value}, ttl); err != nil {
		return Record{}, err
	}
	return Record{RecordID: normSub(sub), Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (n *NSOne) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return n.ReplaceA(ctx, root, sub, []string{value}, ttl)
}

func (n *NSOne) Delete(ctx context.Context, root, recordID string) error {
	return n.do(ctx, http.MethodDelete, "/"+root+"/"+recordID+"/A", nil, nil)
}

func (n *NSOne) Verify(ctx context.Context, root string) error {
	if root == "" {
		return n.do(ctx, http.MethodGet, "", nil, nil)
	}
	return n.do(ctx, http.MethodGet, "/"+root, nil, nil)
}
