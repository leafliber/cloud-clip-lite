use eframe::egui;

use crate::app::{App, TaskResult};
use crate::models::ClipItem;

const MAX_DISPLAY: usize = 10;

/// 剪切板主视图
/// - 顶部文本输入 + 发送（Ctrl/⌘ + Enter 快捷发送）
/// - 文件选择上传
/// - 最新条目列表，WS 实时更新
#[derive(Default)]
pub struct ClipboardView {
    pub text: String,
    pub items: Vec<ClipItem>,
    pub sending: bool,
    pub uploading: bool,
    pub loading: bool,
    pub ws_connected: bool,
}

impl ClipboardView {
    /// 将新条目插入顶部（去重）并截断
    pub fn upsert_front(&mut self, item: ClipItem) {
        if self.items.iter().any(|it| it.id == item.id) {
            return;
        }
        self.items.insert(0, item);
        if self.items.len() > MAX_DISPLAY {
            self.items.truncate(MAX_DISPLAY);
        }
    }

    /// 删除指定 id
    pub fn remove_by_id(&mut self, id: i64) {
        self.items.retain(|it| it.id != id);
    }

    /// 重置状态
    pub fn reset(&mut self) {
        self.text.clear();
        self.items.clear();
        self.sending = false;
        self.uploading = false;
        self.loading = false;
        self.ws_connected = false;
    }

