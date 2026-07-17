use eframe::egui;

use crate::app::{App, TaskResult, ToastKind};
use crate::models::ClipItem;

const PAGE_SIZE: i32 = 20;

/// 历史记录视图
/// - 类型过滤标签
/// - 搜索框（客户端过滤已加载条目）
/// - 游标分页（加载更多）
/// - WS 实时同步
#[derive(Default)]
pub struct HistoryView {
    pub items: Vec<ClipItem>,
    pub cursor: Option<i64>,
    pub has_more: bool,
    pub filter_type: String,
    pub search_query: String,
    pub loading: bool,
    pub loading_more: bool,
}

impl HistoryView {
    /// 将新条目插入顶部（去重）
    pub fn upsert_front(&mut self, item: ClipItem) {
        if self.items.iter().any(|it| it.id == item.id) {
            return;
        }
        self.items.insert(0, item);
    }

    /// 删除指定 id
    pub fn remove_by_id(&mut self, id: i64) {
        self.items.retain(|it| it.id != id);
    }

    /// 重置状态
    pub fn reset(&mut self) {
        self.items.clear();
        self.cursor = None;
        self.has_more = false;
        self.filter_type.clear();
        self.search_query.clear();
        self.loading = false;
        self.loading_more = false;
    }

    pub fn render(&mut self, ui: &mut egui::Ui, app: &mut App) {
        // 标题 + 搜索
        ui.horizontal(|ui| {
            ui.label(
                egui::RichText::new("历史记录")
                    .strong()
                    .size(16.0),
            );
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                ui.add(
                    egui::TextEdit::singleline(&mut self.search_query)
                        .desired_width(200.0)
                        .hint_text("搜索文本 / 文件名…"),
                );
            });
        });

        ui.add_space(8.0);

        // 类型过滤标签
        ui.horizontal(|ui| {
            let tabs: &[(&str, &str)] = &[
                ("all", "全部"),
                ("text", "文本"),
                ("image", "图片"),
                ("file", "文件"),
            ];

            for (key, label) in tabs {
                let selected = self.filter_type == *key || (self.filter_type.is_empty() && *key == "all");
                let btn = egui::SelectableLabel::new(selected, *label);
                if ui.add(btn).clicked() {
                    self.filter_type = key.to_string();
                    self.reload(app);
                }
            }
        });

        ui.add_space(8.0);

        // 客户端搜索过滤 - 克隆以避免借用冲突
        let q = self.search_query.trim().to_lowercase();
        let filtered: Vec<ClipItem> = if q.is_empty() {
            self.items.clone()
        } else {
            self.items
                .iter()
                .filter(|item| {
                    if item.is_text() {
                        item.text
                            .as_ref()
                            .map(|t| t.to_lowercase().contains(&q))
                            .unwrap_or(false)
                    } else {
                        item.filename().to_lowercase().contains(&q)
                    }
                })
                .cloned()
                .collect()
        };

        // 捕获状态以避免在闭包中借用 self
        let has_more = self.has_more;
        let loading_more = self.loading_more;
        let items_not_empty = !self.items.is_empty();

        // 列表
        if self.loading {
            ui.vertical_centered(|ui| {
                ui.add_space(50.0);
                ui.spinner();
            });
            return;
        }

        if filtered.is_empty() {
            ui.vertical_centered(|ui| {
                ui.add_space(50.0);
                ui.label(
                    egui::RichText::new(if q.is_empty() {
                        "暂无历史记录"
                    } else {
                        "没有匹配的条目"
                    })
                    .weak()
                    .size(14.0),
                );
                if !q.is_empty() {
                    ui.add_space(4.0);
                    ui.label(
                        egui::RichText::new("尝试更换关键词")
                            .small()
                            .weak(),
                    );
                }
            });
            return;
        }

        // 可滚动列表
        let mut to_delete: Vec<i64> = Vec::new();
        let mut to_copy: Vec<(i64, String)> = Vec::new();
        let mut to_copy_blob: Vec<i64> = Vec::new();
        let mut load_more_clicked = false;

        egui::ScrollArea::vertical().show(ui, |ui| {
            for item in &filtered {
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
                            ui.horizontal(|ui| {
                                ui.label("📄");
                                ui.label(
                                    egui::RichText::new(item.filename()).strong(),
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
                                    if ui.button("删除").clicked() {
                                        to_delete.push(id);
                                    }
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

            // 加载更多
            ui.add_space(8.0);
            if has_more {
                ui.vertical_centered(|ui| {
                    if loading_more {
                        ui.spinner();
                    } else if ui.button("加载更多").clicked() {
                        load_more_clicked = true;
                    }
                });
            } else if items_not_empty {
                ui.vertical_centered(|ui| {
                    ui.label(
                        egui::RichText::new("没有更多了")
                            .small()
                            .weak(),
                    );
                });
            }
        });

        // 在闭包外执行操作
        for (_id, text) in to_copy {
            copy_to_clipboard(&text);
            app.show_toast(ToastKind::Success, "已复制到剪贴板");
        }
        for id in to_copy_blob {
            fetch_and_copy_content(app, id);
        }
        for id in to_delete {
            delete_item(app, id);
        }
        if load_more_clicked {
            self.load_more(app);
        }
    }

    /// 重新加载（类型切换时）
    fn reload(&mut self, app: &mut App) {
        self.items.clear();
        self.cursor = None;
        self.has_more = false;
        self.loading = true;

        let api = app.api().clone();
        let tx = app.task_tx().clone();
        let filter = if self.filter_type == "all" || self.filter_type.is_empty() {
            None
        } else {
            Some(self.filter_type.clone())
        };

        app.rt().spawn(async move {
            match api.list_clips(PAGE_SIZE, None, filter.as_deref()).await {
                Ok(resp) => {
                    let _ = tx.send(TaskResult::ListClipsOk(resp));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::ListClipsErr(e.to_string()));
                }
            }
        });
    }

    /// 加载更多
    fn load_more(&mut self, app: &mut App) {
        if !self.has_more || self.loading_more {
            return;
        }
        let cursor = self.cursor.unwrap_or(0);
        self.loading_more = true;

        let api = app.api().clone();
        let tx = app.task_tx().clone();
        let filter = if self.filter_type == "all" || self.filter_type.is_empty() {
            None
        } else {
            Some(self.filter_type.clone())
        };

        app.rt().spawn(async move {
            match api.list_clips(PAGE_SIZE, Some(cursor), filter.as_deref()).await {
                Ok(resp) => {
                    let _ = tx.send(TaskResult::ListClipsOk(resp));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::ListClipsErr(e.to_string()));
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

/// 获取并复制内容
fn fetch_and_copy_content(app: &mut App, id: i64) {
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

/// 删除条目
fn delete_item(app: &mut App, id: i64) {
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
