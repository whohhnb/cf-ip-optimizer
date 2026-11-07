package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// DNSPod talks to the Tencent Cloud DNSPod API v3 (TC3-HMAC-SHA256).
// The flow mirrors jeessy2/ddns-go, the de-facto reference for this API.
type DNSPod struct {
	secretID  string
	secretKey string
	client    *http.Client
	endpoint  string // overridable so tests can point at httptest
}

// errEmptyRecordList is DNSPod's way of saying "this name has no records yet".
var errEmptyRecordList = errors.New("dnspod: empty record list")

const (
	dnspodEndpoint = "https://dnspod.tencentcloudapi.com"
	dnspodVersion  = "2021-03-23"
	dnspodService  = "dnspod"
)

func init() {
	Register(Descriptor{
		Name:     "dnspod",
		Label:    "腾讯云 DNSPod",
		Docs:     "https://console.cloud.tencent.com/cam/capi",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "secretId", Label: "SecretId", Placeholder: "AKIDxxxxxxxxxxxxxxxx", Required: true},
			{Key: "secretKey", Label: "SecretKey", Secret: true, Required: true, Help: "建议使用仅授权 DNSPod 的子账号密钥"},
		},
	}, func(c Credentials) (Provider, error) {
		return &DNSPod{
			secretID:  c.Get("secretId"),
			secretKey: c.Get("secretKey"),
			client:    httpClient(),
			endpoint:  dnspodEndpoint,
		}, nil
	})
}

func (d *DNSPod) call(ctx context.Context, action string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	signTencent(req, d.secretID, d.secretKey, dnspodService, action, dnspodVersion, string(body))

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return httpErr("DNSPod", resp, raw)
	}

	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return fmt.Errorf("DNSPod 返回了无法解析的内容")
	}
	respObj, ok := generic["Response"]
	if !ok {
		return fmt.Errorf("DNSPod 响应缺少 Response 字段")
	}
	var meta struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	}
	if err := json.Unmarshal(respObj, &meta); err == nil && meta.Error != nil {
		// A brand-new hostname has no records, and DNSPod signals that as an
		// error. Treat it as an empty result so the first push can create them.
		if meta.Error.Code == "ResourceNotFound.NoDataOfRecord" ||
			strings.Contains(meta.Error.Message, "记录列表为空") {
			return errEmptyRecordList
		}
		return fmt.Errorf("DNSPod: %s", meta.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(respObj, out)
	}
	return nil
}

type dnspodRecord struct {
	RecordID uint64 `json:"RecordId"`
	Value    string `json:"Value"`
	Name     string `json:"Name"`
	Line     string `json:"Line"`
	TTL      int    `json:"TTL"`
	Remark   string `json:"Remark"`
	Status   string `json:"Status"`
}

func (d *DNSPod) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	payload := map[string]any{
		"Domain":     root,
		"Subdomain":  normSub(sub),
		"RecordType": "A",
		"Limit":      3000,
		"Offset":     0,
	}
	if line != "" {
		payload["RecordLine"] = line
	}
	var out struct {
		RecordList []dnspodRecord `json:"RecordList"`
	}
	if err := d.call(ctx, "DescribeRecordList", payload, &out); err != nil {
		if errors.Is(err, errEmptyRecordList) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]Record, 0, len(out.RecordList))
	for _, r := range out.RecordList {
		records = append(records, Record{
			RecordID: strconv.FormatUint(r.RecordID, 10),
			Name:     r.Name,
			Value:    r.Value,
			Line:     r.Line,
			TTL:      r.TTL,
			Remark:   r.Remark,
			Status:   r.Status,
		})
	}
	return records, nil
}

func (d *DNSPod) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	if line == "" {
		line = "默认"
	}
	payload := map[string]any{
		"Domain":     root,
		"SubDomain":  normSub(sub),
		"RecordType": "A",
		"RecordLine": line,
		"Value":      value,
		"TTL":        ttl,
	}
	if remark != "" {
		payload["Remark"] = remark
	}
	var out struct {
		RecordID uint64 `json:"RecordId"`
	}
	if err := d.call(ctx, "CreateRecord", payload, &out); err != nil {
		return Record{}, err
	}
	return Record{RecordID: strconv.FormatUint(out.RecordID, 10), Name: normSub(sub), Value: value, Line: line, TTL: ttl, Remark: remark}, nil
}

func (d *DNSPod) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	if line == "" {
		line = "默认"
	}
	rid, err := strconv.ParseUint(recordID, 10, 64)
	if err != nil {
		return fmt.Errorf("记录 ID 无效: %q", recordID)
	}
	return d.call(ctx, "ModifyRecord", map[string]any{
		"Domain":     root,
		"SubDomain":  normSub(sub),
		"RecordId":   rid,
		"RecordType": "A",
		"RecordLine": line,
		"Value":      value,
		"TTL":        ttl,
	}, nil)
}

func (d *DNSPod) Delete(ctx context.Context, root, recordID string) error {
	rid, err := strconv.ParseUint(recordID, 10, 64)
	if err != nil {
		return fmt.Errorf("记录 ID 无效: %q", recordID)
	}
	return d.call(ctx, "DeleteRecord", map[string]any{"Domain": root, "RecordId": rid}, nil)
}

func (d *DNSPod) Verify(ctx context.Context, root string) error {
	if root == "" {
		var out struct {
			DomainCountInfo struct {
				AllTotal int `json:"AllTotal"`
			} `json:"DomainCountInfo"`
		}
		return d.call(ctx, "DescribeDomainList", map[string]any{"Type": "ALL", "Limit": 1}, &out)
	}
	return d.call(ctx, "DescribeDomain", map[string]any{"Domain": root}, nil)
}
