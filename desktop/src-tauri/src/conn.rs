use std::path::PathBuf;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use futures_util::StreamExt;
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_notification::NotificationExt;
use tokio::sync::watch;

use classroom_notify_core::{valid_id, Client, RemoteMessage, SseParser};

use crate::state::{write_config, AppState, MessageView};
use crate::window::present_popup;

pub async fn supervise(app: AppHandle, mut rx: watch::Receiver<u64>) {
    let mut backoff: u64 = 1;
    loop {
        let cfg = app.state::<AppState>().config.lock().expect("config").clone();
        if !cfg.is_complete() {
            set_status(&app, "未设置", "off");
            if rx.changed().await.is_err() {
                return;
            }
            backoff = 1;
            continue;
        }
        set_status(&app, "正在连接…", "wait");
        let mut wake = rx.clone();
        tokio::select! {
            _ = wake.changed() => {
                backoff = 1;
                continue;
            }
            result = run_session(&app, cfg) => {
                let wait = match &result {
                    Ok(true) => {
                        set_status(&app, "连接中断，正在重试…", "wait");
                        backoff = 1;
                        1
                    }
                    Ok(false) => {
                        set_status(&app, "连接中断，正在重试…", "wait");
                        let wait = backoff;
                        backoff = (backoff * 2).min(15);
                        wait
                    }
                    Err(err) => {
                        set_status(&app, &format!("连接失败：{}", short(err)), "bad");
                        let wait = backoff;
                        backoff = (backoff * 2).min(15);
                        wait
                    }
                };
                let mut wake = rx.clone();
                tokio::select! {
                    _ = wake.changed() => { backoff = 1; }
                    _ = tokio::time::sleep(Duration::from_secs(wait)) => {}
                }
            }
        }
    }
}

async fn run_session(app: &AppHandle, cfg: crate::state::Config) -> Result<bool, String> {
    let client = Client::new(&cfg.server_url, &cfg.channel, &cfg.token)?;
    let response = client.open_stream().await?;
    set_status(app, "已连接", "on");
    if let Err(err) = catch_up(app, &client).await {
        eprintln!("catch-up: {err}");
    }
    let mut parser = SseParser::default();
    let mut stream = response.bytes_stream();
    let mut received = false;
    while let Some(chunk) = stream.next().await {
        let chunk = match chunk {
            Ok(chunk) => chunk,
            Err(err) => {
                eprintln!("stream: {err}");
                return Ok(received);
            }
        };
        received = true;
        for ev in parser.push_bytes(&chunk) {
            if ev.event == "message" {
                if let Ok(msg) = serde_json::from_str::<RemoteMessage>(&ev.data) {
                    deliver(app.clone(), client.clone(), msg, true);
                }
            } else if ev.event == "signal" {
                if let Ok(value) = serde_json::from_str::<serde_json::Value>(&ev.data) {
                    let _ = app.emit("signal", value);
                }
            }
        }
    }
    Ok(received)
}

async fn catch_up(app: &AppHandle, client: &Client) -> Result<(), String> {
    let page = client.list(None, 20).await?;
    let last_seen = app.state::<AppState>().config.lock().expect("config").last_seen_ms;
    let cutoff = now_ms().saturating_sub(30 * 60 * 1000);
    let newest = page
        .messages
        .into_iter()
        .filter(|msg| msg.created_ms > last_seen && msg.created_ms >= cutoff)
        .max_by_key(|msg| msg.created_ms);
    if let Some(msg) = newest {
        deliver(app.clone(), client.clone(), msg, true);
    }
    Ok(())
}

