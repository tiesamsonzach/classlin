use serde::Deserialize;
use tauri::{AppHandle, Manager, WebviewWindow};

use classroom_notify_core::Client;

use crate::conn::{deliver, set_status};
use crate::state::{write_config, AppState, Config, HistoryItem, MessageView};
use crate::window::{hide_window, logical_monitor};

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConfigInput {
    pub server_url: String,
    pub channel: String,
    pub token: String,
}

#[derive(serde::Serialize)]
pub struct StatusView {
    text: String,
    kind: String,
}

#[tauri::command]
pub fn load_config(app: AppHandle) -> Config {
    app.state::<AppState>().config.lock().expect("config").clone()
}

#[tauri::command]
pub async fn save_config(app: AppHandle, input: ConfigInput) -> Result<(), String> {
    let server_url = classroom_notify_core::normalize_base(&input.server_url)?;
    let channel = input.channel.trim().to_string();
    if !classroom_notify_core::valid_channel(&channel) {
        return Err("教室编号请用 1–32 个字符，不要空格".into());
    }
    let token = input.token.trim().to_string();
    if !classroom_notify_core::valid_token(&token) {
        return Err("口令请使用英文字母或数字，不要空格".into());
    }
    match Client::new(&server_url, &channel, &token) {
        Ok(probe) => match probe.info().await {
            Ok(info) if info.token_required && token.is_empty() => {
                return Err("这台服务器设置了口令，请填和手机一样的口令".into());
            }
            Ok(_) => {}
            Err(err) => eprintln!("probe: {err}"),
        },
        Err(err) => return Err(err),
    }
    {
        let state = app.state::<AppState>();
        let mut cfg = state.config.lock().expect("config");
        let last_seen_ms = cfg.last_seen_ms;
        let welcome_dismissed = cfg.welcome_dismissed;
        *cfg = Config {
            server_url,
            channel,
            token,
            last_seen_ms,
            welcome_dismissed,
        };
        write_config(&state.config_path, &cfg)?;
    }
    crate::tray::refresh_identity(&app);
    let _ = app
        .state::<AppState>()
        .generation
        .send_modify(|gen| *gen += 1);
    set_status(&app, "正在连接…", "wait");
    Ok(())
}

#[tauri::command]
pub fn generate_classroom_id() -> String {
    classroom_notify_core::generate_channel_id()
}

#[tauri::command]
pub fn latest_message(app: AppHandle) -> Option<MessageView> {
    app.state::<AppState>().latest.lock().expect("latest").clone()
}

#[tauri::command]
pub async fn list_history(app: AppHandle) -> Result<Vec<HistoryItem>, String> {
    let cfg = app.state::<AppState>().config.lock().expect("config").clone();
    if !cfg.is_complete() {
        return Err("请先填写服务器和教室编号".into());
    }
    let client = Client::new(&cfg.server_url, &cfg.channel, &cfg.token)?;
    let page = client.list(None, 50).await?;
    Ok(page.messages.iter().map(HistoryItem::from).collect())
}

#[tauri::command]
pub async fn open_message(app: AppHandle, id: String) -> Result<(), String> {
    let cfg = app.state::<AppState>().config.lock().expect("config").clone();
    if !cfg.is_complete() {
        return Err("请先填写服务器和教室编号".into());
    }
    let client = Client::new(&cfg.server_url, &cfg.channel, &cfg.token)?;
    let msg = client.get(&id).await?;
    deliver(app, client, msg, false);
    Ok(())
}

#[tauri::command]
pub fn dismiss_popup(app: AppHandle) {
    hide_window(&app, "popup");
}

#[tauri::command]
pub fn connection_status(app: AppHandle) -> StatusView {
    let state = app.state::<AppState>();
    let text = state.status_text.lock().expect("status").clone();
    let kind = state.status_kind.lock().expect("kind").clone();
    StatusView { text, kind }
}

#[tauri::command]
pub fn screen_metrics(window: WebviewWindow) -> serde_json::Value {
    if let Some((width, height, scale)) = logical_monitor(&window) {
        let ui = classroom_notify_core::ui_scale(width);
        serde_json::json!({
            "logicalWidth": width,
            "logicalHeight": height,
            "scaleFactor": scale,
            "uiScale": ui,
        })
    } else {
        serde_json::json!({
            "logicalWidth": 1920.0,
            "logicalHeight": 1080.0,
            "scaleFactor": 1.0,
            "uiScale": 1.0,
        })
    }
}
