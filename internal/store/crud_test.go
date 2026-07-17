package store

import (
	"context"
	"database/sql"
	"testing"
)

// --- 设备测试 ---

func TestStore_CreateAndGetDeviceByAPIToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 先创建用户
	u, _ := s.CreateUser(ctx, &User{Username: "devuser", PasswordHash: "h"})

	// 创建设备
	d := &Device{
		UserID:       u.ID,
		Name:         "iPhone-快捷指令",
		Type:         "ios-shortcut",
		APITokenHash: sql.NullString{String: "hash_of_token", Valid: true},
	}
	created, err := s.CreateDevice(ctx, d)
	if err != nil {
		t.Fatalf("CreateDevice 失败: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("设备 ID 不应为 0")
	}

	// 按 API Token 哈希查询
	dev, user, err := s.GetDeviceByAPIToken(ctx, "hash_of_token")
	if err != nil {
		t.Fatalf("GetDeviceByAPIToken 失败: %v", err)
	}
	if dev.ID != created.ID {
		t.Errorf("设备 ID = %d, 期望 %d", dev.ID, created.ID)
	}
	if user.ID != u.ID {
		t.Errorf("用户 ID = %d, 期望 %d", user.ID, u.ID)
	}
	if user.Username != "devuser" {
		t.Errorf("用户名 = %s, 期望 devuser", user.Username)
	}
}

func TestStore_GetDeviceByAPIToken_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _, err := s.GetDeviceByAPIToken(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("期望 ErrNotFound, 实际 %v", err)
	}
}

func TestStore_ListDevicesByUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, &User{Username: "multi-dev", PasswordHash: "h"})

	for i := 0; i < 3; i++ {
		s.CreateDevice(ctx, &Device{
			UserID: u.ID,
			Name:   "device-" + string(rune('A'+i)),
			Type:   "web",
		})
	}

	devices, err := s.ListDevicesByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListDevicesByUser 失败: %v", err)
	}
	if len(devices) != 3 {
		t.Errorf("返回 %d 个设备, 期望 3", len(devices))
	}
}

func TestStore_RevokeDeviceAPIToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, &User{Username: "revoke-user", PasswordHash: "h"})
	d, _ := s.CreateDevice(ctx, &Device{
		UserID:       u.ID,
		Name:         "test-dev",
		Type:         "ios-shortcut",
		APITokenHash: sql.NullString{String: "token-hash-123", Valid: true},
	})

	// 吊销
	err := s.RevokeDeviceAPIToken(ctx, d.ID, u.ID)
	if err != nil {
		t.Fatalf("RevokeDeviceAPIToken 失败: %v", err)
	}

	// 吊销后查不到
	_, _, err = s.GetDeviceByAPIToken(ctx, "token-hash-123")
	if err != ErrNotFound {
		t.Errorf("吊销后应查不到, 实际 %v", err)
	}
}

func TestStore_DeleteDevice(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, &User{Username: "del-dev-user", PasswordHash: "h"})
	d, _ := s.CreateDevice(ctx, &Device{
		UserID: u.ID,
		Name:   "to-delete",
		Type:   "desktop",
	})

	err := s.DeleteDevice(ctx, d.ID, u.ID)
	if err != nil {
		t.Fatalf("DeleteDevice 失败: %v", err)
	}

	devices, _ := s.ListDevicesByUser(ctx, u.ID)
	if len(devices) != 0 {
		t.Errorf("删除后设备数 = %d, 期望 0", len(devices))
	}
}

func TestStore_DeleteDevice_WrongUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u1, _ := s.CreateUser(ctx, &User{Username: "owner", PasswordHash: "h"})
	u2, _ := s.CreateUser(ctx, &User{Username: "attacker", PasswordHash: "h"})
	d, _ := s.CreateDevice(ctx, &Device{UserID: u1.ID, Name: "dev", Type: "web"})

	// 用别的用户 ID 删除应失败
	err := s.DeleteDevice(ctx, d.ID, u2.ID)
	if err != ErrNotFound {
		t.Errorf("其他用户删除应返回 ErrNotFound, 实际 %v", err)
	}
}

