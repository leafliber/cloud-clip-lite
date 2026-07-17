package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// createTestUser 创建测试用户并返回
func createTestUser(t *testing.T, s *Store, username string) *User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), &User{Username: username, PasswordHash: "h"})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return u
}

func TestStore_CreateAndGetClipItem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "clip-user")

	item := &ClipItem{
		UserID:      u.ID,
		Type:        "text",
		Size:        11,
		TextContent: sql.NullString{String: "hello world", Valid: true},
		SHA256:      sql.NullString{String: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9", Valid: true},
		Meta:        `{"source":"web"}`,
	}

	created, err := s.CreateClipItem(ctx, item)
	if err != nil {
		t.Fatalf("CreateClipItem 失败: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("ID 不应为 0")
	}

	// 按 ID 查询
	found, err := s.GetClipItem(ctx, created.ID, u.ID)
	if err != nil {
		t.Fatalf("GetClipItem 失败: %v", err)
	}
	if found.Type != "text" {
		t.Errorf("Type = %s, 期望 text", found.Type)
	}
	if !found.TextContent.Valid || found.TextContent.String != "hello world" {
		t.Errorf("TextContent = %v", found.TextContent)
	}
	if found.Size != 11 {
		t.Errorf("Size = %d, 期望 11", found.Size)
	}
}

func TestStore_GetClipItem_WrongUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u1 := createTestUser(t, s, "owner-clip")
	u2 := createTestUser(t, s, "attacker-clip")

	item, _ := s.CreateClipItem(ctx, &ClipItem{
		UserID: u1.ID, Type: "text", Size: 5,
		TextContent: sql.NullString{String: "secret", Valid: true},
	})

	// 用 u2 查询 u1 的条目应失败
	_, err := s.GetClipItem(ctx, item.ID, u2.ID)
	if err != ErrNotFound {
		t.Errorf("其他用户查询应返回 ErrNotFound, 实际 %v", err)
	}
}

func TestStore_GetLatestClipItem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "latest-user")

	// 创建 3 条
	for i := 0; i < 3; i++ {
		s.CreateClipItem(ctx, &ClipItem{
			UserID: u.ID, Type: "text", Size: 1,
			TextContent: sql.NullString{String: fmt.Sprintf("item-%d", i), Valid: true},
		})
	}

	// 查最新
	latest, err := s.GetLatestClipItem(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetLatestClipItem 失败: %v", err)
	}
	if !latest.TextContent.Valid || latest.TextContent.String != "item-2" {
		t.Errorf("最新条目 = %v, 期望 item-2", latest.TextContent)
	}
}

func TestStore_GetLatestClipItem_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "empty-latest")

	_, err := s.GetLatestClipItem(ctx, u.ID)
	if err != ErrNotFound {
		t.Errorf("空用户应返回 ErrNotFound, 实际 %v", err)
	}
}

func TestStore_ListClipItems(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "list-user")

	// 创建 5 条文本 + 2 条图片
	for i := 0; i < 5; i++ {
		s.CreateClipItem(ctx, &ClipItem{
			UserID: u.ID, Type: "text", Size: 1,
			TextContent: sql.NullString{String: fmt.Sprintf("t%d", i), Valid: true},
		})
	}
	for i := 0; i < 2; i++ {
		s.CreateClipItem(ctx, &ClipItem{
			UserID: u.ID, Type: "image", Size: 100,
			BlobKey: sql.NullString{String: fmt.Sprintf("blobs/%d/2026/07/img%d", u.ID, i), Valid: true},
		})
	}

	// 全部（7 条），分页 3
	page1, err := s.ListClipItems(ctx, u.ID, 0, 3, "")
	if err != nil {
		t.Fatalf("ListClipItems 失败: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("第一页返回 %d 条, 期望 3", len(page1))
	}

	// 第二页（beforeID = page1 最后一条的 ID）
	page2, err := s.ListClipItems(ctx, u.ID, page1[len(page1)-1].ID, 3, "")
	if err != nil {
		t.Fatalf("ListClipItems 第二页失败: %v", err)
	}
	if len(page2) != 3 {
		t.Fatalf("第二页返回 %d 条, 期望 3", len(page2))
	}

	// 第三页
	page3, err := s.ListClipItems(ctx, u.ID, page2[len(page2)-1].ID, 3, "")
	if err != nil {
		t.Fatalf("ListClipItems 第三页失败: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("第三页返回 %d 条, 期望 1", len(page3))
	}

	// 类型过滤：仅 image
	images, err := s.ListClipItems(ctx, u.ID, 0, 100, "image")
	if err != nil {
		t.Fatalf("ListClipItems image 过滤失败: %v", err)
	}
	if len(images) != 2 {
		t.Errorf("image 过滤返回 %d 条, 期望 2", len(images))
	}

	// 类型过滤：仅 text
	texts, err := s.ListClipItems(ctx, u.ID, 0, 100, "text")
	if err != nil {
		t.Fatalf("ListClipItems text 过滤失败: %v", err)
	}
	if len(texts) != 5 {
		t.Errorf("text 过滤返回 %d 条, 期望 5", len(texts))
	}
}

func TestStore_DeleteClipItem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "del-clip")

	item, _ := s.CreateClipItem(ctx, &ClipItem{
		UserID: u.ID, Type: "text", Size: 5,
		TextContent: sql.NullString{String: "todelete", Valid: true},
	})

	// 删除
	deleted, err := s.DeleteClipItem(ctx, item.ID, u.ID)
	if err != nil {
		t.Fatalf("DeleteClipItem 失败: %v", err)
	}
	if deleted.ID != item.ID {
		t.Errorf("返回的 ID = %d, 期望 %d", deleted.ID, item.ID)
	}

	// 删除后查不到
	_, err = s.GetClipItem(ctx, item.ID, u.ID)
	if err != ErrNotFound {
		t.Errorf("删除后应查不到, 实际 %v", err)
	}
}

