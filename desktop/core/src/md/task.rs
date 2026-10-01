//! Разбор и запись строки-задачи. Порт `server/internal/markdown/task.go` и `write.go`.

use super::inline::{extract_fields, extract_tags, mask_code_spans, trailing_block_id};
use crate::model::{LDate, Priority, Task, TaskStatus};
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;

/// Лексическая форма строки-чекбокса.
#[derive(Clone, Debug)]
pub struct TaskLine {
    pub indent: String,
    pub bullet: String,
    pub gap1: String,
    pub marker: char,
    pub gap2: String,
    pub rest: String,
}

impl TaskLine {
    pub fn render(&self) -> String {
        format!(
            "{}{}{}[{}]{}{}",
            self.indent, self.bullet, self.gap1, self.marker, self.gap2, self.rest
        )
    }

    pub fn indent_width(&self) -> usize {
        self.indent.chars().map(|c| if c == '\t' { 4 } else { 1 }).sum()
    }
}

/// Распознаёт `- [ ] текст` на любом уровне вложенности.
pub fn parse_task_line(line: &str) -> Option<TaskLine> {
    let b = line.as_bytes();
    let mut i = 0usize;
    while i < b.len() && (b[i] == b' ' || b[i] == b'\t') {
        i += 1;
    }
    let indent = line[..i].to_string();
    let start = i;

    if i < b.len() && (b[i] == b'-' || b[i] == b'*' || b[i] == b'+') {
        i += 1;
    } else {
        while i < b.len() && b[i].is_ascii_digit() {
            i += 1;
        }
        if i == start || i >= b.len() || (b[i] != b'.' && b[i] != b')') {
            return None;
        }
        i += 1;
    }
    let bullet = line[start..i].to_string();

    let gap = i;
    while i < b.len() && (b[i] == b' ' || b[i] == b'\t') {
        i += 1;
    }
    if i == gap {
        return None;
    }
    let gap1 = line[gap..i].to_string();

    if i >= b.len() || b[i] != b'[' {
        return None;
    }
    let close = line[i..].find(']')? + i;
    let marker_str = &line[i + 1..close];
    let mut marker_chars = marker_str.chars();
    let marker = marker_chars.next()?;
    if marker_chars.next().is_some() {
        return None;
    }
    i = close + 1;

    let gap = i;
    while i < b.len() && (b[i] == b' ' || b[i] == b'\t') {
        i += 1;
    }
    Some(TaskLine {
        indent,
        bullet,
        gap1,
        marker,
        gap2: line[gap..i].to_string(),
        rest: line[i..].to_string(),
    })
}

/// Строит доменную задачу из строки файла.
pub fn build_task(note_path: &str, note_id: &str, line_no: usize, line: &str) -> Option<Task> {
    let tl = parse_task_line(line)?;
    let (rest, block_id) = trailing_block_id(&tl.rest);
    let fields = extract_fields(rest);

    // Текст без инлайн-полей.
    let mut plain = String::new();
    let mut prev = 0usize;
    for f in &fields {
        plain.push_str(&rest[prev..f.start]);
        prev = f.end;
    }
    plain.push_str(&rest[prev..]);
    let text = plain.split_whitespace().collect::<Vec<_>>().join(" ");

    let mut task = Task {
        id: String::new(),
        block_id: block_id.clone(),
        note_id: note_id.to_string(),
        note_path: note_path.to_string(),
        line: line_no,
        indent: tl.indent_width(),
        status: TaskStatus::from_marker(tl.marker),
        marker: tl.marker,
        tags: extract_tags(&mask_code_spans(&text)),
        text: text.clone(),
        raw: line.to_string(),
        due: None,
        scheduled: None,
        start: None,
        done: None,
        priority: None,
        estimate: None,
        project: None,
        repeat: None,
        fields: BTreeMap::new(),
    };

    for f in &fields {
        match f.key.as_str() {
            "due" => task.due = LDate::parse(&f.value),
            "scheduled" => task.scheduled = LDate::parse(&f.value),
            "start" => task.start = LDate::parse(&f.value),
            "done" | "completion" => task.done = LDate::parse(&f.value),
            "prio" | "priority" => task.priority = Priority::parse(&f.value),
            "est" | "estimate" => task.estimate = Some(f.value.clone()),
            "project" => task.project = Some(f.value.trim_matches(['[', ']', ' ']).to_string()),
            "repeat" | "recur" => task.repeat = Some(f.value.clone()),
            _ => {
                task.fields.insert(f.key.clone(), f.value.clone());
            }
        }
    }

    task.id = block_id.unwrap_or_else(|| derived_task_id(note_path, &text, line_no));
    Some(task)
}

