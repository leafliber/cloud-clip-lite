package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// 写超时
	writeTimeout = 10 * time.Second
	// 心跳 ping 间隔
	pingInterval = 30 * time.Second
	// 读超时（等待 pong 的最长时间）
	pongTimeout = 60 * time.Second
	// 发送缓冲区大小
	sendBufferSize = 64
	// 同步查询上限
	syncLimit = 100
	// 最大消息大小
	maxMessageSize = 4096
)

// SyncFunc 增量同步回调函数
// 由 Handler 注入，Connection 在收到 sync 消息时调用
type SyncFunc func(userID, sinceID int64) ([]map[string]any, error)

// Connection 封装单个 WebSocket 连接
type Connection struct {
	conn     *websocket.Conn
	hub      *Hub
	userID   int64
	username string
	deviceID int64
	logger   *slog.Logger
	syncFunc SyncFunc

	// send 从 Hub 接收消息，writePump 消费
	send chan []byte
}

// NewConnection 创建连接
func NewConnection(conn *websocket.Conn, hub *Hub, userID int64, username string, deviceID int64, logger *slog.Logger, syncFunc SyncFunc) *Connection {
	return &Connection{
		conn:     conn,
		hub:      hub,
		userID:   userID,
		username: username,
		deviceID: deviceID,
		logger:   logger,
		syncFunc: syncFunc,
		send:     make(chan []byte, sendBufferSize),
	}
}

// Start 启动读写循环（阻塞直到连接关闭）
func (c *Connection) Start() {
	// 注册到 Hub
	c.hub.Register(c)

	// 发送连接成功消息
	c.sendConnected()

	// 启动读写循环
	done := make(chan struct{})

	go c.writePump(done)
	c.readPump()

	// readPump 退出后，立即从 Hub 注销（关闭 send channel → 触发 writePump 退出）
	c.hub.Unregister(c)

	// 等待 writePump 完成清理
	<-done

	// 确保底层连接已关闭
	_ = c.conn.Close()
}

// sendConnected 发送连接成功消息
func (c *Connection) sendConnected() {
	msg, _ := newServerMessage(MsgTypeConnected, ConnectedData{
		UserID:   c.userID,
		Username: c.username,
	})
	select {
	case c.send <- msg:
	default:
	}
}

// readPump 读取客户端消息（单 goroutine）
func (c *Connection) readPump() {
	defer func() {
		// readPump 退出意味着连接断开
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Debug("WS 读取异常", "error", err, "user_id", c.userID)
			}
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.sendError("INVALID_MESSAGE", "消息格式错误")
			continue
		}

		c.handleClientMessage(&msg)
	}
}

// handleClientMessage 处理客户端消息
func (c *Connection) handleClientMessage(msg *ClientMessage) {
	switch msg.Type {
	case MsgTypePing:
		// 心跳 ping，回复 pong
		pong, _ := newServerMessage(MsgTypePong, nil)
		select {
		case c.send <- pong:
		default:
		}

	case MsgTypeAck:
		// 消息确认，目前仅记录日志
		c.logger.Debug("WS 收到 ack", "user_id", c.userID, "device_id", c.deviceID)

	case MsgTypeSync:
		c.handleSync(msg)

	default:
		c.sendError("UNKNOWN_TYPE", "未知的消息类型: "+msg.Type)
	}
}

// handleSync 处理增量同步请求
func (c *Connection) handleSync(msg *ClientMessage) {
	if c.syncFunc == nil {
		c.sendError("SYNC_UNAVAILABLE", "增量同步不可用")
		return
	}

	var req SyncRequestData
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError("INVALID_SYNC_DATA", "同步请求数据格式错误")
		return
	}

	items, err := c.syncFunc(c.userID, req.Since)
	if err != nil {
		c.logger.Error("WS 同步查询失败", "error", err, "user_id", c.userID)
		c.sendError("SYNC_FAILED", "同步查询失败")
		return
	}

	if items == nil {
		items = []map[string]any{}
	}

	result := SyncResultData{
		Since: req.Since,
		Items: items,
		Count: len(items),
	}

	rawMsg, err := newServerMessage(MsgTypeSyncResult, result)
	if err != nil {
		c.logger.Error("WS 创建同步结果消息失败", "error", err)
		return
	}

	select {
	case c.send <- rawMsg:
	default:
		c.logger.Warn("WS 同步结果发送失败，缓冲区满", "user_id", c.userID)
	}
}

// sendError 发送错误消息
func (c *Connection) sendError(code, message string) {
	msg, _ := newServerMessage(MsgTypeError, ErrorData{
		Code:    code,
		Message: message,
	})
	select {
	case c.send <- msg:
	default:
	}
}

// writePump 向客户端写入消息（单 goroutine）
func (c *Connection) writePump(done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// send channel 已关闭，Hub 注销了连接
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			// 发送 WebSocket ping 帧（协议层心跳）
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// contextKey 用于传递连接上下文（供 broadcast 排除自身）
type connContextKey struct{}

// ConnFromContext 从 context 中获取当前连接（用于排除自身广播）
func ConnFromContext(ctx context.Context) *Connection {
	if v, ok := ctx.Value(connContextKey{}).(*Connection); ok {
		return v
	}
	return nil
}
