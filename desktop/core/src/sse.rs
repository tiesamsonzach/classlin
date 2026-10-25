#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SseEvent {
    pub event: String,
    pub data: String,
    pub id: String,
}

#[derive(Debug, Default)]
pub struct SseParser {
    pending: Vec<u8>,
    line_buf: String,
    event: String,
    data: Vec<String>,
    id: String,
}

impl SseParser {
    pub fn push_bytes(&mut self, chunk: &[u8]) -> Vec<SseEvent> {
        self.pending.extend_from_slice(chunk);
        let valid = match std::str::from_utf8(&self.pending) {
            Ok(_) => self.pending.len(),
            Err(err) => err.valid_up_to(),
        };
        if valid == 0 {
            return Vec::new();
        }
        let text = std::str::from_utf8(&self.pending[..valid]).expect("valid utf-8");
        self.line_buf.push_str(text);
        self.pending.drain(..valid);
        self.drain_lines()
    }

    fn drain_lines(&mut self) -> Vec<SseEvent> {
        let mut out = Vec::new();
        loop {
            let Some(pos) = self.line_buf.find('\n') else {
                break;
            };
            let mut line = self.line_buf[..pos].to_string();
            self.line_buf.drain(..=pos);
            if line.ends_with('\r') {
                line.pop();
            }
            if line.is_empty() {
                if let Some(ev) = self.finish() {
                    out.push(ev);
                }
                continue;
            }
            if line.starts_with(':') {
                continue;
            }
            if let Some(rest) = line.strip_prefix("event:") {
                self.event = rest.trim_start().to_string();
            } else if let Some(rest) = line.strip_prefix("data:") {
                let value = rest.strip_prefix(' ').unwrap_or(rest);
                self.data.push(value.to_string());
            } else if let Some(rest) = line.strip_prefix("id:") {
                self.id = rest.trim_start().to_string();
            }
        }
        out
    }

    fn finish(&mut self) -> Option<SseEvent> {
        let data = std::mem::take(&mut self.data).join("\n");
        let event = std::mem::take(&mut self.event);
        let id = std::mem::take(&mut self.id);
        if data.is_empty() {
            return None;
        }
        Some(SseEvent {
            event: if event.is_empty() {
                "message".into()
            } else {
                event
            },
            data,
            id,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn splits_chunks_and_ignores_ping() {
        let mut p = SseParser::default();
        let raw = b"retry: 2000\n\n: ping\n\nevent: message\ndata: {\"body\":\"\xe4\xbd";
        assert!(p.push_bytes(raw).is_empty());
        let ev = p.push_bytes(b"\xa0\xe5\xa5\xbd\"}\n\n");
        assert_eq!(ev.len(), 1);
        assert_eq!(ev[0].event, "message");
        assert!(ev[0].data.contains("你好"));
    }

    #[test]
    fn two_events() {
        let mut p = SseParser::default();
        let ev = p.push_bytes(b"data: one\n\ndata: two\n\n");
        assert_eq!(ev.len(), 2);
        assert_eq!(ev[0].data, "one");
        assert_eq!(ev[1].data, "two");
        assert_eq!(ev[0].event, "message");
    }
}
