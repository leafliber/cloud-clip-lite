use serde::{Deserialize, Serialize};
use std::path::PathBuf;

/// 应用配置
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppConfig {
    /// 服务器地址（如 http://localhost:8080）
    pub server_url: String,
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            server_url: "http://localhost:8080".to_string(),
        }
    }
}

impl AppConfig {
    /// 获取配置文件路径
    pub fn config_path() -> PathBuf {
        let dir = dirs::config_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join("cloud-clip-lite");
        std::fs::create_dir_all(&dir).ok();
        dir.join("config.json")
    }

    /// 从文件加载
    pub fn load() -> Self {
        let path = Self::config_path();
        match std::fs::read_to_string(&path) {
            Ok(content) => serde_json::from_str(&content).unwrap_or_default(),
            Err(_) => Self::default(),
        }
    }

    /// 保存到文件
    pub fn save(&self) -> Result<(), Box<dyn std::error::Error>> {
        let path = Self::config_path();
        let content = serde_json::to_string_pretty(self)?;
        std::fs::write(&path, content)?;
        Ok(())
    }

    /// 获取 WebSocket URL
    pub fn ws_url(&self) -> String {
        let url = &self.server_url;
        if url.starts_with("https://") {
            format!("wss://{}/api/ws", &url[8..])
        } else if url.starts_with("http://") {
            format!("ws://{}/api/ws", &url[7..])
        } else {
            format!("ws://{}/api/ws", url)
        }
    }
}

/// Token 存储（持久化到本地文件）
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct TokenStore {
    pub access_token: String,
    pub refresh_token: String,
}

impl TokenStore {
    /// 获取 token 文件路径
    fn token_path() -> PathBuf {
        let dir = dirs::config_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join("cloud-clip-lite");
        std::fs::create_dir_all(&dir).ok();
        dir.join("tokens.json")
    }

    /// 从文件加载
    pub fn load() -> Option<Self> {
        let path = Self::token_path();
        match std::fs::read_to_string(&path) {
            Ok(content) => {
                let store: Self = serde_json::from_str(&content).ok()?;
                if store.access_token.is_empty() {
                    None
                } else {
                    Some(store)
                }
            }
            Err(_) => None,
        }
    }

    /// 保存到文件
    pub fn save(&self) -> Result<(), Box<dyn std::error::Error>> {
        let path = Self::token_path();
        let content = serde_json::to_string_pretty(self)?;
        std::fs::write(&path, content)?;
        Ok(())
    }

    /// 清除文件
    pub fn clear() {
        let path = Self::token_path();
        let _ = std::fs::remove_file(&path);
    }
}
