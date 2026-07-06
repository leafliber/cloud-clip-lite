package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/db"
	"github.com/leaf/cloud-clip-lite/internal/migrate"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// testSetup 创建测试用 JWT 管理器和 Store
func testSetup(t *testing.T) (*auth.JWTManager, *store.Store) {
	t.Helper()
	ctx := context.Background()

	cfg := &config.Config{
		SQLitePath: ":memory:",
		JWTSecret:  "test-secret-at-least-32-bytes-long!!!",
	}
	database, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := migrate.Run(ctx, database); err != nil {
		t.Fatalf("运行迁移失败: %v", err)
	}

	st := store.New(database)
	jwtMgr := auth.NewJWTManager(cfg.JWTSecret, 15*time.Minute)
	return jwtMgr, st
}

// dummyHandler 返回 200 并写入鉴权上下文的处理器
func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac := GetAuthContext(r.Context())
		if ac != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user_id":   ac.UserID,
				"username":  ac.Username,
				"role":      ac.Role,
				"auth_type": ac.AuthType,
			})
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})
}

func TestRequireAuth_JWT(t *testing.T) {
	jwtMgr, st := testSetup(t)
	ctx := context.Background()

	// 创建用户
	u, _ := st.CreateUser(ctx, &store.User{Username: "jwtuser", PasswordHash: "h", Role: "user"})

	// 生成 JWT
	token, err := jwtMgr.GenerateAccessToken(u.ID, u.Username, "user")
	if err != nil {
		t.Fatalf("GenerateAccessToken 失败: %v", err)
	}

	handler := RequireAuth(jwtMgr, st)(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["username"] != "jwtuser" {
		t.Errorf("username = %v, 期望 jwtuser", resp["username"])
	}
	if resp["auth_type"] != "jwt" {
		t.Errorf("auth_type = %v, 期望 jwt", resp["auth_type"])
	}
}

func TestRequireAuth_APIToken(t *testing.T) {
	jwtMgr, st := testSetup(t)
	ctx := context.Background()

	u, _ := st.CreateUser(ctx, &store.User{Username: "apiuser", PasswordHash: "h", Role: "user"})

	// 生成 API Token 明文
	apiToken, _ := auth.GenerateAPIToken()
	tokenHash := auth.HashToken(apiToken)

	st.CreateDevice(ctx, &store.Device{
		UserID:       u.ID,
		Name:         "iPhone",
		Type:         "ios-shortcut",
		APITokenHash: sql.NullString{String: tokenHash, Valid: true},
	})

	handler := RequireAuth(jwtMgr, st)(dummyHandler())

	// 通过 X-API-Token 头
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Token", apiToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["username"] != "apiuser" {
		t.Errorf("username = %v, 期望 apiuser", resp["username"])
	}
	if resp["auth_type"] != "api_token" {
		t.Errorf("auth_type = %v, 期望 api_token", resp["auth_type"])
	}
}

func TestRequireAuth_APIToken_ViaBearer(t *testing.T) {
	jwtMgr, st := testSetup(t)
	ctx := context.Background()

	u, _ := st.CreateUser(ctx, &store.User{Username: "bearer-api", PasswordHash: "h"})
	apiToken, _ := auth.GenerateAPIToken()
	tokenHash := auth.HashToken(apiToken)

	st.CreateDevice(ctx, &store.Device{
		UserID:       u.ID,
		Name:         "Desktop",
		Type:         "desktop",
		APITokenHash: sql.NullString{String: tokenHash, Valid: true},
	})

	handler := RequireAuth(jwtMgr, st)(dummyHandler())

	// 通过 Authorization: Bearer 头传 API Token
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+apiToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
}

func TestRequireAuth_MissingCredentials(t *testing.T) {
	jwtMgr, st := testSetup(t)

	handler := RequireAuth(jwtMgr, st)(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	jwtMgr, st := testSetup(t)

	handler := RequireAuth(jwtMgr, st)(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-string")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
}

func TestRequireAuth_RevokedAPIToken(t *testing.T) {
	jwtMgr, st := testSetup(t)
	ctx := context.Background()

	u, _ := st.CreateUser(ctx, &store.User{Username: "revoked-api", PasswordHash: "h"})
	apiToken, _ := auth.GenerateAPIToken()
	tokenHash := auth.HashToken(apiToken)

	dev, _ := st.CreateDevice(ctx, &store.Device{
		UserID:       u.ID,
		Name:         "Dev",
		Type:         "web",
		APITokenHash: sql.NullString{String: tokenHash, Valid: true},
	})

	// 吊销
	st.RevokeDeviceAPIToken(ctx, dev.ID, u.ID)

	handler := RequireAuth(jwtMgr, st)(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-API-Token", apiToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("吊销的 Token 应返回 401, 实际 %d", rec.Code)
	}
}

func TestRequireRole_Admin(t *testing.T) {
	jwtMgr, st := testSetup(t)
	ctx := context.Background()

	// 管理员
	admin, _ := st.CreateUser(ctx, &store.User{Username: "admin1", PasswordHash: "h", Role: "admin"})
	// 普通用户
	normalUser, _ := st.CreateUser(ctx, &store.User{Username: "normal1", PasswordHash: "h", Role: "user"})

	adminToken, _ := jwtMgr.GenerateAccessToken(admin.ID, admin.Username, "admin")
	userToken, _ := jwtMgr.GenerateAccessToken(normalUser.ID, normalUser.Username, "user")

	handler := RequireAuth(jwtMgr, st)(RequireAdmin()(dummyHandler()))

	// 管理员访问
	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("管理员应能访问, 状态码 = %d", rec.Code)
	}

	// 普通用户访问
	req2 := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req2.Header.Set("Authorization", "Bearer "+userToken)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("普通用户应被拒绝, 状态码 = %d, 期望 403", rec2.Code)
	}
}
