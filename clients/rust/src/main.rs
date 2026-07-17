#![allow(dead_code)]

mod api;
mod app;
mod config;
mod models;
mod views;
mod ws;

use eframe::egui;

fn main() -> Result<(), eframe::Error> {
    // 初始化日志
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([900.0, 650.0])
            .with_min_inner_size([600.0, 450.0])
            .with_title("Cloud Clip Lite"),
        ..Default::default()
    };

    eframe::run_native(
        "Cloud Clip Lite",
        options,
        Box::new(|cc| {
            Ok(Box::new(app::App::new(cc)))
        }),
    )
}