func TestStore_DeleteClipItem_WrongUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u1 := createTestUser(t, s, "del-owner")
	u2 := createTestUser(t, s, "del-attacker")

	item, _ := s.CreateClipItem(ctx, &ClipItem{
		UserID: u1.ID, Type: "text", Size: 1,
		TextContent: sql.NullString{String: "x", Valid: true},
	})

	_, err := s.DeleteClipItem(ctx, item.ID, u2.ID)
	if err != ErrNotFound {
		t.Errorf("其他用户删除应返回 ErrNotFound, 实际 %v", err)
	}

	// 确认未被删除
	_, err = s.GetClipItem(ctx, item.ID, u1.ID)
	if err != nil {
		t.Errorf("条目应仍存在, 实际 %v", err)
	}
}

func TestStore_GetUserStorageUsage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "quota-user")

	s.CreateClipItem(ctx, &ClipItem{UserID: u.ID, Type: "text", Size: 100})
	s.CreateClipItem(ctx, &ClipItem{UserID: u.ID, Type: "image", Size: 5000})
	s.CreateClipItem(ctx, &ClipItem{UserID: u.ID, Type: "file", Size: 20000})

	usage, err := s.GetUserStorageUsage(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserStorageUsage 失败: %v", err)
	}
	if usage != 25100 {
		t.Errorf("usage = %d, 期望 25100", usage)
	}
}

func TestStore_GetUserStorageUsage_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "empty-quota")

	usage, err := s.GetUserStorageUsage(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserStorageUsage 失败: %v", err)
	}
	if usage != 0 {
		t.Errorf("空用户 usage = %d, 期望 0", usage)
	}
}

func TestStore_ListClipItemsSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "since-user")

	// 创建 5 条
	var ids []int64
	for i := 0; i < 5; i++ {
		item, err := s.CreateClipItem(ctx, &ClipItem{
			UserID:      u.ID,
			Type:        "text",
			Size:        1,
			TextContent: sql.NullString{String: fmt.Sprintf("since-%d", i), Valid: true},
		})
		if err != nil {
			t.Fatalf("CreateClipItem 失败: %v", err)
		}
		ids = append(ids, item.ID)
	}

	// since=0 应返回全部 5 条（按 id ASC）
	items, err := s.ListClipItemsSince(ctx, u.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(items) != 5 {
		t.Errorf("since=0 返回 %d 条, 期望 5", len(items))
	}
	// 验证按 id ASC 排序
	for i, item := range items {
		if item.ID != ids[i] {
			t.Errorf("第 %d 条 ID = %d, 期望 %d", i, item.ID, ids[i])
		}
	}

	// since=ids[2] 应返回 2 条（ids[3], ids[4]）
	items2, err := s.ListClipItemsSince(ctx, u.ID, ids[2], 100)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(items2) != 2 {
		t.Fatalf("since=%d 返回 %d 条, 期望 2", ids[2], len(items2))
	}
	if items2[0].ID != ids[3] {
		t.Errorf("第一条 ID = %d, 期望 %d", items2[0].ID, ids[3])
	}

	// since=最后一个 ID 应返回 0 条
	items3, err := s.ListClipItemsSince(ctx, u.ID, ids[4], 100)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(items3) != 0 {
		t.Errorf("since=最后 ID 返回 %d 条, 期望 0", len(items3))
	}

	// limit 限制
	items4, err := s.ListClipItemsSince(ctx, u.ID, 0, 2)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(items4) != 2 {
		t.Errorf("limit=2 返回 %d 条, 期望 2", len(items4))
	}
}

func TestStore_ListClipItemsSince_WrongUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u1 := createTestUser(t, s, "since-owner")
	u2 := createTestUser(t, s, "since-attacker")

	s.CreateClipItem(ctx, &ClipItem{
		UserID: u1.ID, Type: "text", Size: 1,
		TextContent: sql.NullString{String: "private", Valid: true},
	})

	// u2 查询 u1 的条目应返回空
	items, err := s.ListClipItemsSince(ctx, u2.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("其他用户查询应返回 0 条, 实际 %d", len(items))
	}
}

