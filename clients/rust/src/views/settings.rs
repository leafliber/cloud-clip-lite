use eframe::egui;

use crate::app::{App, TaskResult};

/// 设置视图
/// - 账号信息展示
/// - 修改密码
/// - 服务器配置
#[derive(Default)]
pub struct SettingsView {
    pub old_password: String,
    pub new_password: String,
    pub confirm_password: String,
    pub updating: bool,
    pub show_old: bool,
    pub show_new: bool,
    pub password_error: Option<String>,
}

impl SettingsView {
    pub fn render(&mut self, ui: &mut egui::Ui, app: &mut App) {
        ui.label(
            egui::RichText::new("设置")
                .strong()
                .size(16.0),
        );
        ui.add_space(16.0);

        // 账号信息
        egui::Frame::group(ui.style())
            .inner_margin(egui::Margin::same(12.0))
            .show(ui, |ui| {
                ui.label(
                    egui::RichText::new("账号信息")
                        .strong()
                        .size(14.0),
                );
                ui.add_space(8.0);

                if let Some(user) = app.user() {
                    ui.horizontal(|ui| {
                        ui.label("用户名");
                        ui.add_space(20.0);
                        ui.label(
                            egui::RichText::new(&user.username).strong(),
                        );
                    });
                    ui.add_space(4.0);

                    ui.horizontal(|ui| {
                        ui.label("角色");
                        ui.add_space(26.0);
                        let role = if user.role == "admin" {
                            "管理员"
                        } else {
                            "普通用户"
                        };
                        let color = if user.role == "admin" {
                            egui::Color32::from_rgb(245, 158, 11)
                        } else {
                            egui::Color32::from_rgb(156, 163, 175)
                        };
                        ui.label(egui::RichText::new(role).color(color));
                    });
                    ui.add_space(4.0);

                    if let Some(email) = &user.email {
                        if !email.is_empty() {
                            ui.horizontal(|ui| {
                                ui.label("邮箱");
                                ui.add_space(26.0);
                                ui.label(email);
                            });
                            ui.add_space(4.0);
                        }
                    }

                    ui.horizontal(|ui| {
                        ui.label("存储配额");
                        ui.add_space(8.0);
                        ui.label(format_size(user.quota_bytes));
                    });
                    ui.add_space(4.0);

                    ui.horizontal(|ui| {
                        ui.label("单条上限");
                        ui.add_space(8.0);
                        ui.label(format_size(user.max_item_size));
                    });
                    ui.add_space(4.0);

                    ui.horizontal(|ui| {
                        ui.label("保留天数");
                        ui.add_space(8.0);
                        ui.label(format!("{} 天", user.retention_days));
                    });
                    ui.add_space(4.0);

                    ui.horizontal(|ui| {
                        ui.label("注册时间");
                        ui.add_space(8.0);
                        ui.label(&user.created_at);
                    });
                }
            });

        ui.add_space(16.0);

        // 修改密码
        egui::Frame::group(ui.style())
            .inner_margin(egui::Margin::same(12.0))
            .show(ui, |ui| {
                ui.label(
                    egui::RichText::new("修改密码")
                        .strong()
                        .size(14.0),
                );
                ui.add_space(8.0);

                ui.horizontal(|ui| {
                    ui.label("旧密码");
                    ui.add_space(12.0);
                    ui.add(
                        egui::TextEdit::singleline(&mut self.old_password)
                            .password(!self.show_old)
                            .desired_width(220.0)
                            .hint_text("输入当前密码"),
                    );
                    ui.checkbox(&mut self.show_old, "显示");
                });
                ui.add_space(4.0);

                ui.horizontal(|ui| {
                    ui.label("新密码");
                    ui.add_space(12.0);
                    ui.add(
                        egui::TextEdit::singleline(&mut self.new_password)
                            .password(!self.show_new)
                            .desired_width(220.0)
                            .hint_text("至少 8 位"),
                    );
                    ui.checkbox(&mut self.show_new, "显示");
                });
                ui.add_space(4.0);

                ui.horizontal(|ui| {
                    ui.label("确认密码");
                    ui.add_space(4.0);
                    ui.add(
                        egui::TextEdit::singleline(&mut self.confirm_password)
                            .password(!self.show_new)
                            .desired_width(220.0)
                            .hint_text("再次输入新密码"),
                    );
                });

                ui.add_space(8.0);

                if let Some(e) = &self.password_error {
                    ui.label(
                        egui::RichText::new(e)
                            .color(egui::Color32::from_rgb(239, 68, 68))
                            .small(),
                    );
                    ui.add_space(4.0);
                }

                let can_submit = !self.old_password.is_empty()
                    && !self.new_password.is_empty()
                    && !self.confirm_password.is_empty()
                    && !self.updating;

                let btn_text = if self.updating { "修改中…" } else { "确认修改" };
                if ui.add_enabled(can_submit, egui::Button::new(btn_text)).clicked() {
                    self.do_update_password(app);
                }
            });

        ui.add_space(16.0);

        // 关于
        egui::Frame::group(ui.style())
            .inner_margin(egui::Margin::same(12.0))
            .show(ui, |ui| {
                ui.label(
                    egui::RichText::new("关于")
                        .strong()
                        .size(14.0),
                );
                ui.add_space(4.0);
                ui.label("Cloud Clip Lite 桌面客户端");
                ui.label(
                    egui::RichText::new("版本 0.1.0")
                        .small()
                        .weak(),
                );
                ui.add_space(4.0);
                ui.label(
                    egui::RichText::new("跨平台剪切板同步工具，支持文本、图片、文件")
                        .small()
                        .weak(),
                );
            });
    }

    fn do_update_password(&mut self, app: &mut App) {
        self.password_error = None;

        if self.new_password.len() < 8 {
            self.password_error = Some("新密码至少 8 位".to_string());
            return;
        }

        if self.new_password != self.confirm_password {
            self.password_error = Some("两次输入的密码不一致".to_string());
            return;
        }

        self.updating = true;
        let api = app.api().clone();
        let tx = app.task_tx().clone();
        let old_pwd = self.old_password.clone();
        let new_pwd = self.new_password.clone();

        app.rt().spawn(async move {
            match api.update_password(&old_pwd, &new_pwd).await {
                Ok(()) => {
                    let _ = tx.send(TaskResult::UpdatePasswordOk);
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::UpdatePasswordErr(e.to_string()));
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
