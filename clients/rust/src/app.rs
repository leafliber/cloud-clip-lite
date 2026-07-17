use std::sync::Arc;
use std::time::Instant;

use eframe::egui;
use tokio::sync::Mutex;

use crate::api::ApiClient;
use crate::config::{AppConfig, TokenStore};
use crate::models::*;
use crate::views;
use crate::ws::{WsClient, WsEvent};

/// 视图标签页
#[derive(Debug, Clone, PartialEq)]
pub enum Tab {
    Clipboard,
    History,
    Settings,
}

/// 应用阶段：先选择并连通服务器，再进入会话（登录/主界面）
#[derive(Debug, Clone, PartialEq)]
pub enum Stage {
    /// 服务器选择（启动第一步）
    Server,
    /// 会话阶段（恢复会话 / 登录 / 主界面）
    Session,
}

/// 异步任务结果（通过 channel 发送给 UI）
#[derive(Debug)]
pub enum TaskResult {
    /// 服务器连接检测成功
    ServerCheckOk { url: String, version: String },
    /// 服务器连接检测失败
    ServerCheckErr(String),
    /// 登录成功
    LoginOk(AuthResponse),
    /// 登录失败
    LoginErr(String),
    /// 获取用户信息成功
    GetMeOk(User),
    /// 获取用户信息失败
    GetMeErr(String),
    /// 获取剪切板列表成功
    ListClipsOk(ClipListResponse),
    /// 获取剪切板列表失败
    ListClipsErr(String),
    /// 发送文本成功
    CreateTextOk(ClipItem),
    /// 发送文本失败
    CreateTextErr(String),
    /// 上传文件成功
    CreateFileOk(ClipItem),
    /// 上传文件失败
    CreateFileErr(String),
    /// 删除条目成功
    DeleteClipOk(i64),
    /// 删除条目失败
    DeleteClipErr(String),
    /// 获取内容成功
    GetContentOk { id: i64, data: Vec<u8>, mime: String },
    /// 获取内容失败
    GetContentErr(String),
    /// 修改密码成功
    UpdatePasswordOk,
    /// 修改密码失败
    UpdatePasswordErr(String),
    /// 登出完成
    LogoutDone,
}

/// 主应用状态
pub struct App {
    /// 应用配置
    config: AppConfig,
    /// API 客户端
    api: ApiClient,
    /// Tokio runtime handle
    rt: tokio::runtime::Handle,
    /// 当前应用阶段
    stage: Stage,
    /// 当前登录用户
    user: Option<User>,
    /// 当前视图标签
    current_tab: Tab,
    /// 是否正在加载
    pub loading: bool,
    /// Toast 消息
    toast: Option<(ToastKind, String, Instant)>,
    /// WebSocket 客户端
    ws_client: Option<WsClient>,
    /// 异步任务结果接收器
    task_rx: std::sync::mpsc::Receiver<TaskResult>,
    /// 异步任务结果发送器
    task_tx: std::sync::mpsc::Sender<TaskResult>,
    /// WS 事件接收器
    ws_rx: Option<tokio::sync::mpsc::UnboundedReceiver<WsEvent>>,

    // 视图状态
    server_view: views::ServerView,
    login_view: views::LoginView,
    clipboard_view: views::ClipboardView,
    history_view: views::HistoryView,
    settings_view: views::SettingsView,
}

#[derive(Debug, Clone, PartialEq)]
pub enum ToastKind {
    Success,
    Error,
    Info,
}

impl App {
    pub fn new(cc: &eframe::CreationContext<'_>) -> Self {
        let config = AppConfig::load();
        let token_store = TokenStore::load().unwrap_or_default();
        let token_store = Arc::new(Mutex::new(token_store));

        let rt = tokio::runtime::Runtime::new().expect("Failed to create tokio runtime");
        let rt_handle = rt.handle().clone();

        let api = ApiClient::new(config.server_url.clone(), token_store);

        let (task_tx, task_rx) = std::sync::mpsc::channel::<TaskResult>();

        // 配置 egui 主题与字体
        configure_fonts(&cc.egui_ctx);

        let mut server_view = views::ServerView::default();
        server_view.url = config.server_url.clone();

        Self {
            config,
            api,
            rt: rt_handle,
            stage: Stage::Server,
            user: None,
            current_tab: Tab::Clipboard,
            loading: false,
            toast: None,
            ws_client: None,
            task_rx,
            task_tx,
            ws_rx: None,
            server_view,
            login_view: views::LoginView::default(),
            clipboard_view: views::ClipboardView::default(),
            history_view: views::HistoryView::default(),
            settings_view: views::SettingsView::default(),
        }
    }

