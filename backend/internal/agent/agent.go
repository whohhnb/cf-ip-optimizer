// Package agent implements the probe side of the multi-carrier scheduler.
//
// A probe is the same binary started with `edgepilot probe --token ...`. It
// sits on a machine inside one carrier's network, pulls scan tasks from the
// controller, runs them locally, and posts the measurements back. That is the
// only way to know what a 电信 / 联通 / 移动 visitor actually sees: anycast
// hands each carrier a different path to the same Cloudflare address.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"edgepilot/internal/scanner"
	"edgepilot/internal/service"
)

type Config struct {
	Server   string
	Token    string
	Interval time.Duration
	Version  string
	Once     bool
}

type taskBody struct {
	ID      int64                `json:"id"`
	Host    string               `json:"host"`
	Options service.ProbeOptions `json:"options"`
}

type taskResp struct {
	Task *taskBody `json:"task"`
}

type heartbeatResp struct {
	ProbeID int64  `json:"probeId"`
	Name    string `json:"name"`
	Line    string `json:"line"`
}

// maxTaskDuration stays below the controller's 20 minute expiry so a slow scan
// reports a clean failure instead of being reaped mid-flight.
const maxTaskDuration = 12 * time.Minute

func Run(ctx context.Context, cfg Config) error {
	cfg.Server = strings.TrimRight(strings.TrimSpace(cfg.Server), "/")
	if cfg.Server == "" {
		return errors.New("缺少 --server")
	}
	if !strings.HasPrefix(cfg.Token, "ep_") {
		return errors.New("探针令牌格式不对，应以 ep_ 开头")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// register once so the operator sees the identity the panel has for us
	hb, err := heartbeat(ctx, client, cfg)
	if err != nil {
		return fmt.Errorf("连接主控失败: %w", err)
	}
	log.Printf("已连接主控 %s · 探针「%s」· 线路 %s", cfg.Server, hb.Name, hb.Line)

	// heartbeat is independent of the task loop: a long scan must not make the
	// panel think the agent is dead
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				if _, err := heartbeat(hbCtx, client, cfg); err != nil {
					log.Printf("心跳失败: %v", err)
				}
			}
		}
	}()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("收到退出信号，探针停止")
			return nil
		default:
		}

		task, err := poll(ctx, client, cfg)
		if err != nil {
			failures++
			wait := backoff(failures)
			log.Printf("拉取任务失败（第 %d 次）: %v，%s 后重试", failures, err, wait)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
			continue
		}
		failures = 0

		if task == nil {
			if cfg.Once {
				log.Printf("当前没有待执行任务")
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(cfg.Interval):
			}
			continue
		}

		runTask(ctx, client, cfg, *task)
		if cfg.Once {
			return nil
		}
	}
}

func backoff(n int) time.Duration {
	d := time.Duration(n) * 2 * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func heartbeat(ctx context.Context, c *http.Client, cfg Config) (*heartbeatResp, error) {
	body, _ := json.Marshal(map[string]string{"version": cfg.Version})
	var out heartbeatResp
	if err := doJSON(ctx, c, http.MethodPost, cfg.Server+"/api/probe/heartbeat", cfg.Token, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func poll(ctx context.Context, c *http.Client, cfg Config) (*taskBody, error) {
	var out taskResp
	if err := doJSON(ctx, c, http.MethodGet, cfg.Server+"/api/probe/tasks", cfg.Token, nil, &out); err != nil {
		return nil, err
	}
	return out.Task, nil
}

func runTask(parent context.Context, c *http.Client, cfg Config, t taskBody) {
	ctx, cancel := context.WithTimeout(parent, maxTaskDuration)
	defer cancel()

	opts := t.Options.Options(t.Host)
	log.Printf("收到任务 #%d · %s · 候选 %d · 测速 %d 个", t.ID, t.Host, opts.Candidates, opts.SpeedTop)

	started := time.Now()
	out, err := scanner.Run(ctx, opts, func(p scanner.Progress) {
		if p.Msg != "" {
			log.Printf("  [%s] %s", p.Stage, p.Msg)
		}
	})

	rep := service.ProbeReport{ElapsedMs: time.Since(started).Milliseconds()}
	if err != nil {
		rep.Status = "failed"
		rep.Error = err.Error()
	} else {
		rep.Status = "done"
		rep.Tested = out.Tested
		rep.OK = out.OK
		// 回传前 200 条足够选优，也避免一次 POST 太大
		if len(out.Results) > 200 {
			rep.Results = out.Results[:200]
		} else {
			rep.Results = out.Results
		}
	}

	body, _ := json.Marshal(rep)
	url := fmt.Sprintf("%s/api/probe/tasks/%d/result", cfg.Server, t.ID)
	var ack struct {
		Pushed int `json:"pushed"`
	}
	if err := doJSON(ctx, c, http.MethodPost, url, cfg.Token, body, &ack); err != nil {
		log.Printf("上报结果失败: %v", err)
		return
	}
	if rep.Status == "failed" {
		log.Printf("任务 #%d 失败: %s", t.ID, rep.Error)
		return
	}
	log.Printf("任务 #%d 完成 · 可用 %d · 下发 %d 条 · 耗时 %dms",
		t.ID, len(rep.Results), ack.Pushed, rep.ElapsedMs)
}

func doJSON(ctx context.Context, c *http.Client, method, url, token string, body []byte, out any) error {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Probe-Token", token)
	req.Header.Set("User-Agent", "edgepilot-probe/"+Version())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("探针令牌被拒绝，请在面板确认该探针未被删除")
	}
	if resp.StatusCode != http.StatusOK {
		// 主控的错误统一是 {"error":"..."}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// Version is injected by main from the build tag so the panel can show what
// each agent is running.
var buildVersion = "dev"

func Version() string { return buildVersion }

// SetVersion lets main inject the release tag.
func SetVersion(v string) {
	if v != "" {
		buildVersion = v
	}
}
