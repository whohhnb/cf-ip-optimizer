package dnsprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ClouDNS authenticates with auth-id / auth-password query parameters.
type ClouDNS struct {
	authID  string
	authPwd string
	client  *http.Client
}

const cloudnsAPI = "https://api.cloudns.net/dns"

func init() {
	Register(Descriptor{
		Name:     "cloudns",
		Label:    "ClouDNS",
		Docs:     "https://www.cloudns.net/api-settings/",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "authId", Label: "Auth ID", Required: true},
			{Key: "authPassword", Label: "Auth Password", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &ClouDNS{authID: c.Get("authId"), authPwd: c.Get("authPassword"), client: httpClient()}, nil
	})
}

func (c *ClouDNS) call(ctx context.Context, method, action string, q url.Values, out any) error {
	q.Set("auth-id", c.authID)
	q.Set("auth-password", c.authPwd)
	var req *http.Request
	var err error
	if method == http.MethodGet {
		req, err = http.NewRequestWithContext(ctx, method, cloudnsAPI+"/"+action+"?"+q.Encode(), nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, cloudnsAPI+"/"+action, nil)
		req.URL.RawQuery = q.Encode()
	}
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("ClouDNS", resp, raw)
	}
	var status struct {
		Status  string `json:"status"`
		Message string `json:"statusDescription"`
	}
	_ = json.Unmarshal(raw, &status)
	if status.Status == "Failed" || status.Status == "error" {
		return fmt.Errorf("ClouDNS: %s", status.Message)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// ClouDNS returns records as an object keyed by index.
type cloudnsRecord struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Host   string `json:"host"`
	Record string `json:"record"`
	TTL    string `json:"ttl"`
}

func (c *ClouDNS) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	q := url.Values{}
	q.Set("domain-name", root)
	q.Set("host", normSub(sub))
	q.Set("type", "A")
	var raw map[string]cloudnsRecord
	if err := c.call(ctx, http.MethodGet, "records.json", q, &raw); err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(raw))
	for _, r := range raw {
		if r.Type != "A" {
			continue
		}
		ttl, _ := strconv.Atoi(r.TTL)
		records = append(records, Record{
			RecordID: r.ID, Name: r.Host, Value: r.Record, TTL: ttl, Status: "ENABLE",
		})
	}
	return records, nil
}

func (c *ClouDNS) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	q := url.Values{}
	q.Set("domain-name", root)
	q.Set("host", normSub(sub))
	q.Set("record-type", "A")
	q.Set("record", value)
	q.Set("ttl", strconv.Itoa(ttl))
	var out struct {
		Status string `json:"status"`
		Data   string `json:"data"`
	}
	if err := c.call(ctx, http.MethodPost, "create-record.json", q, &out); err != nil {
		return Record{}, err
	}
	return Record{RecordID: out.Data, Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (c *ClouDNS) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	q := url.Values{}
	q.Set("domain-name", root)
	q.Set("host", normSub(sub))
	q.Set("record-type", "A")
	q.Set("record", value)
	q.Set("ttl", strconv.Itoa(ttl))
	q.Set("record-id", recordID)
	return c.call(ctx, http.MethodPost, "mod-record.json", q, nil)
}

func (c *ClouDNS) Delete(ctx context.Context, root, recordID string) error {
	q := url.Values{}
	q.Set("domain-name", root)
	q.Set("record-id", recordID)
	return c.call(ctx, http.MethodPost, "delete-record.json", q, nil)
}

func (c *ClouDNS) Verify(ctx context.Context, root string) error {
	q := url.Values{}
	if root == "" {
		return c.call(ctx, http.MethodGet, "list-zones.json", q, nil)
	}
	q.Set("domain-name", root)
	return c.call(ctx, http.MethodGet, "get-zone-info.json", q, nil)
}
