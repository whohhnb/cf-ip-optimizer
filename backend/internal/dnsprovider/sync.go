package dnsprovider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"edgepilot/internal/model"
)

// SetProvider is implemented by vendors whose API replaces a whole record set
// in one call (RRset based: Huawei, deSEC, Gcore, NS1, GoDaddy, Spaceship).
type SetProvider interface {
	ReplaceA(ctx context.Context, root, sub string, values []string, ttl int) error
}

// SyncOptions describes one reconciliation pass.
type SyncOptions struct {
	RootDomain string
	SubDomain  string
	RecordLine string
	TTL        int
	Slots      int
	Desired    []string // best first
	Managed    []model.ManagedRecord
	Rolling    bool // replace at most one record per pass
	Remark     string
}

type SyncResult struct {
	Managed []model.ManagedRecord
	Changes []string
}

func (o *SyncOptions) normalize() {
	if o.Slots <= 0 {
		o.Slots = 2
	}
	if o.Slots > 8 {
		o.Slots = 8
	}
	if o.TTL <= 0 {
		o.TTL = 600
	}
	if o.RecordLine == "" {
		o.RecordLine = "默认"
	}
	if o.Remark == "" {
		o.Remark = "edgepilot"
	}
	if len(o.Desired) > o.Slots {
		o.Desired = o.Desired[:o.Slots]
	}
}

// Sync reconciles DNS state with the desired address set.
//
// It is deliberately conservative: only records this system created (tracked in
// `Managed`, or carrying the remark tag) are ever modified or deleted, so
// hand-written A records on the same hostname stay untouched.
func Sync(ctx context.Context, p Provider, opt SyncOptions) (SyncResult, error) {
	opt.normalize()
	if sp, ok := p.(SetProvider); ok {
		return syncSet(ctx, sp, opt)
	}
	return syncRecords(ctx, p, opt)
}

// syncSet handles RRset style vendors: one call replaces the whole set, so
// rolling replace works by only swapping a single position per pass.
func syncSet(ctx context.Context, sp SetProvider, opt SyncOptions) (SyncResult, error) {
	res := SyncResult{}
	existing, err := sp.(Provider).ListA(ctx, opt.RootDomain, opt.SubDomain, opt.RecordLine)
	if err != nil {
		return res, fmt.Errorf("读取解析记录失败: %w", err)
	}

	current := make([]string, 0, len(existing))
	seen := map[string]bool{}
	for _, r := range existing {
		if seen[r.Value] {
			continue
		}
		seen[r.Value] = true
		current = append(current, r.Value)
	}

	if len(current) > opt.Slots {
		current = current[:opt.Slots]
	}

	next := append([]string{}, current...)
	changed := false

	// fill missing slots first — purely additive, safe even in rolling mode
	for len(next) < opt.Slots {
		pick := ""
		for _, ip := range opt.Desired {
			if !contains(next, ip) {
				pick = ip
				break
			}
		}
		if pick == "" {
			break
		}
		next = append(next, pick)
		changed = true
		res.Changes = append(res.Changes, "新增 "+pick)
	}

	for i := 0; i < len(next) && i < len(opt.Desired); i++ {
		if next[i] == opt.Desired[i] {
			continue
		}
		if contains(next, opt.Desired[i]) {
			continue // already present, order is cosmetic
		}
		old := next[i]
		next[i] = opt.Desired[i]
		changed = true
		res.Changes = append(res.Changes, fmt.Sprintf("记录%d %s → %s", i+1, old, opt.Desired[i]))
		if opt.Rolling {
			break
		}
	}

	if changed {
		if err := sp.ReplaceA(ctx, opt.RootDomain, opt.SubDomain, next, opt.TTL); err != nil {
			return res, fmt.Errorf("写入解析失败: %w", err)
		}
	}

	id := normSub(opt.SubDomain)
	for i, v := range next {
		res.Managed = append(res.Managed, model.ManagedRecord{RecordID: id, Slot: i, Value: v})
	}
	return res, nil
}

