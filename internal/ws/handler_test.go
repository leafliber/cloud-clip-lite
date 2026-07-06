package ws

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/config"
	"github.com/leaf/cloud-clip-lite/internal/db"
	"github.com/leaf/cloud-clip-lite/internal/migrate"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

const testJWTSecret = "test-secret-at-least-32-bytes-long!!!"

// wsTestSetup 创建 WS 测试环境
// 返回：测试 HTTP 服务器、store、jwtMgr、hub
func wsTestSetup(t *testing.T) (*httptest.Server, *store.Store, *auth.JWTManager, *Hub) {
	t.Helper()
	ctx := context.Background()

	cfg := &config.Config{
		SQLitePath:           ":memory:",
		JWTSecret:            testJWTSecret,
		AccessTTL:            15 * time.Minute,
		DefaultMaxItemSize:   10485760,
		DefaultQuotaBytes:    1073741824,
		DefaultRetentionDays: 30,
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
	log := slog.Default()

	hub := NewHub(log)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	handler := NewHandler(hub, jwtMgr, st, []string{"*"}, log)
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close() })

	return server, st, jwtMgr, hub
}

// wsURL 将 HTTP URL 转为 WS URL
func wsURL(server *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + path
}

// wsDial 连接 WebSocket 并返回连接
func wsDial(t *testing.T, server *httptest.Server, token string, useQuery bool) *websocket.Conn {
	t.Helper()

	url := wsURL(server, "/ws")
	if useQuery && token != "" {
		url += "?token=" + token
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	var headers http.Header
	if !useQuery && token != "" {
		headers = http.Header{}
		headers.Set("Authorization", "Bearer "+token)
	}

	conn, resp, err := dialer.Dial(url, headers)
	if err != nil {
		if resp != nil {
			t.Fatalf("WS 连接失败: %v, status: %d", err, resp.StatusCode)
		}
		t.Fatalf("WS 连接失败: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// 设置读超时
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	return conn
}

// wsReadMessage 读取一条 WS 消息（带超时）
func wsReadMessage(t *testing.T, conn *websocket.Conn) ServerMessage {
	t.Helper()
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读取 WS 消息失败: %v", err)
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("解析 WS 消息失败: %v, raw: %s", err, string(data))
	}
	return msg
}

// wsReadMessageNoFail 读取消息，失败返回 nil
func wsReadMessageNoFail(conn *websocket.Conn) *ServerMessage {
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}
	return &msg
}

// wsSendJSON 发送 JSON 消息
func wsSendJSON(t *testing.T, conn *websocket.Conn, msg any) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化消息失败: %v", err)
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("发送 WS 消息失败: %v", err)
	}
}

// createTestUserWS 创建测试用户
func createTestUserWS(t *testing.T, st *store.Store, username string) *store.User {
	t.Helper()
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	u, err := st.CreateUser(context.Background(), &store.User{
		Username:     username,
		PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return u
}

// ---------- 鉴权测试 ----------

func TestWS_Handler_AuthJWT_Header(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-jwt-header")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)

	// 应收到 connected 消息
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeConnected {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeConnected)
	}

	var data ConnectedData
	json.Unmarshal(msg.Data, &data)
	if data.UserID != u.ID {
		t.Errorf("user_id = %d, 期望 %d", data.UserID, u.ID)
	}
	if data.Username != "ws-jwt-header" {
		t.Errorf("username = %s, 期望 ws-jwt-header", data.Username)
	}
}

func TestWS_Handler_AuthJWT_QueryParam(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-jwt-query")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, true)

	// 应收到 connected 消息
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeConnected {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeConnected)
	}
}

