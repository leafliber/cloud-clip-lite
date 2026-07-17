package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/db"
	"github.com/leaf/cloud-clip-lite/internal/migrate"
)

// newTestStore 创建内存 SQLite 数据库并运行迁移，返回 Store
func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()

	// 使用内存 SQLite，设置 secret 以通过校验
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

	return New(database)
}

// --- 以下为实际测试用例 ---

func TestStore_CreateAndGetUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u := &User{
		Username:     "alice",
		PasswordHash: "$argon2id$fakehash",
		Email:        sql.NullString{String: "alice@example.com", Valid: true},
	}

	created, err := s.CreateUser(ctx, u)
	if err != nil {
		t.Fatalf("CreateUser 失败: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("用户 ID 不应为 0")
	}
	if created.Role != "user" {
		t.Errorf("Role = %s, 期望 user", created.Role)
	}
	if created.Status != "active" {
		t.Errorf("Status = %s, 期望 active", created.Status)
	}
	if created.MaxItemSize != 10485760 {
		t.Errorf("MaxItemSize = %d, 期望 10485760", created.MaxItemSize)
	}

	// 按用户名查询
	found, err := s.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername 失败: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %d, 期望 %d", found.ID, created.ID)
	}

	// 按 ID 查询
	foundByID, err := s.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID 失败: %v", err)
	}
	if foundByID.Username != "alice" {
		t.Errorf("Username = %s, 期望 alice", foundByID.Username)
	}
}

func TestStore_GetUser_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetUserByUsername(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("期望 ErrNotFound, 实际 %v", err)
	}

	_, err = s.GetUserByID(ctx, 99999)
	if err != ErrNotFound {
		t.Errorf("期望 ErrNotFound, 实际 %v", err)
	}
}

func TestStore_CreateUser_DuplicateUsername(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u1 := &User{Username: "bob", PasswordHash: "hash1"}
	_, err := s.CreateUser(ctx, u1)
	if err != nil {
		t.Fatalf("第一次创建失败: %v", err)
	}

	u2 := &User{Username: "bob", PasswordHash: "hash2"}
	_, err = s.CreateUser(ctx, u2)
	if err == nil {
		t.Fatal("重复用户名应返回错误")
	}
}

func TestStore_UpdateUserPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u := &User{Username: "carol", PasswordHash: "old-hash"}
	created, _ := s.CreateUser(ctx, u)

	err := s.UpdateUserPassword(ctx, created.ID, "new-hash")
	if err != nil {
		t.Fatalf("UpdateUserPassword 失败: %v", err)
	}

	found, _ := s.GetUserByID(ctx, created.ID)
	if found.PasswordHash != "new-hash" {
		t.Errorf("PasswordHash = %s, 期望 new-hash", found.PasswordHash)
	}
}

func TestStore_UpdateUserSettings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u := &User{Username: "dave", PasswordHash: "hash"}
	created, _ := s.CreateUser(ctx, u)

	err := s.UpdateUserSettings(ctx, created.ID, 52428800, 5368709120, 60)
	if err != nil {
		t.Fatalf("UpdateUserSettings 失败: %v", err)
	}

	found, _ := s.GetUserByID(ctx, created.ID)
	if found.MaxItemSize != 52428800 {
		t.Errorf("MaxItemSize = %d, 期望 52428800", found.MaxItemSize)
	}
	if found.QuotaBytes != 5368709120 {
		t.Errorf("QuotaBytes = %d, 期望 5368709120", found.QuotaBytes)
	}
	if found.RetentionDays != 60 {
		t.Errorf("RetentionDays = %d, 期望 60", found.RetentionDays)
	}
}

func TestStore_UpdateUserRoleAndStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u := &User{Username: "eve", PasswordHash: "hash"}
	created, _ := s.CreateUser(ctx, u)

	if err := s.UpdateUserRole(ctx, created.ID, "admin"); err != nil {
		t.Fatalf("UpdateUserRole 失败: %v", err)
	}
	if err := s.UpdateUserStatus(ctx, created.ID, "disabled"); err != nil {
		t.Fatalf("UpdateUserStatus 失败: %v", err)
	}

	found, _ := s.GetUserByID(ctx, created.ID)
	if found.Role != "admin" {
		t.Errorf("Role = %s, 期望 admin", found.Role)
	}
	if found.Status != "disabled" {
		t.Errorf("Status = %s, 期望 disabled", found.Status)
	}
}

func TestStore_ListUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s.CreateUser(ctx, &User{
			Username:     fmt.Sprintf("user%d", i),
			PasswordHash: "hash",
		})
	}

	users, err := s.ListUsers(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListUsers 失败: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("返回 %d 个用户, 期望 3", len(users))
	}

	// 第二页
	users2, _ := s.ListUsers(ctx, 3, 3)
	if len(users2) != 2 {
		t.Errorf("第二页返回 %d 个用户, 期望 2", len(users2))
	}
}

func TestStore_CountUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	count, _ := s.CountUsers(ctx)
	if count != 0 {
		t.Errorf("初始用户数 = %d, 期望 0", count)
	}

	s.CreateUser(ctx, &User{Username: "u1", PasswordHash: "h"})
	s.CreateUser(ctx, &User{Username: "u2", PasswordHash: "h"})

	count, _ = s.CountUsers(ctx)
	if count != 2 {
		t.Errorf("用户数 = %d, 期望 2", count)
	}
}