    /// 显示 toast
    pub fn show_toast(&mut self, kind: ToastKind, msg: impl Into<String>) {
        self.toast = Some((kind, msg.into(), Instant::now()));
    }

    /// 发起异步任务
    pub fn spawn<F>(&self, future: F)
    where
        F: std::future::Future<Output = TaskResult> + Send + 'static,
    {
        self.rt.spawn(future);
    }

    /// 获取 API 客户端
    pub fn api(&self) -> &ApiClient {
        &self.api
    }

    /// 获取任务发送器
    pub fn task_tx(&self) -> &std::sync::mpsc::Sender<TaskResult> {
        &self.task_tx
    }

    /// 获取 Tokio runtime handle
    pub fn rt(&self) -> &tokio::runtime::Handle {
        &self.rt
    }

    /// 获取当前用户
    pub fn user(&self) -> Option<&User> {
        self.user.as_ref()
    }

    /// 获取配置
    pub fn config(&self) -> &AppConfig {
        &self.config
    }

    /// 保存配置
    pub fn save_config(&mut self) {
        let _ = self.config.save();
    }

    /// 设置服务器地址并重建 API 客户端
    pub fn set_server_url(&mut self, url: String) {
        self.config.server_url = url.clone();
        let _ = self.config.save();
        // 重建 API 客户端（保持 token store）
        let token_store = self.api.token_store_clone();
        self.api = ApiClient::new(url, token_store);
        // 同步服务器选择页的编辑缓冲
        self.server_view.url = self.config.server_url.clone();
    }

    /// 切换服务器：保存新地址、断开 WebSocket、清除本地会话并回到登录页
    pub fn switch_server(&mut self, url: String) {
        self.set_server_url(url);
        self.stop_ws();
        // 清除本地 token（旧服务器的凭证对新服务器无效）
        let token_store = self.api.token_store_clone();
        self.rt.block_on(async move {
            let mut t = token_store.lock().await;
            *t = TokenStore::default();
            let _ = t.save();
        });
        self.user = None;
        self.clipboard_view.reset();
        self.history_view.reset();
        // 回到服务器选择页，让用户确认新服务器后再登录
        self.stage = Stage::Server;
        self.server_view.checking = false;
        self.server_view.error = None;
        self.show_toast(ToastKind::Info, "服务器已切换，请重新连接并登录");
    }

    /// 返回服务器选择页（不断开配置，仅回到第一阶段）
    pub fn back_to_server_stage(&mut self) {
        self.stop_ws();
        self.user = None;
        self.stage = Stage::Server;
        self.server_view.url = self.config.server_url.clone();
        self.server_view.checking = false;
        self.server_view.error = None;
    }

    /// 启动 WebSocket 连接
    fn start_ws(&mut self) {
        let ws_url = self.config.ws_url();
        let token = match self.rt.block_on(self.api.get_access_token()) {
            Some(t) => t,
            None => return,
        };

        let (client, event_rx) = WsClient::start(ws_url, token);
        self.ws_client = Some(client);
        self.ws_rx = Some(event_rx);
    }

    /// 停止 WebSocket
    fn stop_ws(&mut self) {
        if let Some(ws) = self.ws_client.take() {
            ws.stop();
        }
        self.ws_rx = None;
    }

