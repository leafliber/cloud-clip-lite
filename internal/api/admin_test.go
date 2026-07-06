package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// 创建 admin 用户并获取 token
func loginAsAdmin(t *testing.T, handler http.Handler, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("adminpass123")
	u, _ := st.CreateUser(ctx, &store.User{
		Username:     "testadmin",
		PasswordHash: hash,
		Role:         "admin",
	})
	_ = u

	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "testadmin",
		Password: "adminpass123",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin 登录失败: %d, %s", rec.Code, rec.Body.String())
	}
	resp := parseJSON(t, rec)
	return resp["access_token"].(string)
}

func TestAdmin_GetStats(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAsAdmin(t, handler, st)

	// 创建一些数据
	loginAndGetToken(t, handler, st, "statuser1", "password123")
	loginAndGetToken(t, handler, st, "statuser2", "password123")

	rec := doRequest(handler, "GET", "/api/admin/stats", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["user_count"].(float64) < 3 {
		t.Errorf("user_count = %v, 期望 >= 3", resp["user_count"])
	}
	if resp["active_count"] == nil {
		t.Error("应返回 active_count")
	}
}

func TestAdmin_ListUsers(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAsAdmin(t, handler, st)

	// 创建几个用户
	loginAndGetToken(t, handler, st, "listuser1", "password123")
	loginAndGetToken(t, handler, st, "listuser2", "password123")

	rec := doRequest(handler, "GET", "/api/admin/users?limit=10", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	resp := parseJSON(t, rec)
	items, _ := resp["items"].([]any)
	if len(items) < 3 {
		t.Errorf("返回 %d 个用户, 期望 >= 3", len(items))
	}
	if resp["total"] == nil {
		t.Error("应返回 total")
	}
}

func TestAdmin_UpdateUser_Status(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	// 创建普通用户
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	target, _ := st.CreateUser(context.Background(), &store.User{Username: "targetuser", PasswordHash: hash})

	// 禁用用户
	rec := doRequest(handler, "PATCH", fmt.Sprintf("/api/admin/users/%d", target.ID), map[string]any{
		"status": "disabled",
	}, adminToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["status"] != "disabled" {
		t.Errorf("status = %v, 期望 disabled", resp["status"])
	}

	// 验证数据库
	updated, _ := st.GetUserByID(context.Background(), target.ID)
	if updated.Status != "disabled" {
		t.Errorf("数据库 status = %s, 期望 disabled", updated.Status)
	}
}

func TestAdmin_UpdateUser_Quota(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	target, _ := st.CreateUser(context.Background(), &store.User{Username: "quotauser", PasswordHash: hash})

	rec := doRequest(handler, "PATCH", fmt.Sprintf("/api/admin/users/%d", target.ID), map[string]any{
		"quota_bytes":    int64(536870912), // 512MB
		"max_item_size":  int64(5242880),   // 5MB
		"retention_days": 7,
	}, adminToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	resp := parseJSON(t, rec)
	if int64(resp["quota_bytes"].(float64)) != 536870912 {
		t.Errorf("quota_bytes = %v, 期望 536870912", resp["quota_bytes"])
	}
	if int64(resp["max_item_size"].(float64)) != 5242880 {
		t.Errorf("max_item_size = %v, 期望 5242880", resp["max_item_size"])
	}
	if int(resp["retention_days"].(float64)) != 7 {
		t.Errorf("retention_days = %v, 期望 7", resp["retention_days"])
	}
}

func TestAdmin_ResetPassword(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("oldpass123")
	target, _ := st.CreateUser(context.Background(), &store.User{Username: "resetuser", PasswordHash: hash})

	rec := doRequest(handler, "POST", fmt.Sprintf("/api/admin/users/%d/reset-password", target.ID), map[string]any{
		"password": "newpass456",
	}, adminToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// 验证新密码可以登录
	rec = doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "resetuser",
		Password: "newpass456",
	}, "")
	if rec.Code != http.StatusOK {
		t.Errorf("新密码登录失败: %d", rec.Code)
	}

	// 验证旧密码不能登录
	rec = doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "resetuser",
		Password: "oldpass123",
	}, "")
	if rec.Code == http.StatusOK {
		t.Error("旧密码应不能登录")
	}
}

func TestAdmin_ForceLogout(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	// 普通用户登录获取 refresh token
	userToken := loginAndGetToken(t, handler, st, "forceuser", "password123")

	// 先获取 refresh token（通过登录响应）
	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: "forceuser",
		Password: "password123",
	}, "")
	loginResp := parseJSON(t, rec)
	refreshToken := loginResp["refresh_token"].(string)

	// admin 强制下线
	target, _ := st.GetUserByUsername(context.Background(), "forceuser")

	rec = doRequest(handler, "POST", fmt.Sprintf("/api/admin/users/%d/force-logout", target.ID), nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// refresh token 应失效
	rec = doRequest(handler, "POST", "/api/auth/refresh", map[string]any{
		"refresh_token": refreshToken,
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("强制下线后 refresh 应 401, 实际 %d", rec.Code)
	}

	_ = userToken // access token 仍有效直到过期（JWT 无状态）
}

func TestAdmin_DeleteUser(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	target, _ := st.CreateUser(context.Background(), &store.User{Username: "deleteuser", PasswordHash: hash})

	rec := doRequest(handler, "DELETE", fmt.Sprintf("/api/admin/users/%d", target.ID), nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// 验证已删除
	_, err := st.GetUserByID(context.Background(), target.ID)
	if err == nil {
		t.Error("用户应已删除")
	}
}

func TestAdmin_DeleteSelf_Forbidden(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	admin, _ := st.GetUserByUsername(context.Background(), "testadmin")

	rec := doRequest(handler, "DELETE", fmt.Sprintf("/api/admin/users/%d", admin.ID), nil, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码 = %d, 期望 400（不能删除自己）", rec.Code)
	}
}

func TestAdmin_ForbiddenForNormalUser(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	userToken := loginAndGetToken(t, handler, st, "normaluser", "password123")

	rec := doRequest(handler, "GET", "/api/admin/stats", nil, userToken)
	if rec.Code != http.StatusForbidden {
		t.Errorf("状态码 = %d, 期望 403", rec.Code)
	}

	rec = doRequest(handler, "GET", "/api/admin/users", nil, userToken)
	if rec.Code != http.StatusForbidden {
		t.Errorf("状态码 = %d, 期望 403", rec.Code)
	}
}

func TestAdmin_ListAuditLogs(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	adminToken := loginAsAdmin(t, handler, st)

	// 触发一些写操作以生成审计日志
	loginAndGetToken(t, handler, st, "audituser1", "password123")
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	target, _ := st.CreateUser(context.Background(), &store.User{Username: "audituser2", PasswordHash: hash})
	doRequest(handler, "PATCH", fmt.Sprintf("/api/admin/users/%d", target.ID), map[string]any{"status": "disabled"}, adminToken)

	rec := doRequest(handler, "GET", "/api/admin/audit-logs?limit=10", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	items, _ := resp["items"].([]any)
	if len(items) == 0 {
		t.Error("应返回审计日志")
	}
	// 验证有 admin 操作的日志
	hasAdminLog := false
	for _, item := range items {
		log := item.(map[string]any)
		if action, ok := log["action"].(string); ok && action == "admin.user.status_update" {
			hasAdminLog = true
			break
		}
	}
	if !hasAdminLog {
		t.Error("应包含 admin.user.status_update 日志")
	}
}