// TestStore_ClipItem_DeviceID_RoundTrip 回归：device_id 是 INTEGER 列，
// 曾扫进 sql.NullString 导致任何带 DeviceID 的条目读取报错。
// 这里对带 DeviceID 的条目逐个走 Get/List/Latest/ListSince/Delete 往返验证。
func TestStore_ClipItem_DeviceID_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "dev-roundtrip")

	dev, err := s.CreateDevice(ctx, &Device{UserID: u.ID, Name: "web", Type: "web"})
	if err != nil {
		t.Fatalf("CreateDevice 失败: %v", err)
	}

	item, err := s.CreateClipItem(ctx, &ClipItem{
		UserID:      u.ID,
		DeviceID:    sql.NullInt64{Int64: dev.ID, Valid: true},
		Type:        "text",
		Size:        5,
		TextContent: sql.NullString{String: "with-device", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateClipItem 失败: %v", err)
	}

	assertDeviceID := func(got *ClipItem, from string) {
		t.Helper()
		if !got.DeviceID.Valid || got.DeviceID.Int64 != dev.ID {
			t.Errorf("%s: DeviceID = %v, 期望 %d", from, got.DeviceID, dev.ID)
		}
	}

	// Get
	found, err := s.GetClipItem(ctx, item.ID, u.ID)
	if err != nil {
		t.Fatalf("GetClipItem 失败: %v", err)
	}
	assertDeviceID(found, "GetClipItem")

	// Latest
	latest, err := s.GetLatestClipItem(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetLatestClipItem 失败: %v", err)
	}
	assertDeviceID(latest, "GetLatestClipItem")

	// List
	items, err := s.ListClipItems(ctx, u.ID, 0, 10, "")
	if err != nil {
		t.Fatalf("ListClipItems 失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListClipItems 返回 %d 条, 期望 1", len(items))
	}
	assertDeviceID(items[0], "ListClipItems")

	// ListSince
	since, err := s.ListClipItemsSince(ctx, u.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(since) != 1 {
		t.Fatalf("ListClipItemsSince 返回 %d 条, 期望 1", len(since))
	}
	assertDeviceID(since[0], "ListClipItemsSince")

	// Delete（内部调 GetClipItem，曾经也因此报错删不掉）
	deleted, err := s.DeleteClipItem(ctx, item.ID, u.ID)
	if err != nil {
		t.Fatalf("DeleteClipItem 失败: %v", err)
	}
	assertDeviceID(deleted, "DeleteClipItem")

	// 删除后查不到
	if _, err = s.GetClipItem(ctx, item.ID, u.ID); err != ErrNotFound {
		t.Errorf("删除后应返回 ErrNotFound, 实际 %v", err)
	}
}

// TestStore_ClipItem_ExpiredFiltered 回归：过期条目在调度器清理前不应再被读到
func TestStore_ClipItem_ExpiredFiltered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "expired-user")

	// 已过期条目
	expired, err := s.CreateClipItem(ctx, &ClipItem{
		UserID:      u.ID,
		Type:        "text",
		Size:        1,
		TextContent: sql.NullString{String: "expired", Valid: true},
		ExpiresAt:   sql.NullString{String: "2020-01-01 00:00:00", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateClipItem 失败: %v", err)
	}

	// 未过期的最新条目
	fresh, err := s.CreateClipItem(ctx, &ClipItem{
		UserID:      u.ID,
		Type:        "text",
		Size:        1,
		TextContent: sql.NullString{String: "fresh", Valid: true},
		ExpiresAt:   sql.NullString{String: "2099-01-01 00:00:00", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateClipItem 失败: %v", err)
	}

	// GetClipItem：过期条目返回 ErrNotFound，未过期可读
	if _, err := s.GetClipItem(ctx, expired.ID, u.ID); err != ErrNotFound {
		t.Errorf("过期条目应返回 ErrNotFound, 实际 %v", err)
	}
	if _, err := s.GetClipItem(ctx, fresh.ID, u.ID); err != nil {
		t.Errorf("未过期条目应可读, 实际 %v", err)
	}

	// GetLatestClipItem：跳过已过期，返回未过期的
	latest, err := s.GetLatestClipItem(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetLatestClipItem 失败: %v", err)
	}
	if latest.ID != fresh.ID {
		t.Errorf("最新条目 ID = %d, 期望 %d（未过期）", latest.ID, fresh.ID)
	}

	// ListClipItems：不含过期条目
	items, err := s.ListClipItems(ctx, u.ID, 0, 100, "")
	if err != nil {
		t.Fatalf("ListClipItems 失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != fresh.ID {
		t.Errorf("ListClipItems 应只返回未过期条目, 实际 %v", items)
	}

	// ListClipItemsSince：不含过期条目
	since, err := s.ListClipItemsSince(ctx, u.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListClipItemsSince 失败: %v", err)
	}
	if len(since) != 1 || since[0].ID != fresh.ID {
		t.Errorf("ListClipItemsSince 应只返回未过期条目, 实际 %v", since)
	}

	// DeleteClipItem：过期条目经 GetClipItem 已不可见，返回 ErrNotFound
	if _, err := s.DeleteClipItem(ctx, expired.ID, u.ID); err != ErrNotFound {
		t.Errorf("删除过期条目应返回 ErrNotFound, 实际 %v", err)
	}
}