    /// 处理异步任务结果
    fn process_task_results(&mut self, ctx: &egui::Context) {
        while let Ok(result) = self.task_rx.try_recv() {
            match result {
                TaskResult::ServerCheckOk { url, version } => {
                    self.server_view.checking = false;
                    self.set_server_url(url);
                    self.stage = Stage::Session;
                    self.show_toast(ToastKind::Success, format!("已连接服务器 (版本 {})", version));
                    // 有本地 token 则尝试恢复会话，否则直接进入登录页
                    let has_token = self.rt.block_on(self.api.get_access_token()).is_some();
                    if has_token {
                        self.try_restore_session();
                    }
                }
                TaskResult::ServerCheckErr(e) => {
                    self.server_view.checking = false;
                    self.server_view.error = Some(e);
                }
                TaskResult::LoginOk(auth) => {
                    self.user = Some(auth.user);
                    self.loading = false;
                    self.show_toast(ToastKind::Success, "登录成功");
                    self.start_ws();
                    // 加载剪切板数据
                    self.load_clips(None, false);
                }
                TaskResult::LoginErr(e) => {
                    self.loading = false;
                    self.login_view.error = Some(e);
                }
                TaskResult::GetMeOk(user) => {
                    self.user = Some(user);
                    self.loading = false;
                    self.start_ws();
                    self.load_clips(None, false);
                }
                TaskResult::GetMeErr(_) => {
                    self.loading = false;
                    // token 无效，回到登录页
                    self.stop_ws();
                }
                TaskResult::ListClipsOk(resp) => {
                    let items_len = resp.items.len();
                    let limit = resp.limit;
                    let cursor = resp.cursor;
                    if self.history_view.loading_more {
                        self.history_view.items.extend(resp.items);
                        self.history_view.cursor = if cursor > 0 {
                            Some(cursor)
                        } else {
                            None
                        };
                        self.history_view.has_more = items_len as i32 >= limit;
                        self.history_view.loading_more = false;
                    } else {
                        self.clipboard_view.items = resp.items.clone();
                        self.history_view.items = resp.items;
                        self.history_view.cursor = if cursor > 0 {
                            Some(cursor)
                        } else {
                            None
                        };
                        self.history_view.has_more = items_len as i32 >= limit;
                        self.clipboard_view.loading = false;
                        self.history_view.loading = false;
                    }
                }
                TaskResult::ListClipsErr(e) => {
                    self.clipboard_view.loading = false;
                    self.history_view.loading = false;
                    self.history_view.loading_more = false;
                    self.show_toast(ToastKind::Error, e);
                }
                TaskResult::CreateTextOk(item) => {
                    self.clipboard_view.text.clear();
                    self.clipboard_view.sending = false;
                    self.clipboard_view.upsert_front(item.clone());
                    self.show_toast(ToastKind::Success, "已发送");
                }
                TaskResult::CreateTextErr(e) => {
                    self.clipboard_view.sending = false;
                    self.show_toast(ToastKind::Error, e);
                }
                TaskResult::CreateFileOk(item) => {
                    self.clipboard_view.uploading = false;
                    self.clipboard_view.upsert_front(item.clone());
                    self.show_toast(ToastKind::Success, "上传成功");
                }
                TaskResult::CreateFileErr(e) => {
                    self.clipboard_view.uploading = false;
                    self.show_toast(ToastKind::Error, e);
                }
                TaskResult::DeleteClipOk(id) => {
                    self.clipboard_view.remove_by_id(id);
                    self.history_view.remove_by_id(id);
                    self.show_toast(ToastKind::Success, "已删除");
                }
                TaskResult::DeleteClipErr(e) => {
                    self.show_toast(ToastKind::Error, e);
                }
                TaskResult::GetContentOk { id, data, mime } => {
                    // 保存到临时文件或复制到剪贴板
                    self.handle_content_ready(id, data, mime, ctx);
                }
                TaskResult::GetContentErr(e) => {
                    self.show_toast(ToastKind::Error, e);
                }
                TaskResult::UpdatePasswordOk => {
                    self.settings_view.updating = false;
                    self.settings_view.old_password.clear();
                    self.settings_view.new_password.clear();
                    self.settings_view.confirm_password.clear();
                    self.show_toast(ToastKind::Success, "密码修改成功");
                }
                TaskResult::UpdatePasswordErr(e) => {
                    self.settings_view.updating = false;
                    self.show_toast(ToastKind::Error, e);
                }
                TaskResult::LogoutDone => {
                    self.stop_ws();
                    self.user = None;
                    self.loading = false;
                    self.clipboard_view.reset();
                    self.history_view.reset();
                    self.show_toast(ToastKind::Info, "已登出");
                }
            }
        }
    }

