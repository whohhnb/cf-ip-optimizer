package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// DESec keeps one RRset per name+type, so several addresses live in a single
// object. ReplaceA writes the whole set.
type DESec struct {
	token  string
	client *http.Client
}

const desecAPI = "https://desec.io/api/v1"

func init() {
	Register(Descriptor{
		Name:     "desec",
		Label:    "deSEC",
		SetBased: true,
		Docs:     "https://desec.io/tokens",
		MaxSlots: 2,
		Note:     "deSEC 以 RRset 为单位，多条 IP 会写进同一个记录集",
		Fields: []Field{
			{Key: "token", Label: "API Token", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &DESec{token: c.Get("token"), client: httpClient()}, nil
	})
}

func (d *DESec) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, desecAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+d.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("deSEC", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type desecRRset struct {
	Subname string   `json:"subname"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Records []string `json:"records"`
}

func (d *DESec) rrsets(ctx context.Context, root, sub string) ([]desecRRset, error) {
	q := url.Values{}
	q.Set("subname", normSub(sub))
	q.Set("type", "A")
	var out []desecRRset
	if err := d.do(ctx, http.MethodGet, "/domains/"+root+"/rrsets/?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *DESec) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	sets, err := d.rrsets(ctx, root, sub)
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, 2)
	for _, s := range sets {
		for _, v := range s.Records {
			records = append(records, Record{
				RecordID: normSub(sub), Name: s.Subname, Value: v, TTL: s.TTL, Status: "ENABLE",
			})
		}
	}
	return records, nil
}

// ReplaceA rewrites the RRset with exactly the given addresses.
func (d *DESec) ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error {
	body := map[string]any{"ttl": ttl, "records": values}
	path := "/domains/" + root + "/rrsets/" + normSub(sub) + "/A/"
	if normSub(sub) == "@" {
		path = "/domains/" + root + "/rrsets/@/A/"
	}
	return d.do(ctx, http.MethodPut, path, body, nil)
}

func (d *DESec) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	if err := d.ReplaceA(ctx, root, sub, []string{value}, ttl); err != nil {
		return Record{}, err
	}
	return Record{RecordID: normSub(sub), Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (d *DESec) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return d.ReplaceA(ctx, root, sub, []string{value}, ttl)
}

func (d *DESec) Delete(ctx context.Context, root, recordID string) error {
	return d.do(ctx, http.MethodDelete, "/domains/"+root+"/rrsets/"+recordID+"/A/", nil, nil)
}

func (d *DESec) Verify(ctx context.Context, root string) error {
	if root == "" {
		return d.do(ctx, http.MethodGet, "/domains/", nil, nil)
	}
	return d.do(ctx, http.MethodGet, "/domains/"+root+"/", nil, nil)
}