// syncRecords handles per-record APIs where each A record has its own ID.
func syncRecords(ctx context.Context, p Provider, opt SyncOptions) (SyncResult, error) {
	res := SyncResult{}

	existing, err := p.ListA(ctx, opt.RootDomain, opt.SubDomain, opt.RecordLine)
	if err != nil {
		return res, fmt.Errorf("读取解析记录失败: %w", err)
	}
	live := make(map[string]Record, len(existing))
	for _, r := range existing {
		live[r.RecordID] = r
	}

	// 1) drop bookkeeping for records that no longer exist, refresh values
	managed := make([]model.ManagedRecord, 0, len(opt.Managed))
	for _, m := range opt.Managed {
		if r, ok := live[m.RecordID]; ok {
			m.Value = r.Value
			managed = append(managed, m)
		}
	}
	sort.Slice(managed, func(i, j int) bool { return managed[i].Slot < managed[j].Slot })
	for i := range managed {
		managed[i].Slot = i
	}

	// 2) adopt records previously created by us but missing from the local DB
	known := make(map[string]bool, len(managed))
	for _, m := range managed {
		known[m.RecordID] = true
	}
	for _, r := range existing {
		if len(managed) >= opt.Slots {
			break
		}
		if known[r.RecordID] {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.Remark), opt.Remark) {
			managed = append(managed, model.ManagedRecord{
				RecordID: r.RecordID,
				Slot:     len(managed),
				Value:    r.Value,
			})
			known[r.RecordID] = true
			res.Changes = append(res.Changes, "接管已有记录 "+r.Value)
		}
	}

	// 3) trim surplus managed records (never touches foreign records)
	for len(managed) > opt.Slots {
		last := managed[len(managed)-1]
		if err := p.Delete(ctx, opt.RootDomain, last.RecordID); err != nil {
			res.Changes = append(res.Changes, "保留多余记录 "+last.Value+"（接口不支持删除）")
			break
		}
		res.Changes = append(res.Changes, "删除多余记录 "+last.Value)
		managed = managed[:len(managed)-1]
	}

	// 4) create missing slots
	inUse := make(map[string]bool, len(managed))
	for _, m := range managed {
		inUse[m.Value] = true
	}
	for len(managed) < opt.Slots {
		want := ""
		for _, ip := range opt.Desired {
			if !inUse[ip] {
				want = ip
				break
			}
		}
		if want == "" {
			break
		}
		rec, err := p.Create(ctx, opt.RootDomain, opt.SubDomain, want, opt.RecordLine, opt.TTL, opt.Remark)
		if err != nil {
			return res, fmt.Errorf("新增记录失败: %w", err)
		}
		managed = append(managed, model.ManagedRecord{RecordID: rec.RecordID, Slot: len(managed), Value: want})
		inUse[want] = true
		res.Changes = append(res.Changes, "新增记录 "+want)
	}

	// 5) align values, best IP on slot 0
	indexOf := func(ip string) int {
		for i := range managed {
			if managed[i].Value == ip {
				return i
			}
		}
		return -1
	}
	updates := 0
	for i := 0; i < len(managed) && i < len(opt.Desired); i++ {
		want := opt.Desired[i]
		if want == "" || managed[i].Value == want {
			continue
		}
		// already present on another record: just re-map, no API call needed
		if j := indexOf(want); j >= 0 {
			managed[i].Value, managed[j].Value = managed[j].Value, managed[i].Value
			managed[i].RecordID, managed[j].RecordID = managed[j].RecordID, managed[i].RecordID
			continue
		}
		if opt.Rolling && updates >= 1 {
			break
		}
		old := managed[i].Value
		if err := p.Update(ctx, opt.RootDomain, opt.SubDomain, managed[i].RecordID, want, opt.RecordLine, opt.TTL); err != nil {
			return res, fmt.Errorf("更新记录失败: %w", err)
		}
		managed[i].Value = want
		updates++
		res.Changes = append(res.Changes, fmt.Sprintf("记录%d %s → %s", i+1, old, want))
	}

	res.Managed = managed
	return res, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