    /// 处理 WebSocket 事件
    fn process_ws_events(&mut self) {
        let events: Vec<WsEvent> = {
            let Some(ws_rx) = self.ws_rx.as_mut() else {
                return;
            };
            let mut events = Vec::new();
            while let Ok(event) = ws_rx.try_recv() {
                events.push(event);
            }
            events
        };

        for event in events {
            match event {
                WsEvent::Connected => {
                    self.clipboard_view.ws_connected = true;
                    // 请求增量同步
                    let max_id = self
                        .clipboard_view
                        .items
                        .iter()
                        .map(|i| i.id)
                        .max()
                        .unwrap_or(0);
                    if let Some(ws) = &self.ws_client {
                        ws.request_sync(max_id);
                    }
                }
                WsEvent::Disconnected => {
                    self.clipboard_view.ws_connected = false;
                }
                WsEvent::ClipCreated(item) => {
                    self.clipboard_view.upsert_front(item.clone());
                    if self.history_view.filter_type == "all"
                        || self.history_view.filter_type == item.clip_type
                    {
                        self.history_view.upsert_front(item);
                    }
                }
                WsEvent::ClipDeleted(id) => {
                    self.clipboard_view.remove_by_id(id);
                    self.history_view.remove_by_id(id);
                }
                WsEvent::SyncResult(result) => {
                    for item in result.items {
                        self.clipboard_view.upsert_front(item.clone());
                        if self.history_view.filter_type == "all"
                            || self.history_view.filter_type == item.clip_type
                        {
                            self.history_view.upsert_front(item);
                        }
                    }
                }
                WsEvent::Error(e) => {
                    self.show_toast(ToastKind::Error, e);
                }
            }
        }
    }

    /// 加载剪切板列表
    fn load_clips(&self, before: Option<i64>, _is_more: bool) {
        let api = self.api.clone();
        let tx = self.task_tx.clone();

        self.rt.spawn(async move {
            match api.list_clips(20, before, None).await {
                Ok(resp) => {
                    let _ = tx.send(TaskResult::ListClipsOk(resp));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::ListClipsErr(e.to_string()));
                }
            }
        });
    }

    /// 加载更多（历史记录分页）
    fn load_more_clips(&mut self) {
        if !self.history_view.has_more || self.history_view.loading_more {
            return;
        }
        let cursor = self.history_view.cursor.unwrap_or(0);
        self.history_view.loading_more = true;
        let api = self.api.clone();
        let tx = self.task_tx.clone();

        self.rt.spawn(async move {
            match api.list_clips(20, Some(cursor), None).await {
                Ok(resp) => {
                    let _ = tx.send(TaskResult::ListClipsOk(resp));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::ListClipsErr(e.to_string()));
                }
            }
        });
    }

    /// 处理内容下载完成
    fn handle_content_ready(&mut self, _id: i64, data: Vec<u8>, mime: String, _ctx: &egui::Context) {
        // 复制到系统剪贴板
        if mime.starts_with("image/") {
            // 尝试作为图片复制
            match image::load_from_memory(&data) {
                Ok(img) => {
                    let rgba = img.to_rgba8();
                    let (w, h) = rgba.dimensions();
                    let image_data = arboard::ImageData {
                        width: w as usize,
                        height: h as usize,
                        bytes: rgba.into_raw().into(),
                    };
                    match arboard::Clipboard::new() {
                        Ok(mut cb) => {
                            if cb.set_image(image_data).is_ok() {
                                self.show_toast(ToastKind::Success, "图片已复制到剪贴板");
                                return;
                            }
                        }
                        Err(_) => {}
                    }
                }
                Err(_) => {}
            }
        }

        // 回退：保存到临时文件
        let ext = mime_to_extension(&mime);
        let temp_dir = std::env::temp_dir();
        let filename = format!("cloud-clip-{}.{}", _id, ext);
        let filepath = temp_dir.join(&filename);
        match std::fs::write(&filepath, &data) {
            Ok(()) => {
                // 尝试用系统默认程序打开
                self.show_toast(
                    ToastKind::Success,
                    format!("已保存到: {}", filepath.display()),
                );
            }
            Err(e) => {
                self.show_toast(ToastKind::Error, format!("保存失败: {}", e));
            }
        }
    }

    /// 初始化：尝试恢复会话
    fn try_restore_session(&mut self) {
        self.loading = true;
        let api = self.api.clone();
        let tx = self.task_tx.clone();

        self.rt.spawn(async move {
            match api.get_me().await {
                Ok(user) => {
                    let _ = tx.send(TaskResult::GetMeOk(user));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::GetMeErr(e.to_string()));
                }
            }
        });
    }
}

impl eframe::App for App {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        // 处理异步结果
        self.process_task_results(ctx);
        self.process_ws_events();

        // 请求重绘（确保 channel 消息及时处理）
        ctx.request_repaint_after(std::time::Duration::from_millis(100));

        // 显示 toast
        self.render_toast(ctx);

        // 第一阶段：选择并连通服务器
        if self.stage == Stage::Server {
            self.render_server(ctx);
            return;
        }

        // 第二阶段：会话（恢复中 → 登录 → 主界面）
        if self.loading && self.user.is_none() {
            self.render_loading(ctx);
            return;
        }

