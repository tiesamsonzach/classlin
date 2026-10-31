use std::path::Path;
use std::time::Duration;

use serde::Deserialize;

use crate::validate::{valid_id, valid_token};

#[derive(Debug, Clone, Deserialize)]
pub struct ServerInfo {
    pub token_required: bool,
    #[serde(default)]
    pub max_image_bytes: i64,
    #[serde(default)]
    pub version: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RemoteMessage {
    pub id: String,
    pub channel: String,
    pub body: String,
    pub has_image: bool,
    #[serde(default)]
    pub image_url: Option<String>,
    #[serde(default)]
    pub image_mime: Option<String>,
    pub created_at: String,
    pub created_ms: i64,
}

#[derive(Debug, Clone)]
pub struct Page {
    pub messages: Vec<RemoteMessage>,
    pub next_before: Option<i64>,
}

#[derive(Clone)]
pub struct Client {
    http: reqwest::Client,
    base: String,
    channel: String,
    token: String,
}

#[derive(Deserialize)]
struct ErrBody {
    error: String,
}

#[derive(Deserialize)]
struct ListResp {
    messages: Vec<RemoteMessage>,
    next_before: Option<i64>,
}

#[derive(Deserialize)]
struct MessageResp {
    message: RemoteMessage,
}

impl Client {
    pub fn new(base: &str, channel: &str, token: &str) -> Result<Self, String> {
        let http = reqwest::Client::builder()
            .connect_timeout(Duration::from_secs(10))
            .user_agent("classroom-notify-desktop/1.0")
            .build()
            .map_err(|_| "无法创建网络客户端".to_string())?;
        Self::with_http(http, base, channel, token)
    }

    pub fn with_http(
        http: reqwest::Client,
        base: &str,
        channel: &str,
        token: &str,
    ) -> Result<Self, String> {
        let base = crate::normalize_base(base)?;
        let channel = channel.trim().to_string();
        if !crate::valid_channel(&channel) {
            return Err("教室编号请用 1–32 个字符，不要空格".into());
        }
        let token = token.trim().to_string();
        if !valid_token(&token) {
            return Err("口令请使用英文字母或数字，不要空格".into());
        }
        Ok(Self {
            http,
            base,
            channel,
            token,
        })
    }

    pub async fn info(&self) -> Result<ServerInfo, String> {
        let res = self
            .http
            .get(self.url("/api/v1/info"))
            .timeout(Duration::from_secs(8))
            .send()
            .await
            .map_err(net_err)?;
        let res = read_ok(res).await?;
        res.json().await.map_err(|_| "服务器响应无法识别".to_string())
    }

    pub async fn list(&self, before: Option<i64>, limit: u32) -> Result<Page, String> {
        let mut req = self
            .http
            .get(self.url("/api/v1/messages"))
            .timeout(Duration::from_secs(20))
            .query(&[("channel", self.channel.as_str()), ("limit", &limit.to_string())]);
        if let Some(before) = before {
            req = req.query(&[("before", before.to_string())]);
        }
        let res = self.authed(req).send().await.map_err(net_err)?;
        let res = read_ok(res).await?;
        let page: ListResp = res.json().await.map_err(|_| "历史记录无法识别".to_string())?;
        Ok(Page {
            messages: page.messages,
            next_before: page.next_before,
        })
    }

    pub async fn get(&self, id: &str) -> Result<RemoteMessage, String> {
        if !valid_id(id) {
            return Err("消息编号不对".into());
        }
        let res = self
            .authed(
                self.http
                    .get(self.url(&format!("/api/v1/messages/{id}")))
                    .timeout(Duration::from_secs(20))
                    .query(&[("channel", self.channel.as_str())]),
            )
            .send()
            .await
            .map_err(net_err)?;
        let res = read_ok(res).await?;
        res.json().await.map_err(|_| "消息无法识别".to_string())
    }

    pub async fn open_stream(&self) -> Result<reqwest::Response, String> {
        let req = self
            .http
            .get(self.url("/api/v1/stream"))
            .query(&[("channel", self.channel.as_str())])
            .header("Accept", "text/event-stream")
            .header("Cache-Control", "no-cache");
        let res = self.authed(req).send().await.map_err(net_err)?;
        read_ok(res).await
    }

    pub async fn download(&self, id: &str, dest: &Path) -> Result<(), String> {
        if !valid_id(id) {
            return Err("照片编号不对".into());
        }
        if let Ok(meta) = std::fs::metadata(dest) {
            if meta.len() > 0 {
                return Ok(());
            }
        }
        let res = self
            .authed(
                self.http
                    .get(self.url(&format!("/api/v1/files/{id}")))
                    .query(&[("channel", self.channel.as_str())])
                    .timeout(Duration::from_secs(60)),
            )
            .send()
            .await
            .map_err(net_err)?;
        let res = read_ok(res).await?;
        let bytes = res.bytes().await.map_err(|_| "照片下载中断".to_string())?;
        if bytes.len() > 12 * 1024 * 1024 + 1024 {
            return Err("照片太大".into());
        }
        if let Some(parent) = dest.parent() {
            std::fs::create_dir_all(parent).map_err(|_| "无法保存照片".to_string())?;
        }
        let tmp = dest.with_extension("part");
        std::fs::write(&tmp, &bytes).map_err(|_| "无法保存照片".to_string())?;
        std::fs::rename(&tmp, dest).map_err(|_| "无法保存照片".to_string())?;
        Ok(())
    }


