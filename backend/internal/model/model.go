package model

import "time"

// User is a SaaS tenant account. The very first registered user becomes admin.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | user
	// TokenVersion 每次改密码或登出时 +1，用来让已签发的 JWT 立即失效。
	// 无状态 JWT 没有吊销机制，只能靠版本号做「改密即踢下线」。
	TokenVersion int64     `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// DNSAccount holds the credentials used to write DNS records.
type DNSAccount struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Meta      string    `json:"meta"` // masked identifier for display only
	CreatedAt time.Time `json:"createdAt"`
}

// Domain is one hostname whose A records are kept pointing at the fastest IPs.
type Domain struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"userId"`
	AccountID  int64  `json:"accountId"`
	RootDomain string `json:"rootDomain"`
	SubDomain  string `json:"subDomain"`
	RecordLine string `json:"recordLine"` // 默认 / 电信 / 联通 / 移动 / 教育网 / 境外
	// ProbeID routes this domain's scans to a remote agent sitting on the
	// matching carrier's network. 0 means "scan locally".
	ProbeID     int64      `json:"probeId"`
	TTL         int        `json:"ttl"`
	Slots       int        `json:"slots"` // how many A records to manage (1..2)
	AutoPush    bool       `json:"autoPush"`
	IntervalMin int        `json:"intervalMin"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"` // idle | scanning | ok | error
	StatusMsg   string     `json:"statusMsg"`
	LastScanAt  *time.Time `json:"lastScanAt"`
	LastPushAt  *time.Time `json:"lastPushAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// FullName returns "sub.root" or "root" when sub is @.
func (d Domain) FullName() string {
	if d.SubDomain == "" || d.SubDomain == "@" {
		return d.RootDomain
	}
	return d.SubDomain + "." + d.RootDomain
}

type ScanJob struct {
	ID         int64      `json:"id"`
	DomainID   int64      `json:"domainId"`
	UserID     int64      `json:"userId"`
	Trigger    string     `json:"trigger"` // manual | schedule | health
	Status     string     `json:"status"`  // running | done | failed | canceled
	Candidates int        `json:"candidates"`
	Tested     int        `json:"tested"`
	OK         int        `json:"ok"`
	Progress   int        `json:"progress"`
	Stage      string     `json:"stage"`
	Error      string     `json:"error"`
	Pushed     bool       `json:"pushed"`
	TopIP      string     `json:"topIp"`
	TopLatency float64    `json:"topLatency"`
	TopSpeed   float64    `json:"topSpeed"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

type ScanResult struct {
	ID      int64   `json:"id"`
	JobID   int64   `json:"jobId"`
	IP      string  `json:"ip"`
	Latency float64 `json:"latency"` // ms (median of samples)
	Jitter  float64 `json:"jitter"`  // ms (max-min)
	Loss    float64 `json:"loss"`    // 0..1
	// HTTPRTT is the full HTTP round trip through this edge (TCP + TLS +
	// request/response). Comparing it with Latency exposes paths that accept
	// the SYN quickly but then queue or throttle the actual transfer.
	HTTPRTT float64 `json:"httpRtt"` // ms, 0 = not measured
	Speed   float64 `json:"speed"`   // Mbps, 0 = not measured
	Score   float64 `json:"score"`   // 0..100
	Colo    string  `json:"colo"`
	OK      bool    `json:"ok"`
}

// ManagedRecord tracks which DNSPod record IDs this system owns, so that
// hand-made records are never touched.
type ManagedRecord struct {
	ID        int64     `json:"id"`
	DomainID  int64     `json:"domainId"`
	RecordID  string    `json:"recordId"`
	Slot      int       `json:"slot"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type LogEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	DomainID  int64     `json:"domainId"`
	Level     string    `json:"level"` // info | warn | error
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// Overview powers the dashboard header cards.
type Overview struct {
	Domains     int     `json:"domains"`
	Healthy     int     `json:"healthy"`
	Scanning    int     `json:"scanning"`
	AvgLatency  float64 `json:"avgLatency"`
	BestLatency float64 `json:"bestLatency"`
	PushesToday int     `json:"pushesToday"`
	LastScanAt  string  `json:"lastScanAt"`
}

// Probe is a remote scan agent. It lives on a machine inside one carrier's
// network, which is the only way to know what a 电信 / 联通 / 移动 user will
// actually experience — anycast gives each carrier a different path to the
// same Cloudflare IP.
type Probe struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"userId"`
	Name       string     `json:"name"`
	Line       string     `json:"line"`
	TokenHint  string     `json:"tokenHint"` // first 6 chars, for recognition only
	Version    string     `json:"version"`
	LastIP     string     `json:"lastIp"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	Online     bool       `json:"online"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// ProbeTask is one scan request handed to a probe.
type ProbeTask struct {
	ID         int64      `json:"id"`
	ProbeID    int64      `json:"probeId"`
	DomainID   int64      `json:"domainId"`
	JobID      int64      `json:"jobId"`
	Status     string     `json:"status"` // pending | running | done | failed
	Payload    string     `json:"payload,omitempty"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}
