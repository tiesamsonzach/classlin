use std::collections::VecDeque;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::menu::{CheckMenuItem, MenuItem};
use tauri::Wry;
use tokio::sync::watch;

use classroom_notify_core::RemoteMessage;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Config {
    #[serde(default)]
    pub server_url: String,
    #[serde(default)]
    pub channel: String,
    #[serde(default)]
    pub token: String,
    #[serde(default)]
    pub last_seen_ms: i64,
    /// After the teacher closes the ID window once, later launches stay in the tray.
    #[serde(default)]
    pub welcome_dismissed: bool,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
struct PackagedDefaults {
    #[serde(default)]
    server_url: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            server_url: String::new(),
            channel: String::new(),
            token: String::new(),
            last_seen_ms: 0,
            welcome_dismissed: false,
        }
    }
}

impl Config {
    pub fn is_complete(&self) -> bool {
        !self.server_url.trim().is_empty() && !self.channel.trim().is_empty()
    }
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct MessageView {
    pub id: String,
    pub channel: String,
    pub body: String,
    pub created_at: String,
    pub created_ms: i64,
    pub has_image: bool,
    pub image_path: Option<String>,
    pub image_loading: bool,
    pub image_error: bool,
}

impl MessageView {
    pub fn from_remote(msg: &RemoteMessage) -> Self {
        Self {
            id: msg.id.clone(),
            channel: msg.channel.clone(),
            body: msg.body.clone(),
            created_at: msg.created_at.clone(),
            created_ms: msg.created_ms,
            has_image: msg.has_image,
            image_path: None,
            image_loading: false,
            image_error: false,
        }
    }
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HistoryItem {
    pub id: String,
    pub channel: String,
    pub body: String,
    pub created_at: String,
    pub created_ms: i64,
    pub has_image: bool,
}

impl From<&RemoteMessage> for HistoryItem {
    fn from(msg: &RemoteMessage) -> Self {
        Self {
            id: msg.id.clone(),
            channel: msg.channel.clone(),
            body: msg.body.clone(),
            created_at: msg.created_at.clone(),
            created_ms: msg.created_ms,
            has_image: msg.has_image,
        }
    }
}

pub struct AppState {
    pub config: Mutex<Config>,
    pub config_path: PathBuf,
    pub cache_dir: PathBuf,
    pub latest: Mutex<Option<MessageView>>,
    pub status_text: Mutex<String>,
    pub status_kind: Mutex<String>,
    pub generation: watch::Sender<u64>,
    pub seen: Mutex<VecDeque<String>>,
    pub status_item: Mutex<Option<MenuItem<Wry>>>,
    pub channel_item: Mutex<Option<MenuItem<Wry>>>,
    pub autostart_item: Mutex<Option<CheckMenuItem<Wry>>>,
}

fn packaged_server_url() -> String {
    serde_json::from_str::<PackagedDefaults>(include_str!("../defaults.json"))
        .ok()
        .map(|d| d.server_url.trim().to_string())
        .filter(|url| !url.is_empty())
        .unwrap_or_default()
}

pub fn load_config(path: &Path) -> Config {
    let mut cfg = match std::fs::read_to_string(path) {
        Ok(text) => serde_json::from_str(&text).unwrap_or_default(),
        Err(_) => Config::default(),
    };
    let mut dirty = false;
    if cfg.channel.trim().is_empty() {
        cfg.channel = classroom_notify_core::generate_channel_id();
        dirty = true;
    }
    if cfg.server_url.trim().is_empty() {
        let baked = packaged_server_url();
        if !baked.is_empty() {
            cfg.server_url = baked;
            dirty = true;
        }
    }
    if dirty {
        let _ = write_config(path, &cfg);
    }
    cfg
}

pub fn write_config(path: &Path, cfg: &Config) -> Result<(), String> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|_| "无法保存设置".to_string())?;
    }
    let data = serde_json::to_vec_pretty(cfg).map_err(|_| "无法保存设置".to_string())?;
    let mut opts = std::fs::OpenOptions::new();
    opts.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        opts.mode(0o600);
    }
    let mut file = opts.open(path).map_err(|_| "无法保存设置".to_string())?;
    file.write_all(&data).map_err(|_| "无法保存设置".to_string())?;
    Ok(())
}
