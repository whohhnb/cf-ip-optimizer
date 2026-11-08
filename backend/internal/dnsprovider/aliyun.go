package dnsprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// AliyunDNS drives 阿里云云解析 DNS through the classic RPC style API.
type AliyunDNS struct {
	accessKeyID     string
	accessKeySecret string
	client          *http.Client
}

const (
	aliyunEndpoint = "https://alidns.aliyuncs.com/"
	aliyunVersion  = "2015-01-09"
)

func init() {
	Register(Descriptor{
		Name:  "aliyun",
		Label: "阿里云云解析 DNS",
		// 阿里云用英文线路代码，权威清单可通过 DescribeDomainInfo 的
		// RecordLines 拿到；这里列的是公网权威解析的基础线路
		Lines: []LineOption{
			{Value: "default", Label: "默认"},
			{Value: "telecom", Label: "电信"},
			{Value: "unicom", Label: "联通"},
			{Value: "mobile", Label: "移动"},
			{Value: "edu", Label: "教育网"},
			{Value: "oversea", Label: "境外"},
		},
		Docs:     "https://ram.console.aliyun.com/manage/ak",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "accessKeyId", Label: "AccessKey ID", Placeholder: "LTAIxxxxxxxxxxxx", Required: true},
			{Key: "accessKeySecret", Label: "AccessKey Secret", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &AliyunDNS{
			accessKeyID:     c.Get("accessKeyId"),
			accessKeySecret: c.Get("accessKeySecret"),
			client:          httpClient(),
		}, nil
	})
}

func (a *AliyunDNS) call(ctx context.Context, params url.Values, out any) error {
	aliyunSign(a.accessKeyID, a.accessKeySecret, params, http.MethodGet, aliyunVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aliyunEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return httpErr("阿里云 DNS", resp, raw)
	}
	var envelope struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if envelope.Code != "" {
		return fmt.Errorf("阿里云 DNS: %s", envelope.Message)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// aliyunLine maps an empty line to the vendor's default code.
func aliyunLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return "default"
	}
	return line
}

func (a *AliyunDNS) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	params := url.Values{}
	params.Set("Action", "DescribeSubDomainRecords")
	params.Set("DomainName", root)
	params.Set("SubDomain", fqdn(root, sub))
	params.Set("Type", "A")
	params.Set("PageSize", "500")

	var out struct {
		TotalCount    int `json:"TotalCount"`
		DomainRecords struct {
			Record []struct {
				RecordID string `json:"RecordId"`
				RR       string `json:"RR"`
				Value    string `json:"Value"`
				TTL      int    `json:"TTL"`
				Line     string `json:"Line"`
				Status   string `json:"Status"`
			} `json:"Record"`
		} `json:"DomainRecords"`
	}
	if err := a.call(ctx, params, &out); err != nil {
		return nil, err
	}
	want := aliyunLine(line)
	records := make([]Record, 0, len(out.DomainRecords.Record))
	for _, r := range out.DomainRecords.Record {
		if !strings.EqualFold(r.Line, want) {
			continue
		}
		records = append(records, Record{
			RecordID: r.RecordID,
			Name:     r.RR,
			Value:    r.Value,
			Line:     r.Line,
			TTL:      r.TTL,
			Status:   r.Status,
		})
	}
	return records, nil
}

func (a *AliyunDNS) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	params := url.Values{}
	params.Set("Action", "AddDomainRecord")
	params.Set("DomainName", root)
	params.Set("RR", normSub(sub))
	params.Set("Type", "A")
	params.Set("Value", value)
	params.Set("TTL", strconv.Itoa(ttl))
	params.Set("Line", aliyunLine(line))

	var out struct {
		RecordID string `json:"RecordId"`
	}
	if err := a.call(ctx, params, &out); err != nil {
		return Record{}, err
	}
	return Record{RecordID: out.RecordID, Name: normSub(sub), Value: value, TTL: ttl, Line: line}, nil
}

func (a *AliyunDNS) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	params := url.Values{}
	params.Set("Action", "UpdateDomainRecord")
	params.Set("RecordId", recordID)
	params.Set("RR", normSub(sub))
	params.Set("Type", "A")
	params.Set("Value", value)
	params.Set("TTL", strconv.Itoa(ttl))
	params.Set("Line", aliyunLine(line))
	return a.call(ctx, params, nil)
}

func (a *AliyunDNS) Delete(ctx context.Context, root, recordID string) error {
	params := url.Values{}
	params.Set("Action", "DeleteDomainRecord")
	params.Set("RecordId", recordID)
	return a.call(ctx, params, nil)
}

func (a *AliyunDNS) Verify(ctx context.Context, root string) error {
	params := url.Values{}
	if root == "" {
		params.Set("Action", "DescribeDomains")
		params.Set("PageSize", "1")
		return a.call(ctx, params, nil)
	}
	params.Set("Action", "DescribeDomainInfo")
	params.Set("DomainName", root)
	return a.call(ctx, params, nil)
}
