use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// 用户实体（对应后端 User）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct User {
    pub id: i64,
    pub username: String,
    #[serde(default)]
    pub email: Option<String>,
    pub role: String,
    pub status: String,
    pub max_item_size: i64,
    pub quota_bytes: i64,
    pub retention_days: i32,
    pub created_at: String,
    #[serde(default)]
    pub updated_at: Option<String>,
}

/// 剪切板条目类型
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum ClipType {
    Text,
    Image,
    File,
}

impl std::fmt::Display for ClipType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ClipType::Text => write!(f, "文本"),
            ClipType::Image => write!(f, "图片"),
            ClipType::File => write!(f, "文件"),
        }
    }
}

/// 剪切板条目（对应后端 ClipItem）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClipItem {
    pub id: i64,
    #[serde(rename = "type")]
    pub clip_type: String,
    pub size: i64,
    #[serde(default)]
    pub mime_type: Option<String>,
    #[serde(default)]
    pub text: Option<String>,
    #[serde(default)]
    pub has_blob: Option<bool>,
    #[serde(default)]
    pub sha256: Option<String>,
    #[serde(default)]
    pub meta: Option<HashMap<String, serde_json::Value>>,
    pub created_at: String,
    #[serde(default)]
    pub expires_at: Option<String>,
}

impl ClipItem {
    /// 获取文件名
    pub fn filename(&self) -> String {
        self.meta
            .as_ref()
            .and_then(|m| m.get("filename"))
            .and_then(|v| v.as_str())
            .map(|s| s.to_string())
            .unwrap_or_else(|| format!("clip-{}", self.id))
    }

    /// 是否为文本
    pub fn is_text(&self) -> bool {
        self.clip_type == "text"
    }

    /// 是否为图片
    pub fn is_image(&self) -> bool {
        self.clip_type == "image"
    }

    /// 是否为文件
    pub fn is_file(&self) -> bool {
        self.clip_type == "file"
    }

    /// 文本预览（截断）
    pub fn text_preview(&self, max_len: usize) -> String {
        self.text
            .as_ref()
            .map(|t| {
                if t.len() > max_len {
                    format!("{}…", &t[..max_len])
                } else {
                    t.clone()
                }
            })
            .unwrap_or_default()
    }
}

/// 认证响应
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthResponse {
    pub access_token: String,
    pub refresh_token: String,
    #[serde(default)]
    pub token_type: Option<String>,
    #[serde(default)]
    pub expires_in: Option<i32>,
    pub user: User,
}

/// 剪切板列表响应
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClipListResponse {
    pub items: Vec<ClipItem>,
    #[serde(default)]
    pub cursor: i64,
    #[serde(default)]
    pub limit: i32,
}

/// 设备信息
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Device {
    pub id: i64,
    pub name: String,
    #[serde(rename = "type")]
    pub device_type: String,
    #[serde(default)]
    pub has_token: Option<bool>,
    #[serde(default)]
    pub api_token: Option<String>,
    #[serde(default)]
    pub last_seen_at: Option<String>,
    pub created_at: String,
}

/// 设备列表响应
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DeviceListResponse {
    pub devices: Vec<Device>,
}

/// 创建设备请求
#[derive(Debug, Serialize)]
pub struct CreateDeviceRequest {
    pub name: String,
    #[serde(rename = "type")]
    pub device_type: String,
}

/// API 错误结构
#[derive(Debug, Clone, Deserialize)]
pub struct ApiErrorBody {
    pub error: ApiErrorDetail,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ApiErrorDetail {
    pub code: String,
    pub message: String,
    #[serde(default)]
    pub extra: Option<HashMap<String, serde_json::Value>>,
}

/// 文本上传请求
#[derive(Debug, Serialize)]
pub struct CreateTextRequest {
    #[serde(rename = "type")]
    pub clip_type: String,
    pub text: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expires_in: Option<i32>,
}

/// 修改密码请求
#[derive(Debug, Serialize)]
pub struct UpdatePasswordRequest {
    pub old_password: String,
    pub password: String,
}

/// WebSocket 服务端消息
#[derive(Debug, Clone, Deserialize)]
pub struct WsServerMessage {
    #[serde(rename = "type")]
    pub msg_type: String,
    #[serde(default)]
    pub data: Option<serde_json::Value>,
    #[serde(default)]
    pub ts: Option<String>,
    #[serde(default)]
    pub id: Option<String>,
}

/// WebSocket 客户端消息
#[derive(Debug, Serialize)]
pub struct WsClientMessage {
    #[serde(rename = "type")]
    pub msg_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<serde_json::Value>,
}

impl WsClientMessage {
    pub fn ping() -> Self {
        Self {
            msg_type: "ping".to_string(),
            data: None,
        }
    }

    pub fn sync(since: i64) -> Self {
        Self {
            msg_type: "sync".to_string(),
            data: Some(serde_json::json!({ "since": since })),
        }
    }
}

/// 增量同步结果
#[derive(Debug, Clone, Deserialize)]
pub struct SyncResult {
    pub since: i64,
    pub items: Vec<ClipItem>,
    pub count: i32,
}
