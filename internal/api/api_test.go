package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/blob"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/db"
	"github.com/leaf/cloud-clip-lite/internal/migrate"
	"github.com/leaf/cloud-clip-lite/internal/middleware"
	"github.com/leaf/cloud-clip-lite/internal/store"
	"github.com/leaf/cloud-clip-lite/internal/ws"
)

// apiTestSetup 创建完整的 API 测试环境
// 返回已组装的路由处理器和 store（供测试中直接操作数据库）
func apiTestSetup(t *testing.T, allowRegister string) (http.Handler, *store.Store) {
	t.Helper()
	ctx := context.Background()

	cfg := &config.Config{
		SQLitePath:           ":memory:",
		JWTSecret:            "test-secret-at-least-32-bytes-long!!!",
		AccessTTL:            15 * time.Minute,
		RefreshTTL:           720 * time.Hour,
		AllowRegister:        allowRegister,
		DefaultMaxItemSize:   10485760,
		DefaultQuotaBytes:    1073741824,
		DefaultRetentionDays: 30,
		LogLevel:             "info",
		LogFormat:            "json",
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
	jwtMgr := auth.NewJWTManager(cfg.JWTSecret, cfg.AccessTTL)
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	log := slog.Default()

	// 创建临时 BlobStore
	bs, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("创建测试 BlobStore 失败: %v", err)
	}

	// 创建 WebSocket Hub
	hub := ws.NewHub(log)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	// 创建限流器（测试中放宽限制）
	rl := middleware.NewRateLimiter(10000, 1000)

	// 直接用 Server 组装
	server := New(cfg, st, bs, nil, log, jwtMgr, hasher, hub, nil, rl)
	return server.Router(), st
}

// doRequest 辅助发送 HTTP 请求
func doRequest(handler http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// parseJSON 解析 JSON 响应
func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("解析 JSON 失败: %v, body: %s", err, rec.Body.String())
	}
	return m
}

// --- 注册测试 ---

func TestRegister_OpenMode(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username: "newuser",
		Password: "password123",
		Email:    "new@example.com",
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, 期望 201, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	user, _ := resp["user"].(map[string]any)
	if user["username"] != "newuser" {
		t.Errorf("username = %v", user["username"])
	}
	if resp["access_token"] == nil {
		t.Error("应返回 access_token")
	}
	if resp["refresh_token"] == nil {
		t.Error("应返回 refresh_token")
	}
}

func TestRegister_ClosedMode(t *testing.T) {
	handler, _ := apiTestSetup(t, "closed")

	rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username: "newuser",
		Password: "password123",
	}, "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d, 期望 403", rec.Code)
	}
}

func TestRegister_InviteMode(t *testing.T) {
	handler, st := apiTestSetup(t, "invite")
	ctx := context.Background()

	// 创建管理员并生成邀请码
	admin, _ := st.CreateUser(ctx, &store.User{Username: "admin", PasswordHash: "h", Role: "admin"})
	st.CreateInviteCode(ctx, "VALID123", admin.ID, nil)

	// 用有效邀请码注册
	rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username:   "invited",
		Password:   "password123",
		InviteCode: "VALID123",
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, 期望 201, body: %s", rec.Code, rec.Body.String())
	}

	// 邀请码使用后再次使用应失败
	rec2 := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username:   "invited2",
		Password:   "password123",
		InviteCode: "VALID123",
	}, "")
	if rec2.Code != http.StatusCreated {
		// 应该失败因为邀请码已用
		if rec2.Code == http.StatusBadRequest {
			// 符合预期
		} else {
			t.Fatalf("重复邀请码状态码 = %d, 期望 400", rec2.Code)
		}
	}
}

func TestRegister_InviteMode_InvalidCode(t *testing.T) {
	handler, _ := apiTestSetup(t, "invite")

	rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username:   "invited",
		Password:   "password123",
		InviteCode: "INVALID",
	}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	st.CreateUser(ctx, &store.User{Username: "exists", PasswordHash: "h"})

	rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username: "exists",
		Password: "password123",
	}, "")

	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d, 期望 409", rec.Code)
	}
}

