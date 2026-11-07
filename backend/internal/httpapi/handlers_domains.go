package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"edgepilot/internal/dnsprovider"
	"edgepilot/internal/logx"
	"edgepilot/internal/model"
)

// ---------- accounts ----------

type accountInput struct {
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Credentials map[string]string `json:"credentials"`
	Root        string            `json:"root"`
}

// maskMeta builds a short, non-reversible identifier for the account list.
func maskMeta(cred map[string]string) string {
	keys := make([]string, 0, len(cred))
	for k := range cred {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(cred[k])
		if v == "" || k == "url" {
			continue
		}
		if len(v) <= 8 {
			return "••••"
		}
		return v[:4] + "••••" + v[len(v)-4:]
	}
	return ""
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"providers": dnsprovider.List()})
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	list, err := s.st.ListAccounts(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取 DNS 账号失败")
		return
	}
	ok(w, map[string]any{"accounts": list})
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	in.Provider = strings.TrimSpace(in.Provider)
	in.Name = strings.TrimSpace(in.Name)
	if in.Provider == "" {
		in.Provider = "dnspod"
	}
	desc, exists := dnsprovider.DescriptorOf(in.Provider)
	if !exists {
		fail(w, http.StatusBadRequest, "不支持的服务商: "+in.Provider)
		return
	}
	if in.Name == "" {
		in.Name = desc.Label
	}
	cred := dnsprovider.Credentials{}
	for k, v := range in.Credentials {
		cred[k] = strings.TrimSpace(v)
	}
	if err := s.svc.TestAccount(r.Context(), in.Provider, cred, strings.TrimSpace(in.Root)); err != nil {
		fail(w, http.StatusBadRequest, "凭据校验失败: "+err.Error())
		return
	}
	blob, err := json.Marshal(cred)
	if err != nil {
		fail(w, http.StatusInternalServerError, "凭据序列化失败")
		return
	}
	enc, err := s.svc.Box.Seal(string(blob))
	if err != nil {
		fail(w, http.StatusInternalServerError, "凭据加密失败")
		return
	}
	acct, err := s.st.CreateAccount(u.ID, in.Name, in.Provider, maskMeta(in.Credentials), enc)
	if err != nil {
		fail(w, http.StatusInternalServerError, "保存账号失败")
		return
	}
	logx.Log(u.ID, 0, logx.Info, "新增 DNS 账号: "+acct.Name+" ("+desc.Label+")")
	ok(w, map[string]any{"account": acct})
}

// handleUpdateAccount lets users rotate credentials or rename an account without
// deleting it (deleting would cascade to every domain bound to the account).
// Leaving the credential fields empty keeps the stored secret untouched.
func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	existing, encOld, err := s.st.AccountSecret(id)
	if err != nil || existing.UserID != u.ID {
		fail(w, http.StatusNotFound, "账号不存在")
		return
	}

	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = existing.Provider
	}
	desc, exists := dnsprovider.DescriptorOf(provider)
	if !exists {
		fail(w, http.StatusBadRequest, "不支持的服务商: "+provider)
		return
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = existing.Name
	}

	// only touch the secret when the caller actually sent something
	submitted := false
	for _, v := range in.Credentials {
		if strings.TrimSpace(v) != "" {
			submitted = true
			break
		}
	}

	if provider != existing.Provider && !submitted {
		fail(w, http.StatusBadRequest, "切换服务商需要重新填写凭据")
		return
	}

	encNew := encOld
	meta := existing.Meta
	if submitted {
		cred := dnsprovider.Credentials{}
		for k, v := range in.Credentials {
			cred[k] = strings.TrimSpace(v)
		}
		if err := s.svc.TestAccount(r.Context(), provider, cred, strings.TrimSpace(in.Root)); err != nil {
			fail(w, http.StatusBadRequest, "凭据校验失败: "+err.Error())
			return
		}
		blob, err := json.Marshal(cred)
		if err != nil {
			fail(w, http.StatusInternalServerError, "凭据序列化失败")
			return
		}
		encNew, err = s.svc.Box.Seal(string(blob))
		if err != nil {
			fail(w, http.StatusInternalServerError, "凭据加密失败")
			return
		}
		meta = maskMeta(in.Credentials)
	}

	if err := s.st.UpdateAccount(id, u.ID, name, provider, meta, encNew); err != nil {
		fail(w, http.StatusInternalServerError, "保存失败")
		return
	}
	logx.Log(u.ID, 0, logx.Info, "更新 DNS 账号: "+name+" ("+desc.Label+")")
	ok(w, map[string]any{"ok": true, "credentialsUpdated": submitted})
}