    pub fn render(&mut self, ui: &mut egui::Ui, app: &mut App) {
        ui.spacing();

        // 文本输入区
        egui::Frame::group(ui.style())
            .inner_margin(egui::Margin::same(12.0))
            .show(ui, |ui| {
                ui.label(
                    egui::RichText::new("发送文本")
                        .strong()
                        .size(14.0),
                );
                ui.add_space(6.0);

                let response = ui.add(
                    egui::TextEdit::multiline(&mut self.text)
                        .desired_width(f32::MAX)
                        .desired_rows(3)
                        .hint_text("粘贴或输入文本，发送到剪切板…"),
                );

                ui.add_space(6.0);

                ui.horizontal(|ui| {
                    ui.label(
                        egui::RichText::new("Ctrl/⌘ + Enter 快速发送")
                            .small()
                            .weak(),
                    );

                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        let can_send = !self.text.trim().is_empty() && !self.sending;
                        let btn_text = if self.sending { "发送中…" } else { "发送" };
                        if ui.add_enabled(can_send, egui::Button::new(btn_text)).clicked() {
                            self.do_send_text(app);
                        }
                    });
                });

                // Ctrl+Enter 快捷发送
                if !self.text.trim().is_empty()
                    && !self.sending
                    && response.has_focus()
                    && ui.input(|i| {
                        i.key_pressed(egui::Key::Enter)
                            && (i.modifiers.ctrl || i.modifiers.command)
                    })
                {
                    self.do_send_text(app);
                }
            });

        ui.add_space(12.0);

        // 文件上传区
        egui::Frame::group(ui.style())
            .inner_margin(egui::Margin::same(12.0))
            .show(ui, |ui| {
                ui.horizontal(|ui| {
                    if self.uploading {
                        ui.spinner();
                        ui.label("上传中…");
                    } else {
                        ui.label("上传文件");
                        ui.with_layout(
                            egui::Layout::right_to_left(egui::Align::Center),
                            |ui| {
                                if ui.button("选择文件").clicked() {
                                    self.pick_file(app);
                                }
                            },
                        );
                    }
                });
            });

        ui.add_space(16.0);

        // 最新条目标题
        ui.horizontal(|ui| {
            ui.label(
                egui::RichText::new("最新条目")
                    .strong()
                    .size(15.0),
            );
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                ui.label(
                    egui::RichText::new(format!("最多显示 {} 条", MAX_DISPLAY))
                        .small()
                        .weak(),
                );
            });
        });

        ui.add_space(8.0);

        // 条目列表
        if self.loading {
            ui.vertical_centered(|ui| {
                ui.add_space(50.0);
                ui.spinner();
            });
            return;
        }

        if self.items.is_empty() {
            ui.vertical_centered(|ui| {
                ui.add_space(50.0);
                ui.label(
                    egui::RichText::new("还没有剪切板内容")
                        .weak()
                        .size(14.0),
                );
                ui.add_space(4.0);
                ui.label(
                    egui::RichText::new("发送文本或上传文件后会显示在这里")
                        .small()
                        .weak(),
                );
            });
            return;
        }

        // 使用可滚动区域显示条目
        egui::ScrollArea::vertical().show(ui, |ui| {
            let mut to_delete: Vec<i64> = Vec::new();
            let mut to_copy: Vec<(i64, String)> = Vec::new();
            let mut to_copy_blob: Vec<i64> = Vec::new();

            for item in &self.items {
                let id = item.id;
                let item_clone = item.clone();

                egui::Frame::group(ui.style())
                    .inner_margin(egui::Margin::same(10.0))
                    .show(ui, |ui| {
                        // 内容区
                        if item.is_text() {
                            let preview = item.text_preview(200);
                            ui.horizontal_wrapped(|ui| {
                                ui.label(
                                    egui::RichText::new(&preview)
                                        .color(egui::Color32::from_rgb(229, 231, 235)),
                                );
                            });
                            if let Some(text) = &item.text {
                                if text.len() > 200 {
                                    ui.add_space(2.0);
                                    ui.label(
                                        egui::RichText::new(format!("共 {} 字符", text.len()))
                                            .small()
                                            .weak(),
                                    );
                                }
                            }
                        } else if item.is_image() {
                            ui.horizontal(|ui| {
                                ui.label(
                                    egui::RichText::new("图片")
                                        .color(egui::Color32::from_rgb(34, 197, 94)),
                                );
                                ui.label(format_size(item.size));
                            });
                        } else {
                            // 文件
                            ui.horizontal(|ui| {
                                ui.label("📄");
                                ui.label(
                                    egui::RichText::new(item.filename())
                                        .strong(),
                                );
                                ui.label(
                                    egui::RichText::new(format_size(item.size))
                                        .small()
                                        .weak(),
                                );
                            });
                        }

                        ui.add_space(6.0);

                        // 底部操作栏
                        ui.horizontal(|ui| {
                            // 类型标签
                            let type_color = if item.is_text() {
                                egui::Color32::from_rgb(59, 130, 246)
                            } else if item.is_image() {
                                egui::Color32::from_rgb(34, 197, 94)
                            } else {
                                egui::Color32::from_rgb(245, 158, 11)
                            };
                            ui.label(
                                egui::RichText::new(match item.clip_type.as_str() {
                                    "text" => "文本",
                                    "image" => "图片",
                                    _ => "文件",
                                })
                                .color(type_color)
                                .small(),
                            );

                            ui.label(
                                egui::RichText::new(time_ago(&item.created_at))
                                    .small()
                                    .weak(),
                            );

                            ui.with_layout(
                                egui::Layout::right_to_left(egui::Align::Center),
                                |ui| {
                                    // 删除按钮
                                    if ui.button("删除").clicked() {
                                        to_delete.push(id);
                                    }

                                    // 下载/复制按钮
                                    if item.is_text() {
                                        if let Some(text) = &item_clone.text {
                                            if ui.button("复制").clicked() {
                                                to_copy.push((id, text.clone()));
                                            }
                                        }
                                    } else {
                                        if ui.button("复制内容").clicked() {
                                            to_copy_blob.push(id);
                                        }
                                    }
                                },
                            );
                        });
                    });

                ui.add_space(4.0);
            }

            // 在循环外执行操作
            for (_id, text) in to_copy {
                copy_to_clipboard(&text);
                app.show_toast(crate::app::ToastKind::Success, "已复制到剪贴板");
            }

            for id in to_copy_blob {
                self.fetch_and_copy_content(app, id);
            }

            for id in to_delete {
                self.delete_item(app, id);
            }
        });
    }

    fn do_send_text(&mut self, app: &mut App) {
        let content = self.text.trim().to_string();
        if content.is_empty() {
            return;
        }
        self.sending = true;

        let api = app.api().clone();
        let tx = app.task_tx().clone();

        app.rt().spawn(async move {
            match api.create_text(&content).await {
                Ok(item) => {
                    let _ = tx.send(TaskResult::CreateTextOk(item));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::CreateTextErr(e.to_string()));
                }
            }
        });
    }

    fn pick_file(&mut self, app: &mut App) {
        if let Some(path) = rfd::FileDialog::new().pick_file() {
            self.uploading = true;

            let api = app.api().clone();
            let tx = app.task_tx().clone();
            let filename = path
                .file_name()
                .map(|n| n.to_string_lossy().to_string())
                .unwrap_or_else(|| "file".to_string());

            app.rt().spawn(async move {
                match tokio::fs::read(&path).await {
                    Ok(data) => {
                        let mime = guess_mime(&filename);
                        match api.create_file(data, &filename, &mime).await {
                            Ok(item) => {
                                let _ = tx.send(TaskResult::CreateFileOk(item));
                            }
                            Err(e) => {
                                let _ = tx.send(TaskResult::CreateFileErr(e.to_string()));
                            }
                        }
                    }
                    Err(e) => {
                        let _ = tx.send(TaskResult::CreateFileErr(format!("读取文件失败: {}", e)));
                    }
                }
            });
        }
    }

    fn fetch_and_copy_content(&self, app: &mut App, id: i64) {
        let api = app.api().clone();
        let tx = app.task_tx().clone();

        app.rt().spawn(async move {
            match api.get_content(id).await {
                Ok(data) => {
                    let _ = tx.send(TaskResult::GetContentOk {
                        id,
                        data,
                        mime: "application/octet-stream".to_string(),
                    });
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::GetContentErr(e.to_string()));
                }
            }
        });
    }

    fn delete_item(&self, app: &mut App, id: i64) {
        let api = app.api().clone();
        let tx = app.task_tx().clone();

        app.rt().spawn(async move {
            match api.delete_clip(id).await {
                Ok(()) => {
                    let _ = tx.send(TaskResult::DeleteClipOk(id));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::DeleteClipErr(e.to_string()));
                }
            }
        });
    }
}

