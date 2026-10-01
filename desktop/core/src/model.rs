//! Домменные типы ядра. Зеркалят `server/internal/domain` — см. spec/FORMAT.md.

use chrono::{NaiveDate, NaiveDateTime, NaiveTime};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::fmt;

/// Дата задачи или события: календарный день и, необязательно, время.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Debug)]
pub struct LDate {
    pub date: NaiveDate,
    pub time: Option<NaiveTime>,
}

impl LDate {
    pub fn parse(s: &str) -> Option<LDate> {
        let s = s.trim().replacen(' ', "T", 1);
        if let Ok(d) = NaiveDate::parse_from_str(&s, "%Y-%m-%d") {
            return Some(LDate { date: d, time: None });
        }
        for fmt in ["%Y-%m-%dT%H:%M", "%Y-%m-%dT%H:%M:%S"] {
            if let Ok(dt) = NaiveDateTime::parse_from_str(&s, fmt) {
                return Some(LDate { date: dt.date(), time: Some(dt.time()) });
            }
        }
        // RFC3339 со смещением: берём локальную часть, как и сервер.
        if s.len() >= 19 {
            if let Ok(dt) = NaiveDateTime::parse_from_str(&s[..19], "%Y-%m-%dT%H:%M:%S") {
                return Some(LDate { date: dt.date(), time: Some(dt.time()) });
            }
        }
        None
    }

    /// Относительные значения принимаются только от UI и сразу нормализуются.
    pub fn relative(s: &str, today: NaiveDate) -> Option<LDate> {
        match s.trim().to_lowercase().as_str() {
            "today" | "сегодня" => Some(LDate { date: today, time: None }),
            "tomorrow" | "завтра" => Some(LDate { date: today.succ_opt()?, time: None }),
            "yesterday" | "вчера" => Some(LDate { date: today.pred_opt()?, time: None }),
            _ => None,
        }
    }
}

impl fmt::Display for LDate {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.time {
            Some(t) => write!(f, "{}T{}", self.date.format("%Y-%m-%d"), t.format("%H:%M")),
            None => write!(f, "{}", self.date.format("%Y-%m-%d")),
        }
    }
}

impl Serialize for LDate {
    fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&self.to_string())
    }
}

impl<'de> Deserialize<'de> for LDate {
    fn deserialize<D: serde::Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        let s = String::deserialize(d)?;
        LDate::parse(&s).ok_or_else(|| serde::de::Error::custom(format!("bad date {s}")))
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TaskStatus {
    Todo,
    Doing,
    Done,
    Cancelled,
    Deferred,
}

impl TaskStatus {
    pub fn from_marker(c: char) -> TaskStatus {
        match c {
            '/' => TaskStatus::Doing,
            'x' | 'X' => TaskStatus::Done,
            '-' => TaskStatus::Cancelled,
            '>' => TaskStatus::Deferred,
            _ => TaskStatus::Todo,
        }
    }

    pub fn marker(self) -> char {
        match self {
            TaskStatus::Doing => '/',
            TaskStatus::Done => 'x',
            TaskStatus::Cancelled => '-',
            TaskStatus::Deferred => '>',
            TaskStatus::Todo => ' ',
        }
    }

    pub fn is_open(self) -> bool {
        matches!(self, TaskStatus::Todo | TaskStatus::Doing)
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize)]
pub enum Priority {
    A,
    B,
    C,
    D,
}

impl Priority {
    pub fn parse(s: &str) -> Option<Priority> {
        match s.trim().to_lowercase().as_str() {
            "a" | "high" | "highest" | "1" => Some(Priority::A),
            "b" | "medium" | "med" | "2" => Some(Priority::B),
            "c" | "low" | "3" => Some(Priority::C),
            "d" | "lowest" | "4" => Some(Priority::D),
            _ => None,
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum NoteType {
    #[default]
    Note,
    Project,
    Area,
    Daily,
    Meeting,
    Event,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Task {
    pub id: String,
    pub block_id: Option<String>,
    pub note_id: String,
    pub note_path: String,
    pub line: usize,
    pub indent: usize,
    pub status: TaskStatus,
    pub marker: char,
    pub text: String,
    #[serde(skip)]
    pub raw: String,
    pub tags: Vec<String>,
    pub due: Option<LDate>,
    pub scheduled: Option<LDate>,
    pub start: Option<LDate>,
    pub done: Option<LDate>,
    pub priority: Option<Priority>,
    pub estimate: Option<String>,
    pub project: Option<String>,
    pub repeat: Option<String>,
    pub fields: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Link {
    pub target: String,
    pub heading: String,
    pub alias: String,
    pub line: usize,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Note {
    pub id: String,
    pub path: String,
    pub title: String,
    #[serde(rename = "type")]
    pub note_type: NoteType,
    pub tags: Vec<String>,
    pub frontmatter: BTreeMap<String, String>,
    pub body: String,
    pub links: Vec<Link>,
    pub tasks: Vec<Task>,
    pub start: Option<LDate>,
    pub end: Option<LDate>,
    pub all_day: bool,
    pub hash: String,
    pub size: u64,
    /// Окончание строк исходного файла — нужно для побайтовой записи.
    #[serde(skip)]
    pub eol: String,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum EventSource {
    Note,
    Task,
    Daily,
    Ics,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Event {
    pub id: String,
    pub source: EventSource,
    pub ref_id: String,
    pub path: String,
    pub title: String,
    pub start: LDate,
    pub end: LDate,
    pub all_day: bool,
    pub status: Option<TaskStatus>,
}
