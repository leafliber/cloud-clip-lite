package ws

import (
	"encoding/json"
	"time"
)

// 消息类型常量
const (
	MsgTypeConnected   = "connected"    // 服务端 → 客户端：连接成功
	MsgTypeClipCreated = "clip.created" // 服务端 → 客户端：新条目
	MsgTypeClipDeleted = "clip.deleted" // 服务端 → 客户端：条目删除
	MsgTypePong        = "pong"         // 服务端 → 客户端：心跳响应
	MsgTypeSyncResult  = "sync.result"  // 服务端 → 客户端：增量同步结果
	MsgTypeError       = "error"        // 服务端 → 客户端：错误

	MsgTypePing = "ping" // 客户端 → 服务端：心跳
	MsgTypeAck  = "ack"  // 客户端 → 服务端：消息确认
	MsgTypeSync = "sync" // 客户端 → 服务端：请求增量同步
)

// ServerMessage 服务端发给客户端的消息
type ServerMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
	TS   string          `json:"ts"`
	ID   string          `json:"id,omitempty"` // 消息 ID，供客户端 ack
}

// ClientMessage 客户端发给服务端的消息
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ConnectedData 连接成功消息数据
type ConnectedData struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

// ClipCreatedData 新条目广播数据
type ClipCreatedData struct {
	ID   int64          `json:"id"`
	Item map[string]any `json:"item"`
}

// ClipDeletedData 删除广播数据
type ClipDeletedData struct {
	ID int64 `json:"id"`
}

// SyncRequestData 增量同步请求数据
type SyncRequestData struct {
	Since int64 `json:"since"` // 上次同步的最大 ID
}

// SyncResultData 增量同步结果
type SyncResultData struct {
	Since int64           `json:"since"`
	Items []map[string]any `json:"items"`
	Count int             `json:"count"`
}

// ErrorData 错误消息数据
type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// newServerMessage 创建服务端消息（自动填充时间戳和消息 ID）
func newServerMessage(msgType string, data any) ([]byte, error) {
	msg := ServerMessage{
		Type: msgType,
		TS:   time.Now().Format(time.RFC3339),
		ID:   generateMsgID(),
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		msg.Data = raw
	}
	return json.Marshal(msg)
}

// generateMsgID 生成简单的消息 ID
func generateMsgID() string {
	return time.Now().Format("20060102150405.000000000")
}
