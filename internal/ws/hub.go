package ws

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// Hub 管理所有 WebSocket 连接，按 user_id 分组
// 提供 Register/Unregister/BroadcastToUser 等线程安全方法
type Hub struct {
	// connections 按 user_id 分组的连接集合
	connections map[int64]map[*Connection]bool

	// register 注册新连接
	register chan *Connection

	// unregister 注销连接
	unregister chan *Connection

	// broadcast 广播消息（按 user_id）
	broadcast chan *broadcastMessage

	// stop 停止信号
	stop chan struct{}

	// 在线用户计数（原子操作）
	onlineCount atomic.Int64

	// mu 保护 connections map
	mu sync.RWMutex

	logger *slog.Logger

	// 是否正在运行
	running atomic.Bool
}

// broadcastMessage 广播消息内部结构
type broadcastMessage struct {
	userID   int64
	msgType  string
	data     any
	exclude  *Connection // 排除的连接（通常是发起操作的连接）
}

// NewHub 创建 WebSocket Hub
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		connections: make(map[int64]map[*Connection]bool),
		register:    make(chan *Connection, 64),
		unregister:  make(chan *Connection, 64),
		broadcast:   make(chan *broadcastMessage, 256),
		stop:        make(chan struct{}),
		logger:      logger,
	}
}

// Run 启动 Hub 事件循环（应在独立 goroutine 中运行）
func (h *Hub) Run() {
	h.running.Store(true)
	h.logger.Info("WebSocket Hub 已启动")

	for {
		select {
		case <-h.stop:
			h.logger.Info("WebSocket Hub 已停止")
			h.running.Store(false)
			return

		case conn := <-h.register:
			h.handleRegister(conn)

		case conn := <-h.unregister:
			h.handleUnregister(conn)

		case msg := <-h.broadcast:
			h.handleBroadcast(msg)
		}
	}
}

// Stop 停止 Hub 事件循环
func (h *Hub) Stop() {
	select {
	case <-h.stop:
		// 已经关闭
	default:
		close(h.stop)
	}
}

// handleRegister 处理连接注册
func (h *Hub) handleRegister(conn *Connection) {
	h.mu.Lock()
	if h.connections[conn.userID] == nil {
		h.connections[conn.userID] = make(map[*Connection]bool)
	}
	h.connections[conn.userID][conn] = true
	h.mu.Unlock()

	h.onlineCount.Add(1)
	h.logger.Debug("WS 连接注册",
		"user_id", conn.userID,
		"username", conn.username,
		"device_id", conn.deviceID,
		"online_total", h.onlineCount.Load(),
	)
}

// handleUnregister 处理连接注销
func (h *Hub) handleUnregister(conn *Connection) {
	h.mu.Lock()
	if conns, ok := h.connections[conn.userID]; ok {
		if _, exists := conns[conn]; exists {
			delete(conns, conn)
			if len(conns) == 0 {
				delete(h.connections, conn.userID)
			}
		}
	}
	h.mu.Unlock()

	h.onlineCount.Add(-1)
	close(conn.send) // 通知 writePump 退出

	h.logger.Debug("WS 连接注销",
		"user_id", conn.userID,
		"username", conn.username,
		"online_total", h.onlineCount.Load(),
	)
}

// handleBroadcast 处理消息广播
func (h *Hub) handleBroadcast(msg *broadcastMessage) {
	h.mu.RLock()
	conns := h.connections[msg.userID]
	// 复制一份避免长时间持有锁
	targets := make([]*Connection, 0, len(conns))
	for c := range conns {
		if c != msg.exclude {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	// 序列化消息一次，发送给所有目标
	rawMsg, err := newServerMessage(msg.msgType, msg.data)
	if err != nil {
		h.logger.Error("创建广播消息失败", "error", err, "type", msg.msgType)
		return
	}

	sent := 0
	for _, c := range targets {
		select {
		case c.send <- rawMsg:
			sent++
		default:
			// 发送缓冲区满，跳过该连接（客户端可能卡死）
			h.logger.Warn("WS 发送缓冲区满，跳过",
				"user_id", c.userID,
				"device_id", c.deviceID,
			)
		}
	}

	h.logger.Debug("WS 广播完成",
		"user_id", msg.userID,
		"type", msg.msgType,
		"targets", len(targets),
		"sent", sent,
	)
}

// Register 注册一个连接到 Hub
func (h *Hub) Register(conn *Connection) {
	h.register <- conn
}

// Unregister 从 Hub 注销一个连接
func (h *Hub) Unregister(conn *Connection) {
	h.unregister <- conn
}

// BroadcastToUser 向指定用户的所有连接广播消息
// excludeConn 不为 nil 时，跳过该连接（避免回声）
func (h *Hub) BroadcastToUser(userID int64, msgType string, data any, excludeConn *Connection) {
	h.broadcast <- &broadcastMessage{
		userID:  userID,
		msgType: msgType,
		data:    data,
		exclude: excludeConn,
	}
}

// BroadcastClipCreated 广播新条目创建事件
func (h *Hub) BroadcastClipCreated(userID int64, item map[string]any, excludeConn *Connection) {
	h.BroadcastToUser(userID, MsgTypeClipCreated, ClipCreatedData{
		ID:   item["id"].(int64),
		Item: item,
	}, excludeConn)
}

// BroadcastClipDeleted 广播条目删除事件
func (h *Hub) BroadcastClipDeleted(userID int64, itemID int64, excludeConn *Connection) {
	h.BroadcastToUser(userID, MsgTypeClipDeleted, ClipDeletedData{
		ID: itemID,
	}, excludeConn)
}

// GetOnlineCount 返回当前在线连接数
func (h *Hub) GetOnlineCount() int64 {
	return h.onlineCount.Load()
}

// GetUserConnectionCount 返回指定用户的连接数
func (h *Hub) GetUserConnectionCount(userID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections[userID])
}

// GetOnlineUserCount 返回当前在线用户数（去重）
func (h *Hub) GetOnlineUserCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}
