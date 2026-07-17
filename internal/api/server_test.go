package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestStaticSPA 工作目录下存在 ./web/dist 时伺服前端静态文件，
// 未命中文件的路径回退 index.html（React Router 客户端路由），/api 等端点不进入静态伺服
func TestStaticSPA(t *testing.T) {
	// 在临时目录中构造 web/dist 并切换工作目录
	tmp := t.TempDir()
	dist := filepath.Join(tmp, "web", "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	indexHTML := "<html><body>spa</body></html>"
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmp)

	handler, _ := apiTestSetup(t, "open")

	// 根路径返回 index.html
	rec := doRequest(handler, "GET", "/", nil, "")
	if rec.Code != http.StatusOK || rec.Body.String() != indexHTML {
		t.Errorf("GET /: 状态码 = %d, body = %q", rec.Code, rec.Body.String())
	}

	// 命中真实文件直接伺服
	rec = doRequest(handler, "GET", "/assets/app.js", nil, "")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Errorf("GET /assets/app.js: 状态码 = %d, body = %q", rec.Code, rec.Body.String())
	}

	// 未命中路径 SPA fallback 到 index.html
	rec = doRequest(handler, "GET", "/settings/users", nil, "")
	if rec.Code != http.StatusOK || rec.Body.String() != indexHTML {
		t.Errorf("SPA fallback: 状态码 = %d, body = %q", rec.Code, rec.Body.String())
	}

	// 未注册的 /api 路径不进入静态伺服，应 404
	rec = doRequest(handler, "GET", "/api/nonexistent", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/nonexistent: 状态码 = %d, 期望 404", rec.Code)
	}

	// 已注册 API 路由优先于静态通配
	rec = doRequest(handler, "GET", "/api/public/config", nil, "")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/public/config: 状态码 = %d, 期望 200", rec.Code)
	}
}
