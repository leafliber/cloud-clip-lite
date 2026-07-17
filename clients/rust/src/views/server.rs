use eframe::egui;

use crate::api::ApiClient;
use crate::app::{App, TaskResult};

/// 服务器选择视图（启动第一步：先确认服务器，再登录/恢复会话）
#[derive(Default)]
pub struct ServerView {
    /// 地址编辑缓冲
    pub url: String,
    pub error: Option<String>,
    pub checking: bool,
}

impl ServerView {
    pub fn render(&mut self, ui: &mut egui::Ui, app: &mut App) {
        ui.horizontal(|ui| {
            ui.label("服务器地址");
            ui.add_space(8.0);
            let resp = ui.add(
                egui::TextEdit::singleline(&mut self.url)
                    .desired_width(300.0)
                    .hint_text("http://localhost:8080"),
            );

            // 编辑时清除错误提示
            if resp.changed() {
                self.error = None;
            }

            // 回车快捷连接
            let enter = resp.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter));
            if enter {
                self.do_connect(app);
            }
        });

        ui.add_space(8.0);

        // 错误信息
        if let Some(e) = &self.error {
            ui.label(
                egui::RichText::new(e)
                    .color(egui::Color32::from_rgb(239, 68, 68))
                    .small(),
            );
            ui.add_space(4.0);
        }

        // 连接按钮
        let can_connect = !self.url.trim().is_empty() && !self.checking;
        let btn = egui::Button::new(if self.checking {
            "连接中…"
        } else {
            "连接"
        });
        if ui.add_enabled(can_connect, btn).clicked() {
            self.do_connect(app);
        }

        ui.add_space(15.0);
        ui.label(
            egui::RichText::new("提示：请先启动 Cloud Clip Lite 后端服务")
                .small()
                .weak(),
        );
    }

    fn do_connect(&mut self, app: &mut App) {
        let url = ApiClient::normalize_server_url(&self.url);
        self.url = url.clone();
        self.checking = true;
        self.error = None;

        let tx = app.task_tx().clone();
        app.rt().spawn(async move {
            match ApiClient::health_check(&url).await {
                Ok(version) => {
                    let _ = tx.send(TaskResult::ServerCheckOk { url, version });
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::ServerCheckErr(e.to_string()));
                }
            }
        });
    }
}