pub fn deliver(app: AppHandle, client: Client, msg: RemoteMessage, notify: bool) {
    let suppressed = take_suppressed(&app, &msg.id);
    let fresh = newly_seen(&app, &msg.id);
    if notify && !fresh && !suppressed {
        return;
    }
    if fresh {
        remember(&app, msg.created_ms);
    }
    if suppressed {
        let view = MessageView::from_remote(&msg);
        let _ = app.emit("message", &view);
        return;
    }
    if notify {
        let title = format!("教室 {}", msg.channel);
        let body = toast_body(&msg.body, msg.has_image);
        if let Err(err) = app.notification().builder().title(title).body(body).show() {
            eprintln!("notification: {err}");
        }
    }

    let mut view = MessageView::from_remote(&msg);
    let dest = image_dest(&app, &msg);
    if msg.has_image {
        if let Some(path) = dest.as_ref().filter(|path| path.exists()) {
            view.image_path = Some(path.display().to_string());
        } else if dest.is_some() {
            view.image_loading = true;
        } else {
            view.image_error = true;
        }
    }
    publish(&app, &view);

    if view.image_loading {
        let Some(dest) = dest else { return };
        let app2 = app.clone();
        let id = msg.id.clone();
        let view_for_task = view.clone();
        tauri::async_runtime::spawn(async move {
            let mut updated = view_for_task;
            match client.download(&id, &dest).await {
                Ok(()) => {
                    updated.image_loading = false;
                    updated.image_path = Some(dest.display().to_string());
                    updated.image_error = false;
                }
                Err(err) => {
                    eprintln!("image: {err}");
                    updated.image_loading = false;
                    updated.image_error = true;
                }
            }
            if latest_is(&app2, &id) {
                publish(&app2, &updated);
            }
        });
    }
}

fn publish(app: &AppHandle, view: &MessageView) {
    *app.state::<AppState>().latest.lock().expect("latest") = Some(view.clone());
    present_popup(app);
    let _ = app.emit("message", view);
}

pub fn set_status(app: &AppHandle, text: &str, kind: &str) {
    let state = app.state::<AppState>();
    *state.status_text.lock().expect("status") = text.to_string();
    *state.status_kind.lock().expect("kind") = kind.to_string();
    if let Some(item) = state.status_item.lock().expect("item").as_ref() {
        let _ = item.set_text(format!("状态：{text}"));
    }
    if let Some(tray) = app.tray_by_id("tray") {
        let channel = app
            .state::<AppState>()
            .config
            .lock()
            .expect("config")
            .channel
            .clone();
        let _ = tray.set_tooltip(Some(format!("课堂通知 · {channel} · {text}")));
    }
    let _ = app.emit(
        "status",
        serde_json::json!({ "text": text, "kind": kind }),
    );
}


fn take_suppressed(app: &AppHandle, id: &str) -> bool {
    let state = app.state::<AppState>();
    let mut set = state.suppressed.lock().expect("suppressed");
    set.remove(id)
}

fn newly_seen(app: &AppHandle, id: &str) -> bool {
    let state = app.state::<AppState>();
    let mut seen = state.seen.lock().expect("seen");
    if seen.iter().any(|item| item == id) {
        return false;
    }
    seen.push_back(id.to_string());
    while seen.len() > 200 {
        seen.pop_front();
    }
    true
}

fn remember(app: &AppHandle, created_ms: i64) {
    let state = app.state::<AppState>();
    let mut cfg = state.config.lock().expect("config");
    if created_ms > cfg.last_seen_ms {
        cfg.last_seen_ms = created_ms;
        let _ = write_config(&state.config_path, &cfg);
    }
}

fn latest_is(app: &AppHandle, id: &str) -> bool {
    app.state::<AppState>()
        .latest
        .lock()
        .expect("latest")
        .as_ref()
        .is_some_and(|view| view.id == id)
}

fn image_dest(app: &AppHandle, msg: &RemoteMessage) -> Option<PathBuf> {
    if !msg.has_image || !valid_id(&msg.id) {
        return None;
    }
    let ext = match msg.image_mime.as_deref() {
        Some("image/png") => "png",
        Some("image/gif") => "gif",
        Some("image/webp") => "webp",
        _ => "jpg",
    };
    Some(
        app.state::<AppState>()
            .cache_dir
            .join("images")
            .join(format!("{}.{ext}", msg.id)),
    )
}

fn toast_body(body: &str, has_image: bool) -> String {
    let text = body.trim();
    if text.is_empty() {
        return if has_image {
            "发来一张照片".into()
        } else {
            "新消息".into()
        };
    }
    let count = text.chars().count();
    let short: String = text.chars().take(80).collect();
    if count > 80 {
        format!("{short}…")
    } else {
        short
    }
}

fn short(text: &str) -> String {
    let clipped: String = text.chars().take(36).collect();
    if text.chars().count() > 36 {
        format!("{clipped}…")
    } else {
        clipped
    }
}

fn now_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|dur| dur.as_millis() as i64)
        .unwrap_or(0)
}