// --- Refresh Token 测试 ---

func TestStore_CreateAndGetRefreshToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, &User{Username: "rt-user", PasswordHash: "h"})
	rt := &RefreshToken{
		UserID:    u.ID,
		TokenHash: "rt_hash_123",
		ExpiresAt: "2099-01-01 00:00:00",
	}
	created, err := s.CreateRefreshToken(ctx, rt)
	if err != nil {
		t.Fatalf("CreateRefreshToken 失败: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("Token ID 不应为 0")
	}

	// 查询
	found, err := s.GetRefreshToken(ctx, "rt_hash_123")
	if err != nil {
		t.Fatalf("GetRefreshToken 失败: %v", err)
	}
	if found.UserID != u.ID {
		t.Errorf("UserID = %d, 期望 %d", found.UserID, u.ID)
	}
	if found.Revoked {
		t.Error("新创建的 Token 不应已吊销")
	}
}

func TestStore_RevokeRefreshToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, &User{Username: "revoke-rt", PasswordHash: "h"})
	s.CreateRefreshToken(ctx, &RefreshToken{
		UserID:    u.ID,
		TokenHash: "rt_to_revoke",
		ExpiresAt: "2099-01-01 00:00:00",
	})

	// 吊销
	err := s.RevokeRefreshToken(ctx, "rt_to_revoke")
	if err != nil {
		t.Fatalf("RevokeRefreshToken 失败: %v", err)
	}

	// 吊销后查不到
	_, err = s.GetRefreshToken(ctx, "rt_to_revoke")
	if err != ErrNotFound {
		t.Errorf("吊销后应查不到, 实际 %v", err)
	}
}

func TestStore_RevokeAllRefreshTokensByUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, &User{Username: "revoke-all", PasswordHash: "h"})
	for i := 0; i < 3; i++ {
		s.CreateRefreshToken(ctx, &RefreshToken{
			UserID:    u.ID,
			TokenHash: "rt_" + string(rune('A'+i)),
			ExpiresAt: "2099-01-01 00:00:00",
		})
	}

	err := s.RevokeAllRefreshTokensByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("RevokeAllRefreshTokensByUser 失败: %v", err)
	}

	// 全部吊销后查不到
	for i := 0; i < 3; i++ {
		_, err := s.GetRefreshToken(ctx, "rt_"+string(rune('A'+i)))
		if err != ErrNotFound {
			t.Errorf("吊销后应查不到 token %d", i)
		}
	}
}

// --- 邀请码测试 ---

func TestStore_CreateAndGetInviteCode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin, _ := s.CreateUser(ctx, &User{Username: "admin", PasswordHash: "h", Role: "admin"})

	ic, err := s.CreateInviteCode(ctx, "ABCD1234", admin.ID, nil)
	if err != nil {
		t.Fatalf("CreateInviteCode 失败: %v", err)
	}
	if ic.ID == 0 {
		t.Fatal("邀请码 ID 不应为 0")
	}

	// 查询
	found, err := s.GetInviteCode(ctx, "ABCD1234")
	if err != nil {
		t.Fatalf("GetInviteCode 失败: %v", err)
	}
	if found.Code != "ABCD1234" {
		t.Errorf("Code = %s, 期望 ABCD1234", found.Code)
	}
	if found.Used {
		t.Error("新邀请码不应已使用")
	}
}

func TestStore_UseInviteCode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin, _ := s.CreateUser(ctx, &User{Username: "admin2", PasswordHash: "h", Role: "admin"})
	s.CreateInviteCode(ctx, "USEME01", admin.ID, nil)

	// 新用户使用邀请码
	newUser, _ := s.CreateUser(ctx, &User{Username: "invited", PasswordHash: "h"})

	err := s.UseInviteCode(ctx, "USEME01", newUser.ID)
	if err != nil {
		t.Fatalf("UseInviteCode 失败: %v", err)
	}

	// 使用后查不到（已使用）
	_, err = s.GetInviteCode(ctx, "USEME01")
	if err != ErrNotFound {
		t.Errorf("使用后应查不到, 实际 %v", err)
	}
}

