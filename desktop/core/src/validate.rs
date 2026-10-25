/// Keep the rules aligned with server/internal/api/validate.go.

/// Unambiguous letters (no 0/O/1/I/L) so a teacher can read the ID aloud.
const ID_ALPHABET: &[u8] = b"23456789ABCDEFGHJKLMNPQRSTUVWXYZ";

/// Hard-to-guess classroom ID, easy to type: `7K3M-P9WQ`.
pub fn generate_channel_id() -> String {
    let mut raw = [0u8; 8];
    if getrandom::getrandom(&mut raw).is_err() {
        let nanos = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos() as u64)
            .unwrap_or(0x9e3779b97f4a7c15);
        for (i, slot) in raw.iter_mut().enumerate() {
            *slot = ((nanos >> (i * 8)) & 0xff) as u8;
        }
    }
    let mut out = String::with_capacity(9);
    for (i, byte) in raw.iter().enumerate() {
        if i == 4 {
            out.push('-');
        }
        out.push(ID_ALPHABET[*byte as usize % ID_ALPHABET.len()] as char);
    }
    out
}

pub fn valid_channel(input: &str) -> bool {
    let s = input.trim();
    let n = s.chars().count();
    if !(1..=32).contains(&n) {
        return false;
    }
    s.chars().all(|r| {
        !r.is_control()
            && !r.is_whitespace()
            && !matches!(r, '/' | '\\' | '?' | '#' | '&' | '%' | '"' | '\'' | '<' | '>' | ';')
    })
}

pub fn valid_token(input: &str) -> bool {
    if input.is_empty() {
        return true;
    }
    if input.len() > 64 {
        return false;
    }
    input.bytes().all(|b| (0x21..=0x7e).contains(&b))
}

pub fn valid_id(id: &str) -> bool {
    let b = id.as_bytes();
    if b.len() != 30 || b[13] != b'-' {
        return false;
    }
    b[..13].iter().all(|c| c.is_ascii_digit())
        && b[14..].iter().all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(c))
}

pub fn normalize_base(input: &str) -> Result<String, String> {
    let raw = input.trim();
    if raw.is_empty() {
        return Err("请填写服务器地址".into());
    }
    let parsed = reqwest::Url::parse(raw).map_err(|_| "请加上 http:// 或 https://".to_string())?;
    match parsed.scheme() {
        "http" | "https" => {}
        _ => return Err("请加上 http:// 或 https://".into()),
    }
    if parsed.host().is_none() {
        return Err("服务器地址格式不对".into());
    }
    if parsed.path() != "/" && parsed.path() != "" {
        return Err("请只填写网站根地址，例如 https://notify.example.com".into());
    }
    if parsed.query().is_some() || parsed.fragment().is_some() {
        return Err("请只填写网站根地址，不要带参数".into());
    }
    let mut out = parsed;
    out.set_path("");
    out.set_query(None);
    out.set_fragment(None);
    Ok(out.to_string().trim_end_matches('/').to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn channel_rules() {
        assert!(valid_channel("301"));
        assert!(valid_channel("三年二班"));
        assert!(valid_channel("class-3"));
        assert!(!valid_channel(""));
        assert!(!valid_channel("三年 二班"));
        assert!(!valid_channel("a/b"));
        assert!(!valid_channel(&"a".repeat(33)));
    }

    #[test]
    fn generated_id_is_readable_and_valid() {
        let a = generate_channel_id();
        let b = generate_channel_id();
        assert!(valid_channel(&a), "{a}");
        assert_eq!(a.len(), 9);
        assert_eq!(a.as_bytes()[4], b'-');
        assert!(a.bytes().all(|c| c == b'-' || ID_ALPHABET.contains(&c)));
        assert_ne!(a, b);
    }

    #[test]
    fn token_rules() {
        assert!(valid_token(""));
        assert!(valid_token("Ab12"));
        assert!(!valid_token("a b"));
        assert!(!valid_token("有口令"));
    }

    #[test]
    fn ids() {
        assert!(valid_id("1792890664093-6f70f5a9d7d92cb5"));
        assert!(!valid_id("../etc/passwd"));
        assert!(!valid_id("1792890664093-6F70F5A9D7D92CB5"));
    }

    #[test]
    fn bases() {
        assert_eq!(
            normalize_base("https://notify.example.com/").unwrap(),
            "https://notify.example.com"
        );
        assert_eq!(
            normalize_base("http://192.168.1.8:8787").unwrap(),
            "http://192.168.1.8:8787"
        );
        assert_eq!(
            normalize_base("http://[::1]:8787/").unwrap(),
            "http://[::1]:8787"
        );
        assert!(normalize_base("notify.example.com").is_err());
        assert!(normalize_base("ftp://notify.example.com").is_err());
        assert!(normalize_base("https://notify.example.com/app").is_err());
    }
}
