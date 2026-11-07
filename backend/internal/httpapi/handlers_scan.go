package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
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
	job, err := s.svc.StartScan(r.Context(), d, "manual")
	if err != nil {
		fail(w, http.StatusConflict, err.Error())
		return
	}
	s.st.AddLog(u.ID, d.ID, "info", "手动触发扫描")
	ok(w, map[string]any{"job": job})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := s.st.ListJobs(id, u.ID, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	ok(w, map[string]any{"jobs": jobs})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	job, err := s.st.JobByID(id)
	if err != nil || job.UserID != u.ID {
		fail(w, http.StatusNotFound, "任务不存在")
		return
	}
	results, _ := s.st.ResultsByJob(id, 200)
	ok(w, map[string]any{"job": job, "results": results})
}

func (s *Server) handleJobResults(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	job, err := s.st.JobByID(id)
	if err != nil || job.UserID != u.ID {
		fail(w, http.StatusNotFound, "任务不存在")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.st.ResultsByJob(id, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取结果失败")
		return
	}
	ok(w, map[string]any{"results": results, "job": job})
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, valid := pathID(r)
	if !valid {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	job, err := s.st.JobByID(id)
	if err != nil || job.UserID != u.ID {
		fail(w, http.StatusNotFound, "任务不存在")
		return
	}
	results, err := s.st.ResultsByJob(id, 1000)
	if err != nil {
		fail(w, http.StatusInternalServerError, "读取结果失败")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="edgepilot-job-%d.csv"`, id))
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM so Excel reads UTF-8
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"IP地址", "延迟(ms)", "抖动(ms)", "丢包(%)", "下载(Mbps)", "评分", "机房", "可用"})
	for _, res := range results {
		_ = cw.Write([]string{
			res.IP,
			fmt.Sprintf("%.2f", res.Latency),
			fmt.Sprintf("%.2f", res.Jitter),
			fmt.Sprintf("%.2f", res.Loss),
			fmt.Sprintf("%.2f", res.Speed),
			fmt.Sprintf("%.2f", res.Score),
			res.Colo,
			map[bool]string{true: "是", false: "否"}[res.OK],
		})
	}
	cw.Flush()
}

// handleStream streams scan progress over SSE.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	domainID, err := strconv.ParseInt(r.URL.Query().Get("domainId"), 10, 64)
	if err != nil || domainID <= 0 {
		fail(w, http.StatusBadRequest, "参数有误")
		return
	}
	// 订阅前必须确认域名属于当前用户，否则任何登录用户都能收到别人
	// 扫描进度里携带的 IP 与阶段信息
	if u := userFrom(r); u != nil {
		if _, err := s.st.DomainByID(domainID, u.ID); err != nil {
			fail(w, http.StatusNotFound, "域名不存在")
			return
		}
	} else {
		fail(w, http.StatusUnauthorized, "登录已过期，请重新登录")
		return
	}

	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		fail(w, http.StatusInternalServerError, "不支持流式响应")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.hub.Subscribe(domainID)
	defer cancel()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