func TestStore_GetInviteCode_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetInviteCode(ctx, "NOTEXIST")
	if err != ErrNotFound {
		t.Errorf("期望 ErrNotFound, 实际 %v", err)
	}
}

func TestStore_ListInviteCodes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin, _ := s.CreateUser(ctx, &User{Username: "admin3", PasswordHash: "h", Role: "admin"})

	for i := 0; i < 5; i++ {
		s.CreateInviteCode(ctx, "CODE"+string(rune('A'+i)), admin.ID, nil)
	}

	codes, err := s.ListInviteCodes(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListInviteCodes 失败: %v", err)
	}
	if len(codes) != 3 {
		t.Errorf("返回 %d 个邀请码, 期望 3", len(codes))
	}
}

// TestStore_UseInviteCode_DoubleSpend 回归：一次性邀请码不可双花。
// UPDATE 带 used 未使用守卫，第二个使用者应命中 0 行返回 ErrNotFound。
func TestStore_UseInviteCode_DoubleSpend(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin, _ := s.CreateUser(ctx, &User{Username: "admin-once", PasswordHash: "h", Role: "admin"})
	if _, err := s.CreateInviteCode(ctx, "ONCEONLY", admin.ID, nil); err != nil {
		t.Fatalf("CreateInviteCode 失败: %v", err)
	}

	u1, _ := s.CreateUser(ctx, &User{Username: "first-user", PasswordHash: "h"})
	u2, _ := s.CreateUser(ctx, &User{Username: "second-user", PasswordHash: "h"})

	// 第一次使用成功
	if err := s.UseInviteCode(ctx, "ONCEONLY", u1.ID); err != nil {
		t.Fatalf("第一次 UseInviteCode 失败: %v", err)
	}

	// 第二次使用应返回 ErrNotFound（守卫命中 0 行）
	if err := s.UseInviteCode(ctx, "ONCEONLY", u2.ID); err != ErrNotFound {
		t.Errorf("重复使用邀请码应返回 ErrNotFound, 实际 %v", err)
	}
}

func TestStore_UpdateUserEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u1, _ := s.CreateUser(ctx, &User{Username: "emailuser1", PasswordHash: "h"})
	u2, _ := s.CreateUser(ctx, &User{Username: "emailuser2", PasswordHash: "h",
		Email: sql.NullString{String: "taken@example.com", Valid: true}})

	// 设置邮箱
	if err := s.UpdateUserEmail(ctx, u1.ID, "new@example.com"); err != nil {
		t.Fatalf("UpdateUserEmail 失败: %v", err)
	}
	updated, _ := s.GetUserByID(ctx, u1.ID)
	if !updated.Email.Valid || updated.Email.String != "new@example.com" {
		t.Errorf("email = %v, 期望 new@example.com", updated.Email)
	}

	// 唯一约束冲突应返回 ErrEmailExists
	if err := s.UpdateUserEmail(ctx, u1.ID, "taken@example.com"); err != ErrEmailExists {
		t.Errorf("期望 ErrEmailExists, 实际 %v", err)
	}

	// 空串清除为 NULL
	if err := s.UpdateUserEmail(ctx, u1.ID, ""); err != nil {
		t.Fatalf("清除邮箱失败: %v", err)
	}
	updated, _ = s.GetUserByID(ctx, u1.ID)
	if updated.Email.Valid {
		t.Errorf("email 应已清除为 NULL, 实际 %v", updated.Email)
	}

	// 两个用户可同时为 NULL（不触发唯一约束）
	if err := s.UpdateUserEmail(ctx, u2.ID, ""); err != nil {
		t.Errorf("清除邮箱不应报错: %v", err)
	}

	// 不存在的用户返回 ErrNotFound
	if err := s.UpdateUserEmail(ctx, 99999, "x@example.com"); err != ErrNotFound {
		t.Errorf("期望 ErrNotFound, 实际 %v", err)
	}
}
