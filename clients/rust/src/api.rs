use reqwest::{Client, StatusCode};
use std::sync::Arc;
use tokio::sync::Mutex;

use crate::config::TokenStore;
use crate::models::*;

/// API 客户端错误
#[derive(Debug)]
pub enum ApiError {
    Network(String),
    Auth(String),
    Server { code: String, message: String },
    Other(String),
}

impl std::fmt::Display for ApiError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ApiError::Network(msg) => write!(f, "网络错误: {}", msg),
            ApiError::Auth(msg) => write!(f, "认证失败: {}", msg),
            ApiError::Server { message, .. } => write!(f, "{}", message),
            ApiError::Other(msg) => write!(f, "{}", msg),
        }
    }
}

impl std::error::Error for ApiError {}

/// HTTP API 客户端
/// 封装所有 REST 端点，支持 JWT 自动注入与 401 自动刷新
#[derive(Clone)]
pub struct ApiClient {
    client: Client,
    server_url: String,
    token_store: Arc<Mutex<TokenStore>>,
}

impl ApiClient {
    pub fn new(server_url: String, token_store: Arc<Mutex<TokenStore>>) -> Self {
        let client = Client::builder()
            .connect_timeout(std::time::Duration::from_secs(5))
            .timeout(std::time::Duration::from_secs(30))
            .build()
            .expect("Failed to build HTTP client");

        Self {
            client,
            server_url,
            token_store,
        }
    }

    /// 健康检查：检测服务器是否可达（5 秒超时）
    /// 成功返回服务器版本号
    pub async fn health_check(server_url: &str) -> Result<String, ApiError> {
        let client = Client::builder()
            .connect_timeout(std::time::Duration::from_secs(5))
            .timeout(std::time::Duration::from_secs(5))
            .build()
            .map_err(|e| ApiError::Network(e.to_string()))?;

        let url = format!("{}/healthz", server_url.trim_end_matches('/'));
        let resp = client
            .get(&url)
            .send()
            .await
            .map_err(|e| ApiError::Network(format!("无法连接服务器: {}", e)))?;

        if !resp.status().is_success() {
            return Err(ApiError::Other(format!(
                "服务器响应异常 ({})",
                resp.status()
            )));
        }

        let data: serde_json::Value = resp
            .json()
            .await
            .map_err(|e| ApiError::Other(format!("响应格式错误: {}", e)))?;

        Ok(data["version"].as_str().unwrap_or("unknown").to_string())
    }

    /// 规范化服务器地址：去空格、去尾部斜杠、补全 http:// 前缀
    pub fn normalize_server_url(input: &str) -> String {
        let mut url = input.trim().trim_end_matches('/').to_string();
        if !url.starts_with("http://") && !url.starts_with("https://") {
            url = format!("http://{}", url);
        }
        url
    }

    /// 获取当前 access token
    pub async fn get_access_token(&self) -> Option<String> {
        let store = self.token_store.lock().await;
        if store.access_token.is_empty() {
            None
        } else {
            Some(store.access_token.clone())
        }
    }

    /// 获取当前 refresh token
    pub async fn get_refresh_token(&self) -> Option<String> {
        let store = self.token_store.lock().await;
        if store.refresh_token.is_empty() {
            None
        } else {
            Some(store.refresh_token.clone())
        }
    }

    /// 更新 token 存储
    pub async fn set_tokens(&self, access: String, refresh: String) {
        let mut store = self.token_store.lock().await;
        store.access_token = access;
        store.refresh_token = refresh;
        let _ = store.save();
    }

    /// 克隆 token store 的 Arc 引用
    pub fn token_store_clone(&self) -> Arc<Mutex<TokenStore>> {
        self.token_store.clone()
    }

    /// 清除 token
    pub async fn clear_tokens(&self) {
        let mut store = self.token_store.lock().await;
        store.access_token.clear();
        store.refresh_token.clear();
        TokenStore::clear();
    }