func TestWS_Handler_AuthAPIToken(t *testing.T) {
	server, st, _, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-api-token")

	// 创建设备并绑定 API Token
	apiToken, _ := auth.GenerateAPIToken()
	tokenHash := auth.HashToken(apiToken)
	st.CreateDevice(context.Background(), &store.Device{
		UserID:       u.ID,
		Name:         "test-device",
		Type:         "web",
		APITokenHash: sql.NullString{String: tokenHash, Valid: true},
	})

	conn := wsDial(t, server, apiToken, false)

	// 应收到 connected 消息
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeConnected {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeConnected)
	}

	var data ConnectedData
	json.Unmarshal(msg.Data, &data)
	if data.UserID != u.ID {
		t.Errorf("user_id = %d, 期望 %d", data.UserID, u.ID)
	}
}

func TestWS_Handler_NoToken(t *testing.T) {
	server, _, _, _ := wsTestSetup(t)

	url := wsURL(server, "/ws")
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("无 token 应连接失败")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("状态码 = %v, 期望 401", resp)
	}
}

func TestWS_Handler_InvalidToken(t *testing.T) {
	server, _, _, _ := wsTestSetup(t)

	url := wsURL(server, "/ws") + "?token=invalid-token-123"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("无效 token 应连接失败")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("状态码 = %v, 期望 401", resp)
	}
}

// ---------- 心跳测试 ----------

func TestWS_Handler_PingPong(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-ping")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)

	// 读取 connected 消息
	wsReadMessage(t, conn)

	// 发送 ping
	wsSendJSON(t, conn, ClientMessage{Type: MsgTypePing})

	// 应收到 pong
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypePong {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypePong)
	}
}

// ---------- 错误处理测试 ----------

func TestWS_Handler_UnknownMessageType(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-error")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)

	// 读取 connected 消息
	wsReadMessage(t, conn)

	// 发送未知消息类型
	wsSendJSON(t, conn, ClientMessage{Type: "unknown.type"})

	// 应收到 error 消息
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeError {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeError)
	}

	var data ErrorData
	json.Unmarshal(msg.Data, &data)
	if data.Code != "UNKNOWN_TYPE" {
		t.Errorf("error code = %s, 期望 UNKNOWN_TYPE", data.Code)
	}
}

func TestWS_Handler_InvalidJSON(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-badjson")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)

	// 读取 connected 消息
	wsReadMessage(t, conn)

	// 发送无效 JSON
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	conn.WriteMessage(websocket.TextMessage, []byte("not json{{{"))

	// 应收到 error 消息
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeError {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeError)
	}
}

// ---------- 增量同步测试 ----------

func TestWS_Handler_Sync(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-sync")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	// 预创建 3 条 clip
	ctx := context.Background()
	var ids []int64
	for i := 0; i < 3; i++ {
		item, _ := st.CreateClipItem(ctx, &store.ClipItem{
			UserID:      u.ID,
			Type:        "text",
			Size:        5,
			TextContent: sql.NullString{String: "sync-item", Valid: true},
		})
		ids = append(ids, item.ID)
	}

	conn := wsDial(t, server, token, false)

	// 读取 connected 消息
	wsReadMessage(t, conn)

	// 请求增量同步 since=0（获取全部）
	syncReq := SyncRequestData{Since: 0}
	reqData, _ := json.Marshal(syncReq)
	wsSendJSON(t, conn, ClientMessage{
		Type: MsgTypeSync,
		Data: reqData,
	})

	// 应收到 sync.result
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeSyncResult {
		t.Fatalf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeSyncResult)
	}

	var result SyncResultData
	json.Unmarshal(msg.Data, &result)
	if result.Count != 3 {
		t.Errorf("count = %d, 期望 3", result.Count)
	}

	// 请求增量同步 since=ids[1]（应返回 1 条）
	syncReq2 := SyncRequestData{Since: ids[1]}
	reqData2, _ := json.Marshal(syncReq2)
	wsSendJSON(t, conn, ClientMessage{
		Type: MsgTypeSync,
		Data: reqData2,
	})

	msg2 := wsReadMessage(t, conn)
	if msg2.Type != MsgTypeSyncResult {
		t.Fatalf("消息类型 = %s, 期望 %s", msg2.Type, MsgTypeSyncResult)
	}

	var result2 SyncResultData
	json.Unmarshal(msg2.Data, &result2)
	if result2.Count != 1 {
		t.Errorf("count = %d, 期望 1", result2.Count)
	}
}

