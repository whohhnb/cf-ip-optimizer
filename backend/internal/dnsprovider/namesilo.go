package dnsprovider

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// NameSilo speaks a simple XML API where every call is a GET with query params.
type NameSilo struct {
	key    string
	client *http.Client
}

const namesiloAPI = "https://www.namesilo.com/api"

func init() {
	Register(Descriptor{
		Name:     "namesilo",
		Label:    "NameSilo",
		Docs:     "https://www.namesilo.com/account/api-manager",
		MaxSlots: 2,
		Fields: []Field{
			{Key: "key", Label: "API Key", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &NameSilo{key: c.Get("key"), client: httpClient()}, nil
	})
}

type namesiloReply struct {
	Reply struct {
		Code   int    `xml:"code"`
		Detail string `xml:"detail"`
		Record []struct {
			RecordID string `xml:"record_id"`
			Type     string `xml:"type"`
			Host     string `xml:"host"`
			Value    string `xml:"value"`
			TTL      int    `xml:"ttl"`
		} `xml:"resource_record"`
	} `xml:"reply"`
}

func (n *NameSilo) call(ctx context.Context, action string, q url.Values, out *namesiloReply) error {
	q.Set("version", "1")
	q.Set("type", "xml")
	q.Set("key", n.key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, namesiloAPI+"/"+action+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
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
		return httpErr("NameSilo", resp, raw)
	}
	var reply namesiloReply
	if err := xml.Unmarshal(raw, &reply); err != nil {
		return fmt.Errorf("NameSilo 返回内容无法解析")
	}
	if reply.Reply.Code != 300 {
		return fmt.Errorf("NameSilo: %s (%d)", reply.Reply.Detail, reply.Reply.Code)
	}
	if out != nil {
		*out = reply
	}
	return nil
}

func (n *NameSilo) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	q := url.Values{}
	q.Set("domain", root)
	var reply namesiloReply
	if err := n.call(ctx, "dnsListRecords", q, &reply); err != nil {
		return nil, err
	}
	want := normSub(sub)
	records := make([]Record, 0, 2)
	for _, r := range reply.Reply.Record {
		if r.Type != "A" || r.Host != want {
			continue
		}
		records = append(records, Record{
			RecordID: r.RecordID, Name: r.Host, Value: r.Value, TTL: r.TTL, Status: "ENABLE",
		})
	}
	return records, nil
}

func (n *NameSilo) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	q := url.Values{}
	q.Set("domain", root)
	q.Set("rrhost", normSub(sub))
	q.Set("rrtype", "A")
	q.Set("rrvalue", value)
	q.Set("rrttl", fmt.Sprintf("%d", ttl))
	if err := n.call(ctx, "dnsAddRecord", q, nil); err != nil {
		return Record{}, err
	}
	list, err := n.ListA(ctx, root, sub, line)
	if err != nil {
		return Record{}, err
	}
	for _, r := range list {
		if r.Value == value {
			return r, nil
		}
	}
	return Record{Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (n *NameSilo) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	q := url.Values{}
	q.Set("domain", root)
	q.Set("rrhost", normSub(sub))
	q.Set("rrid", recordID)
	q.Set("rrvalue", value)
	q.Set("rrttl", fmt.Sprintf("%d", ttl))
	return n.call(ctx, "dnsUpdateRecord", q, nil)
}

func (n *NameSilo) Delete(ctx context.Context, root, recordID string) error {
	q := url.Values{}
	q.Set("domain", root)
	q.Set("rrid", recordID)
	return n.call(ctx, "dnsDeleteRecord", q, nil)
}

func (n *NameSilo) Verify(ctx context.Context, root string) error {
	q := url.Values{}
	if root == "" {
		return n.call(ctx, "listDomains", q, nil)
	}
	q.Set("domain", root)
	return n.call(ctx, "dnsListRecords", q, nil)
}
