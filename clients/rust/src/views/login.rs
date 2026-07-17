use eframe::egui;

use crate::app::{App, TaskResult};

/// 登录视图
#[derive(Default)]
pub struct LoginView {
    pub username: String,
    pub password: String,
    pub error: Option<String>,
    pub loading: bool,
}

impl LoginView {
    pub fn render(&mut self, ui: &mut egui::Ui, app: &mut App) {
        let max_width = 320.0;

        ui.horizontal(|ui| {
            ui.label("用户名");
            ui.add_space(8.0);
            ui.add(
                egui::TextEdit::singleline(&mut self.username)
                    .desired_width(max_width - 80.0)
                    .hint_text("输入用户名"),
            );
        });

        ui.add_space(8.0);

        ui.horizontal(|ui| {
            ui.label("密  码");
            ui.add_space(8.0);
            ui.add(
                egui::TextEdit::singleline(&mut self.password)
                    .password(true)
                    .desired_width(max_width - 80.0)
                    .hint_text("输入密码"),
            );
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

        // 登录按钮
        let can_login = !self.username.is_empty()
            && !self.password.is_empty()
            && !self.loading;

        ui.horizontal(|ui| {
            let btn = egui::Button::new(if self.loading {
                "登录中…"
            } else {
                "登录"
            });

            if ui.add_enabled(can_login, btn).clicked() {
                self.do_login(app);
            }

            // Ctrl+Enter 快捷登录
            if can_login
                && ui.input(|i| {
                    i.key_pressed(egui::Key::Enter)
                        && (i.modifiers.ctrl || i.modifiers.command)
                })
            {
                self.do_login(app);
            }
        });

        ui.add_space(15.0);
        ui.label(
            egui::RichText::new("提示：首次使用请先在 Web 界面注册账号")
                .small()
                .weak(),
        );
    }

    fn do_login(&mut self, app: &mut App) {
        self.loading = true;
        self.error = None;
        app.loading = true;

        let api = app.api().clone();
        let tx = app.task_tx().clone();
        let username = self.username.clone();
        let password = self.password.clone();

        app.rt().spawn(async move {
            match api.login(&username, &password).await {
                Ok(auth) => {
                    let _ = tx.send(TaskResult::LoginOk(auth));
                }
                Err(e) => {
                    let _ = tx.send(TaskResult::LoginErr(e.to_string()));
                }
            }
        });
    }
}