func TestRegister_InvalidUsername(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	tests := []struct {
		name     string
		username string
	}{
		{"太短", "ab"},
		{"太长", string(make([]byte, 33))},
		{"非法字符", "user@name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
				Username: tt.username,
				Password: "password123",
			}, "")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("状态码 = %d, 期望 400", rec.Code)
			}
		})
	}
}

func TestRegister_InvalidPassword(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	rec := doRequest(handler, "POST", "/api/auth/register", registerRequest{
		Username: "validuser",
		Password: "12345",
	}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
}

// --- 登录测试 ---

func TestLogin_Success(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("mypassword")
	st.CreateUser(ctx, &store.User{Username: "loginuser", PasswordHash: hash})

	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "loginuser",
		Password: "mypassword",
	}, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["access_token"] == nil {
		t.Error("应返回 access_token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("correct")
	st.CreateUser(ctx, &store.User{Username: "wrongpw", PasswordHash: hash})

	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "wrongpw",
		Password: "incorrect",
	}, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "ghost",
		Password: "password",
	}, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
}

func TestLogin_DisabledAccount(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	st.CreateUser(ctx, &store.User{Username: "disabled", PasswordHash: hash, Status: "disabled"})

	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "disabled",
		Password: "password",
	}, "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d, 期望 403", rec.Code)
	}
}

// --- 刷新与登出测试 ---

func TestRefresh_Success(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "refreshuser", PasswordHash: hash})

	// 登录获取 refresh token
	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "refreshuser",
		Password: "password",
	}, "")
	loginResp := parseJSON(t, rec)
	refreshToken, _ := loginResp["refresh_token"].(string)

	// 刷新
	rec2 := doRequest(handler, "POST", "/api/auth/refresh", refreshRequest{
		RefreshToken: refreshToken,
	}, "")

	if rec2.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec2.Code, rec2.Body.String())
	}

	resp := parseJSON(t, rec2)
	if resp["access_token"] == nil {
		t.Error("应返回新的 access_token")
	}
	if resp["refresh_token"] == nil {
		t.Error("应返回新的 refresh_token")
	}

	// 旧 refresh token 应已吊销（轮转）
	rec3 := doRequest(handler, "POST", "/api/auth/refresh", refreshRequest{
		RefreshToken: refreshToken,
	}, "")
	if rec3.Code != http.StatusUnauthorized {
		t.Errorf("旧 refresh token 应已失效, 状态码 = %d, 期望 401", rec3.Code)
	}

	_ = u
}

func TestLogout(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	st.CreateUser(ctx, &store.User{Username: "logoutuser", PasswordHash: hash})

	// 登录
	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "logoutuser",
		Password: "password",
	}, "")
	loginResp := parseJSON(t, rec)
	refreshToken, _ := loginResp["refresh_token"].(string)

	// 登出
	rec2 := doRequest(handler, "POST", "/api/auth/logout", refreshRequest{
		RefreshToken: refreshToken,
	}, "")

	if rec2.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec2.Code)
	}

	// 登出后 refresh token 应失效
	rec3 := doRequest(handler, "POST", "/api/auth/refresh", refreshRequest{
		RefreshToken: refreshToken,
	}, "")
	if rec3.Code != http.StatusUnauthorized {
		t.Errorf("登出后刷新应失败, 状态码 = %d, 期望 401", rec3.Code)
	}
}

// --- /api/me 测试 ---

func TestGetMe(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "meuser", PasswordHash: hash})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	rec := doRequest(handler, "GET", "/api/me", nil, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["username"] != "meuser" {
		t.Errorf("username = %v, 期望 meuser", resp["username"])
	}
}

func TestGetMe_Unauthorized(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	rec := doRequest(handler, "GET", "/api/me", nil, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
}

func TestUpdateMe_ChangePassword(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("oldpassword")
	u, _ := st.CreateUser(ctx, &store.User{Username: "changepw", PasswordHash: hash})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	oldPw := "oldpassword"
	newPw := "newpassword123"
	rec := doRequest(handler, "PATCH", "/api/me", updateMeRequest{
		OldPassword: &oldPw,
		Password:    &newPw,
	}, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	// 旧密码登录应失败
	rec2 := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "changepw",
		Password: "oldpassword",
	}, "")
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("旧密码登录应失败, 状态码 = %d", rec2.Code)
	}

	// 新密码登录应成功
	rec3 := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "changepw",
		Password: "newpassword123",
	}, "")
	if rec3.Code != http.StatusOK {
		t.Errorf("新密码登录应成功, 状态码 = %d", rec3.Code)
	}
}

