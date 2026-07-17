package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetrics_Handler(t *testing.T) {
	m := New()

	// 记录一些指标
	m.IncCounter("requests", map[string]string{"method": "GET", "status": "200"})
	m.IncCounter("requests", map[string]string{"method": "GET", "status": "200"})
	m.SetGauge("online", 42, nil)
	m.Observe("duration", 0.05, map[string]string{"path": "/api/clip"})

	// 获取 /metrics
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	body := rec.Body.String()

	// 应包含 Go 运行时指标
	if !strings.Contains(body, "go_goroutines") {
		t.Error("应包含 go_goroutines")
	}
	if !strings.Contains(body, "go_mem_alloc_bytes") {
		t.Error("应包含 go_mem_alloc_bytes")
	}

	// 应包含自定义计数器
	if !strings.Contains(body, "cloud_clip_requests_total") {
		t.Error("应包含 cloud_clip_requests_total")
	}
	if !strings.Contains(body, `method="GET"`) {
		t.Error("应包含 method 标签")
	}
	if !strings.Contains(body, `status="200"`) {
		t.Error("应包含 status 标签")
	}

	// 应包含自定义仪表
	if !strings.Contains(body, "cloud_clip_gauge") {
		t.Error("应包含 cloud_clip_gauge")
	}

	// 应包含直方图
	if !strings.Contains(body, "cloud_clip_request_duration_seconds") {
		t.Error("应包含直方图")
	}

	// Content-Type 应为 Prometheus 格式
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %s, 期望包含 text/plain", ct)
	}
}

func TestMetrics_IncCounter(t *testing.T) {
	m := New()

	m.IncCounter("test", nil)
	m.IncCounter("test", nil)
	m.IncCounter("test", nil)

	// 再次增加应复用同一个 counter
	m.IncCounter("test", nil)

	// 验证通过 handler 输出
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler()(rec, req)
	body := rec.Body.String()

	// 应包含值 4
	if !strings.Contains(body, "cloud_clip_requests_total 4") {
		t.Errorf("计数器值不正确, body: %s", body)
	}
}

func TestMetrics_SetGauge(t *testing.T) {
	m := New()

	m.SetGauge("gauge1", 100, map[string]string{"name": "test"})
	m.SetGauge("gauge1", 200, map[string]string{"name": "test"}) // 覆盖

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler()(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, "200") {
		t.Errorf("仪表值应被覆盖为 200, body: %s", body)
	}
}

func TestMetrics_Observe(t *testing.T) {
	m := New()

	m.Observe("hist", 0.01, nil)
	m.Observe("hist", 0.5, nil)
	m.Observe("hist", 2.0, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler()(rec, req)
	body := rec.Body.String()

	// 应包含 count=3
	if !strings.Contains(body, "_count 3") {
		t.Errorf("直方图 count 不正确, body: %s", body)
	}
	// 应包含 sum
	if !strings.Contains(body, "_sum") {
		t.Error("应包含 _sum")
	}
}


func TestMetrics_LabelOrderIndependent(t *testing.T) {
	m := New()

	// 相同 label 集、不同插入顺序，应归为同一个计数器
	m.IncCounter("req", map[string]string{"method": "GET", "status": "200"})
	m.IncCounter("req", map[string]string{"status": "200", "method": "GET"})

	m.mu.Lock()
	n := len(m.counters)
	m.mu.Unlock()
	if n != 1 {
		t.Fatalf("相同 label 集应只产生 1 个计数器, 实际 %d", n)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler()(rec, req)
	body := rec.Body.String()

	// label 排序后输出稳定，且值为 2
	if !strings.Contains(body, `cloud_clip_requests_total{method="GET",status="200"} 2`) {
		t.Errorf("计数器输出不正确, body: %s", body)
	}
}

func TestMetrics_HistogramBucketFormat(t *testing.T) {
	m := New()
	m.Observe("lat", 0.03, map[string]string{"path": "/api/clip"})
	m.Observe("plain", 0.03, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler()(rec, req)
	body := rec.Body.String()

	// le 必须在花括号内（合法 exposition 格式），不允许出现 }_le= 或 _le= 拼接
	if strings.Contains(body, "_le=") {
		t.Errorf("bucket 行格式非法（le 在花括号外）, body: %s", body)
	}
	// 有 label 时 le 合入 label 集合（le 排序在 path 前）
	if !strings.Contains(body, `cloud_clip_request_duration_seconds_bucket{le="0.05",path="/api/clip"} 1`) {
		t.Errorf("带 label 的 bucket 行不正确, body: %s", body)
	}
	// 无 label 时输出 {le="..."}
	if !strings.Contains(body, `cloud_clip_request_duration_seconds_bucket{le="0.05"} 1`) {
		t.Errorf("无 label 的 bucket 行不正确, body: %s", body)
	}
	if !strings.Contains(body, `cloud_clip_request_duration_seconds_bucket{le="+Inf",path="/api/clip"} 1`) {
		t.Errorf("+Inf bucket 行不正确, body: %s", body)
	}
}
