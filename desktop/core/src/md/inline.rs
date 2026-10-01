//! Сканеры инлайн-синтаксиса: `[key:: value]`, `#tag`, `[[wikilink]]`, `^blockid`.
//! Точный порт `server/internal/markdown/inline.go` (ADR-0004).

/// Инлайн-поле вместе с байтовыми границами в исходной строке.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct InlineField {
    pub key: String,
    pub value: String,
    pub start: usize,
    pub end: usize,
}

/// Возвращает позицию сразу за `]`, парной к `[` в позиции `start`.
fn scan_bracket(s: &[u8], start: usize) -> Option<usize> {
    let mut depth = 0usize;
    for i in start..s.len() {
        match s[i] {
            b'[' => depth += 1,
            b']' => {
                depth -= 1;
                if depth == 0 {
                    return Some(i + 1);
                }
            }
            _ => {}
        }
    }
    None
}

/// Позиция разделителя `::` вне вложенных скобок.
fn top_level_sep(s: &str) -> Option<usize> {
    let b = s.as_bytes();
    let mut depth = 0i32;
    let mut i = 0usize;
    while i + 1 < b.len() {
        match b[i] {
            b'[' => depth += 1,
            b']' => depth -= 1,
            b':' if depth == 0 && b[i + 1] == b':' => return Some(i),
            _ => {}
        }
        i += 1;
    }
    None
}

fn is_field_key(k: &str) -> bool {
    let mut chars = k.chars();
    match chars.next() {
        Some(c) if c.is_alphabetic() => {}
        _ => return false,
    }
    k.chars().all(|c| c.is_alphanumeric() || c == '_' || c == '-')
}

/// Находит все инлайн-поля. Викилинки пропускаются: внутри них нет `::`.
pub fn extract_fields(s: &str) -> Vec<InlineField> {
    let b = s.as_bytes();
    let mut out = Vec::new();
    let mut i = 0usize;
    while i < b.len() {
        if b[i] != b'[' {
            i += 1;
            continue;
        }
        if i + 1 < b.len() && b[i + 1] == b'[' {
            i = scan_bracket(b, i).unwrap_or(b.len());
            continue;
        }
        let Some(end) = scan_bracket(b, i) else { break };
        let inner = &s[i + 1..end - 1];
        match top_level_sep(inner) {
            Some(sep) => {
                let key = inner[..sep].trim();
                if is_field_key(key) {
                    out.push(InlineField {
                        key: key.to_lowercase(),
                        value: inner[sep + 2..].trim().to_string(),
                        start: i,
                        end,
                    });
                }
            }
            None => {}
        }
        i = end;
    }
    out
}

/// Отделяет завершающий `^blockid` от текста строки.
pub fn trailing_block_id(s: &str) -> (&str, Option<String>) {
    let trimmed = s.trim_end_matches([' ', '\t']);
    let Some(i) = trimmed.rfind('^') else { return (s, None) };
    if i == 0 {
        return (s, None);
    }
    let prev = trimmed.as_bytes()[i - 1];
    if prev != b' ' && prev != b'\t' {
        return (s, None);
    }
    let id = &trimmed[i + 1..];
    if id.len() < 3 || id.len() > 32 {
        return (s, None);
    }
    if !id.chars().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-') {
        return (s, None);
    }
    (trimmed[..i].trim_end_matches([' ', '\t']), Some(id.to_string()))
}

/// Извлекает `#теги`. Код-спаны должен замаскировать вызывающий.
pub fn extract_tags(s: &str) -> Vec<String> {
    let chars: Vec<char> = s.chars().collect();
    let mut out = Vec::new();
    let mut i = 0usize;
    while i < chars.len() {
        if chars[i] != '#' {
            i += 1;
            continue;
        }
        if i > 0 && !chars[i - 1].is_whitespace() && chars[i - 1] != '(' && chars[i - 1] != '[' {
            i += 1;
            continue;
        }
        let mut j = i + 1;
        while j < chars.len()
            && (chars[j].is_alphanumeric() || chars[j] == '_' || chars[j] == '-' || chars[j] == '/')
        {
            j += 1;
        }
        let tag: String = chars[i + 1..j].iter().collect();
        if !tag.is_empty() && !tag.chars().all(|c| c.is_ascii_digit()) {
            out.push(tag.trim_matches('/').to_string());
        }
        i = j.max(i + 1);
    }
    out
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WikiLink {
    pub target: String,
    pub heading: String,
    pub alias: String,
}

/// Разбирает все `[[цель#заголовок|алиас]]` в строке.
pub fn extract_wiki_links(s: &str) -> Vec<WikiLink> {
    let mut out = Vec::new();
    let mut rest = s;
    let mut consumed = 0usize;
    while let Some(open) = rest.find("[[") {
        let after = &rest[open + 2..];
        let Some(close) = after.find("]]") else { break };
        let mut inner = &after[..close];
        consumed += open + 2 + close + 2;
        rest = &s[consumed..];

        let mut alias = String::new();
        let mut heading = String::new();
        if let Some(p) = inner.find('|') {
            alias = inner[p + 1..].trim().to_string();
            inner = &inner[..p];
        }
        if let Some(p) = inner.find('#') {
            heading = inner[p + 1..].trim().to_string();
            inner = &inner[..p];
        }
        let target = inner.trim().to_string();
        if !target.is_empty() || !heading.is_empty() {
            out.push(WikiLink { target, heading, alias });
        }
    }
    out
}

/// Заменяет содержимое инлайн-кода пробелами, сохраняя длину строки.
pub fn mask_code_spans(s: &str) -> String {
    let mut b: Vec<char> = s.chars().collect();
    let mut open: Option<usize> = None;
    for i in 0..b.len() {
        if b[i] != '`' {
            continue;
        }
        match open {
            None => open = Some(i),
            Some(start) => {
                for c in b.iter_mut().take(i + 1).skip(start) {
                    *c = ' ';
                }
                open = None;
            }
        }
    }
    b.into_iter().collect()
}
