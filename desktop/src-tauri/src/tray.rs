use tauri::menu::{CheckMenuItem, Menu, MenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{Manager, Wry};
use tauri_plugin_autostart::ManagerExt;

use crate::conn::set_status;
use crate::state::AppState;
use crate::window::{present_popup, show_main};

pub fn install(app: &tauri::App) -> tauri::Result<()> {
    let channel = app
        .state::<AppState>()
        .config
        .lock()
        .expect("config")
        .channel
        .clone();
    let status = MenuItem::with_id(app, "status", "状态：未连接", false, None::<&str>)?;
    let channel_item = MenuItem::with_id(
        app,
        "channel",
        format!("教室 {channel}"),
        false,
        None::<&str>,
    )?;
    let show = MenuItem::with_id(app, "show", "打开", true, None::<&str>)?;
    let latest = MenuItem::with_id(app, "latest", "最新一条", true, None::<&str>)?;
    let mut checked = false;
    if let Ok(enabled) = app.autolaunch().is_enabled() {
        checked = enabled;
    }
    let autostart = CheckMenuItem::with_id(app, "autostart", "开机自启", true, checked, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&channel_item, &status, &show, &latest, &autostart, &quit])?;

    let icon = tauri::image::Image::from_bytes(include_bytes!("../icons/32x32.png"))
        .map_err(|err| tauri::Error::AssetNotFound(err.to_string()))?;

    TrayIconBuilder::<Wry>::with_id("tray")
        .icon(icon)
        .menu(&menu)
        .tooltip(format!("课堂通知 · {channel}"))
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => show_main(app),
            "latest" => show_latest(app),
            "autostart" => toggle_autostart(app),
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main(tray.app_handle());
            }
        })
        .build(app)?;

    let state = app.state::<AppState>();
    *state.status_item.lock().expect("status") = Some(status);
    *state.channel_item.lock().expect("channel") = Some(channel_item);
    *state.autostart_item.lock().expect("autostart") = Some(autostart);
    Ok(())
}

pub fn refresh_identity(app: &tauri::AppHandle) {
    let channel = app
        .state::<AppState>()
        .config
        .lock()
        .expect("config")
        .channel
        .clone();
    if let Some(item) = app
        .state::<AppState>()
        .channel_item
        .lock()
        .expect("channel")
        .as_ref()
    {
        let _ = item.set_text(format!("教室 {channel}"));
    }
    if let Some(tray) = app.tray_by_id("tray") {
        let status = app
            .state::<AppState>()
            .status_text
            .lock()
            .expect("status")
            .clone();
        let _ = tray.set_tooltip(Some(format!("课堂通知 · {channel} · {status}")));
    }
}

fn show_latest(app: &tauri::AppHandle) {
    let latest = app.state::<AppState>().latest.lock().expect("latest").clone();
    if let Some(view) = latest {
        present_popup(app);
        use tauri::Emitter;
        let _ = app.emit("message", &view);
        return;
    }
    show_main(app);
}

fn toggle_autostart(app: &tauri::AppHandle) {
    let auto = app.autolaunch();
    let enabled = auto.is_enabled().unwrap_or(false);
    let result = if enabled { auto.disable() } else { auto.enable() };
    if let Err(err) = result {
        eprintln!("autostart: {err}");
        set_status(app, "开机自启设置失败", "bad");
        return;
    }
    if let Some(item) = app
        .state::<AppState>()
        .autostart_item
        .lock()
        .expect("autostart")
        .as_ref()
    {
        let _ = item.set_checked(!enabled);
    }
}