    pub async fn post_text(&self, body: &str) -> Result<RemoteMessage, String> {
        let payload = serde_json::json!({
            "channel": self.channel,
            "token": self.token,
            "body": body,
        });
        let res = self
            .authed(
                self.http
                    .post(self.url("/api/v1/messages"))
                    .timeout(Duration::from_secs(30))
                    .header("Content-Type", "application/json")
                    .json(&payload),
            )
            .send()
            .await
            .map_err(net_err)?;
        let res = read_ok(res).await?;
        let wrapped: MessageResp = res.json().await.map_err(|_| "发送结果无法识别".to_string())?;
        Ok(wrapped.message)
    }

    pub async fn post_image(
        &self,
        body: &str,
        bytes: Vec<u8>,
        filename: &str,
        mime: &str,
    ) -> Result<RemoteMessage, String> {
        if bytes.is_empty() {
            return Err("请选择照片".into());
        }
        if bytes.len() > 12 * 1024 * 1024 {
            return Err("照片太大，最大 12MB".into());
        }
        let boundary = format!("----cn{}", now_nonce());
        let mime = if mime.is_empty() {
            "application/octet-stream"
        } else {
            mime
        };
        let safe_name = filename.replace('"', "_");
        let mut form = Vec::new();
        for (name, value) in [
            ("channel", self.channel.as_str()),
            ("token", self.token.as_str()),
            ("body", body),
        ] {
            form.extend_from_slice(format!("--{boundary}\r\n").as_bytes());
            form.extend_from_slice(
                format!("Content-Disposition: form-data; name=\"{name}\"\r\n\r\n").as_bytes(),
            );
            form.extend_from_slice(value.as_bytes());
            form.extend_from_slice(b"\r\n");
        }
        form.extend_from_slice(format!("--{boundary}\r\n").as_bytes());
        form.extend_from_slice(
            format!(
                "Content-Disposition: form-data; name=\"image\"; filename=\"{safe_name}\"\r\nContent-Type: {mime}\r\n\r\n"
            )
            .as_bytes(),
        );
        form.extend_from_slice(&bytes);
        form.extend_from_slice(b"\r\n");
        form.extend_from_slice(format!("--{boundary}--\r\n").as_bytes());

        let res = self
            .authed(
                self.http
                    .post(self.url("/api/v1/messages"))
                    .timeout(Duration::from_secs(60))
                    .header(
                        "Content-Type",
                        format!("multipart/form-data; boundary={boundary}"),
                    )
                    .body(form),
            )
            .send()
            .await
            .map_err(net_err)?;
        let res = read_ok(res).await?;
        let wrapped: MessageResp = res.json().await.map_err(|_| "发送结果无法识别".to_string())?;
        Ok(wrapped.message)
    }

    pub async fn post_signal(
        &self,
        kind: &str,
        from: &str,
        call_id: &str,
        sdp: Option<&str>,
        candidate: Option<&str>,
    ) -> Result<(), String> {
        let mut payload = serde_json::json!({
            "channel": self.channel,
            "token": self.token,
            "kind": kind,
            "from": from,
            "callId": call_id,
        });
        if let Some(sdp) = sdp.filter(|s| !s.is_empty()) {
            payload["sdp"] = serde_json::Value::String(sdp.to_string());
        }
        if let Some(candidate) = candidate.filter(|s| !s.is_empty()) {
            payload["candidate"] = serde_json::Value::String(candidate.to_string());
        }
        let res = self
            .authed(
                self.http
                    .post(self.url("/api/v1/signal"))
                    .timeout(Duration::from_secs(20))
                    .header("Content-Type", "application/json")
                    .json(&payload),
            )
            .send()
            .await
            .map_err(net_err)?;
        let _ = read_ok(res).await?;
        Ok(())
    }

    fn url(&self, path: &str) -> String {
        format!("{}{}", self.base.trim_end_matches('/'), path)
    }

    fn authed(&self, req: reqwest::RequestBuilder) -> reqwest::RequestBuilder {
        let mut req = req;
        if !self.token.is_empty() {
            req = req.header("X-Token", &self.token);
        }
        if self
            .channel
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.'))
        {
            req = req.header("X-Channel", &self.channel);
        }
        req
    }
}

async fn read_ok(res: reqwest::Response) -> Result<reqwest::Response, String> {
    let status = res.status();
    if status.is_success() {
        return Ok(res);
    }
    let text = res.text().await.unwrap_or_default();
    if let Ok(body) = serde_json::from_str::<ErrBody>(&text) {
        if !body.error.is_empty() {
            return Err(body.error);
        }
    }
    Err(match status.as_u16() {
        401 => "口令不正确".into(),
        413 => "照片太大".into(),
        404 => "找不到内容".into(),
        _ => format!("服务器返回 {}", status.as_u16()),
    })
}

fn now_nonce() -> u64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos() as u64)
        .unwrap_or(0)
}

fn net_err(err: reqwest::Error) -> String {
    if err.is_timeout() {
        return "连接超时".into();
    }
    if err.is_connect() {
        return "连不上服务器，请检查地址和端口".into();
    }
    let text = err.to_string().to_lowercase();
    if text.contains("certificate") || text.contains("tls") || text.contains("ssl") {
        return "证书不受信任，请检查 HTTPS".into();
    }
    "网络错误".into()
}
