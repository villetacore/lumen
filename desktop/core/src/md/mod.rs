//! Реализация формата Lumen (spec/FORMAT.md) для десктопного ядра.

pub mod inline;
pub mod task;

use crate::model::{Event, EventSource, LDate, Link, Note, NoteType};
use inline::{extract_tags, extract_wiki_links, mask_code_spans};
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
pub use task::{apply_task_patch, build_task, parse_task_line, TaskPatch};

/// Отделяет YAML-заголовок от тела. Возвращает строки заголовка и индекс начала тела.
pub fn split_frontmatter(lines: &[String]) -> Option<(Vec<String>, usize)> {
    let first = lines.first()?.trim_start_matches('\u{feff}');
    if first.trim_end() != "---" {
        return None;
    }
    for (i, l) in lines.iter().enumerate().skip(1) {
        let t = l.trim_end();
        if t == "---" || t == "..." {
            return Some((lines[1..i].to_vec(), i + 1));
        }
    }
    None
}

/// Плоское представление frontmatter: скаляры как строки, списки через запятую.
fn parse_frontmatter(raw: &str) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    let Ok(value) = serde_yaml::from_str::<serde_yaml::Value>(raw) else {
        return out;
    };
    let serde_yaml::Value::Mapping(map) = value else {
        return out;
    };
    for (k, v) in map {
        let Some(key) = k.as_str() else { continue };
        out.insert(key.to_string(), scalar(&v));
    }
    out
}

fn scalar(v: &serde_yaml::Value) -> String {
    match v {
        serde_yaml::Value::String(s) => s.clone(),
        serde_yaml::Value::Number(n) => n.to_string(),
        serde_yaml::Value::Bool(b) => b.to_string(),
        serde_yaml::Value::Sequence(items) => {
            items.iter().map(scalar).collect::<Vec<_>>().join(", ")
        }
        serde_yaml::Value::Null => String::new(),
        other => serde_yaml::to_string(other).unwrap_or_default().trim().to_string(),
    }
}

fn note_type(v: &str, rel_path: &str) -> NoteType {
    match v.trim().to_lowercase().as_str() {
        "project" => NoteType::Project,
        "area" => NoteType::Area,
        "daily" => NoteType::Daily,
        "meeting" => NoteType::Meeting,
        "event" => NoteType::Event,
        "note" => NoteType::Note,
        _ => {
            if is_daily_name(&title_from_path(rel_path)) {
                NoteType::Daily
            } else {
                NoteType::Note
            }
        }
    }
}

fn is_daily_name(name: &str) -> bool {
    let b = name.as_bytes();
    b.len() == 10
        && b[4] == b'-'
        && b[7] == b'-'
        && b.iter().enumerate().all(|(i, c)| i == 4 || i == 7 || c.is_ascii_digit())
}

/// Имя файла без расширения — запасной заголовок заметки.
pub fn title_from_path(rel_path: &str) -> String {
    let base = rel_path.rsplit('/').next().unwrap_or(rel_path);
    base.strip_suffix(".md").unwrap_or(base).to_string()
}

fn fence_marker(trimmed: &str) -> Option<&'static str> {
    for m in ["```", "~~~"] {
        if trimmed.starts_with(m) {
            return Some(m);
        }
    }
    None
}

/// Разбирает файл vault в доменную заметку. Номера строк — абсолютные в файле.
pub fn parse_note(rel_path: &str, data: &[u8]) -> Note {
    let src = String::from_utf8_lossy(data).to_string();
    let eol = if src.contains("\r\n") { "\r\n" } else { "\n" };
    let normalized = src.replace("\r\n", "\n");
    let lines: Vec<String> = normalized.split('\n').map(str::to_string).collect();

    let (fm_lines, body_start) = split_frontmatter(&lines).unwrap_or((Vec::new(), 0));
    let fm = parse_frontmatter(&fm_lines.join("\n"));

    let mut hasher = Sha256::new();
    hasher.update(data);
    let hash = format!("sha256:{}", hasher.finalize().iter().map(|b| format!("{b:02x}")).collect::<String>());

    let id = fm.get("id").filter(|s| !s.is_empty()).cloned().unwrap_or_else(|| rel_path.to_string());
    let mut note = Note {
        id: id.clone(),
        path: rel_path.to_string(),
        title: fm.get("title").cloned().unwrap_or_default(),
        note_type: note_type(fm.get("type").map(String::as_str).unwrap_or(""), rel_path),
        tags: fm
            .get("tags")
            .map(|t| {
                t.split(',')
                    .map(|s| s.trim().trim_start_matches('#').to_string())
                    .filter(|s| !s.is_empty())
                    .collect()
            })
            .unwrap_or_default(),
        frontmatter: fm.clone(),
        body: lines[body_start..].join(eol),
        links: Vec::new(),
        tasks: Vec::new(),
        start: fm.get("start").and_then(|s| LDate::parse(s)),
        end: fm.get("end").and_then(|s| LDate::parse(s)),
        all_day: fm.get("allday").map(|v| v == "true").unwrap_or(false),
        hash,
        size: data.len() as u64,
        eol: eol.to_string(),
    };

    let mut in_fence = false;
    let mut fence = "";
    for (i, line) in lines.iter().enumerate().skip(body_start) {
        let trimmed = line.trim();

        if let Some(m) = fence_marker(trimmed) {
            if !in_fence {
                in_fence = true;
                fence = m;
            } else if trimmed.starts_with(fence) {
                in_fence = false;
                fence = "";
            }
            continue;
        }
        if in_fence {
            continue;
        }

        if note.title.is_empty() {
            if let Some(h1) = trimmed.strip_prefix("# ") {
                note.title = h1.trim().to_string();
            }
        }

        match build_task(rel_path, &id, i, line) {
            Some(task) => {
                for tag in &task.tags {
                    if !note.tags.contains(tag) {
                        note.tags.push(tag.clone());
                    }
                }
                note.tasks.push(task);
            }
            None => {
                for tag in extract_tags(&mask_code_spans(line)) {
                    if !note.tags.contains(&tag) {
                        note.tags.push(tag);
                    }
                }
            }
        }

        for l in extract_wiki_links(&mask_code_spans(line)) {
            note.links.push(Link {
                target: l.target,
                heading: l.heading,
                alias: l.alias,
                line: i,
            });
        }
    }

    if note.title.is_empty() {
        note.title = title_from_path(rel_path);
    }
    if note.note_type == NoteType::Daily && note.start.is_none() {
        note.start = LDate::parse(&title_from_path(rel_path));
        note.all_day = note.start.is_some();
    }
    note
}

/// Собирает события календаря, которые даёт заметка.
pub fn events(note: &Note) -> Vec<Event> {
    let mut out = Vec::new();
    if let Some(start) = note.start {
        let source = if note.note_type == NoteType::Daily { EventSource::Daily } else { EventSource::Note };
        out.push(Event {
            id: format!("note:{}", note.id),
            source,
            ref_id: note.id.clone(),
            path: note.path.clone(),
            title: note.title.clone(),
            start,
            end: note.end.unwrap_or(start),
            all_day: note.all_day || start.time.is_none(),
            status: None,
        });
    }
    for t in &note.tasks {
        let Some(d) = t.scheduled.or(t.due) else { continue };
        out.push(Event {
            id: format!("task:{}", t.id),
            source: EventSource::Task,
            ref_id: t.id.clone(),
            path: note.path.clone(),
            title: t.text.clone(),
            start: d,
            end: d,
            all_day: d.time.is_none(),
            status: Some(t.status),
        });
    }
    out
}
