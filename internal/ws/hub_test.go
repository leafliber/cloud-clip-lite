package ws

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

// newTestHub 创建测试用 Hub（已启动 Run 循环）
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	hub := NewHub(slog.Default())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })
	// 等待 Hub 启动
	time.Sleep(10 * time.Millisecond)
	return hub
}

// newTestConn 创建测试用连接（无真实 WebSocket，仅 send channel）
func newTestConn(hub *Hub, userID int64, username string) *Connection {
	return &Connection{
		hub:      hub,
		userID:   userID,
		username: username,
		send:     make(chan []byte, sendBufferSize),
	}
}

// readWithTimeout 从 send channel 读取消息，超时返回 nil
func readWithTimeout(conn *Connection, timeout time.Duration) []byte {
	select {
	case msg := <-conn.send:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

func TestHub_RegisterAndUnregister(t *testing.T) {
	hub := newTestHub(t)

	conn := newTestConn(hub, 1, "user1")
	hub.Register(conn)
	time.Sleep(20 * time.Millisecond) // 等待异步处理

	if hub.GetOnlineCount() != 1 {
		t.Errorf("在线数 = %d, 期望 1", hub.GetOnlineCount())
	}
	if hub.GetUserConnectionCount(1) != 1 {
		t.Errorf("用户 1 连接数 = %d, 期望 1", hub.GetUserConnectionCount(1))
	}
	if hub.GetOnlineUserCount() != 1 {
		t.Errorf("在线用户数 = %d, 期望 1", hub.GetOnlineUserCount())
	}

	// 注销
	hub.Unregister(conn)
	time.Sleep(20 * time.Millisecond)

	if hub.GetOnlineCount() != 0 {
		t.Errorf("注销后在线数 = %d, 期望 0", hub.GetOnlineCount())
	}
	if hub.GetUserConnectionCount(1) != 0 {
		t.Errorf("注销后用户 1 连接数 = %d, 期望 0", hub.GetUserConnectionCount(1))
	}
}

func TestHub_MultipleConnectionsSameUser(t *testing.T) {
	hub := newTestHub(t)

	conn1 := newTestConn(hub, 1, "user1")
	conn2 := newTestConn(hub, 1, "user1")
	hub.Register(conn1)
	hub.Register(conn2)
	time.Sleep(20 * time.Millisecond)

	if hub.GetOnlineCount() != 2 {
		t.Errorf("在线数 = %d, 期望 2", hub.GetOnlineCount())
	}
	if hub.GetUserConnectionCount(1) != 2 {
		t.Errorf("用户 1 连接数 = %d, 期望 2", hub.GetUserConnectionCount(1))
	}
	// 同一用户应只算一个在线用户
	if hub.GetOnlineUserCount() != 1 {
		t.Errorf("在线用户数 = %d, 期望 1", hub.GetOnlineUserCount())
	}

	// 注销一个
	hub.Unregister(conn1)
	time.Sleep(20 * time.Millisecond)

	if hub.GetOnlineCount() != 1 {
		t.Errorf("注销一个后在线数 = %d, 期望 1", hub.GetOnlineCount())
	}
	if hub.GetUserConnectionCount(1) != 1 {
		t.Errorf("注销一个后用户 1 连接数 = %d, 期望 1", hub.GetUserConnectionCount(1))
	}
}

func TestHub_BroadcastToUser(t *testing.T) {
	hub := newTestHub(t)

	conn1 := newTestConn(hub, 1, "user1")
	conn2 := newTestConn(hub, 1, "user1")
	conn3 := newTestConn(hub, 2, "user2") // 不同用户
	hub.Register(conn1)
	hub.Register(conn2)
	hub.Register(conn3)
	time.Sleep(20 * time.Millisecond)

	// 广播给用户 1
	hub.BroadcastToUser(1, "test.message", map[string]any{"hello": "world"}, nil)
	time.Sleep(50 * time.Millisecond)

	// conn1 和 conn2 应收到消息
	msg1 := readWithTimeout(conn1, 100*time.Millisecond)
	if msg1 == nil {
		t.Error("conn1 未收到消息")
	} else {
		// 验证消息内容
		var sm ServerMessage
		if err := json.Unmarshal(msg1, &sm); err != nil {
			t.Fatalf("解析消息失败: %v", err)
		}
		if sm.Type != "test.message" {
			t.Errorf("消息类型 = %s, 期望 test.message", sm.Type)
		}
	}

	msg2 := readWithTimeout(conn2, 100*time.Millisecond)
	if msg2 == nil {
		t.Error("conn2 未收到消息")
	}

	// 先消费 connected 消息（如果有）
	_ = readWithTimeout(conn3, 100*time.Millisecond)

	// conn3 不应收到消息
	msg3 := readWithTimeout(conn3, 100*time.Millisecond)
	if msg3 != nil {
		t.Error("conn3 不应收到用户 1 的消息")
	}
}

func TestHub_BroadcastExcludeConn(t *testing.T) {
	hub := newTestHub(t)

	conn1 := newTestConn(hub, 1, "user1")
	conn2 := newTestConn(hub, 1, "user1")
	hub.Register(conn1)
	hub.Register(conn2)
	time.Sleep(20 * time.Millisecond)

	// 广播给用户 1，排除 conn1
	hub.BroadcastToUser(1, "test.exclude", map[string]any{"data": 1}, conn1)
	time.Sleep(50 * time.Millisecond)

	// conn1 不应收到
	msg1 := readWithTimeout(conn1, 100*time.Millisecond)
	if msg1 != nil {
		t.Error("conn1 应被排除，不应收到消息")
	}

	// conn2 应收到
	msg2 := readWithTimeout(conn2, 100*time.Millisecond)
	if msg2 == nil {
		t.Error("conn2 应收到消息")
	}
}

func TestHub_BroadcastClipCreated(t *testing.T) {
	hub := newTestHub(t)

	conn := newTestConn(hub, 1, "user1")
	hub.Register(conn)
	time.Sleep(20 * time.Millisecond)

	item := map[string]any{
		"id":   int64(42),
		"type": "text",
		"text": "hello",
	}
	hub.BroadcastClipCreated(1, item, nil)
	time.Sleep(50 * time.Millisecond)

	msg := readWithTimeout(conn, 100*time.Millisecond)
	if msg == nil {
		t.Fatal("未收到 clip.created 消息")
	}

	var sm ServerMessage
	if err := json.Unmarshal(msg, &sm); err != nil {
		t.Fatalf("解析消息失败: %v", err)
	}
	if sm.Type != MsgTypeClipCreated {
		t.Errorf("消息类型 = %s, 期望 %s", sm.Type, MsgTypeClipCreated)
	}

	var data ClipCreatedData
	if err := json.Unmarshal(sm.Data, &data); err != nil {
		t.Fatalf("解析消息数据失败: %v", err)
	}
	if data.ID != 42 {
		t.Errorf("data.ID = %d, 期望 42", data.ID)
	}
	if data.Item["text"] != "hello" {
		t.Errorf("data.Item[text] = %v, 期望 hello", data.Item["text"])
	}
}

func TestHub_BroadcastClipDeleted(t *testing.T) {
	hub := newTestHub(t)

	conn := newTestConn(hub, 1, "user1")
	hub.Register(conn)
	time.Sleep(20 * time.Millisecond)

	hub.BroadcastClipDeleted(1, 99, nil)
	time.Sleep(50 * time.Millisecond)

	msg := readWithTimeout(conn, 100*time.Millisecond)
	if msg == nil {
		t.Fatal("未收到 clip.deleted 消息")
	}

	var sm ServerMessage
	if err := json.Unmarshal(msg, &sm); err != nil {
		t.Fatalf("解析消息失败: %v", err)
	}
	if sm.Type != MsgTypeClipDeleted {
		t.Errorf("消息类型 = %s, 期望 %s", sm.Type, MsgTypeClipDeleted)
	}

	var data ClipDeletedData
	if err := json.Unmarshal(sm.Data, &data); err != nil {
		t.Fatalf("解析消息数据失败: %v", err)
	}
	if data.ID != 99 {
		t.Errorf("data.ID = %d, 期望 99", data.ID)
	}
}

func TestHub_BroadcastNoConnections(t *testing.T) {
	hub := newTestHub(t)

	// 广播给没有连接的用户，不应 panic
	hub.BroadcastToUser(999, "test.nodata", map[string]any{"x": 1}, nil)
	time.Sleep(50 * time.Millisecond)

	if hub.GetOnlineCount() != 0 {
		t.Errorf("在线数 = %d, 期望 0", hub.GetOnlineCount())
	}
}

func TestHub_Stop(t *testing.T) {
	hub := NewHub(slog.Default())
	go hub.Run()
	time.Sleep(10 * time.Millisecond)

	if !hub.running.Load() {
		t.Error("Hub 应在运行中")
	}

	hub.Stop()
	time.Sleep(10 * time.Millisecond)

	if hub.running.Load() {
		t.Error("Hub 应已停止")
	}

	// 重复 Stop 不应 panic
	hub.Stop()
}

func TestHub_StopClosesConnectionSend(t *testing.T) {
	hub := NewHub(slog.Default())
	go hub.Run()
	time.Sleep(10 * time.Millisecond)

	conn1 := newTestConn(hub, 1, "user1")
	conn2 := newTestConn(hub, 2, "user2")
	hub.Register(conn1)
	hub.Register(conn2)
	time.Sleep(20 * time.Millisecond) // 等待注册完成

	hub.Stop()
	time.Sleep(20 * time.Millisecond)

	// Stop 后已注册连接的 send channel 应被关闭（读立即返回 ok=false）
	for i, conn := range []*Connection{conn1, conn2} {
		select {
		case _, ok := <-conn.send:
			if ok {
				t.Errorf("连接 %d 的 send channel 应已关闭", i+1)
			}
		default:
			t.Errorf("连接 %d 的 send channel 应已关闭且可立即读取", i+1)
		}
	}

	if hub.GetOnlineCount() != 0 {
		t.Errorf("Stop 后在线数 = %d, 期望 0", hub.GetOnlineCount())
	}
	if hub.GetOnlineUserCount() != 0 {
		t.Errorf("Stop 后在线用户数 = %d, 期望 0", hub.GetOnlineUserCount())
	}
}

