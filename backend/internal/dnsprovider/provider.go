// Package dnsprovider abstracts DNS vendors so the optimiser is not locked to
// a single API. Each provider implements record-level CRUD; the vendor list and
// credential schema are described by a Descriptor so the UI can render the
// right form without hard-coding anything.
package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Record struct {
	RecordID string
	Name     string
	Value    string
	Line     string
	TTL      int
	Remark   string
	Status   string
}

// Field describes one credential input for a provider.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
}

// Descriptor is the static metadata the frontend needs to build a form.
type Descriptor struct {
	Name     string  `json:"name"`
	Label    string  `json:"label"`
	Docs     string  `json:"docs,omitempty"`
	Fields   []Field `json:"fields"`
	MaxSlots int     `json:"maxSlots"` // how many A records this vendor can hold
	Note     string  `json:"note,omitempty"`
	// SetBased marks vendors whose API replaces a whole record set at once
	// (RRset semantics) instead of addressing individual records.
	SetBased bool `json:"setBased"`
	// NoList marks vendors that cannot enumerate remote records (webhook etc),
	// so the UI does not pretend the list is simply empty.
	NoList bool `json:"noList"`
}

type Provider interface {
	ListA(ctx context.Context, root, sub, line string) ([]Record, error)
	Create(ctx context.Context, root, sub, value, line string, ttl int, remark string) (Record, error)
	Update(ctx context.Context, root, sub, recordID, value, line string, ttl int) error
	Delete(ctx context.Context, root, recordID string) error
	// Verify performs a cheap authenticated call to validate credentials.
	Verify(ctx context.Context, root string) error
}

// Credentials is the decrypted credential map for one account.
type Credentials map[string]string

func (c Credentials) Get(key string) string { return strings.TrimSpace(c[key]) }

// Factory builds a provider from decrypted credentials.
type Factory func(cred Credentials) (Provider, error)

type entry struct {
	desc    Descriptor
	factory Factory
}

var (
	regMu    sync.RWMutex
	registry = map[string]entry{}
)

// Register adds a provider to the global registry. Called from init().
func Register(d Descriptor, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if d.MaxSlots <= 0 {
		d.MaxSlots = 2
	}
	registry[d.Name] = entry{desc: d, factory: f}
}

// List returns every registered provider, sorted by name.
func List() []Descriptor {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Descriptor, 0, len(registry))
	for _, e := range registry {
		out = append(out, e.desc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DescriptorOf returns metadata for a provider.
func DescriptorOf(name string) (Descriptor, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	e, ok := registry[name]
	return e.desc, ok
}

// New builds a provider instance.
func New(name string, cred Credentials) (Provider, error) {
	regMu.RLock()
	e, ok := registry[name]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("不支持的 DNS 服务商: %s", name)
	}
	for _, f := range e.desc.Fields {
		if f.Required && cred.Get(f.Key) == "" {
			return nil, fmt.Errorf("请填写「%s」", f.Label)
		}
	}
	return e.factory(cred)
}

var ErrNotConfigured = errors.New("dns provider not configured")

// httpClient is shared by every provider; timeouts stay short because the UI
// waits on these calls synchronously.
func httpClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// fqdn builds the fully qualified record name.
func fqdn(root, sub string) string {
	if sub == "" || sub == "@" {
		return root
	}
	return sub + "." + root
}

func normSub(sub string) string {
	s := strings.TrimSpace(sub)
	if s == "" {
		return "@"
	}
	return s
}

// apiError keeps vendor error payloads readable in the UI.
type apiError struct {
	Provider string
	Status   int
	Body     string
}

func (e *apiError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("%s 接口返回 %d: %s", e.Provider, e.Status, body)
}

// isNotFound reports whether the vendor answered 404, which for record lookups
// simply means "this name has no records yet".
func isNotFound(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusNotFound
	}
	return false
}

func httpErr(provider string, resp *http.Response, body []byte) error {
	return &apiError{Provider: provider, Status: resp.StatusCode, Body: string(body)}
}
