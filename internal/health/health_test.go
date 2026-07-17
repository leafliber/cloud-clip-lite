package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLiveness_Version(t *testing.T) {
	// 默认版本为 dev
	ready := &atomic.Bool{}
	h := New(nil, ready)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	h.Liveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp["version"] != "dev" {
		t.Errorf("version = %v, 期望 dev", resp["version"])
	}
}

func TestNewWithVersion(t *testing.T) {
	// 注入构建版本（对应 Dockerfile 的 -X main.Version）
	ready := &atomic.Bool{}
	h := NewWithVersion(nil, ready, "1.2.3")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	h.Liveness(rec, req)

	if !strings.Contains(rec.Body.String(), `"version":"1.2.3"`) {
		t.Errorf("版本未注入, body: %s", rec.Body.String())
	}

	// 空版本回退 dev
	h2 := NewWithVersion(nil, ready, "")
	rec2 := httptest.NewRecorder()
	h2.Liveness(rec2, httptest.NewRequest("GET", "/healthz", nil))
	if !strings.Contains(rec2.Body.String(), `"version":"dev"`) {
		t.Errorf("空版本应回退 dev, body: %s", rec2.Body.String())
	}
}

func TestReadiness_NotReady(t *testing.T) {
	ready := &atomic.Bool{}
	h := NewWithVersion(nil, ready, "1.2.3")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/readyz", nil)
	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状态码 = %d, 期望 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"version":"1.2.3"`) {
		t.Errorf("not_ready 响应版本不正确, body: %s", rec.Body.String())
	}
}
