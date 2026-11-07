package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Spaceship only offers a whole-record-set PUT, so every write is a
// read-modify-write that preserves records belonging to other names.
type Spaceship struct {
	apiKey string
	secret string
	client *http.Client
}

const spaceshipAPI = "https://spaceship.dev/api/v1/dns/records"

func init() {
	Register(Descriptor{
		Name:     "spaceship",
		Label:    "Spaceship",
		SetBased: true,
		Docs:     "https://www.spaceship.com/application/api-manager/",
		MaxSlots: 2,
		Note:     "整组覆盖式接口，写入时会读回全部记录后只替换目标主机的 A 记录",
		Fields: []Field{
			{Key: "apiKey", Label: "API Key", Required: true},
			{Key: "apiSecret", Label: "API Secret", Secret: true, Required: true},
		},
	}, func(c Credentials) (Provider, error) {
		return &Spaceship{apiKey: c.Get("apiKey"), secret: c.Get("apiSecret"), client: httpClient()}, nil
	})
}

type spaceshipItem struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	CName   string `json:"cname,omitempty"`
	TXT     string `json:"value,omitempty"`
	TTL     int    `json:"ttl"`
}

func (s *Spaceship) do(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	u := spaceshipAPI + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", s.apiKey)
	req.Header.Set("X-API-Secret", s.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return httpErr("Spaceship", resp, raw)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// all fetches every record of the zone (Spaceship has no server-side filter).
func (s *Spaceship) all(ctx context.Context, root string) ([]spaceshipItem, error) {
	q := url.Values{}
	q.Set("take", "500")
	q.Set("skip", "0")
	var out struct {
		Items []spaceshipItem `json:"items"`
	}
	if err := s.do(ctx, http.MethodGet, "/"+root, q, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (s *Spaceship) ListA(ctx context.Context, root, sub, line string) ([]Record, error) {
	items, err := s.all(ctx, root)
	if err != nil {
		return nil, err
	}
	want := normSub(sub)
	records := make([]Record, 0, 2)
	for _, it := range items {
		if it.Type != "A" || it.Name != want {
			continue
		}
		records = append(records, Record{
			RecordID: want, Name: it.Name, Value: it.Address, TTL: it.TTL, Status: "ENABLE",
		})
	}
	return records, nil
}

// ReplaceA swaps the A records of one host while keeping everything else.
func (s *Spaceship) ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error {
	items, err := s.all(ctx, root)
	if err != nil {
		return err
	}
	want := normSub(sub)
	kept := make([]spaceshipItem, 0, len(items)+len(values))
	for _, it := range items {
		if it.Type == "A" && it.Name == want {
			continue
		}
		kept = append(kept, it)
	}
	for _, v := range values {
		kept = append(kept, spaceshipItem{Type: "A", Name: want, Address: v, TTL: ttl})
	}
	return s.do(ctx, http.MethodPut, "/"+root, nil, map[string]any{
		"force": true,
		"items": kept,
	}, nil)
}

func (s *Spaceship) Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error) {
	if err := s.ReplaceA(ctx, root, sub, []string{value}, ttl); err != nil {
		return Record{}, err
	}
	return Record{RecordID: normSub(sub), Name: normSub(sub), Value: value, TTL: ttl}, nil
}

func (s *Spaceship) Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error {
	return s.ReplaceA(ctx, root, sub, []string{value}, ttl)
}

func (s *Spaceship) Delete(ctx context.Context, root, recordID string) error {
	return fmt.Errorf("Spaceship 不支持按记录删除，请使用整组覆盖")
}

func (s *Spaceship) Verify(ctx context.Context, root string) error {
	if root == "" {
		return s.do(ctx, http.MethodGet, "", url.Values{}, nil, nil)
	}
	_, err := s.all(ctx, root)
	return err
}
