package store

import (
	"context"
	"testing"
)

// TestStore_ListAuditLogs_PaginationStable 回归：按 created_at DESC 分页时同秒记录会重复/遗漏，
// 加 id DESC 决胜键后翻页应稳定覆盖全部记录且无重复
func TestStore_ListAuditLogs_PaginationStable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 快速写入 5 条（SQLite 秒级精度下 created_at 大概率同秒）
	const total = 5
	for i := 0; i < total; i++ {
		if err := s.CreateAuditLog(ctx, &AuditLog{Action: "clip.create"}); err != nil {
			t.Fatalf("CreateAuditLog 失败: %v", err)
		}
	}

	// 以 limit=2 翻页收集全部 ID
	var got []int64
	for offset := 0; ; offset += 2 {
		logs, err := s.ListAuditLogs(ctx, 0, "", 2, offset)
		if err != nil {
			t.Fatalf("ListAuditLogs 失败: %v", err)
		}
		for _, l := range logs {
			got = append(got, l.ID)
		}
		if len(logs) < 2 {
			break
		}
	}

	if len(got) != total {
		t.Fatalf("翻页共收集 %d 条, 期望 %d", len(got), total)
	}
	// 无重复且严格按 id DESC
	seen := make(map[int64]bool)
	for i, id := range got {
		if seen[id] {
			t.Fatalf("翻页结果重复: ID %d", id)
		}
		seen[id] = true
		if i > 0 && got[i-1] < id {
			t.Errorf("翻页结果未按 id DESC 排列: %v", got)
		}
	}
}
