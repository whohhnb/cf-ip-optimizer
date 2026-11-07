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

// HuaweiCloud drives 华为云云解析 DNS (v2.1 recordset API).
type HuaweiCloud struct {
	ak     string
	sk     string
	client *http.Client
}

const huaweiEndpoint = "https://dns.myhuaweicloud.com"

func init() {
	Register(Descriptor{
		Name:     "huawei",
		Label:    "华为云云解析 DNS",
		SetBased: true,
		Docs:     "https://console.huaweicloud.com/iam/#/myCredential",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "ak", Label: "Access Key ID", Placeholder: "AK...", Required: true},
			{Key: "sk", Label: "Secret Access Key", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &HuaweiCloud{ak: c.Get("ak"), sk: c.Get("sk"), client: httpClient()}, nil
	})
}

func (h *HuaweiCloud) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, huaweiEndpoint+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = "dns.myhuaweicloud.com"
	if err := signHuawei(req, h.ak, h.sk); err != nil {
		return err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("华为云 DNS", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (h *HuaweiCloud) zoneID(ctx context.Context, root string) (string, error) {
	q := url.Values{}
	q.Set("name", strings.TrimSuffix(root, ".")+".")
	q.Set("type", "public")
	var out struct {
		Zones []struct {
			ID string `json:"id"`
		} `json:"zones"`
	}
	if err := h.do(ctx, http.MethodGet, "/v2.1/zones?"+q.Encode(), nil, &out); err != nil {
		return "", err
	}
	if len(out.Zones) == 0 {
		return "", fmt.Errorf("华为云账号下找不到公网域名 %s", root)
	}
	return out.Zones[0].ID, nil
}

type huaweiRecordset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Records []string `json:"records"`
	Status  string   `json:"status"`
}

func (h *HuaweiCloud) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	zid, err := h.zoneID(ctx, root)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("name", fqdn(root, sub)+".")
	q.Set("type", "A")
	var out struct {
		Recordsets []huaweiRecordset `json:"recordsets"`
	}
	if err := h.do(ctx, http.MethodGet, "/v2.1/zones/"+zid+"/recordsets?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(out.Recordsets))
	for _, r := range out.Recordsets {
		// one recordset may carry several values; expand so the caller can
		// reason about individual addresses
		for _, v := range r.Records {
			records = append(records, Record{
				RecordID: r.ID,
				Name:     strings.TrimSuffix(r.Name, "."),
				Value:    v,
				TTL:      r.TTL,
				Status:   r.Status,
			})
		}
	}
	return records, nil
}

func (h *HuaweiCloud) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	zid, err := h.zoneID(ctx, root)
	if err != nil {
		return Record{}, err
	}
	body := map[string]any{
		"name":    fqdn(root, sub) + ".",
		"type":    "A",
		"ttl":     ttl,
		"records": []string{value},
	}
	var rec huaweiRecordset
	if err := h.do(ctx, http.MethodPost, "/v2.1/zones/"+zid+"/recordsets", body, &rec); err != nil {
		return Record{}, err
	}
	return Record{RecordID: rec.ID, Name: fqdn(root, sub), Value: value, TTL: ttl}, nil
}

func (h *HuaweiCloud) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	zid, err := h.zoneID(ctx, root)
	if err != nil {
		return err
	}
	return h.do(ctx, http.MethodPut, "/v2.1/zones/"+zid+"/recordsets/"+recordID,
		map[string]any{"ttl": ttl, "records": []string{value}}, nil)
}

func (h *HuaweiCloud) Delete(ctx context.Context, root, recordID string) error {
	zid, err := h.zoneID(ctx, root)
	if err != nil {
		return err
	}
	return h.do(ctx, http.MethodDelete, "/v2.1/zones/"+zid+"/recordsets/"+recordID, nil, nil)
}

func (h *HuaweiCloud) Verify(ctx context.Context, root string) error {
	if root == "" {
		return h.do(ctx, http.MethodGet, "/v2.1/zones?limit=1", nil, nil)
	}
	_, err := h.zoneID(ctx, root)
	return err
}
