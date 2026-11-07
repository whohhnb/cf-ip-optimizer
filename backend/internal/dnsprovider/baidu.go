package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// BaiduCloud drives 百度智能云域名服务 BCD.
type BaiduCloud struct {
	ak     string
	sk     string
	client *http.Client
}

const baiduEndpoint = "https://bcd.baidubce.com"

func init() {
	Register(Descriptor{
		Name:     "baidu",
		Label:    "百度智能云 DNS",
		Docs:     "https://console.bce.baidu.com/iam/#/iam/accesslist",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "ak", Label: "Access Key ID", Required: true},
			{Key: "sk", Label: "Secret Access Key", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &BaiduCloud{ak: c.Get("ak"), sk: c.Get("sk"), client: httpClient()}, nil
	})
}

func (b *BaiduCloud) do(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baiduEndpoint+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = "bcd.baidubce.com"
	if err := signBaidu(req, b.ak, b.sk, "bcd.baidubce.com"); err != nil {
		return err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("百度云 DNS", resp, data)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type baiduRecord struct {
	RecordID uint   `json:"recordId"`
	Domain   string `json:"domain"`
	RdType   string `json:"rdtype"`
	TTL      int    `json:"ttl"`
	Rdata    string `json:"rdata"`
	Status   string `json:"status"`
}

func (b *BaiduCloud) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	var out struct {
		TotalCount int           `json:"totalCount"`
		Result     []baiduRecord `json:"result"`
	}
	if err := b.do(ctx, "/v1/domain/resolve/list", map[string]any{
		"domain": root, "pageNum": 1, "pageSize": 1000,
	}, &out); err != nil {
		return nil, err
	}
	want := fqdn(root, sub)
	records := make([]Record, 0, 2)
	for _, r := range out.Result {
		if r.RdType != "A" || r.Domain != want {
			continue
		}
		records = append(records, Record{
			RecordID: fmt.Sprintf("%d", r.RecordID),
			Name:     r.Domain,
			Value:    r.Rdata,
			TTL:      r.TTL,
			Status:   r.Status,
		})
	}
	return records, nil
}

func (b *BaiduCloud) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	var out struct {
		RecordID uint `json:"recordId"`
	}
	if err := b.do(ctx, "/v1/domain/resolve/add", map[string]any{
		"domain":   fqdn(root, sub),
		"rdType":   "A",
		"ttl":      ttl,
		"rdata":    value,
		"zoneName": root,
	}, &out); err != nil {
		return Record{}, err
	}
	return Record{RecordID: fmt.Sprintf("%d", out.RecordID), Name: fqdn(root, sub), Value: value, TTL: ttl}, nil
}

func (b *BaiduCloud) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	var id uint
	if _, err := fmt.Sscanf(recordID, "%d", &id); err != nil {
		return fmt.Errorf("记录 ID 无效: %q", recordID)
	}
	return b.do(ctx, "/v1/domain/resolve/edit", map[string]any{
		"recordId": id,
		"domain":   fqdn(root, sub),
		"rdType":   "A",
		"ttl":      ttl,
		"rdata":    value,
		"zoneName": root,
	}, nil)
}

func (b *BaiduCloud) Delete(ctx context.Context, root, recordID string) error {
	var id uint
	if _, err := fmt.Sscanf(recordID, "%d", &id); err != nil {
		return fmt.Errorf("记录 ID 无效: %q", recordID)
	}
	return b.do(ctx, "/v1/domain/resolve/delete", map[string]any{
		"recordId": id, "zoneName": root,
	}, nil)
}

func (b *BaiduCloud) Verify(ctx context.Context, root string) error {
	if root == "" {
		return b.do(ctx, "/v1/domain/resolve/list", map[string]any{"domain": "", "pageNum": 1, "pageSize": 1}, nil)
	}
	return b.do(ctx, "/v1/domain/resolve/list", map[string]any{"domain": root, "pageNum": 1, "pageSize": 1}, nil)
}