func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if in.Provider == "" {
		in.Provider = "dnspod"
	}
	cred := dnsprovider.Credentials{}
	for k, v := range in.Credentials {
		cred[k] = strings.TrimSpace(v)
	}
	if err := s.svc.TestAccount(r.Context(), in.Provider, cred, strings.TrimSpace(in.Root)); err != nil {
		fail(w, http.StatusBadRequest, "校验失败: "+err.Error())
		return
	}
	ok(w, map[string]string{"message": "凭据有效"})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	if err := s.st.DeleteAccount(id, u.ID); err != nil {
		fail(w, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// ---------- domains ----------

type domainInput struct {
	AccountID   int64  `json:"accountId"`
	RootDomain  string `json:"rootDomain"`
	SubDomain   string `json:"subDomain"`
	RecordLine  string `json:"recordLine"`
	TTL         int    `json:"ttl"`
	Slots       int    `json:"slots"`
	AutoPush    bool   `json:"autoPush"`
	IntervalMin int    `json:"intervalMin"`
	Enabled     bool   `json:"enabled"`
}

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeDomain(in *domainInput, maxSlots int) string {
	root := strings.ToLower(strings.TrimSpace(in.RootDomain))
	root = strings.TrimPrefix(root, "https://")
	root = strings.TrimPrefix(root, "http://")
	root = strings.Trim(root, "/.")
	if strings.ContainsAny(root, " \t\r\n") || !strings.Contains(root, ".") {
		return "根域名格式不正确"
	}
	for _, l := range strings.Split(root, ".") {
		if !labelRe.MatchString(l) {
			return "根域名格式不正确"
		}
	}
	sub := strings.ToLower(strings.TrimSpace(in.SubDomain))
	sub = strings.Trim(sub, ".")
	if sub == "" {
		sub = "@"
	}
	if sub != "@" {
		for _, l := range strings.Split(sub, ".") {
			if !labelRe.MatchString(l) {
				return "子域名格式不正确"
			}
		}
	}
	if in.TTL == 0 {
		in.TTL = 600
	}
	if in.TTL < 60 || in.TTL > 86400 {
		return "TTL 需在 60 - 86400 秒之间"
	}
	if in.Slots == 0 {
		in.Slots = 2
	}
	if in.Slots < 1 {
		return "解析条数至少 1 条"
	}
	if maxSlots <= 0 {
		maxSlots = 2
	}
	if in.Slots > maxSlots {
		return fmt.Sprintf("该 DNS 服务商最多支持 %d 条 A 记录", maxSlots)
	}
	if in.IntervalMin == 0 {
		in.IntervalMin = 60
	}
	if in.IntervalMin < 5 || in.IntervalMin > 1440 {
		return "扫描间隔需在 5 - 1440 分钟之间"
	}
	if in.RecordLine == "" {
		in.RecordLine = "默认"
	}
	in.RootDomain = root
	in.SubDomain = sub
	return ""
}

// providerInfo exposes the capability flags the UI needs to phrase things
// correctly for each vendor (record model, whether records can be listed).
func providerInfo(provider string) map[string]any {
	desc, ok := dnsprovider.DescriptorOf(provider)
	if !ok {
		return map[string]any{"name": provider, "label": provider, "maxSlots": 2}
	}
	return map[string]any{
		"name":     desc.Name,
		"label":    desc.Label,
		"maxSlots": desc.MaxSlots,
		"setBased": desc.SetBased,
		"noList":   desc.NoList,
		"note":     desc.Note,
	}
}

func providerMaxSlots(provider string) int {
	if d, ok := dnsprovider.DescriptorOf(provider); ok && d.MaxSlots > 0 {
		return d.MaxSlots
	}
	return 2
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	list, err := s.st.ListDomains(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取域名失败")
		return
	}
	accounts, _ := s.st.ListAccounts(u.ID)
	providerByAccount := make(map[int64]string, len(accounts))
	for _, a := range accounts {
		providerByAccount[a.ID] = a.Provider
	}

	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, map[string]any{
			"domain":   d,
			"full":     d.FullName(),
			"running":  s.svc.IsRunning(d.ID),
			"provider": providerInfo(providerByAccount[d.AccountID]),
		})
	}
	ok(w, map[string]any{"domains": out})
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in domainInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	acct, _, err := s.st.AccountSecret(in.AccountID)
	if err != nil {
		fail(w, http.StatusBadRequest, "请先添加 DNS 账号")
		return
	}
	if msg := normalizeDomain(&in, providerMaxSlots(acct.Provider)); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	d, err := s.st.CreateDomain(u.ID, storeDomainInput(in))
	if err != nil {
		fail(w, http.StatusInternalServerError, "创建失败")
		return
	}
	logx.Log(u.ID, d.ID, logx.Info, "新增域名 "+d.FullName())

	// Kick off the first scan right away instead of waiting for the scheduler
	// tick, so the user sees results within seconds of saving the domain.
	var job *model.ScanJob
	if d.Enabled {
		j, err := s.svc.StartScan(context.Background(), d, "create")
		if err != nil {
			logx.Log(u.ID, d.ID, logx.Warn, "首次扫描未能启动: "+err.Error())
		} else {
			job = j
			// reflect the status StartScan just wrote instead of returning a
			// stale "idle" row to the caller
			if fresh, err := s.st.DomainByID(d.ID, u.ID); err == nil {
				d = fresh
			}
		}
	}
	ok(w, map[string]any{"domain": d, "job": job})
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	d, err := s.st.DomainByID(id, u.ID)
	if err != nil {
		fail(w, http.StatusNotFound, "域名不存在")
		return
	}
	managed, _ := s.st.ListManaged(d.ID)
	job, _ := s.st.LatestDoneJob(d.ID)
	var results []model.ScanResult
	if job != nil {
		results, _ = s.st.ResultsByJob(job.ID, 10)
	}
	provider := ""
	if acct, _, err := s.st.AccountSecret(d.AccountID); err == nil {
		provider = acct.Provider
	}
	ok(w, map[string]any{
		"domain":   d,
		"full":     d.FullName(),
		"managed":  managed,
		"job":      job,
		"top":      results,
		"running":  s.svc.IsRunning(d.ID),
		"provider": providerInfo(provider),
	})
}