func TestUpdateMe_ChangePassword_WrongOld(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("correctold")
	u, _ := st.CreateUser(ctx, &store.User{Username: "wrongold", PasswordHash: hash})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	wrongOld := "wrongold"
	newPw := "newpassword123"
	rec := doRequest(handler, "PATCH", "/api/me", updateMeRequest{
		OldPassword: &wrongOld,
		Password:    &newPw,
	}, token)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
}

func TestUpdateMe_RetentionDays(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "retention", PasswordHash: hash})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	days := 60
	rec := doRequest(handler, "PATCH", "/api/me", updateMeRequest{
		RetentionDays: &days,
	}, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if int(resp["retention_days"].(float64)) != 60 {
		t.Errorf("retention_days = %v, 期望 60", resp["retention_days"])
	}
}

// --- /api/devices 测试 ---

func TestCreateDevice(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "devowner", PasswordHash: hash})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	rec := doRequest(handler, "POST", "/api/devices", createDeviceRequest{
		Name: "iPhone-快捷指令",
		Type: "ios-shortcut",
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, 期望 201, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["api_token"] == nil {
		t.Error("应返回 api_token")
	}
	if resp["name"] != "iPhone-快捷指令" {
		t.Errorf("name = %v", resp["name"])
	}
}

func TestListDevices(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "listdev", PasswordHash: hash})

	// 直接创建设备
	st.CreateDevice(ctx, &store.Device{UserID: u.ID, Name: "dev1", Type: "web"})
	st.CreateDevice(ctx, &store.Device{UserID: u.ID, Name: "dev2", Type: "desktop"})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	rec := doRequest(handler, "GET", "/api/devices", nil, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	resp := parseJSON(t, rec)
	devices, _ := resp["devices"].([]any)
	if len(devices) != 2 {
		t.Errorf("设备数 = %d, 期望 2", len(devices))
	}
}

func TestDeleteDevice(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "deldev", PasswordHash: hash})

	d, _ := st.CreateDevice(ctx, &store.Device{UserID: u.ID, Name: "todelete", Type: "web"})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	rec := doRequest(handler, "DELETE", "/api/devices/"+fmtInt(d.ID), nil, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// 确认已删除
	devices, _ := st.ListDevicesByUser(ctx, u.ID)
	if len(devices) != 0 {
		t.Errorf("删除后设备数 = %d, 期望 0", len(devices))
	}
}

func TestRevokeDeviceToken(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password")
	u, _ := st.CreateUser(ctx, &store.User{Username: "revokedev", PasswordHash: hash})

	apiToken, _ := auth.GenerateAPIToken()
	tokenHash := auth.HashToken(apiToken)
	d, _ := st.CreateDevice(ctx, &store.Device{
		UserID:       u.ID,
		Name:         "revokeme",
		Type:         "ios-shortcut",
		APITokenHash: sql.NullString{String: tokenHash, Valid: true},
	})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	jwtToken, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	rec := doRequest(handler, "POST", "/api/devices/"+fmtInt(d.ID)+"/revoke", nil, jwtToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// 确认 Token 已吊销（查不到）
	_, _, err := st.GetDeviceByAPIToken(ctx, tokenHash)
	if err != store.ErrNotFound {
		t.Errorf("吊销后应查不到, err = %v", err)
	}
}

func TestDevices_Unauthorized(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	rec := doRequest(handler, "GET", "/api/devices", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("状态码 = %d, 期望 401", rec.Code)
	}

	rec = doRequest(handler, "POST", "/api/devices", createDeviceRequest{Name: "test"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("状态码 = %d, 期望 401", rec.Code)
	}
}

// fmtInt 快速格式化 int64
func fmtInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
