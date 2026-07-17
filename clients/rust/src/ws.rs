use std::time::Duration;
use futures_util::{SinkExt, StreamExt};
use tokio::sync::mpsc;
use tokio_tungstenite::tungstenite::Message;

use crate::models::*;

/// WebSocket 事件（发送给 UI 层）
#[derive(Debug, Clone)]
pub enum WsEvent {
    /// 连接成功
    Connected,
    /// 断开连接
    Disconnected,
    /// 新条目创建
    ClipCreated(ClipItem),
    /// 条目删除
    ClipDeleted(i64),
    /// 增量同步结果
    SyncResult(SyncResult),
    /// 错误
    Error(String),
}

/// WebSocket 命令（UI → WS 任务）
#[derive(Debug)]
pub enum WsCommand {
    /// 请求增量同步
    Sync(i64),
    /// 停止
    Shutdown,
}

/// WebSocket 客户端
/// - 鉴权：通过 ?token= 参数传递 JWT
/// - 心跳：每 30s 发送 ping 消息
/// - 断线重连：指数退避（1s, 2s, 4s, ..., 上限 30s）
pub struct WsClient {
    cmd_tx: mpsc::UnboundedSender<WsCommand>,
}

const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(30);
const MAX_RECONNECT_DELAY: Duration = Duration::from_secs(30);
const RECONNECT_BASE: Duration = Duration::from_secs(1);

impl WsClient {
    /// 启动 WebSocket 客户端，返回事件接收器
    pub fn start(
        ws_url: String,
        token: String,
    ) -> (Self, mpsc::UnboundedReceiver<WsEvent>) {
        let (event_tx, event_rx) = mpsc::unbounded_channel();
        let (cmd_tx, cmd_rx) = mpsc::unbounded_channel::<WsCommand>();

        let event_tx_clone = event_tx.clone();

        tokio::spawn(async move {
            let mut reconnect_attempts: u32 = 0;
            let mut cmd_rx = cmd_rx;

            loop {
                // 检查命令
                if let Ok(WsCommand::Shutdown) = cmd_rx.try_recv() {
                    break;
                }

                // 构建 URL（token 通过查询参数传递）
                let url = format!("{}?token={}", ws_url, urlencoding::encode(&token));

                match connect_and_run(&url, &event_tx_clone, &mut cmd_rx).await {
                    Ok(()) => {
                        break;
                    }
                    Err(e) => {
                        let _ = event_tx_clone.send(WsEvent::Error(e));
                    }
                }

                let _ = event_tx_clone.send(WsEvent::Disconnected);

                if let Ok(WsCommand::Shutdown) = cmd_rx.try_recv() {
                    break;
                }

                // 指数退避
                let delay = std::cmp::min(
                    RECONNECT_BASE * 2u32.saturating_pow(reconnect_attempts),
                    MAX_RECONNECT_DELAY,
                );
                reconnect_attempts += 1;

                tokio::select! {
                    _ = tokio::time::sleep(delay) => {}
                    cmd = cmd_rx.recv() => {
                        if matches!(cmd, Some(WsCommand::Shutdown) | None) {
                            break;
                        }
                    }
                }
            }
        });

        (Self { cmd_tx }, event_rx)
    }

    /// 请求增量同步
    pub fn request_sync(&self, since_id: i64) {
        let _ = self.cmd_tx.send(WsCommand::Sync(since_id));
    }

    /// 停止客户端
    pub fn stop(&self) {
        let _ = self.cmd_tx.send(WsCommand::Shutdown);
    }
}

/// 连接并运行 WebSocket 消息循环
async fn connect_and_run(
    url: &str,
    event_tx: &mpsc::UnboundedSender<WsEvent>,
    cmd_rx: &mut mpsc::UnboundedReceiver<WsCommand>,
) -> Result<(), String> {
    use tokio_tungstenite::connect_async;

    let (ws_stream, _) = connect_async(url)
        .await
        .map_err(|e| format!("WS 连接失败: {}", e))?;

    let (mut write, mut read) = ws_stream.split();

    // 心跳 ticker
    let mut heartbeat = tokio::time::interval(HEARTBEAT_INTERVAL);
    heartbeat.tick().await; // 跳过第一次立即触发

    loop {
        tokio::select! {
            // 命令通道
            cmd = cmd_rx.recv() => {
                match cmd {
                    Some(WsCommand::Shutdown) | None => {
                        let _ = write.send(Message::Close(None)).await;
                        return Ok(());
                    }
                    Some(WsCommand::Sync(since_id)) => {
                        let msg = serde_json::to_string(&WsClientMessage::sync(since_id))
                            .unwrap_or_default();
                        if write.send(Message::Text(msg.into())).await.is_err() {
                            return Err("发送同步请求失败".into());
                        }
                    }
                }
            }

            // 心跳
            _ = heartbeat.tick() => {
                let ping = serde_json::to_string(&WsClientMessage::ping()).unwrap_or_default();
                if write.send(Message::Text(ping.into())).await.is_err() {
                    return Err("发送心跳失败".into());
                }
            }

            // 接收服务端消息
            msg = read.next() => {
                match msg {
                    Some(Ok(Message::Text(text))) => {
                        handle_server_message(&text, event_tx);
                    }
                    Some(Ok(Message::Ping(data))) => {
                        let _ = write.send(Message::Pong(data)).await;
                    }
                    Some(Ok(Message::Close(_))) => {
                        return Ok(());
                    }
                    Some(Err(e)) => {
                        return Err(format!("WS 读取错误: {}", e));
                    }
                    None => {
                        return Ok(());
                    }
                    _ => {}
                }
            }
        }
    }
}

/// 处理服务端消息
fn handle_server_message(raw: &str, event_tx: &mpsc::UnboundedSender<WsEvent>) {
    let msg: WsServerMessage = match serde_json::from_str(raw) {
        Ok(m) => m,
        Err(_) => return,
    };

    match msg.msg_type.as_str() {
        "connected" => {
            let _ = event_tx.send(WsEvent::Connected);
        }
        "clip.created" => {
            if let Some(data) = &msg.data {
                if let Some(item) = data.get("item") {
                    if let Ok(item) = serde_json::from_value::<ClipItem>(item.clone()) {
                        let _ = event_tx.send(WsEvent::ClipCreated(item));
                    }
                }
            }
        }
        "clip.deleted" => {
            if let Some(data) = &msg.data {
                if let Some(id) = data.get("id").and_then(|v| v.as_i64()) {
                    let _ = event_tx.send(WsEvent::ClipDeleted(id));
                }
            }
        }
        "pong" => {}
        "sync.result" => {
            if let Some(data) = &msg.data {
                if let Ok(result) = serde_json::from_value::<SyncResult>(data.clone()) {
                    let _ = event_tx.send(WsEvent::SyncResult(result));
                }
            }
        }
        "error" => {
            if let Some(data) = &msg.data {
                let message = data
                    .get("message")
                    .and_then(|v| v.as_str())
                    .unwrap_or("未知错误")
                    .to_string();
                let _ = event_tx.send(WsEvent::Error(message));
            }
        }
        _ => {}
    }
}