/// Запасной идентификатор задачи без `^blockid`.
pub fn derived_task_id(path: &str, text: &str, line: usize) -> String {
    // Сервер использует sha1; здесь префикс тот же, но алгоритм отличается,
    // поэтому вычисляемые идентификаторы локальны и не участвуют в синхронизации.
    let mut h = Sha256::new();
    h.update(path.as_bytes());
    h.update(b"\n");
    h.update(text.as_bytes());
    h.update(b"\n");
    h.update(line.to_string().as_bytes());
    format!("l{}", &hex(&h.finalize())[..11])
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

/// Патч одной задачи: меняется только то, что названо явно.
#[derive(Default, Debug, Clone)]
pub struct TaskPatch {
    pub status: Option<TaskStatus>,
    pub text: Option<String>,
    /// Пустая строка в значении удаляет поле.
    pub set: BTreeMap<String, String>,
    pub block_id: Option<String>,
}

const FIELD_ORDER: [&str; 8] =
    ["due", "scheduled", "start", "done", "prio", "est", "project", "repeat"];

fn order_of(k: &str) -> usize {
    FIELD_ORDER.iter().position(|f| *f == k).unwrap_or(FIELD_ORDER.len())
}

/// Переписывает одну строку-задачу, не трогая остальной файл (ADR-0003).
pub fn apply_task_patch(line: &str, patch: &TaskPatch) -> Option<String> {
    let mut tl = parse_task_line(line)?;

    if let Some(status) = patch.status {
        if TaskStatus::from_marker(tl.marker) != status {
            tl.marker = status.marker();
        }
    }

    let rest = tl.rest.clone();
    let trimmed_len = rest.trim_end_matches([' ', '\t']).len();
    let trailing = rest[trimmed_len..].to_string();
    let rest = &rest[..trimmed_len];

    let (body, existing_block) = trailing_block_id(rest);
    let block_id = existing_block.or_else(|| patch.block_id.clone());

    let mut body = body.to_string();
    if let Some(new_text) = &patch.text {
        body = replace_text(&body, new_text);
    }
    body = apply_fields(&body, &patch.set);

    let mut out = format!(
        "{}{}{}[{}]{}{}",
        tl.indent, tl.bullet, tl.gap1, tl.marker, tl.gap2, body
    );
    if let Some(id) = block_id {
        out.push_str(&format!(" ^{id}"));
    }
    out.push_str(&trailing);
    Some(out)
}

fn apply_fields(body: &str, set: &BTreeMap<String, String>) -> String {
    if set.is_empty() {
        return body.to_string();
    }
    let existing = extract_fields(body);

    let mut keys: Vec<&String> = set.keys().collect();
    keys.sort_by_key(|k| order_of(k));

    let mut edits: Vec<(usize, usize, String)> = Vec::new();
    let mut added: Vec<String> = Vec::new();

    for k in keys {
        let v = set[k].trim();
        match existing.iter().find(|f| &f.key == k) {
            Some(f) if v.is_empty() => edits.push((f.start, f.end, String::new())),
            Some(f) => edits.push((f.start, f.end, format!("[{k}:: {v}]"))),
            None if !v.is_empty() => added.push(format!("[{k}:: {v}]")),
            None => {}
        }
    }

    // Правки применяются с конца, чтобы смещения оставались корректными.
    edits.sort_by(|a, b| b.0.cmp(&a.0));
    let mut out = body.to_string();
    for (start, end, repl) in edits {
        out = format!("{}{}{}", &out[..start], repl, &out[end..]);
    }
    let mut out = collapse_spaces(&out).trim_end().to_string();
    for a in added {
        out.push(' ');
        out.push_str(&a);
    }
    out
}

fn collapse_spaces(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut prev_space = false;
    for c in s.chars() {
        if c == ' ' {
            if prev_space {
                continue;
            }
            prev_space = true;
        } else {
            prev_space = false;
        }
        out.push(c);
    }
    out
}

fn replace_text(body: &str, new_text: &str) -> String {
    let kept: Vec<String> =
        extract_fields(body).iter().map(|f| body[f.start..f.end].to_string()).collect();
    let mut out = new_text.trim().to_string();
    for f in kept {
        out.push(' ');
        out.push_str(&f);
    }
    out
}