func TestWS_Handler_Sync_Empty(t *testing.T) {
	server, st, jwtMgr, _ := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-sync-empty")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)
	wsReadMessage(t, conn) // connected

	// 请求同步（无数据）
	syncReq := SyncRequestData{Since: 0}
	reqData, _ := json.Marshal(syncReq)
	wsSendJSON(t, conn, ClientMessage{
		Type: MsgTypeSync,
		Data: reqData,
	})

	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeSyncResult {
		t.Fatalf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeSyncResult)
	}

	var result SyncResultData
	json.Unmarshal(msg.Data, &result)
	if result.Count != 0 {
		t.Errorf("count = %d, 期望 0", result.Count)
	}
	if result.Items == nil {
		t.Error("items 不应为 nil")
	}
}

// ---------- 广播集成测试 ----------

func TestWS_Handler_BroadcastReceived(t *testing.T) {
	server, st, jwtMgr, hub := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-broadcast")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)
	wsReadMessage(t, conn) // connected

	// 通过 Hub 广播 clip.created
	item := map[string]any{
		"id":   int64(100),
		"type": "text",
		"text": "broadcast-test",
	}
	hub.BroadcastClipCreated(u.ID, item, nil)

	// 客户端应收到广播
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg := wsReadMessage(t, conn)
	if msg.Type != MsgTypeClipCreated {
		t.Errorf("消息类型 = %s, 期望 %s", msg.Type, MsgTypeClipCreated)
	}

	var data ClipCreatedData
	json.Unmarshal(msg.Data, &data)
	if data.ID != 100 {
		t.Errorf("data.ID = %d, 期望 100", data.ID)
	}
}

func TestWS_Handler_MultipleClientsBroadcast(t *testing.T) {
	server, st, jwtMgr, hub := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-multi")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	// 两个客户端连接同一用户
	conn1 := wsDial(t, server, token, false)
	wsReadMessage(t, conn1) // connected

	conn2 := wsDial(t, server, token, false)
	wsReadMessage(t, conn2) // connected

	// 广播
	hub.BroadcastClipDeleted(u.ID, 555, nil)

	// 两个客户端都应收到
	msg1 := wsReadMessageNoFail(conn1)
	if msg1 == nil || msg1.Type != MsgTypeClipDeleted {
		t.Error("conn1 未收到 clip.deleted 消息")
	}

	msg2 := wsReadMessageNoFail(conn2)
	if msg2 == nil || msg2.Type != MsgTypeClipDeleted {
		t.Error("conn2 未收到 clip.deleted 消息")
	}
}

// ---------- 连接管理测试 ----------

func TestWS_Handler_DisconnectUpdatesCount(t *testing.T) {
	server, st, jwtMgr, hub := wsTestSetup(t)
	u := createTestUserWS(t, st, "ws-count")
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	conn := wsDial(t, server, token, false)
	wsReadMessage(t, conn) // connected

	// 等待 Hub 注册完成
	waitFor(t, 2*time.Second, func() bool {
		return hub.GetOnlineCount() == 1
	})
	if hub.GetOnlineCount() != 1 {
		t.Fatalf("在线数 = %d, 期望 1", hub.GetOnlineCount())
	}

	// 关闭连接
	_ = conn.Close()

	// 轮询等待 Hub 检测到断开并注销
	waitFor(t, 3*time.Second, func() bool {
		return hub.GetOnlineCount() == 0
	})
	if hub.GetOnlineCount() != 0 {
		t.Errorf("断开后在线数 = %d, 期望 0", hub.GetOnlineCount())
	}
}

// waitFor 轮询等待条件满足，超时返回 false
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}
