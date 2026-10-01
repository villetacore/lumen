//! Индекс в памяти: перестраиваемое представление vault для запросов UI.
//! Реализация на SQLite + FTS5 придёт на M1, интерфейс останется прежним.

use crate::md;
use crate::model::{Event, LDate, Note, Task, TaskStatus};
use crate::vault::Vault;
use chrono::{Datelike, Local, NaiveDate};
use std::collections::HashMap;

#[derive(Default)]
pub struct Index {
    notes: HashMap<String, Note>, // по относительному пути
}

/// Запрос задач. Пустые поля не фильтруют.
#[derive(Default, Clone, Debug)]
pub struct TaskQuery {
    pub statuses: Vec<TaskStatus>,
    pub scheduled_from: Option<NaiveDate>,
    pub scheduled_to: Option<NaiveDate>,
    pub overdue: bool,
    pub path_prefix: Option<String>,
    pub tag: Option<String>,
    pub text: Option<String>,
}

impl Index {
    pub fn new() -> Index {
        Index::default()
    }

    /// Полная переиндексация vault.
    pub fn rebuild(&mut self, vault: &Vault) -> usize {
        self.notes.clear();
        for fi in vault.list() {
            if let Ok(data) = vault.read(&fi.path) {
                let note = md::parse_note(&fi.path, &data);
                self.notes.insert(fi.path.clone(), note);
            }
        }
        self.notes.len()
    }

    pub fn put(&mut self, note: Note) {
        self.notes.insert(note.path.clone(), note);
    }

    pub fn remove(&mut self, path: &str) {
        self.notes.remove(path);
    }

    pub fn note(&self, path: &str) -> Option<&Note> {
        self.notes.get(path)
    }

    pub fn notes(&self) -> impl Iterator<Item = &Note> {
        self.notes.values()
    }

    pub fn task(&self, id: &str) -> Option<&Task> {
        self.notes.values().flat_map(|n| n.tasks.iter()).find(|t| t.id == id)
    }

    /// Возвращает задачи, отсортированные по сроку, затем приоритету и пути.
    pub fn tasks(&self, q: &TaskQuery) -> Vec<&Task> {
        let today = Local::now().date_naive();
        let mut out: Vec<&Task> = self
            .notes
            .values()
            .flat_map(|n| n.tasks.iter())
            .filter(|t| matches(t, q, today))
            .collect();
        out.sort_by(|a, b| {
            date_key(a).cmp(&date_key(b)).then(prio_key(a).cmp(&prio_key(b))).then(
                a.note_path.cmp(&b.note_path).then(a.line.cmp(&b.line)),
            )
        });
        out
    }

    /// События календаря в полуинтервале [from, to).
    pub fn events(&self, from: NaiveDate, to: NaiveDate) -> Vec<Event> {
        let mut out: Vec<Event> = self
            .notes
            .values()
            .flat_map(md::events)
            .filter(|e| e.start.date >= from && e.start.date < to)
            .collect();
        out.sort_by(|a, b| a.start.cmp(&b.start).then(a.title.cmp(&b.title)));
        out
    }

    /// Простой подстрочный поиск: место для FTS5 на M1.
    pub fn search(&self, query: &str, limit: usize) -> Vec<&Note> {
        let q = query.trim().to_lowercase();
        if q.is_empty() {
            return Vec::new();
        }
        let mut hits: Vec<(&Note, usize)> = self
            .notes
            .values()
            .filter_map(|n| {
                let mut score = 0usize;
                if n.title.to_lowercase().contains(&q) {
                    score += 10;
                }
                if n.body.to_lowercase().contains(&q) {
                    score += 2;
                }
                (score > 0).then_some((n, score))
            })
            .collect();
        hits.sort_by(|a, b| b.1.cmp(&a.1).then(a.0.path.cmp(&b.0.path)));
        hits.into_iter().take(limit).map(|(n, _)| n).collect()
    }
}

fn matches(t: &Task, q: &TaskQuery, today: NaiveDate) -> bool {
    if !q.statuses.is_empty() && !q.statuses.contains(&t.status) {
        return false;
    }
    if q.overdue {
        let overdue = t.status.is_open() && t.due.map(|d| d.date < today).unwrap_or(false);
        if !overdue {
            return false;
        }
    }
    if q.scheduled_from.is_some() || q.scheduled_to.is_some() {
        let Some(d) = t.scheduled.or(t.due) else { return false };
        if let Some(from) = q.scheduled_from {
            if d.date < from {
                return false;
            }
        }
        if let Some(to) = q.scheduled_to {
            if d.date > to {
                return false;
            }
        }
    }
    if let Some(prefix) = &q.path_prefix {
        if !t.note_path.starts_with(prefix) {
            return false;
        }
    }
    if let Some(tag) = &q.tag {
        if !t.tags.iter().any(|x| x.eq_ignore_ascii_case(tag)) {
            return false;
        }
    }
    if let Some(text) = &q.text {
        if !t.text.to_lowercase().contains(&text.to_lowercase()) {
            return false;
        }
    }
    true
}

/// Задачи без даты сортируются после датированных.
fn date_key(t: &Task) -> (u8, i32, u32, u32) {
    match t.due.or(t.scheduled) {
        Some(LDate { date, .. }) => (0, date.year(), date.month(), date.day()),
        None => (1, 0, 0, 0),
    }
}

fn prio_key(t: &Task) -> u8 {
    match t.priority {
        Some(crate::model::Priority::A) => 0,
        Some(crate::model::Priority::B) => 1,
        Some(crate::model::Priority::C) => 2,
        Some(crate::model::Priority::D) => 3,
        None => 4,
    }
}

/// Именованные представления UI — это запросы, а не отдельные сущности.
pub fn resolve_view(name: &str, today: NaiveDate) -> TaskQuery {
    let open = vec![TaskStatus::Todo, TaskStatus::Doing];
    let mut q = TaskQuery { statuses: open, ..Default::default() };
    match name {
        "inbox" => q.path_prefix = Some("00-inbox/".into()),
        "today" => q.scheduled_to = Some(today),
        "overdue" => q.overdue = true,
        "week" => {
            let monday = today - chrono::Duration::days(today.weekday().num_days_from_monday() as i64);
            q.scheduled_from = Some(monday);
            q.scheduled_to = Some(monday + chrono::Duration::days(6));
        }
        "done" => q.statuses = vec![TaskStatus::Done],
        _ => {}
    }
    q
}