        if self.user.is_none() {
            self.render_login(ctx);
        } else {
            self.render_main(ctx);
        }
    }
}

impl App {
    fn render_server(&mut self, ctx: &egui::Context) {
        egui::CentralPanel::default().show(ctx, |ui| {
            ui.add_space(40.0);
            ui.vertical_centered(|ui| {
                ui.heading("Cloud Clip Lite");
                ui.add_space(5.0);
                ui.label(
                    egui::RichText::new("跨平台剪切板客户端")
                        .weak(),
                );
                ui.add_space(20.0);

                let mut view = std::mem::take(&mut self.server_view);
                view.render(ui, self);
                self.server_view = view;
            });
        });
    }
}

impl App {
    fn render_loading(&mut self, ctx: &egui::Context) {
        egui::CentralPanel::default().show(ctx, |ui| {
            ui.vertical_centered(|ui| {
                ui.add_space(200.0);
                ui.spinner();
                ui.add_space(10.0);
                ui.label("正在恢复会话…");
                ui.add_space(20.0);
                if ui.button("取消").clicked() {
                    self.loading = false;
                }
            });
        });
    }

    fn render_login(&mut self, ctx: &egui::Context) {
        egui::CentralPanel::default().show(ctx, |ui| {
            ui.add_space(40.0);
            ui.vertical_centered(|ui| {
                ui.heading("Cloud Clip Lite");
                ui.add_space(5.0);
                ui.label(
                    egui::RichText::new("跨平台剪切板客户端")
                        .weak(),
                );
                ui.add_space(20.0);

                // 当前服务器 + 更换入口
                ui.horizontal(|ui| {
                    ui.label(
                        egui::RichText::new(format!("服务器：{}", self.config.server_url))
                            .small()
                            .weak(),
                    );
                    if ui.small_button("更换").clicked() {
                        self.back_to_server_stage();
                    }
                });
                ui.add_space(15.0);

                let mut login_view = std::mem::take(&mut self.login_view);
                login_view.render(ui, self);
                self.login_view = login_view;
            });
        });
    }

    fn render_main(&mut self, ctx: &egui::Context) {
        // 顶部标签栏
        egui::TopBottomPanel::top("top_bar").show(ctx, |ui| {
            ui.horizontal(|ui| {
                ui.add_space(8.0);
                ui.heading(
                    egui::RichText::new("Cloud Clip")
                        .strong()
                        .size(18.0),
                );

                ui.add_space(30.0);

                // 标签按钮
                let tabs = [
                    (Tab::Clipboard, "剪切板"),
                    (Tab::History, "历史记录"),
                    (Tab::Settings, "设置"),
                ];

                for (tab, label) in tabs {
                    let selected = self.current_tab == tab;
                    let btn = egui::SelectableLabel::new(selected, label);
                    if ui.add(btn).clicked() {
                        self.current_tab = tab.clone();
                        if tab == Tab::History && self.history_view.items.is_empty() {
                            self.load_clips(None, false);
                            self.history_view.loading = true;
                        }
                    }
                }

                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    // WS 状态指示
                    let (dot_color, status_text) = if self.clipboard_view.ws_connected {
                        (egui::Color32::from_rgb(34, 197, 94), "已连接")
                    } else {
                        (egui::Color32::from_rgb(156, 163, 175), "未连接")
                    };
                    ui.label(
                        egui::RichText::new(status_text)
                            .small()
                            .color(dot_color),
                    );
                    ui.add_space(4.0);
                    let (rect, _) = ui.allocate_exact_size(
                        egui::vec2(8.0, 8.0),
                        egui::Sense::hover(),
                    );
                    ui.painter().circle_filled(
                        rect.center(),
                        4.0,
                        dot_color,
                    );

                    ui.add_space(15.0);

                    // 用户名
                    if let Some(user) = &self.user {
                        ui.label(
                            egui::RichText::new(&user.username)
                                .strong(),
                        );
                        if user.role == "admin" {
                            ui.label(
                                egui::RichText::new("管理员")
                                    .small()
                                    .color(egui::Color32::from_rgb(245, 158, 11)),
                            );
                        }
                    }

                    ui.add_space(10.0);

                    // 登出按钮
                    if ui.button("登出").clicked() {
                        let api = self.api.clone();
                        let tx = self.task_tx.clone();
                        self.rt.spawn(async move {
                            let _ = api.logout().await;
                            let _ = tx.send(TaskResult::LogoutDone);
                        });
                    }
                });
            });
        });

        // 主内容区
        egui::CentralPanel::default().show(ctx, |ui| {
            match self.current_tab {
                Tab::Clipboard => {
                    let mut view = std::mem::take(&mut self.clipboard_view);
                    view.render(ui, self);
                    self.clipboard_view = view;
                }
                Tab::History => {
                    let mut view = std::mem::take(&mut self.history_view);
                    view.render(ui, self);
                    self.history_view = view;
                }
                Tab::Settings => {
                    let mut view = std::mem::take(&mut self.settings_view);
                    view.render(ui, self);
                    self.settings_view = view;
                }
            }
        });
    }

    fn render_toast(&mut self, ctx: &egui::Context) {
        if let Some((kind, msg, time)) = &self.toast {
            let elapsed = time.elapsed();
            if elapsed > std::time::Duration::from_secs(3) {
                self.toast = None;
                return;
            }

            let color = match kind {
                ToastKind::Success => egui::Color32::from_rgb(34, 197, 94),
                ToastKind::Error => egui::Color32::from_rgb(239, 68, 68),
                ToastKind::Info => egui::Color32::from_rgb(59, 130, 246),
            };

            egui::Area::new(egui::Id::new("toast"))
                .fixed_pos(ctx.screen_rect().center_top() + egui::vec2(0.0, 20.0))
                .anchor(egui::Align2::CENTER_TOP, egui::vec2(0.0, 20.0))
                .show(ctx, |ui| {
                    egui::Frame::popup(ui.style())
                        .fill(egui::Color32::from_rgb(31, 31, 35))
                        .stroke(egui::Stroke::new(1.0_f32, color))
                        .inner_margin(egui::Margin::same(12.0))
                        .show(ui, |ui| {
                            ui.horizontal(|ui| {
                                ui.label(egui::RichText::new("●").color(color));
                                ui.label(
                                    egui::RichText::new(msg)
                                        .color(egui::Color32::from_rgb(229, 231, 235)),
                                );
                            });
                        });
                });
        }
    }
}