    /// 尝试刷新令牌
    pub async fn refresh_token(&self) -> Result<(), ApiError> {
        let refresh_token = self
            .get_refresh_token()
            .await
            .ok_or_else(|| ApiError::Auth("无刷新令牌".into()))?;

        let url = format!("{}/api/auth/refresh", self.server_url);
        let resp = self
            .client
            .post(&url)
            .json(&serde_json::json!({ "refresh_token": refresh_token }))
            .send()
            .await
            .map_err(|e| ApiError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            self.clear_tokens().await;
            return Err(ApiError::Auth("刷新令牌无效或已过期".into()));
        }

        let data: serde_json::Value = resp
            .json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))?;

        let access = data["access_token"]
            .as_str()
            .ok_or_else(|| ApiError::Other("响应缺少 access_token".into()))?;
        let refresh = data["refresh_token"]
            .as_str()
            .ok_or_else(|| ApiError::Other("响应缺少 refresh_token".into()))?;

        self.set_tokens(access.to_string(), refresh.to_string())
            .await;
        Ok(())
    }

    /// 构建带鉴权的请求
    async fn authed_request(
        &self,
        method: reqwest::Method,
        path: &str,
    ) -> reqwest::RequestBuilder {
        let token = self.get_access_token().await.unwrap_or_default();
        let url = format!("{}{}", self.server_url, path);
        let mut req = self.client.request(method, &url);
        if !token.is_empty() {
            req = req.bearer_auth(&token);
        }
        req
    }

    /// 发送请求，401 时自动刷新重试
    async fn send_with_retry(
        &self,
        req: reqwest::RequestBuilder,
        is_auth_endpoint: bool,
    ) -> Result<reqwest::Response, ApiError> {
        let resp = req
            .try_clone()
            .ok_or_else(|| ApiError::Other("请求无法重试".into()))?
            .send()
            .await
            .map_err(|e| ApiError::Network(e.to_string()))?;

        if resp.status() == StatusCode::UNAUTHORIZED && !is_auth_endpoint {
            // 尝试刷新
            match self.refresh_token().await {
                Ok(()) => {
                    // 重新构建请求
                    let token = self.get_access_token().await.unwrap_or_default();
                    let new_req = req.bearer_auth(&token);
                    new_req
                        .send()
                        .await
                        .map_err(|e| ApiError::Network(e.to_string()))
                }
                Err(e) => Err(e),
            }
        } else {
            Ok(resp)
        }
    }

    /// 解析错误响应
    async fn parse_error(&self, resp: reqwest::Response) -> ApiError {
        let status = resp.status();
        match resp.json::<ApiErrorBody>().await {
            Ok(body) => ApiError::Server {
                code: body.error.code,
                message: body.error.message,
            },
            Err(_) => ApiError::Other(format!("请求失败 ({})", status)),
        }
    }

    // ========== 认证接口 ==========

    /// 登录
    pub async fn login(
        &self,
        username: &str,
        password: &str,
    ) -> Result<AuthResponse, ApiError> {
        let url = format!("{}/api/auth/login", self.server_url);
        let resp = self
            .client
            .post(&url)
            .json(&serde_json::json!({ "username": username, "password": password }))
            .send()
            .await
            .map_err(|e| ApiError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        let auth: AuthResponse = resp
            .json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))?;

        self.set_tokens(auth.access_token.clone(), auth.refresh_token.clone())
            .await;
        Ok(auth)
    }

    /// 登出
    pub async fn logout(&self) -> Result<(), ApiError> {
        if let Some(refresh) = self.get_refresh_token().await {
            let url = format!("{}/api/auth/logout", self.server_url);
            let _ = self
                .client
                .post(&url)
                .json(&serde_json::json!({ "refresh_token": refresh }))
                .send()
                .await;
        }
        self.clear_tokens().await;
        Ok(())
    }

    // ========== 用户信息 ==========

    /// 获取当前用户信息
    pub async fn get_me(&self) -> Result<User, ApiError> {
        let req = self.authed_request(reqwest::Method::GET, "/api/me").await;
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 修改密码
    pub async fn update_password(
        &self,
        old_password: &str,
        new_password: &str,
    ) -> Result<(), ApiError> {
        let req = self
            .authed_request(reqwest::Method::PATCH, "/api/me")
            .await
            .json(&UpdatePasswordRequest {
                old_password: old_password.to_string(),
                password: new_password.to_string(),
            });
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }
        Ok(())
    }

    // ========== 剪切板接口 ==========

    /// 发送文本
    pub async fn create_text(&self, text: &str) -> Result<ClipItem, ApiError> {
        let req = self
            .authed_request(reqwest::Method::POST, "/api/clip")
            .await
            .json(&CreateTextRequest {
                clip_type: "text".to_string(),
                text: text.to_string(),
                expires_in: None,
            });
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 上传文件
    pub async fn create_file(
        &self,
        file_data: Vec<u8>,
        filename: &str,
        mime_type: &str,
    ) -> Result<ClipItem, ApiError> {
        let part = reqwest::multipart::Part::bytes(file_data)
            .file_name(filename.to_string())
            .mime_str(mime_type)
            .map_err(|e| ApiError::Other(e.to_string()))?;

        let form = reqwest::multipart::Form::new().part("file", part);

        let req = self
            .authed_request(reqwest::Method::POST, "/api/clip")
            .await
            .multipart(form);
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 获取剪切板列表
    pub async fn list_clips(
        &self,
        limit: i32,
        before: Option<i64>,
        clip_type: Option<&str>,
    ) -> Result<ClipListResponse, ApiError> {
        let mut params = vec![("limit", limit.to_string())];
        if let Some(b) = before {
            params.push(("before", b.to_string()));
        }
        if let Some(t) = clip_type {
            params.push(("type", t.to_string()));
        }

        let req = self
            .authed_request(reqwest::Method::GET, "/api/clip")
            .await
            .query(&params);
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 获取最新一条
    pub async fn get_latest(&self) -> Result<ClipItem, ApiError> {
        let req = self
            .authed_request(reqwest::Method::GET, "/api/clip/latest")
            .await;
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 获取条目内容（二进制）
    pub async fn get_content(&self, id: i64) -> Result<Vec<u8>, ApiError> {
        let path = format!("/api/clip/{}/content", id);
        let req = self.authed_request(reqwest::Method::GET, &path).await;
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.bytes()
            .await
            .map(|b| b.to_vec())
            .map_err(|e| ApiError::Network(e.to_string()))
    }

    /// 获取条目内容（文本）
    pub async fn get_text_content(&self, id: i64) -> Result<String, ApiError> {
        let bytes = self.get_content(id).await?;
        String::from_utf8(bytes).map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 删除条目
    pub async fn delete_clip(&self, id: i64) -> Result<(), ApiError> {
        let path = format!("/api/clip/{}", id);
        let req = self.authed_request(reqwest::Method::DELETE, &path).await;
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }
        Ok(())
    }

    // ========== 设备接口 ==========

    /// 列出设备
    pub async fn list_devices(&self) -> Result<DeviceListResponse, ApiError> {
        let req = self
            .authed_request(reqwest::Method::GET, "/api/devices")
            .await;
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 创建设备
    pub async fn create_device(
        &self,
        name: &str,
        device_type: &str,
    ) -> Result<Device, ApiError> {
        let req = self
            .authed_request(reqwest::Method::POST, "/api/devices")
            .await
            .json(&CreateDeviceRequest {
                name: name.to_string(),
                device_type: device_type.to_string(),
            });
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }

        resp.json()
            .await
            .map_err(|e| ApiError::Other(e.to_string()))
    }

    /// 删除设备
    pub async fn delete_device(&self, id: i64) -> Result<(), ApiError> {
        let path = format!("/api/devices/{}", id);
        let req = self.authed_request(reqwest::Method::DELETE, &path).await;
        let resp = self.send_with_retry(req, false).await?;

        if !resp.status().is_success() {
            return Err(self.parse_error(resp).await);
        }
        Ok(())
    }
}