func (s *Server) handleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	var in domainInput
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	acct, _, err := s.st.AccountSecret(in.AccountID)
	if err != nil {
		fail(w, http.StatusBadRequest, "请先添加 DNS 账号")
		return
	}
	if msg := normalizeDomain(&in, providerMaxSlots(acct.Provider)); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.st.UpdateDomain(id, u.ID, storeDomainInput(in)); err != nil {
		fail(w, http.StatusInternalServerError, "更新失败")
		return
	}
	d, err := s.st.DomainByID(id, u.ID)
	if err != nil {
		fail(w, http.StatusNotFound, "域名不存在")
		return
	}
	ok(w, map[string]any{"domain": d})
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	if err := s.st.DeleteDomain(id, u.ID); err != nil {
		fail(w, http.StatusInternalServerError, "删除失败")
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	d, err := s.st.DomainByID(id, u.ID)
	if err != nil {
		fail(w, http.StatusNotFound, "域名不存在")
		return
	}
	n, err := s.svc.PushLatest(r.Context(), d)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]any{"message": "已下发", "count": n})
}

func (s *Server) handleLiveRecords(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	d, err := s.st.DomainByID(id, u.ID)
	if err != nil {
		fail(w, http.StatusNotFound, "域名不存在")
		return
	}
	acct, enc, err := s.st.AccountSecret(d.AccountID)
	if err != nil {
		fail(w, http.StatusBadRequest, "DNS 账号不存在")
		return
	}
	plain, err := s.svc.Box.Open(enc)
	if err != nil {
		fail(w, http.StatusInternalServerError, "凭据解密失败")
		return
	}
	cred := dnsprovider.Credentials{}
	if plain != "" {
		_ = json.Unmarshal([]byte(plain), &cred)
	}
	p, err := dnsprovider.New(acct.Provider, cred)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := p.ListA(r.Context(), d.RootDomain, d.SubDomain, d.RecordLine)
	if err != nil {
		fail(w, http.StatusBadGateway, "读取解析记录失败: "+err.Error())
		return
	}
	ok(w, map[string]any{"records": records, "full": d.FullName()})
}
