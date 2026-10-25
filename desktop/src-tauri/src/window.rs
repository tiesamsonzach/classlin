use tauri::{AppHandle, Emitter, LogicalSize, Manager, WebviewWindow};

use classroom_notify_core::{main_window, popup_window};

use crate::state::{write_config, AppState};

pub fn install_close_to_hide(app: &tauri::App) {
    for label in ["main", "popup"] {
        let Some(win) = app.get_webview_window(label) else {
            continue;
        };
        let win2 = win.clone();
        let app_handle = app.handle().clone();
        let is_main = label == "main";
        win.on_window_event(move |event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = win2.hide();
                if is_main {
                    mark_welcome_seen(&app_handle);
                }
                hide_dock_if_idle(&app_handle);
            }
        });
    }
}

fn mark_welcome_seen(app: &AppHandle) {
    let state = app.state::<AppState>();
    let mut cfg = state.config.lock().expect("config");
    if cfg.welcome_dismissed {
        return;
    }
    cfg.welcome_dismissed = true;
    let _ = write_config(&state.config_path, &cfg);
}

pub fn fit_main(win: &WebviewWindow) {
    fit(win, true);
}

pub fn fit_popup(win: &WebviewWindow) {
    fit(win, false);
}

fn fit(win: &WebviewWindow, main: bool) {
    let Some((lw, lh, scale)) = logical_monitor(win) else {
        return;
    };
    let sized = if main {
        main_window(lw, lh)
    } else {
        popup_window(lw, lh)
    };
    let _ = win.set_size(LogicalSize::new(sized.width, sized.height));
    let _ = win.center();
    let _ = win.emit(
        "metrics",
        serde_json::json!({
            "logicalWidth": lw,
            "logicalHeight": lh,
            "scaleFactor": scale,
            "uiScale": sized.ui_scale,
        }),
    );
}

pub fn logical_monitor(win: &WebviewWindow) -> Option<(f64, f64, f64)> {
    let mon = win
        .current_monitor()
        .ok()
        .flatten()
        .or_else(|| win.primary_monitor().ok().flatten())?;
    let scale = mon.scale_factor();
    if scale <= 0.0 {
        return None;
    }
    let size = mon.size();
    Some((size.width as f64 / scale, size.height as f64 / scale, scale))
}

pub fn present_popup(app: &AppHandle) {
    let Some(win) = app.get_webview_window("popup") else {
        return;
    };
    fit_popup(&win);
    let _ = win.set_always_on_top(true);
    let _ = win.show();
    let _ = win.unminimize();
    let _ = win.set_focus();
    #[cfg(target_os = "macos")]
    let _ = app.set_activation_policy(tauri::ActivationPolicy::Regular);
}

pub fn show_main(app: &AppHandle) {
    let Some(win) = app.get_webview_window("main") else {
        return;
    };
    fit_main(&win);
    let _ = win.show();
    let _ = win.unminimize();
    let _ = win.set_focus();
    #[cfg(target_os = "macos")]
    let _ = app.set_activation_policy(tauri::ActivationPolicy::Regular);
}

pub fn hide_window(app: &AppHandle, label: &str) {
    if let Some(win) = app.get_webview_window(label) {
        let _ = win.hide();
    }
    hide_dock_if_idle(app);
}

fn hide_dock_if_idle(app: &AppHandle) {
    #[cfg(target_os = "macos")]
    {
        let visible = ["main", "popup"].iter().any(|label| {
            app.get_webview_window(label)
                .and_then(|win| win.is_visible().ok())
                .unwrap_or(false)
        });
        if !visible {
            let _ = app.set_activation_policy(tauri::ActivationPolicy::Accessory);
        }
    }
    #[cfg(not(target_os = "macos"))]
    let _ = app;
}
