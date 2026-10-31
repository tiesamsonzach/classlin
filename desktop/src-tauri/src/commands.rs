use serde::Deserialize;
use tauri::{AppHandle, Emitter, Manager, WebviewWindow};

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

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SendTextInput {
    pub body: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SendImageInput {
    pub body: String,
    pub data_base64: String,
    pub file_name: String,
    pub mime: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SendSignalInput {
    pub kind: String,
    pub call_id: String,
    pub sdp: Option<String>,
    pub candidate: Option<String>,
}

fn remember_suppressed(app: &AppHandle, id: &str) {
    let state = app.state::<AppState>();
    let mut set = state.suppressed.lock().expect("suppressed");
    set.insert(id.to_string());
    if set.len() > 100 {
        // drop arbitrary extras by rebuilding from seen order roughly
        let keep: Vec<String> = set.iter().take(50).cloned().collect();
        set.clear();
        set.extend(keep);
    }
}

#[tauri::command]
pub async fn send_text(app: AppHandle, input: SendTextInput) -> Result<HistoryItem, String> {
    let body = input.body.trim().to_string();
    if body.is_empty() {
        return Err("请填写文字".into());
    }
    let cfg = app.state::<AppState>().config.lock().expect("config").clone();
    if !cfg.is_complete() {
        return Err("请先填写服务器和教室编号".into());
    }
    let client = Client::new(&cfg.server_url, &cfg.channel, &cfg.token)?;
    let msg = client.post_text(&body).await?;
    remember_suppressed(&app, &msg.id);
    let item = HistoryItem::from(&msg);
    let _ = app.emit("message", MessageView::from_remote(&msg));
    Ok(item)
}

#[tauri::command]
pub async fn send_image(app: AppHandle, input: SendImageInput) -> Result<HistoryItem, String> {
    let cfg = app.state::<AppState>().config.lock().expect("config").clone();
    if !cfg.is_complete() {
        return Err("请先填写服务器和教室编号".into());
    }
    let raw = input.data_base64.trim();
    let b64 = raw
        .split(',')
        .last()
        .unwrap_or(raw)
        .trim();
    use base64::Engine;
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(b64)
        .map_err(|_| "照片数据无法识别".to_string())?;
    let file_name = if input.file_name.trim().is_empty() {
        "photo.jpg".into()
    } else {
        input.file_name.trim().to_string()
    };
    let client = Client::new(&cfg.server_url, &cfg.channel, &cfg.token)?;
    let msg = client
        .post_image(input.body.trim(), bytes, &file_name, input.mime.trim())
        .await?;
    remember_suppressed(&app, &msg.id);
    let item = HistoryItem::from(&msg);
    let _ = app.emit("message", MessageView::from_remote(&msg));
    Ok(item)
}

#[tauri::command]
pub async fn send_signal(app: AppHandle, input: SendSignalInput) -> Result<(), String> {
    let cfg = app.state::<AppState>().config.lock().expect("config").clone();
    if !cfg.is_complete() {
        return Err("请先填写服务器和教室编号".into());
    }
    let kind = input.kind.trim();
    let call_id = input.call_id.trim();
    if kind.is_empty() || call_id.is_empty() {
        return Err("信令参数不完整".into());
    }
    let client = Client::new(&cfg.server_url, &cfg.channel, &cfg.token)?;
    client
        .post_signal(
            kind,
            "desktop",
            call_id,
            input.sdp.as_deref(),
            input.candidate.as_deref(),
        )
        .await
}