/// 格式化文件大小
fn format_size(bytes: i64) -> String {
    if bytes < 1024 {
        format!("{} B", bytes)
    } else if bytes < 1024 * 1024 {
        format!("{:.1} KB", bytes as f64 / 1024.0)
    } else if bytes < 1024 * 1024 * 1024 {
        format!("{:.1} MB", bytes as f64 / (1024.0 * 1024.0))
    } else {
        format!("{:.1} GB", bytes as f64 / (1024.0 * 1024.0 * 1024.0))
    }
}

/// 简易相对时间
fn time_ago(created_at: &str) -> String {
    // 尝试解析 "2006-01-02 15:04:05" 格式
    let formats = [
        "%Y-%m-%d %H:%M:%S",
        "%Y-%m-%dT%H:%M:%S",
        "%Y-%m-%dT%H:%M:%SZ",
    ];

    let now = chrono::Utc::now();
    let parsed = formats
        .iter()
        .find_map(|fmt| chrono::NaiveDateTime::parse_from_str(created_at, fmt).ok())
        .map(|dt| dt.and_utc());

    if let Some(dt) = parsed {
        let diff = now.signed_duration_since(dt);
        let secs = diff.num_seconds();
        if secs < 60 {
            "刚刚".to_string()
        } else if secs < 3600 {
            format!("{} 分钟前", secs / 60)
        } else if secs < 86400 {
            format!("{} 小时前", secs / 3600)
        } else if secs < 86400 * 30 {
            format!("{} 天前", secs / 86400)
        } else {
            format!("{} 月前", secs / (86400 * 30))
        }
    } else {
        created_at.to_string()
    }
}

/// 复制文本到系统剪贴板
fn copy_to_clipboard(text: &str) {
    if let Ok(mut cb) = arboard::Clipboard::new() {
        let _ = cb.set_text(text.to_string());
    }
}

/// 根据文件名猜测 MIME 类型
fn guess_mime(filename: &str) -> String {
    let ext = filename.rsplit('.').next().map(|e| e.to_lowercase());
    match ext.as_deref() {
        Some("png") => "image/png",
        Some("jpg") | Some("jpeg") => "image/jpeg",
        Some("gif") => "image/gif",
        Some("webp") => "image/webp",
        Some("svg") => "image/svg+xml",
        Some("pdf") => "application/pdf",
        Some("txt") => "text/plain",
        Some("json") => "application/json",
        Some("zip") => "application/zip",
        Some("mp4") => "video/mp4",
        Some("mp3") => "audio/mpeg",
        _ => "application/octet-stream",
    }
    .to_string()
}
