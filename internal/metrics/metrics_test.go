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

