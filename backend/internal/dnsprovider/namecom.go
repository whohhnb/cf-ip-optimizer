package dnsprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// NameCom uses HTTP Basic auth (username + API token).
type NameCom struct {
	username string
	token    string
	client   *http.Client
}

const namecomAPI = "https://api.name.com/core/v1/domains"

func init() {
	Register(Descriptor{
		Name:     "namecom",
		Label:    "Name.com",
		Docs:     "https://www.name.com/account/settings/api",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "username", Label: "用户名", Required: true},
			{Key: "token", Label: "API Token", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &NameCom{username: c.Get("username"), token: c.Get("token"), client: httpClient()}, nil
	})
}

func (n *NameCom) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, namecomAPI+path, rdr)
	if err != nil {
		return err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(n.username + ":" + n.token))
	req.Header.Set("Authorization", "Basic "+auth)
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
		return httpErr("Name.com", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type namecomRecord struct {
	ID     int64  `json:"id"`
	Host   string `json:"host"`
	Type   string `json:"type"`
	Answer string `json:"answer"`
	TTL    int    `json:"ttl"`
}

func (n *NameCom) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	var out struct {
		Records []namecomRecord `json:"records"`
	}
	if err := n.do(ctx, http.MethodGet, "/"+root+"/records", nil, &out); err != nil {
		return nil, err
	}
	want := normSub(sub)
	records := make([]Record, 0, 2)
	for _, r := range out.Records {
		if r.Type != "A" || r.Host != want {
			continue
		}
		records = append(records, Record{
			RecordID: fmt.Sprintf("%d", r.ID), Name: r.Host, Value: r.Answer, TTL: r.TTL, Status: "ENABLE",
		})
	}
	return records, nil
}

func (n *NameCom) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	var out namecomRecord
	if err := n.do(ctx, http.MethodPost, "/"+root+"/records", map[string]any{
		"host": normSub(sub), "type": "A", "answer": value, "ttl": ttl,
	}, &out); err != nil {
		return Record{}, err
	}
	return Record{RecordID: fmt.Sprintf("%d", out.ID), Name: out.Host, Value: value, TTL: ttl}, nil
}

func (n *NameCom) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return n.do(ctx, http.MethodPut, "/"+root+"/records/"+recordID, map[string]any{
		"host": normSub(sub), "type": "A", "answer": value, "ttl": ttl,
	}, nil)
}

func (n *NameCom) Delete(ctx context.Context, root, recordID string) error {
	return n.do(ctx, http.MethodDelete, "/"+root+"/records/"+recordID, nil, nil)
}

func (n *NameCom) Verify(ctx context.Context, root string) error {
	if root == "" {
		return n.do(ctx, http.MethodGet, "?perPage=1", nil, nil)
	}
	return n.do(ctx, http.MethodGet, "/"+root, nil, nil)
}
