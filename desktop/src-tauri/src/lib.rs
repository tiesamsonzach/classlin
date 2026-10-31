mod commands;
mod conn;
mod state;
mod tray;
mod window;

use tauri::Manager;

pub fn run() {
    let builder = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            window::show_main(app);
        }))
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .invoke_handler(tauri::generate_handler![
            commands::load_config,
            commands::save_config,
            commands::latest_message,
            commands::list_history,
            commands::open_message,
            commands::dismiss_popup,
            commands::connection_status,
            commands::screen_metrics,
            commands::generate_classroom_id,
            commands::send_text,
            commands::send_image,
            commands::send_signal
        ])
        .setup(|app| {
            let config_dir = app.path().app_config_dir()?;
            let cache_dir = app.path().app_cache_dir()?;
            std::fs::create_dir_all(&config_dir)?;
            std::fs::create_dir_all(cache_dir.join("images"))?;
            let config_path = config_dir.join("config.json");
            let cfg = state::load_config(&config_path);
            let (tx, rx) = tokio::sync::watch::channel(0u64);
            app.manage(state::AppState {
                config: std::sync::Mutex::new(cfg.clone()),
                config_path,
                cache_dir,
                latest: std::sync::Mutex::new(None),
                status_text: std::sync::Mutex::new("未连接".into()),
                status_kind: std::sync::Mutex::new("off".into()),
                generation: tx,
                seen: std::sync::Mutex::new(std::collections::VecDeque::new()),
                suppressed: std::sync::Mutex::new(std::collections::HashSet::new()),
                status_item: std::sync::Mutex::new(None),
                channel_item: std::sync::Mutex::new(None),
                autostart_item: std::sync::Mutex::new(None),
            });
            tray::install(app)?;
            window::install_close_to_hide(app);
            let show_id = !cfg.welcome_dismissed || !cfg.is_complete();
            if show_id {
                if let Some(win) = app.get_webview_window("main") {
                    window::fit_main(&win);
                    win.show()?;
                }
            } else {
                #[cfg(target_os = "macos")]
                let _ = app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            }
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                conn::supervise(handle, rx).await;
            });
            Ok(())
        });

    builder
        .run(tauri::generate_context!())
        .expect("课堂通知启动失败");
}
