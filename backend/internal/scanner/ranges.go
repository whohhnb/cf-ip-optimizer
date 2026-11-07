package scanner

import (
	"context"
	"io"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Official Cloudflare edge ranges (https://www.cloudflare.com/ips/).
var fallbackV4 = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

var fallbackV6 = []string{
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

type rangeCache struct {
	mu      sync.Mutex
	v4      []*net.IPNet
	v6      []*net.IPNet
	fetched time.Time
}

var cache rangeCache

func parseCIDRs(list []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// Ranges returns the Cloudflare CIDR list, refreshing at most every 12h.
// Network failures degrade gracefully to the embedded snapshot.
func Ranges(ctx context.Context, wantV6 bool) []*net.IPNet {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if time.Since(cache.fetched) > 12*time.Hour || len(cache.v4) == 0 {
		v4, v6 := fetchRanges(ctx)
		cache.v4, cache.v6 = v4, v6
		cache.fetched = time.Now()
	}
	if wantV6 {
		return cache.v6
	}
	return cache.v4
}

func fetchRanges(ctx context.Context) ([]*net.IPNet, []*net.IPNet) {
	client := &http.Client{Timeout: 8 * time.Second}
	get := func(url string) []string {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if err != nil {
			return nil
		}
		return strings.Fields(string(body))
	}
	v4 := parseCIDRs(get("https://www.cloudflare.com/ips-v4"))
	v6 := parseCIDRs(get("https://www.cloudflare.com/ips-v6"))
	if len(v4) == 0 {
		v4 = parseCIDRs(fallbackV4)
	}
	if len(v6) == 0 {
		v6 = parseCIDRs(fallbackV6)
	}
	return v4, v6
}

// Sample picks n addresses across the ranges.
//
// Half the budget is split evenly, so a small block (a single /22 in one
// region) still gets sampled on every scan instead of being statistically
// invisible next to 104.16.0.0/13. The other half is weighted by block size,
// because the big blocks really do hold most of the usable space.
func Sample(ranges []*net.IPNet, n int, rnd *rand.Rand) []string {
	if n <= 0 || len(ranges) == 0 {
		return nil
	}

	type block struct {
		net  *net.IPNet
		host uint
		size float64
	}
	blocks := make([]block, 0, len(ranges))
	var total float64
	for _, r := range ranges {
		ones, bits := r.Mask.Size()
		if bits == 0 {
			continue
		}
		host := bits - ones
		if host <= 0 {
			continue
		}
		size := float64(uint64(1) << uint(min(host, 32)))
		blocks = append(blocks, block{net: r, host: uint(host), size: size})
		total += size
	}
	if total == 0 || len(blocks) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)

	take := func(b block) bool {
		base := ipToBigInt(b.net.IP)
		for attempt := 0; attempt < 8; attempt++ {
			var offset uint64
			if b.host >= 63 {
				offset = rnd.Uint64()
			} else {
				offset = uint64(rnd.Int63n(int64(uint64(1) << b.host)))
			}
			// skip network / broadcast-ish edges
			if offset < 2 && b.host > 2 {
				offset += 2
			}
			ip := bigIntToIP(base.Add(base, newBigInt(offset)), len(b.net.IP))
			if ip == nil || !b.net.Contains(ip) {
				continue
			}
			s := ip.String()
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
			return true
		}
		return false
	}

	// 1) even quota, so no block is crowded out
	even := n / 2
	per := even / len(blocks)
	if per < 1 {
		per = 1
	}
	for _, b := range blocks {
		if len(out) >= even {
			break
		}
		for k := 0; k < per && len(out) < n; k++ {
			take(b)
		}
	}

	// 2) the rest follows block size
	misses := 0
	for len(out) < n {
		target := rnd.Float64() * total
		var acc float64
		chosen := blocks[len(blocks)-1]
		for _, b := range blocks {
			acc += b.size
			if target <= acc {
				chosen = b
				break
			}
		}
		if take(chosen) {
			misses = 0
			continue
		}
		// a block too small to yield a fresh address must not spin forever
		if misses++; misses > 64 {
			break
		}
	}
	sort.Strings(out)
	return out
}