/// 配置字体
fn configure_fonts(ctx: &egui::Context) {
    let mut fonts = egui::FontDefinitions::default();

    // 尝试加载系统中文字体
    let font_paths: Vec<std::path::PathBuf> = if cfg!(target_os = "windows") {
        vec![
            std::path::PathBuf::from("C:/Windows/Fonts/msyh.ttc"),
            std::path::PathBuf::from("C:/Windows/Fonts/simhei.ttf"),
            std::path::PathBuf::from("C:/Windows/Fonts/simsun.ttc"),
        ]
    } else if cfg!(target_os = "macos") {
        vec![
            std::path::PathBuf::from("/System/Library/Fonts/PingFang.ttc"),
            std::path::PathBuf::from("/System/Library/Fonts/Hiragino Sans GB.ttc"),
            std::path::PathBuf::from("/Library/Fonts/Arial Unicode.ttf"),
        ]
    } else {
        vec![
            std::path::PathBuf::from("/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc"),
            std::path::PathBuf::from("/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"),
            std::path::PathBuf::from("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"),
        ]
    };

    for path in &font_paths {
        if path.exists() {
            if let Ok(font_data) = std::fs::read(path) {
                fonts.font_data.insert(
                    "cjk".to_string(),
                    egui::FontData::from_owned(font_data),
                );
                fonts
                    .families
                    .entry(egui::FontFamily::Proportional)
                    .or_default()
                    .insert(0, "cjk".to_string());
                fonts
                    .families
                    .entry(egui::FontFamily::Monospace)
                    .or_default()
                    .push("cjk".to_string());
                break;
            }
        }
    }

    ctx.set_fonts(fonts);

    // 设置深色主题
    let mut visuals = egui::Visuals::dark();
    visuals.window_shadow = egui::epaint::Shadow::NONE;
    ctx.set_visuals(visuals);
}

/// MIME 类型转扩展名
fn mime_to_extension(mime: &str) -> String {
    match mime {
        "image/png" => "png",
        "image/jpeg" => "jpg",
        "image/gif" => "gif",
        "image/webp" => "webp",
        "image/svg+xml" => "svg",
        "text/plain" => "txt",
        "application/pdf" => "pdf",
        "application/zip" => "zip",
        "application/x-tar" => "tar",
        "application/gzip" => "gz",
        "video/mp4" => "mp4",
        "audio/mpeg" => "mp3",
        _ => "bin",
    }
    .to_string()
}
